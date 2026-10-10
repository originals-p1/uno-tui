package server

import (
	"math/rand"
	"sync"
	"testing"

	"terminal-uno/internal/game"
)

// --- test fixtures ---

func testRoom(t *testing.T, startRev uint64) *Room {
	t.Helper()
	g, err := game.NewGame([]string{"Alice", "Bob"}, rand.NewSource(1))
	if err != nil {
		t.Fatalf("NewGame: %v", err)
	}
	// Deterministic hands & top card: p0 red1+red3, p1 blue2, top red2, p0's turn.
	g.Players[0].Hand = []game.Card{
		{ID: "a1", Color: game.ColorRed, Rank: game.RankOne},
		{ID: "a3", Color: game.ColorRed, Rank: game.RankThree},
	}
	g.Players[1].Hand = []game.Card{
		{ID: "b4", Color: game.ColorRed, Rank: game.RankFour},
	}
	g.DiscardPile = []game.Card{{ID: "top", Color: game.ColorRed, Rank: game.RankTwo}}
	g.ActiveColor = game.ColorRed
	g.CurrentIndex = 0
	g.Direction = 1
	g.Phase = game.PhaseTurn

	r := NewRoom(g, startRev, NewRealClock())
	r.BindPlayer("s0", "p0")
	r.BindPlayer("s1", "p1")
	go r.Run()
	t.Cleanup(r.Close)
	return r
}

func exp(u uint64) *uint64 { return &u }

// --- Task 5: revision semantics ---

func TestRoom_RevisionIncrementsExactlyOnce(t *testing.T) {
	r := testRoom(t, 0)

	// p0 plays a3 (red3, playable on red2) -> commits, revision 0->1.
	out := r.Submit(NewRequest(KindPlayCard, "s0", "r1", exp(0), game.Action{
		Type: game.ActionPlayCard, CardID: "a3",
	}))
	if !out.OK() {
		t.Fatalf("play failed: %+v", out)
	}
	if out.Commit.Revision != 1 {
		t.Fatalf("revision = %d, want 1", out.Commit.Revision)
	}
	// Requester's view reflects the new revision.
	if out.Commit.Requester.RoomRevision != 1 {
		t.Fatalf("requester view revision = %d, want 1", out.Commit.Requester.RoomRevision)
	}

	// Rejected request must not bump revision.
	bad := r.Submit(NewRequest(KindPlayCard, "s1", "r2", exp(1), game.Action{
		Type: game.ActionPlayCard, CardID: "nope", // not owned
	}))
	if bad.OK() {
		t.Fatal("expected rejection")
	}
	if bad.ErrCode != "CARD_NOT_OWNED" {
		t.Fatalf("err = %s, want CARD_NOT_OWNED", bad.ErrCode)
	}
	// p1's turn after p0's play; p1 plays b2 (blue2, rank-matches red2) -> rev 2.
	out2 := r.Submit(NewRequest(KindPlayCard, "s1", "r3", exp(1), game.Action{
		Type: game.ActionPlayCard, CardID: "b4",
	}))
	if !out2.OK() {
		t.Fatalf("p1 play failed: %+v", out2)
	}
	if out2.Commit.Revision != 2 {
		t.Fatalf("revision = %d, want 2", out2.Commit.Revision)
	}
}

// --- Task 5: idempotency ---

func TestRoom_DuplicateReplay_NoReexecution(t *testing.T) {
	r := testRoom(t, 0)
	act := game.Action{Type: game.ActionPlayCard, CardID: "a3"}

	first := r.Submit(NewRequest(KindPlayCard, "s0", "req-x", exp(0), act))
	if !first.OK() {
		t.Fatalf("first failed: %+v", first)
	}
	if first.Commit.Revision != 1 {
		t.Fatalf("revision = %d, want 1", first.Commit.Revision)
	}

	// Re-send identical request (e.g. client re-sends after lost response).
	replay := r.Submit(NewRequest(KindPlayCard, "s0", "req-x", exp(1), act))
	if !replay.OK() {
		t.Fatalf("replay should return cached success: %+v", replay)
	}
	if replay.Commit.Revision != 1 {
		t.Fatalf("replay revision = %d, want cached 1 (no re-execution)", replay.Commit.Revision)
	}
	// No revision bump from the replay: keep issuing a fresh p1 action.
	next := r.Submit(NewRequest(KindPlayCard, "s1", "req-y", exp(1), game.Action{
		Type: game.ActionPlayCard, CardID: "b4",
	}))
	if !next.OK() || next.Commit.Revision != 2 {
		t.Fatalf("next revision = %d ok=%v, want 2", next.Commit.Revision, next.OK())
	}
}

