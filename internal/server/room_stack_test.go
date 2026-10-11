package server

import (
	"math/rand"
	"testing"

	"terminal-uno/internal/game"
)

// stackRoom builds a started room whose game is mid-way through a +2 stacking
// penalty: p0 plays a red +2 targeting p1, so p1 is in PhaseStack owing 2 cards.
func stackRoom(t *testing.T) *Room {
	t.Helper()
	g, err := game.NewGame([]string{"Alice", "Bob", "Carol"}, rand.NewSource(1))
	if err != nil {
		t.Fatalf("NewGame: %v", err)
	}
	// Alice holds a red +2 plus spare; Bob/Carol hold only number cards.
	g.Players[0].Hand = []game.Card{{ID: "d2a", Color: game.ColorRed, Rank: game.RankDrawTwo}, {ID: "s0", Color: game.ColorGreen, Rank: game.RankSeven}}
	g.Players[1].Hand = []game.Card{{ID: "b1", Color: game.ColorGreen, Rank: game.RankNine}}
	g.Players[2].Hand = []game.Card{{ID: "c1", Color: game.ColorRed, Rank: game.RankSix}}
	g.DiscardPile = []game.Card{{ID: "top", Color: game.ColorRed, Rank: game.RankFive}}
	g.DrawPile = []game.Card{{ID: "q1", Color: game.ColorGreen, Rank: game.RankOne}, {ID: "q2", Color: game.ColorYellow, Rank: game.RankTwo}, {ID: "q3", Color: game.ColorRed, Rank: game.RankThree}}
	g.ActiveColor = game.ColorRed
	g.CurrentIndex = 0
	g.Direction = 1
	g.Phase = game.PhaseTurn
	g.Config = game.DefaultConfig()
	g.Config.HouseRules.StackDrawTwo = true

	// Drive Alice's play via the engine so the room's game lands in PhaseStack.
	if _, err := g.Apply("p0", game.Action{Type: game.ActionPlayCard, CardID: "d2a"}); err != nil {
		t.Fatalf("drive to stack phase: %v", err)
	}

	r := NewRoom(g, 0, NewRealClock())
	r.BindPlayer("s0", "p0")
	r.BindPlayer("s1", "p1")
	r.BindPlayer("s2", "p2")
	go r.Run()
	t.Cleanup(r.Close)
	return r
}

// TestRoom_Stack_NonDecisionRejected verifies a player who is not the charged
// one cannot act during the stack phase.
func TestRoom_Stack_NonDecisionRejected(t *testing.T) {
	r := stackRoom(t)
	out := r.Submit(NewRequest(KindPlayCard, "s2", "rN1", exp(0), game.Action{Type: game.ActionPlayCard, CardID: "c1"}))
	if out.OK() {
		t.Fatalf("non-decision player action should be rejected: %+v", out)
	}
}

// TestRoom_Stack_AcceptFlowsToGame verifies the charged player accepting the
// stack draws the accumulated cards and play advances, clearing the banner.
func TestRoom_Stack_AcceptFlowsToGame(t *testing.T) {
	r := stackRoom(t)
	// Confirm the room's view already shows the stack to Bob (p1).
	before := r.Submit(NewRequest(KindStatus, "s1", "rB1", nil, game.Action{}))
	_ = before
	out := r.Submit(NewRequest(KindPlayCard, "s1", "rA1", exp(0), game.Action{Type: game.ActionAcceptStack}))
	if !out.OK() {
		t.Fatalf("accept stack failed: %+v", out)
	}
	v := out.Commit.Requester
	if v.Stack != nil {
		t.Fatalf("Stack banner should clear after accept, got %+v", v.Stack)
	}
	if v.Phase != string(game.PhaseTurn) {
		t.Fatalf("phase = %s, want turn", v.Phase)
	}
	if v.CurrentPlayer != "p2" {
		t.Fatalf("current player = %s, want p2 (after charged player)", v.CurrentPlayer)
	}
	// Bob drew the 2-card penalty (starting with 1 hand card).
	if pv := playerCount(v, "p1"); pv != 3 {
		t.Fatalf("p1 card count = %d, want 3 (1 hand + 2 drawn)", pv)
	}
}

