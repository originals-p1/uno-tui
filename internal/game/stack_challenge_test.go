package game

import "testing"

// combinedStackGame builds a 4-player game in PhaseStack where p0 played a +4
// onto an empty pile (stackCredits=4, current is p1). Under the current rule
// (stacking + challenge are mutually exclusive) this pile is NOT challengeable.
func combinedStackGame(t *testing.T) *Game {
	g := mkGame(
		[][]Card{
			{tcard("w4", ColorWild, RankWildDrawFour), tcard("a", ColorRed, RankOne)},
			{tcard("b", ColorGreen, RankTwo)},
			{tcard("c", ColorYellow, RankThree)},
			{tcard("d", ColorBlue, RankFour)},
		},
		[]Card{tcard("q1", ColorGreen, RankOne), tcard("q2", ColorYellow, RankTwo),
			tcard("q3", ColorRed, RankThree), tcard("q4", ColorBlue, RankFour),
			tcard("q5", ColorGreen, RankFive), tcard("q6", ColorRed, RankSix)},
		tcard("top", ColorRed, RankFive), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.HouseRules.StackDrawFour = true
	g.Config.Cards.ChallengeDrawFour = true
	red := ColorRed
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: &red}); err != nil {
		t.Fatalf("p0 plays +4: %v", err)
	}
	if g.Phase != PhaseStack || g.CurrentIndex != 1 {
		t.Fatalf("setup: phase=%s current=%d, want stack/1", g.Phase, g.CurrentIndex)
	}
	return g
}

// TestStack_Challenge_Disabled verifies stacking and +4 challenge are mutually
// exclusive: a +4 pile never offers a challenge and the action is rejected.
func TestStack_Challenge_Disabled(t *testing.T) {
	g := combinedStackGame(t)
	if g.stackChallengeAvailable() {
		t.Fatal("stacking pile must not offer a challenge")
	}
	lm := g.LegalMoves("p1")
	if lm.CanChallengeStack {
		t.Fatal("LegalMoves must not expose CanChallengeStack during stacking")
	}
	v := g.BuildViewFor("p1")
	if v.Stack == nil || v.Stack.CanChallenge {
		t.Fatalf("view stack=%+v, want can_challenge=false", v.Stack)
	}
	if _, err := g.Apply("p1", Action{Type: ActionChallengeStack}); err == nil {
		t.Fatal("challenge_stack during stacking should be rejected")
	}
}

// TestStack_Challenge_RejectedLeavesPileIntact verifies a rejected challenge does
// not drain, roll back, or alter the pile: the player may only re-stack/accept.
func TestStack_Challenge_RejectedLeavesPileIntact(t *testing.T) {
	g := combinedStackGame(t)
	creditsBefore := g.stackCredits
	handBefore := len(g.Players[1].Hand)
	if _, err := g.Apply("p1", Action{Type: ActionChallengeStack}); err == nil {
		t.Fatal("challenge_stack must be rejected during stacking")
	}
	if g.stackCredits != creditsBefore {
		t.Fatalf("credits changed on rejected challenge: %d -> %d", creditsBefore, g.stackCredits)
	}
	if len(g.Players[1].Hand) != handBefore {
		t.Fatalf("challenger hand changed on rejected challenge")
	}
	if g.Phase != PhaseStack || g.CurrentIndex != 1 {
		t.Fatalf("phase=%s current=%d, want unchanged stack/1", g.Phase, g.CurrentIndex)
	}
}

// TestStack_Challenge_NotAvailableForAnyPile builds several pile compositions
// (mixed +2/+4 and pure +4) and asserts a challenge is never available once the
// penalty is stacking — the only options are re-stack or accept.
func TestStack_Challenge_NotAvailableForAnyPile(t *testing.T) {
	cases := []struct {
		name string
		// build plays a sequence of card IDs onto the pile (each with a chosen
		// color where needed) and returns the current player's options.
		setup func(*testing.T) *Game
	}{
		{"single +4", func(t *testing.T) *Game { return combinedStackGame(t) }},
		{"+2 then +4", func(t *testing.T) *Game {
			g := mkGame(
				[][]Card{
					{tcard("d2a", ColorRed, RankDrawTwo), tcard("x0", ColorRed, RankOne)},
					{tcard("w4", ColorWild, RankWildDrawFour), tcard("x1", ColorGreen, RankTwo)},
					{tcard("c", ColorYellow, RankThree)},
					{tcard("d", ColorBlue, RankFour)},
				},
				[]Card{tcard("q1", ColorGreen, RankOne), tcard("q2", ColorYellow, RankTwo),
					tcard("q3", ColorRed, RankThree), tcard("q4", ColorBlue, RankFour),
					tcard("q5", ColorGreen, RankFive), tcard("q6", ColorRed, RankSix)},
				tcard("top", ColorRed, RankFive), ColorRed, 1, 0, PhaseTurn,
			)
			g.Config.HouseRules.MixedStacking = true
			g.Config.Cards.ChallengeDrawFour = true
			if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d2a"}); err != nil {
				t.Fatalf("p0 +2: %v", err)
			}
			red := ColorRed
			if _, err := g.Apply("p1", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: &red}); err != nil {
				t.Fatalf("p1 +4: %v", err)
			}
			return g
		}},
		{"two +4s", func(t *testing.T) *Game {
			g := mkGame(
				[][]Card{
					{tcard("w4a", ColorWild, RankWildDrawFour), tcard("x0", ColorRed, RankOne)},
					{tcard("w4b", ColorWild, RankWildDrawFour), tcard("x1", ColorGreen, RankTwo)},
					{tcard("c", ColorYellow, RankThree)},
					{tcard("d", ColorBlue, RankFour)},
				},
				[]Card{tcard("q1", ColorGreen, RankOne), tcard("q2", ColorYellow, RankTwo),
					tcard("q3", ColorRed, RankThree), tcard("q4", ColorBlue, RankFour),
					tcard("q5", ColorGreen, RankFive), tcard("q6", ColorRed, RankSix),
					tcard("q7", ColorGreen, RankSeven), tcard("q8", ColorRed, RankEight)},
				tcard("top", ColorRed, RankFive), ColorRed, 1, 0, PhaseTurn,
			)
			g.Config.HouseRules.StackDrawFour = true
			g.Config.Cards.ChallengeDrawFour = true
			red := ColorRed
			if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4a", ChosenColor: &red}); err != nil {
				t.Fatalf("p0 +4: %v", err)
			}
			if _, err := g.Apply("p1", Action{Type: ActionPlayCard, CardID: "w4b", ChosenColor: &red}); err != nil {
				t.Fatalf("p1 +4: %v", err)
			}
			if g.Phase != PhaseStack {
				t.Fatalf("phase = %s, want stack", g.Phase)
			}
			return g
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := tc.setup(t)
			if g.stackChallengeAvailable() {
				t.Fatal("challenge must be unavailable during stacking")
			}
			lm := g.LegalMoves(g.currentPlayer().ID)
			if lm.CanChallengeStack {
				t.Fatal("CanChallengeStack must be false during stacking")
			}
			if _, err := g.Apply(g.currentPlayer().ID, Action{Type: ActionChallengeStack}); err == nil {
				t.Fatal("challenge_stack action must be rejected during stacking")
			}
		})
	}
}

func (r *Result) eventsContain(t EventType) (bool, bool) {
	for _, e := range r.Events {
		if e.Type == t {
			return true, true
		}
	}
	return false, false
}
