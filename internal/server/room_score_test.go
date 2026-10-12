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
	// p1's red four is worth 4 points; match not over at target 500. The room
	// now pauses in a round-over state (any player presses "c" to advance).
	if v.RoundNumber != 1 {
		t.Fatalf("round number = %d, want 1 (paused at end of round 1)", v.RoundNumber)
	}
	if !v.RoundOver {
		t.Fatal("round should be paused (round_over) awaiting the next round")
	}
	if v.MatchOver {
		t.Fatalf("match should not be over at 4 points vs target 500")
	}
	if v.Scores["p0"] != 4 {
		t.Fatalf("p0 score = %d, want 4", v.Scores["p0"])
	}
	if v.Phase != string(game.PhaseFinished) {
		t.Fatalf("phase = %s, want finished while paused", v.Phase)
	}
	if v.RoundResult == nil || v.RoundResult.Points != 4 || v.RoundResult.WinnerPlayerID != "p0" {
		t.Fatalf("round result banner missing/incorrect: %+v", v.RoundResult)
	}
	// The round result must carry each player's end-of-round hand (the winner's is
	// empty, p1 held a red four).
	if v.RoundResult.Hands == nil {
		t.Fatal("round result must expose final hands")
	}
	if len(v.RoundResult.Hands["p0"]) != 0 {
		t.Fatalf("winner final hand = %v, want empty", v.RoundResult.Hands["p0"])
	}
	if len(v.RoundResult.Hands["p1"]) != 1 || v.RoundResult.Hands["p1"][0].ID != "b4" {
		t.Fatalf("p1 final hand = %v, want its red four", v.RoundResult.Hands["p1"])
	}

	// Any player presses "c" (KindAdvanceRound) to start the next round.
	outAdv := r.Submit(NewRequest(KindAdvanceRound, "s1", "adv1", exp(v.RoomRevision), game.Action{}))
	if !outAdv.OK() {
		t.Fatalf("advance round failed: %+v", outAdv)
	}
	v2 := outAdv.Commit.Requester
	if v2.RoundNumber != 2 || v2.RoundOver || v2.MatchOver {
		t.Fatalf("after advance round=%d over=%v matchover=%v, want 2/false/false",
			v2.RoundNumber, v2.RoundOver, v2.MatchOver)
	}
	if v2.Phase != string(game.PhaseTurn) {
		t.Fatalf("phase = %s, want turn (round 2 dealt)", v2.Phase)
	}
	if len(v2.YourHand) != 7 {
		t.Fatalf("next round should deal a fresh 7-card hand, got %d", len(v2.YourHand))
	}

	// The first in-round action of round 2 clears the banner.
	lm := v2.PlayableIDs
	rev := v2.RoomRevision
	if len(lm) != 0 {
		out2 := r.Submit(NewRequest(KindPlayCard, "s0", "r2", exp(rev), game.Action{
			Type: game.ActionPlayCard, CardID: lm[0],
		}))
		if out2.OK() && out2.Commit.Requester.RoundResult != nil {
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

// TestRoom_ScoreMode_LoserDeductedByRemainingHand verifies the "扣分" rule:
// each non-winner is deducted the value of the cards remaining in their own
// hand, so p1 (holding a red four) goes to -4 while the winner gains the same.
func TestRoom_ScoreMode_LoserDeductedByRemainingHand(t *testing.T) {
	r := scoreRoom(t, 500)
	out := r.Submit(NewRequest(KindPlayCard, "s0", "r1", exp(0), game.Action{
		Type: game.ActionPlayCard, CardID: "a1",
	}))
	if !out.OK() {
		t.Fatalf("winning play failed: %+v", out)
	}
	v := out.Commit.Requester
	if v.Scores["p0"] != 4 {
		t.Fatalf("p0 score = %d, want +4 (winner gains p1's hand)", v.Scores["p0"])
	}
	if v.Scores["p1"] != -4 {
		t.Fatalf("p1 score = %d, want -4 (deducted own remaining hand)", v.Scores["p1"])
	}
	if v.RoundResult == nil || v.RoundResult.Deltas == nil {
		t.Fatalf("round result should carry per-player deltas: %+v", v.RoundResult)
	}
	if v.RoundResult.Deltas["p0"] != 4 || v.RoundResult.Deltas["p1"] != -4 {
		t.Fatalf("deltas = %+v, want p0 +4 / p1 -4", v.RoundResult.Deltas)
	}
}

// TestRoom_AdvanceRound_RejectedOutsideRoundOver verifies advance_round only
// works during the round-over pause (e.g. not during a live turn).
func TestRoom_AdvanceRound_RejectedOutsideRoundOver(t *testing.T) {
	r := scoreRoom(t, 500)
	// No round has ended yet — round_over is false.
	before := r.Submit(NewRequest(KindStatus, "s0", "st1", nil, game.Action{}))
	_ = before
	out := r.Submit(NewRequest(KindAdvanceRound, "s0", "advBad", exp(0), game.Action{}))
	if out.OK() {
		t.Fatalf("advance_round must be rejected outside the round-over pause: %+v", out)
	}
}