func TestRoom_SameIDDifferentPayload_RequestIDReused(t *testing.T) {
	r := testRoom(t, 0)
	first := r.Submit(NewRequest(KindPlayCard, "s0", "req-z", exp(0), game.Action{
		Type: game.ActionPlayCard, CardID: "a3",
	}))
	if !first.OK() {
		t.Fatalf("first failed: %+v", first)
	}
	before := r.revision
	// Same request_id, different action -> REQUEST_ID_REUSED, no bump.
	reused := r.Submit(NewRequest(KindPlayCard, "s0", "req-z", exp(1), game.Action{
		Type: game.ActionPlayCard, CardID: "a1",
	}))
	if reused.OK() {
		t.Fatal("expected REQUEST_ID_REUSED")
	}
	if reused.ErrCode != CodeRequestIDReused {
		t.Fatalf("err = %s, want REQUEST_ID_REUSED", reused.ErrCode)
	}
	if r.revision != before {
		t.Fatalf("revision changed on REQUEST_ID_REUSED: %d -> %d", before, r.revision)
	}
}

func TestRoom_StaleRevision_ReturnsSnapshot(t *testing.T) {
	r := testRoom(t, 0)
	first := r.Submit(NewRequest(KindPlayCard, "s0", "q1", exp(0), game.Action{
		Type: game.ActionPlayCard, CardID: "a3",
	}))
	if !first.OK() {
		t.Fatalf("first failed: %+v", first)
	}
	// Stale expected revision (0) vs current (1) -> STALE_REVISION + snapshot.
	stale := r.Submit(NewRequest(KindPlayCard, "s1", "q2", exp(0), game.Action{
		Type: game.ActionPlayCard, CardID: "b4",
	}))
	if stale.OK() {
		t.Fatal("expected STALE_REVISION")
	}
	if stale.ErrCode != CodeStaleRevision {
		t.Fatalf("err = %s, want STALE_REVISION", stale.ErrCode)
	}
	if stale.Snapshot == nil {
		t.Fatal("stale error must carry a personal snapshot")
	}
	if stale.Snapshot.RoomRevision != 1 {
		t.Fatalf("snapshot revision = %d, want 1", stale.Snapshot.RoomRevision)
	}
	// Snapshot contains only the requester's view (privacy).
	if len(stale.Snapshot.YourHand) == 0 || stale.Snapshot.YourHand[0].ID != "b4" {
		t.Fatalf("snapshot hand = %+v, want requester's cards", stale.Snapshot.YourHand)
	}
}

// --- Task 5: replay across reconnect (cache preserves) ---

func TestRoom_ReplayAfterReconnect(t *testing.T) {
	r := testRoom(t, 0)
	act := game.Action{Type: game.ActionPlayCard, CardID: "a3"}
	first := r.Submit(NewRequest(KindPlayCard, "s0", "rc-1", exp(0), act))
	if !first.OK() {
		t.Fatalf("first failed: %+v", first)
	}
	// Simulate reconnect: drop old binding, bind a NEW session for the same
	// player. The logical (player-keyed) cache survives.
	r.sessions["s0"].PlayerID = ""
	delete(r.playerSessions, "p0")
	// Also keep s0 removed and add s0new -> p0
	delete(r.sessions, "s0")
	r.BindPlayer("s0new", "p0")

	// Client re-sends the same request it never got an ack for. Because the
	// cache is keyed by player, it replays the cached ack without re-executing.
	replay := r.Submit(NewRequest(KindPlayCard, "s0new", "rc-1", exp(0), act))
	if !replay.OK() {
		t.Fatalf("replay after reconnect should be cached OK: %+v", replay)
	}
	if replay.Commit.Revision != 1 {
		t.Fatalf("revision = %d, want cached 1 (no re-execution)", replay.Commit.Revision)
	}
	if r.revision != 1 {
		t.Fatalf("current revision = %d, want 1", r.revision)
	}
}

// --- Task 5: concurrent submissions ---

func TestRoom_ConcurrentSubmissions(t *testing.T) {
	r := testRoom(t, 0)
	const n = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0

	// Many goroutines submit p0's (current) legal play and p1's attempt to play
	// p0's card. Across all orderings, exactly ONE transaction can commit.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out := r.Submit(NewRequest(KindPlayCard, "s0", "c-play-"+itoa(i), exp(0), game.Action{
				Type: game.ActionPlayCard, CardID: "a3",
			}))
			if out.OK() {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}(i)
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out := r.Submit(NewRequest(KindPlayCard, "s1", "c-bad-"+itoa(i), exp(0), game.Action{
				Type: game.ActionPlayCard, CardID: "a3", // p0's card, not p1's
			}))
			if out.OK() {
				mu.Lock()
				success++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if success != 1 {
		t.Fatalf("committed count = %d, want exactly 1", success)
	}
	if r.revision != 1 {
		t.Fatalf("revision = %d, want 1", r.revision)
	}
}

// --- Task 5: event ID monotonicity & personalized broadcast ---