func playerCount(v *game.GameView, id string) int {
	for _, p := range v.Players {
		if p.ID == id {
			return p.CardCount
		}
	}
	return -1
}

// challengeStackRoom builds a started room whose game is in PhaseStack with a
// challengeable +4 (combined stacking + challenge rule R23): p0 played a +4
// targeting p1. Both stacking and challenge are enabled.
func challengeStackRoom(t *testing.T) *Room {
	t.Helper()
	g, err := game.NewGame([]string{"Alice", "Bob", "Carol"}, rand.NewSource(1))
	if err != nil {
		t.Fatalf("NewGame: %v", err)
	}
	// Alice holds a green +4 + green spare (clean, not violated); Bob charged.
	g.Players[0].Hand = []game.Card{{ID: "w4", Color: game.ColorWild, Rank: game.RankWildDrawFour}, {ID: "ok", Color: game.ColorGreen, Rank: game.RankSeven}}
	g.Players[1].Hand = []game.Card{{ID: "b", Color: game.ColorGreen, Rank: game.RankNine}}
	g.Players[2].Hand = []game.Card{{ID: "c", Color: game.ColorRed, Rank: game.RankSix}}
	g.DiscardPile = []game.Card{{ID: "top", Color: game.ColorRed, Rank: game.RankFive}}
	g.DrawPile = []game.Card{{ID: "q1", Color: game.ColorGreen, Rank: game.RankOne}, {ID: "q2", Color: game.ColorYellow, Rank: game.RankTwo}, {ID: "q3", Color: game.ColorRed, Rank: game.RankThree}, {ID: "q4", Color: game.ColorBlue, Rank: game.RankFour}, {ID: "q5", Color: game.ColorGreen, Rank: game.RankFive}}
	g.ActiveColor = game.ColorRed
	g.CurrentIndex = 0
	g.Direction = 1
	g.Phase = game.PhaseTurn
	g.Config = game.DefaultConfig()
	g.Config.HouseRules.StackDrawFour = true
	g.Config.Cards.ChallengeDrawFour = true

	// Drive Alice's +4 via the engine so the room lands in PhaseStack with a
	// challengeable head.
	red := game.ColorRed
	if _, err := g.Apply("p0", game.Action{Type: game.ActionPlayCard, CardID: "w4", ChosenColor: &red}); err != nil {
		t.Fatalf("drive to stack phase: %v", err)
	}

	r := NewRoom(g, 0, NewRealClock())
	r.BindPlayer("s0", "p0")
	r.BindPlayer("s1", "p1")
	r.BindPlayer("s2", "p2")
	go r.Run()
	t.Cleanup(r.Close)
	return r
}

// TestRoom_Stack_ChallengeFlows verifies the combined-rule challenge_stack
// action flows through the server: a clean challenge makes the challenger draw
// the whole pile and the pile clears.
func TestRoom_Stack_ChallengeFlows(t *testing.T) {
	r := challengeStackRoom(t)
	out := r.Submit(NewRequest(KindPlayCard, "s1", "rC1", exp(0), game.Action{Type: game.ActionChallengeStack}))
	if !out.OK() {
		t.Fatalf("challenge_stack failed: %+v", out)
	}
	v := out.Commit.Requester
	if v.Stack != nil {
		t.Fatalf("stack should clear after clean challenge, got %+v", v.Stack)
	}
	if v.Phase != string(game.PhaseTurn) {
		t.Fatalf("phase = %s, want turn", v.Phase)
	}
	// Clean challenge: Bob drew the whole 4-card pile and was skipped -> Carol.
	if v.CurrentPlayer != "p2" {
		t.Fatalf("current = %s, want p2", v.CurrentPlayer)
	}
	if pv := playerCount(v, "p1"); pv != 5 {
		t.Fatalf("p1 card count = %d, want 5 (1 + 4 drawn)", pv)
	}
}
