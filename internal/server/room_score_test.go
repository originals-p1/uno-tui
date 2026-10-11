package server

import (
	"math/rand"
	"testing"

	"terminal-uno/internal/game"
)

// scoreRoom builds an already-started room running a SCORE-mode match with a
// controllable target. p0 holds a single red one (wins round 1), p1 holds a
// red four (worth 4 points to the winner).
func scoreRoom(t *testing.T, target int) *Room {
	t.Helper()
	g, err := game.NewGame([]string{"Alice", "Bob"}, rand.NewSource(1))
	if err != nil {
		t.Fatalf("NewGame: %v", err)
	}
	g.Players[0].Hand = []game.Card{{ID: "a1", Color: game.ColorRed, Rank: game.RankOne}}
	g.Players[1].Hand = []game.Card{{ID: "b4", Color: game.ColorRed, Rank: game.RankFour}}
	g.DiscardPile = []game.Card{{ID: "top", Color: game.ColorRed, Rank: game.RankTwo}}
	g.ActiveColor = game.ColorRed
	g.CurrentIndex = 0
	g.Direction = 1
	g.Phase = game.PhaseTurn

	r := NewRoom(g, 0, NewRealClock())
	r.config = game.DefaultConfig()
	r.config.Game.WinMode = game.WinScore
	r.config.Game.TargetScore = target
	r.BindPlayer("s0", "p0")
	r.BindPlayer("s1", "p1")
	r.beginScorebook()
	go r.Run()
	t.Cleanup(r.Close)
	return r
}

func TestRoom_ScoreMode_RoundAdvancesAndCarriesScore(t *testing.T) {
	r := scoreRoom(t, 500)

	out := r.Submit(NewRequest(KindPlayCard, "s0", "r1", exp(0), game.Action{
		Type: game.ActionPlayCard, CardID: "a1",
	}))
	if !out.OK() {
		t.Fatalf("winning play failed: %+v", out)
	}
	v := out.Commit.Requester
	// p1's red four is worth 4 points; match not over at target 500.
	if v.RoundNumber != 2 {
		t.Fatalf("round number = %d, want 2 (next round started)", v.RoundNumber)
	}
	if v.MatchOver {
		t.Fatalf("match should not be over at 4 points vs target 500")
	}
	if v.Scores["p0"] != 4 {
		t.Fatalf("p0 score = %d, want 4", v.Scores["p0"])
	}
	if v.Phase != string(game.PhaseTurn) {
		t.Fatalf("phase = %s, want turn (next round already dealt)", v.Phase)
	}
	if v.RoundResult == nil || v.RoundResult.Points != 4 || v.RoundResult.WinnerPlayerID != "p0" {
		t.Fatalf("round result banner missing/incorrect: %+v", v.RoundResult)
	}
	if len(v.YourHand) != 7 {
		t.Fatalf("next round should deal a fresh 7-card hand, got %d", len(v.YourHand))
	}

	// The first in-round action of round 2 clears the banner.
	// p0 is current again (fresh deal); play its first playable card if any.
	lm := v.PlayableIDs
	if len(lm) == 0 {
		return // nothing playable without drawing; banner-clear is covered below
	}
	rev := v.RoomRevision
	out2 := r.Submit(NewRequest(KindPlayCard, "s0", "r2", exp(rev), game.Action{
		Type: game.ActionPlayCard, CardID: lm[0],
	}))
	if out2.OK() {
		if out2.Commit.Requester.RoundResult != nil {
			t.Fatalf("round result banner should clear on the next action")
		}
	}
}

func TestRoom_ScoreMode_MatchOverAtTarget(t *testing.T) {
	r := scoreRoom(t, 2) // 4 points >= target 2

	out := r.Submit(NewRequest(KindPlayCard, "s0", "m1", exp(0), game.Action{
		Type: game.ActionPlayCard, CardID: "a1",
	}))
	if !out.OK() {
		t.Fatalf("winning play failed: %+v", out)
	}
	v := out.Commit.Requester
	if !v.MatchOver {
		t.Fatalf("match should be over at 4 points vs target 2")
	}
	if v.MatchWinnerPlayerID != "p0" {
		t.Fatalf("match winner = %s, want p0", v.MatchWinnerPlayerID)
	}
	if v.Phase != string(game.PhaseFinished) {
		t.Fatalf("phase = %s, want finished", v.Phase)
	}
	if v.Scores["p0"] != 4 {
		t.Fatalf("p0 score = %d, want 4", v.Scores["p0"])
	}
}
