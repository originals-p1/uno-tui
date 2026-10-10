package server

import (
	"crypto/sha256"
	"testing"
	"time"

	"terminal-uno/internal/game"
)

func lobbyRoom(t *testing.T, cfg *Config) (*Room, *fakeClock) {
	t.Helper()
	c := DefaultConfig()
	c.RoomKey = "topsecret"
	c.HostBootstrapToken = "bootstrap-token"
	c.ReconnectGrace = 60 * time.Second
	if cfg != nil {
		if cfg.RoomKey != "" {
			c.RoomKey = cfg.RoomKey
		}
		if cfg.HostBootstrapToken != "" {
			c.HostBootstrapToken = cfg.HostBootstrapToken
		}
		if cfg.ReconnectGrace != 0 {
			c.ReconnectGrace = cfg.ReconnectGrace
		}
	}
	clock := newFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	r := NewLobbyRoom(c, clock)
	go r.Run()
	t.Cleanup(r.Close)
	return r, clock
}

func hello(name, key, rtoken, htoken string) *Request {
	return &Request{
		Kind: KindHello, SessionID: "conn-" + name, RequestID: "h-" + name,
		Hello: &HelloReq{
			PlayerName: name, RoomKey: key,
			ReconnectToken: rtoken, HostBootstrapToken: htoken,
		},
	}
}

// --- Task 6: auth ---

func TestAuth_RoomKeyValidation(t *testing.T) {
	r, _ := lobbyRoom(t, nil)
	bad := r.Submit(hello("Alice", "wrong", "", ""))
	if bad.ErrCode != CodeAuthFailed || bad.Joined {
		t.Fatalf("bad key should be rejected: %+v", bad)
	}
	good := r.Submit(hello("Alice", "topsecret", "", ""))
	if good.ErrCode != "" || !good.Joined || good.PlayerID == "" || good.SessionToken == "" {
		t.Fatalf("good key should join: %+v", good)
	}
}

func TestAuth_HostBootstrapIdentity(t *testing.T) {
	r, _ := lobbyRoom(t, nil)
	p := r.Submit(hello("Bob", "topsecret", "", ""))
	if p.IsHost {
		t.Fatal("plain player must not be host")
	}
	h := r.Submit(hello("HostName", "topsecret", "", "bootstrap-token"))
	if !h.IsHost || h.PlayerID == "" {
		t.Fatalf("bootstrap should grant host: %+v", h)
	}
	dup := r.Submit(hello("Intruder", "topsecret", "", "bootstrap-token"))
	if dup.ErrCode != CodeAuthFailed {
		t.Fatalf("second host should be rejected, got %s", dup.ErrCode)
	}
}

func TestAuth_TokenStoredAsDigestOnly(t *testing.T) {
	r, _ := lobbyRoom(t, nil)
	out := r.Submit(hello("Carol", "topsecret", "", ""))
	if !out.Joined {
		t.Fatalf("join failed: %+v", out)
	}
	sess := r.sessions[out.SessionID]
	if len(sess.tokenHash) != sha256.Size {
		t.Fatalf("session token digest len = %d", len(sess.tokenHash))
	}
	if string(sess.tokenHash) == out.SessionToken {
		t.Fatal("raw session token must not equal stored digest")
	}
	if len(r.reconnect[out.PlayerID]) != sha256.Size || string(r.reconnect[out.PlayerID]) == out.ReconnectToken {
		t.Fatal("reconnect token must be stored as digest only")
	}
}

func TestAuth_ReconnectTokenRotation(t *testing.T) {
	r, _ := lobbyRoom(t, nil)
	out := r.Submit(hello("Dave", "topsecret", "", ""))
	tok1 := out.ReconnectToken
	if tok1 == "" {
		t.Fatal("expected a reconnect token")
	}

	r.Submit(&Request{Kind: KindDisconnect, SessionID: out.SessionID, Disconnect: &Disconnect{PlayerID: out.PlayerID}})

	rec := r.Submit(hello("Dave", "topsecret", tok1, ""))
	if rec.ErrCode != "" || rec.PlayerID != out.PlayerID {
		t.Fatalf("reconnect failed: %+v", rec)
	}
	if rec.ReconnectToken == "" || rec.ReconnectToken == tok1 {
		t.Fatalf("reconnect token must rotate (old=%q new=%q)", tok1, rec.ReconnectToken)
	}
	// Old token invalid now.
	if old := r.Submit(hello("Dave", "topsecret", tok1, "")); old.ErrCode != CodeAuthFailed {
		t.Fatalf("old reconnect token should be rejected, got %s", old.ErrCode)
	}
}

func TestAuth_SanitizedAndNoDuplicateNames(t *testing.T) {
	r, _ := lobbyRoom(t, nil)
	a := r.Submit(hello("  Alice  ", "topsecret", "", ""))
	if a.Name != "Alice" {
		t.Fatalf("name not sanitized: %q", a.Name)
	}
	if dup := r.Submit(hello("Alice", "topsecret", "", "")); dup.ErrCode != CodeNameInvalid {
		t.Fatalf("duplicate name err = %s, want NAME_INVALID", dup.ErrCode)
	}
}

