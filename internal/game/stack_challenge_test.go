package game

import "testing"

// challengeGame builds a 4-player game where p0 played a +4 onto an empty pile
// with BOTH +4 stacking (R20) and the +4 challenge (R13) enabled. Under the
// combined stacking + challenge rule (Option A / rule.md R23) the FIRST +4 does
// not begin a stack: it enters the challenge phase, and stacking only starts
// when the target re-stacks. p1 holds a stackable +4.
func challengeGame(t *testing.T) *Game {
	g := mkGame(
		[][]Card{
			{tcard("w4", ColorWild, RankWildDrawFour), tcard("a", ColorRed, RankOne)},
			{tcard("b", ColorGreen, RankTwo), tcard("w4b", ColorWild, RankWildDrawFour)},
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
	if g.Phase != PhaseChallenge || g.CurrentIndex != 1 {
		t.Fatalf("setup: phase=%s current=%d, want challenge/1", g.Phase, g.CurrentIndex)
	}
	return g
}

// TestStack_FirstFour_EntersChallengeNotStack verifies the FIRST +4 with both
// stacking and challenge enabled is challengeable and does not begin a stack
// pile: the game enters the challenge phase, the target may offer a +2/+4 to
// re-stack, and accept/challenge remain available.
func TestStack_FirstFour_EntersChallengeNotStack(t *testing.T) {
	g := challengeGame(t)
	if g.Phase != PhaseChallenge {
		t.Fatalf("phase = %s, want challenge (first +4 challengeable)", g.Phase)
	}
	lm := g.LegalMoves("p1")
	if len(lm.PlayableIDs) != 1 || lm.PlayableIDs[0] != "w4b" {
		t.Fatalf("p1 stackable in challenge = %v, want [w4b]", lm.PlayableIDs)
	}
	v := g.BuildViewFor("p1")
	if v.Challenge == nil || !v.Challenge.YourDecision {
		t.Fatalf("p1 should hold the +4 challenge decision, got %+v", v.Challenge)
	}
	if v.Stack != nil {
		t.Fatalf("no stack pile should exist before re-stacking, got %+v", v.Stack)
	}
}

// TestStack_ReStackFromChallenge_StartsNonChallengeablePile verifies the target
// can re-stack the first +4 from the challenge screen, which starts an
// accumulating pile (base +4 + re-stack) that can no longer be challenged.
func TestStack_ReStackFromChallenge_StartsNonChallengeablePile(t *testing.T) {
	g := challengeGame(t)
	red := ColorRed
	if _, err := g.Apply("p1", Action{Type: ActionPlayCard, CardID: "w4b", ChosenColor: &red}); err != nil {
		t.Fatalf("p1 re-stacks +4 from challenge: %v", err)
	}
	if g.Phase != PhaseStack || g.CurrentIndex != 2 {
		t.Fatalf("phase=%s current=%d, want stack/2", g.Phase, g.CurrentIndex)
	}
	if g.stackCredits != 8 { // original +4 (4) + re-stacked +4 (4)
		t.Fatalf("credits = %d, want 8", g.stackCredits)
	}
	if g.stackChallengeAvailable() {
		t.Fatal("once the pile is stacking, no challenge may be available")
	}
	v := g.BuildViewFor("p2")
	if v.Stack == nil || v.Stack.CanChallenge {
		t.Fatalf("p2 stack view = %+v, want can_challenge=false", v.Stack)
	}
	if _, err := g.Apply("p2", Action{Type: ActionChallengeStack}); err == nil {
		t.Fatal("challenge_stack during the accumulated pile should be rejected")
	}
}

// TestStack_RejectedChallengeLeavesPileIntact verifies an attempted challenge
// against a re-stacked pile does not drain or alter it: the penalized player may
// only re-stack or accept.
func TestStack_RejectedChallengeLeavesPileIntact(t *testing.T) {
	g := challengeGame(t)
	red := ColorRed
	_, _ = g.Apply("p1", Action{Type: ActionPlayCard, CardID: "w4b", ChosenColor: &red}) // PhaseStack, p2 owes 8
	creditsBefore := g.stackCredits
	handBefore := len(g.Players[2].Hand)
	if _, err := g.Apply("p2", Action{Type: ActionChallengeStack}); err == nil {
		t.Fatal("challenge_stack must be rejected during stacking")
	}
	if g.stackCredits != creditsBefore {
		t.Fatalf("credits changed on rejected challenge: %d -> %d", creditsBefore, g.stackCredits)
	}
	if len(g.Players[2].Hand) != handBefore {
		t.Fatalf("challenger hand changed on rejected challenge")
	}
	if g.Phase != PhaseStack || g.CurrentIndex != 2 {
		t.Fatalf("phase=%s current=%d, want unchanged stack/2", g.Phase, g.CurrentIndex)
	}
}

// TestStack_NotChallengeableForAnyPile builds several re-stacked pile
// compositions and asserts a challenge is never available once the penalty is an
// accumulating pile — the only options are re-stack or accept.
func TestStack_NotChallengeableForAnyPile(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*testing.T) *Game
	}{
		{"base +4 then +2", func(t *testing.T) *Game { return challengeGame(t) }},
		{"base +4 then +4", func(t *testing.T) *Game {
			g := mkGame(
				[][]Card{
					{tcard("w4", ColorWild, RankWildDrawFour), tcard("a", ColorRed, RankOne)},
					{tcard("w4b", ColorWild, RankWildDrawFour), tcard("b", ColorGreen, RankTwo)},
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
				t.Fatalf("p0 +4: %v", err)
			}
			if _, err := g.Apply("p1", Action{Type: ActionPlayCard, CardID: "w4b", ChosenColor: &red}); err != nil {
				t.Fatalf("p1 re-stacks +4: %v", err)
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

// TestStack_FirstFour_AcceptDrawsFourNoPile verifies that in the combined
// stacking + challenge mode the target can simply accept the first +4, which
// draws 4 and leaves no stack pile behind (stacking only begins on an actual
// re-stack).
func TestStack_FirstFour_AcceptDrawsFourNoPile(t *testing.T) {
	g := challengeGame(t)
	before := len(g.Players[1].Hand)
	res, err := g.Apply("p1", Action{Type: ActionAcceptFour})
	if err != nil {
		t.Fatalf("p1 accepts +4: %v", err)
	}
	if got := len(g.Players[1].Hand) - before; got != 4 {
		t.Fatalf("p1 drew %d cards, want 4", got)
	}
	if g.Phase == PhaseStack || g.Phase == PhaseChallenge {
		t.Fatalf("phase = %s, want a normal turn after accepting", g.Phase)
	}
	if g.stackCredits != 0 || len(g.stackPile) != 0 {
		t.Fatalf("no stack pile should remain after accepting, credits=%d pile=%d", g.stackCredits, len(g.stackPile))
	}
	_ = res
}

// TestStack_FinalFourDeferredWin_ResolvesOnAccept reproduces the reported bug:
// a player who plays a w+4 as their FINAL card while re-stacking a pile gets its
// win deferred (R27). That deferred win must be confirmed once the pile is
// finally accepted. Previously applyAcceptStack never called
// maybeResolveDeferredWin, so the game kept running with a 0-card player.
func TestStack_FinalFourDeferredWin_ResolvesOnAccept(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("d2", ColorRed, RankDrawTwo), tcard("a", ColorRed, RankOne)}, // extra so p0 doesn't empty
			{tcard("w4", ColorWild, RankWildDrawFour)},                          // p1's only card
			{tcard("c", ColorGreen, RankTwo)},
		},
		[]Card{
			tcard("q1", ColorBlue, RankOne), tcard("q2", ColorGreen, RankTwo),
			tcard("q3", ColorYellow, RankThree), tcard("q4", ColorRed, RankFour),
			tcard("q5", ColorBlue, RankFive), tcard("q6", ColorGreen, RankSix),
			tcard("q7", ColorRed, RankSeven),
		},
		tcard("top", ColorRed, RankFive), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.HouseRules.StackDrawTwo = true
	g.Config.HouseRules.StackDrawFour = true
	// Challenge stays ON (default) so a final +4 defers its win (R27).

	// p0 plays a +2 -> starts a 2-credit pile handed to p1.
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d2"}); err != nil {
		t.Fatalf("p0 plays +2: %v", err)
	}
	if g.Phase != PhaseStack || g.stackCredits != 2 || g.currentPlayer().ID != "p1" {
		t.Fatalf("after p0 +2: phase=%s credits=%d current=%s", g.Phase, g.stackCredits, g.currentPlayer().ID)
	}

	// p1 re-stacks with its final card, the w+4 -> hand empties, win deferred.
	green := ColorGreen
	res, err := g.Apply("p1", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: &green})
	if err != nil {
		t.Fatalf("p1 re-stacks final +4: %v", err)
	}
	if res.Ended {
		t.Fatal("win must be deferred, not immediate, while the stack is unsettled")
	}
	if len(g.Players[1].Hand) != 0 {
		t.Fatalf("p1 hand = %d, want 0 after playing final card", len(g.Players[1].Hand))
	}
	if g.stackCredits != 6 || g.currentPlayer().ID != "p2" {
		t.Fatalf("after p1 +4: credits=%d current=%s, want 6/p2", g.stackCredits, g.currentPlayer().ID)
	}

	// p2 accepts the 6-credit pile -> p1's deferred win must now be confirmed.
	res, err = g.Apply("p2", Action{Type: ActionAcceptStack})
	if err != nil {
		t.Fatalf("p2 accepts pile: %v", err)
	}
	if !res.Ended || g.WinnerPlayerID != "p1" {
		t.Fatalf("deferred +4 win not resolved on pile accept: ended=%v winner=%q", res.Ended, g.WinnerPlayerID)
	}
	if g.Phase != PhaseFinished {
		t.Fatalf("phase=%s, want finished", g.Phase)
	}
	if len(g.Players[1].Hand) != 0 {
		t.Fatalf("winner p1 should have 0 cards, has %d", len(g.Players[1].Hand))
	}
}
