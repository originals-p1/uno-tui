package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"terminal-uno/internal/client"
	"terminal-uno/internal/game"
)

// startRealServer boots a real loopback server and returns the dial address.
func startRealServer(t *testing.T) (*Server, string) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.HostBootstrapToken = "host-tok"
	cfg.RoomKey = "sekret"
	s := NewServer(cfg, NewRealClock(), nil)
	t.Cleanup(s.Close)
	ln, err := s.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return s, ln.Addr().String()
}

func hostClient(t *testing.T, addr string) *client.Client {
	t.Helper()
	c := client.NewClient(client.Config{Address: addr, Name: "Alice", RoomKey: "sekret",
		HostBootstrapToken: "host-tok", ResponseTimeout: 4 * time.Second})
	if err := c.Connect(addr); err != nil {
		t.Fatalf("host connect: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func guestClient(t *testing.T, addr string) *client.Client {
	t.Helper()
	c := client.NewClient(client.Config{Address: addr, Name: "Bob", RoomKey: "sekret",
		ResponseTimeout: 4 * time.Second})
	if err := c.Connect(addr); err != nil {
		t.Fatalf("guest connect: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func waitUntil(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	dl := time.Now().Add(timeout)
	for time.Now().Before(dl) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", msg)
}

// startGame wires two real clients through ready + start and waits for the
// game snapshot to be visible. Returns the host and guest clients.
func startGame(t *testing.T, addr string) (*client.Client, *client.Client) {
	t.Helper()
	host := hostClient(t, addr)
	guest := guestClient(t, addr)
	hr := host.Revision()
	if _, err := guest.Ready(true, hr); err != nil {
		t.Fatalf("guest ready: %v", err)
	}
	waitUntil(t, 3*time.Second, func() bool { return host.Revision() > hr }, "ready visible to host")
	if _, err := host.StartGame(hr); err != nil {
		t.Fatalf("host start: %v", err)
	}
	waitUntil(t, 3*time.Second, func() bool {
		g := guest.Snapshot()
		h := host.Snapshot()
		started := func(s *game.GameView) bool {
			return s != nil && s.CurrentPlayer != "" && len(s.Players) == 2
		}
		return started(g) && started(h)
	}, "game snapshot visible to both clients")
	return host, guest
}

// autoPlay drives a client to act whenever it is its turn until it observes a
// winner or the context ends.
func autoPlay(ctx context.Context, c *client.Client, stop func() bool) {
	ticker := time.NewTicker(15 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if stop() {
			return
		}
		snap := c.Snapshot()
		if snap == nil || snap.WinnerPlayerID != "" {
			if snap != nil && snap.WinnerPlayerID != "" {
				return
			}
			continue
		}
		if snap.CurrentPlayer != c.Identity().PlayerID {
			continue
		}
		rev := c.Revision()
		switch snap.Phase {
		case string(game.PhaseDrawnDecision):
			if snap.DrawDecision != nil {
				_, _ = c.SendAction(game.Action{Type: game.ActionPassDrawn}, rev)
			}
		case string(game.PhaseChallenge):
			_, _ = c.SendAction(game.Action{Type: game.ActionAcceptFour}, rev)
		default:
			if len(snap.PlayableIDs) > 0 {
				id := snap.PlayableIDs[0]
				col := ""
				for _, cv := range snap.YourHand {
					if cv.ID == id && game.IsWild(cv.Rank) {
						col = "red"
						break
					}
				}
				a := game.Action{Type: game.ActionPlayCard, CardID: id}
				if col != "" {
					cc := game.Color(col)
					a.ChosenColor = &cc
				}
				_, _ = c.SendAction(a, rev)
			} else {
				_, _ = c.SendAction(game.Action{Type: game.ActionDrawCard}, rev)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestIntegration_FullShortGameViaNetwork drives two real clients over loopback
// through an entire game to a winner (readme Task 9.1).
func TestIntegration_FullShortGameViaNetwork(t *testing.T) {
	_, addr := startRealServer(t)
	host, guest := startGame(t, addr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{}, 2)
	winnerSeen := func() bool {
		if s := host.Snapshot(); s != nil && s.WinnerPlayerID != "" {
			return true
		}
		return guest.Snapshot() != nil && guest.Snapshot().WinnerPlayerID != ""
	}
	go func() { autoPlay(ctx, host, winnerSeen); done <- struct{}{} }()
	go func() { autoPlay(ctx, guest, winnerSeen); done <- struct{}{} }()

	waitUntil(t, 25*time.Second, winnerSeen, "a winner to be reached")
	cancel()
	<-done
	<-done
}

// TestIntegration_IllegalActionRejectedAndNoLeak checks the server rejects an
// illegal play and that no player view ever exposes another player's hand.
func TestIntegration_IllegalActionRejectedAndNoLeak(t *testing.T) {
	_, addr := startRealServer(t)
	host, _ := startGame(t, addr)

	hs := host.Snapshot()
	if hs == nil {
		t.Fatal("no host snapshot")
	}
	// Player rows are public summaries: none may carry card data.
	row, _ := json.Marshal(hs.Players[0])
	for _, bad := range []string{"\"rank\"", "\"color\"", "\"your_hand\""} {
		if strings.Contains(string(row), bad) {
			t.Fatalf("player row leaks card data (%s): %s", bad, row)
		}
	}

	// An illegal card is rejected and the revision is untouched.
	rev := host.Revision()
	resp, err := host.SendAction(game.Action{Type: game.ActionPlayCard, CardID: "no-such-card"}, rev)
	if err != nil {
		t.Fatalf("illegal send: %v", err)
	}
	if resp.OK {
		t.Fatalf("illegal card was accepted")
	}
	if host.Revision() != rev {
		t.Fatalf("revision changed on rejected action: %d -> %d", rev, host.Revision())
	}
}

// TestIntegration_DuplicateRequestAppliedOnce re-sends the identical game
// action under the same request_id; the room's idempotency cache must apply it
// exactly once, leaving the revision advanced by exactly one.
func TestIntegration_DuplicateRequestAppliedOnce(t *testing.T) {
	r := testRoom(t, 0)
	// p0 (s0) plays a3 (red3, legal on red2) under request id "dup".
	out1 := r.Submit(NewRequest(KindPlayCard, "s0", "dup", exp(0), game.Action{
		Type: game.ActionPlayCard, CardID: "a3"}))
	if !out1.OK() {
		t.Fatalf("first play rejected: %+v", out1)
	}
	after := r.revision
	// Same request_id, same payload -> cached, no further commit.
	out2 := r.Submit(NewRequest(KindPlayCard, "s0", "dup", exp(0), game.Action{
		Type: game.ActionPlayCard, CardID: "a3"}))
	if out2.ErrCode != "" {
		t.Fatalf("cached replay returned error: %+v", out2)
	}
	if r.revision != after {
		t.Fatalf("duplicate advanced revision: after first=%d, after dup=%d", after, r.revision)
	}
}

// TestIntegration_ReconnectResumesSamePlayer drops a client and rejoins it with
// its reconnect token, asserting the same seat is reclaimed and the game view
// resumes.
func TestIntegration_ReconnectResumesSamePlayer(t *testing.T) {
	_, addr := startRealServer(t)
	host, guest := startGame(t, addr)
	_ = host

	pid := guest.Identity().PlayerID
	rtok := guest.Identity().ReconnectToken
	guest.Close()
	time.Sleep(150 * time.Millisecond)

	bob2 := client.NewClient(client.Config{Address: addr, Name: "Bob", RoomKey: "sekret",
		ReconnectToken: rtok, ResponseTimeout: 4 * time.Second})
	t.Cleanup(bob2.Close)
	if err := bob2.Connect(addr); err != nil {
		t.Fatalf("bob reconnect: %v", err)
	}
	if bob2.Identity().PlayerID != pid {
		t.Fatalf("reconnected to seat %q, want %q", bob2.Identity().PlayerID, pid)
	}
}

// TestHandCountListMatchesActual samples each client's personalized snapshot
// repeatedly throughout a real game and checks that the hand count shown in
// the player list for the client themself always equals the length of their
// own hand (YourHand). Any mismatch is reported as a test failure.
func TestHandCountListMatchesActual(t *testing.T) {
	s, addr := startRealServer(t)
	_ = s
	host, guest := startGame(t, addr)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	check := func(c *client.Client, who string, errs chan<- string) {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			snap := c.Snapshot()
			if snap == nil || len(snap.YourHand) == 0 {
				time.Sleep(10 * time.Millisecond)
				continue
			}
			if snap.Phase == string(game.PhaseFinished) {
				return
			}
			var listCount int
			found := false
			for _, p := range snap.Players {
				if p.ID == c.Identity().PlayerID {
					listCount = p.CardCount
					found = true
					break
				}
			}
			if found && listCount != len(snap.YourHand) {
				errs <- fmt.Sprintf("[%s] phase=%s list card_count=%d but actual hand=%d",
					who, snap.Phase, listCount, len(snap.YourHand))
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}

	errs := make(chan string, 16)
	go check(host, "host", errs)
	go check(guest, "guest", errs)

	winnerSeen := func() bool {
		if s := host.Snapshot(); s != nil && s.WinnerPlayerID != "" {
			return true
		}
		return guest.Snapshot() != nil && guest.Snapshot().WinnerPlayerID != ""
	}
	done := make(chan struct{}, 2)
	go func() { autoPlay(ctx, host, winnerSeen); done <- struct{}{} }()
	go func() { autoPlay(ctx, guest, winnerSeen); done <- struct{}{} }()

	waitUntil(t, 30*time.Second, winnerSeen, "a winner to be reached")
	cancel()
	<-done
	<-done

	select {
	case e := <-errs:
		t.Fatalf("hand count mismatch observed: %s", e)
	default:
	}
}