// seat records identity returned by hello.
type seat struct {
	sessID   string
	playerID string
	rtoken   string
}

func joinSeat(r *Room, name string) seat {
	out := r.Submit(hello(name, "topsecret", "", ""))
	if out.ErrCode != "" {
		panic("join failed: " + out.ErrCode)
	}
	return seat{sessID: out.SessionID, playerID: out.PlayerID, rtoken: out.ReconnectToken}
}

// startThree builds a lobby with host + 2 players and starts the game.
func startThree(t *testing.T) (*Room, *fakeClock, seat, seat, seat) {
	r, clock := lobbyRoom(t, nil)
	ho := r.Submit(hello("Host", "topsecret", "", "bootstrap-token"))
	host := seat{sessID: ho.SessionID, playerID: ho.PlayerID, rtoken: ho.ReconnectToken}
	p1 := joinSeat(r, "P1")
	p2 := joinSeat(r, "P2")
	r.Submit(&Request{Kind: KindReady, SessionID: p1.sessID, RequestID: "r1", Ready: &Ready{Value: true}})
	r.Submit(&Request{Kind: KindReady, SessionID: p2.sessID, RequestID: "r2", Ready: &Ready{Value: true}})
	start := r.Submit(&Request{Kind: KindStartGame, SessionID: host.sessID, RequestID: "s1"})
	if !start.OK() {
		t.Fatalf("start failed: %+v", start)
	}
	return r, clock, host, p1, p2
}

func TestPause_DisconnectAndResumePhase(t *testing.T) {
	r, _, host, p1, _ := startThree(t)
	if r.game.Phase != game.PhaseTurn {
		t.Fatalf("phase after start = %s, want turn", r.game.Phase)
	}

	r.Submit(&Request{Kind: KindDisconnect, SessionID: p1.sessID, Disconnect: &Disconnect{PlayerID: p1.playerID}})
	if r.game.Phase != game.PhasePaused || r.prevPhase != game.PhaseTurn {
		t.Fatalf("phase=%s prev=%s, want paused/turn", r.game.Phase, r.prevPhase)
	}

	// Reconnect with the original raw token.
	rec := r.Submit(hello("P1", "topsecret", p1.rtoken, ""))
	if rec.ErrCode != "" {
		t.Fatalf("reconnect failed: %+v", rec)
	}
	if r.game.Phase != game.PhaseTurn || len(r.disconnected) != 0 {
		t.Fatalf("after resume phase=%s disconnected=%d, want turn/0", r.game.Phase, len(r.disconnected))
	}
	_ = host
}

func TestPause_DuringPauseGameActionsRejected(t *testing.T) {
	r, _, _, p1, p2 := startThree(t)
	r.Submit(&Request{Kind: KindDisconnect, SessionID: p1.sessID, Disconnect: &Disconnect{PlayerID: p1.playerID}})
	if r.game.Phase != game.PhasePaused {
		t.Fatalf("phase = %s, want paused", r.game.Phase)
	}
	out := r.Submit(&Request{
		Kind: KindPlayCard, SessionID: p2.sessID, RequestID: "pa", Expected: exp(r.revision),
		Action: game.Action{Type: game.ActionPlayCard},
	})
	if out.OK() || out.ErrCode != "INVALID_PHASE" {
		t.Fatalf("action during pause = %+v, want INVALID_PHASE", out)
	}
}

func TestPause_MultipleTimersAndTimeout(t *testing.T) {
	r, clock, _, p1, p2 := startThree(t)
	base := clock.Now().Truncate(0)

	r.Submit(&Request{Kind: KindDisconnect, SessionID: p1.sessID, Disconnect: &Disconnect{PlayerID: p1.playerID}})
	clock.Advance(10 * time.Second)
	r.Submit(&Request{Kind: KindDisconnect, SessionID: p2.sessID, Disconnect: &Disconnect{PlayerID: p2.playerID}})

	if len(r.disconnected) != 2 {
		t.Fatalf("disconnected = %d, want 2", len(r.disconnected))
	}
	// Separate 60s grace: p1 due at base+60, p2 due at base+70.
	if !r.disconnected[p1.playerID].Before(r.disconnected[p2.playerID]) {
		_ = base
		t.Fatal("p1 deadline should be before p2 (separate timers)")
	}
	// Advance just past p1's deadline while p1 still offline -> timeout.
	clock.Advance(51 * time.Second) // base+61
	// Synchronize with the room loop so the fired deadline is processed.
	r.Submit(&Request{Kind: KindStatus, SessionID: "sync"})
	if r.game.Phase != game.PhaseFinished || r.game.EndReason != "PLAYER_RECONNECT_TIMEOUT" {
		t.Fatalf("phase=%s reason=%s, want finished/PLAYER_RECONNECT_TIMEOUT", r.game.Phase, r.game.EndReason)
	}
}