func TestRoom_MonotonicEventIDsAndBroadcast(t *testing.T) {
	r := testRoom(t, 5)
	out := r.Submit(NewRequest(KindPlayCard, "s0", "e1", exp(5), game.Action{
		Type: game.ActionPlayCard, CardID: "a3",
	}))
	if !out.OK() {
		t.Fatalf("play failed: %+v", out)
	}
	if out.Commit.Revision != 6 {
		t.Fatalf("revision = %d, want 6", out.Commit.Revision)
	}
	// History event IDs strictly increase.
	hist := out.Commit.Requester.History
	if len(hist) == 0 {
		t.Fatal("expected at least one history event")
	}
	last := uint64(0)
	for _, ev := range hist {
		if ev.ID <= last {
			t.Fatalf("event IDs not monotonic: %d after %d", ev.ID, last)
		}
		last = ev.ID
	}
	if out.Commit.Requester.LastEventID != last {
		t.Fatalf("LastEventID = %d, want %d", out.Commit.Requester.LastEventID, last)
	}
	// Personalized broadcast: each player's view contains only their own hand.
	if len(out.Commit.Views) != 2 {
		t.Fatalf("broadcast views = %d, want 2", len(out.Commit.Views))
	}
	v0 := out.Commit.Views["p0"]
	v1 := out.Commit.Views["p1"]
	// p0 played a3; v0 hand should have [a1], v1 hand [b2].
	if len(v0.YourHand) != 1 || v0.YourHand[0].ID != "a1" {
		t.Fatalf("p0 hand = %+v", v0.YourHand)
	}
	if len(v1.YourHand) != 1 || v1.YourHand[0].ID != "b4" {
		t.Fatalf("p1 hand = %+v", v1.YourHand)
	}
}

// --- Task 5: bounded idempotency cache ---

func TestIdemCache_Bounded(t *testing.T) {
	c := newIdemCache(256)
	for i := 0; i < 300; i++ {
		req := "req" + itoa(i)
		c.put(req, "digest", &Outcome{Commit: &Commit{Revision: uint64(i)}})
	}
	if len(c.entries) != 256 {
		t.Fatalf("cache size = %d, want 256", len(c.entries))
	}
	// The 300 oldest should be evicted; newer survive.
	if _, ok := c.get("req0"); ok {
		t.Fatal("oldest entry should be evicted")
	}
	if e, ok := c.get("req299"); !ok || e.outcome.Commit.Revision != 299 {
		t.Fatal("newest entry should survive")
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestRoom_UnknownSession(t *testing.T) {
	r := testRoom(t, 0)
	out := r.Submit(NewRequest(KindPlayCard, "nosuch", "x", exp(0), game.Action{Type: game.ActionPlayCard}))
	if out.OK() || out.ErrCode != CodeInvalidSession {
		t.Fatalf("outcome = %+v, want INVALID_SESSION", out)
	}
	if r.revision != 0 {
		t.Fatalf("revision changed for invalid session: %d", r.revision)
	}
}

// --- Task: rematch / return to lobby after a finished game ---

func TestRoom_RematchReturnsToLobby(t *testing.T) {
	r := testRoom(t, 0)
	// Simulate a finished game with rules locked.
	r.game.Phase = game.PhaseFinished
	r.game.WinnerPlayerID = "p0"
	r.configLocked = true

	out := r.Submit(NewRequest(KindRematch, "s0", "re-1", exp(0), game.Action{}))
	if !out.OK() {
		t.Fatalf("rematch outcome = %+v, want OK", out)
	}
	if r.game != nil {
		t.Fatalf("game should be reset to nil, got %+v", r.game)
	}
	if r.started {
		t.Fatalf("started should be false after rematch")
	}
	if r.configLocked {
		t.Fatalf("rules should be unlocked after rematch")
	}
	if out.Commit.Requester == nil || out.Commit.Requester.Phase != "lobby" {
		t.Fatalf("requester should get a lobby view, got %+v", out.Commit.Requester)
	}
	if len(out.Commit.Views) != 2 {
		t.Fatalf("both seated players should get a lobby view, got %d", len(out.Commit.Views))
	}
}

func TestRoom_RematchNotAllowedMidGame(t *testing.T) {
	r := testRoom(t, 0) // game is in progress (PhaseTurn), not finished
	out := r.Submit(NewRequest(KindRematch, "s0", "re-2", exp(0), game.Action{}))
	if out.OK() || out.ErrCode != CodeNotFinished {
		t.Fatalf("mid-game rematch outcome = %+v, want NOT_FINISHED", out)
	}
	if r.game == nil || !r.started {
		t.Fatalf("mid-game rematch must not reset the room: started=%v game=%v", r.started, r.game != nil)
	}
}

func TestRoom_RematchUnknownSession(t *testing.T) {
	r := testRoom(t, 0)
	r.game.Phase = game.PhaseFinished
	out := r.Submit(NewRequest(KindRematch, "nosuch", "re-3", exp(0), game.Action{}))
	if out.OK() || out.ErrCode != CodeInvalidSession {
		t.Fatalf("unknown-session rematch outcome = %+v, want INVALID_SESSION", out)
	}
}
