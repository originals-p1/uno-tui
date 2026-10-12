package game

import "testing"

// combinedStackGame builds a 4-player game in PhaseStack where p0 played a +4
// onto an empty pile (challengeable under the combined rule). The pile has
// stackCredits=4 and p0 is the resolver.
func combinedStackGame(t *testing.T, resolverViolated bool) *Game {
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
	if resolverViolated {
		// p0 holds a red card --> violated (they had the active color).
		g.Players[0].Hand = []Card{{ID: "w4", Color: ColorWild, Rank: RankWildDrawFour}, {ID: "redheld", Color: ColorRed, Rank: RankOne}}
	} else {
		g.Players[0].Hand = []Card{{ID: "w4", Color: ColorWild, Rank: RankWildDrawFour}, {ID: "ok", Color: ColorGreen, Rank: RankOne}}
	}
	red := ColorRed
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: &red}); err != nil {
		t.Fatalf("p0 plays +4: %v", err)
	}
	if g.Phase != PhaseStack || g.CurrentIndex != 1 {
		t.Fatalf("setup: phase=%s current=%d, want stack/1", g.Phase, g.CurrentIndex)
	}
	return g
}

func TestStack_Challenge_Availability(t *testing.T) {
	g := combinedStackGame(t, false)
	if !g.stackChallengeAvailable() {
		t.Fatal("a lone +4 pile should be challengeable under the combined rule")
	}
	lm := g.LegalMoves("p1")
	if !lm.CanChallengeStack {
		t.Fatal("LegalMoves should expose CanChallengeStack for a +4 pile")
	}
	v := g.BuildViewFor("p1")
	if v.Stack == nil || !v.Stack.CanChallenge {
		t.Fatalf("view stack=%+v, want can_challenge=true", v.Stack)
	}
}

func TestStack_Challenge_NotAvailableAfterTwo(t *testing.T) {
	// Build a +2 heap (not a +4): no challenge should be offered.
	g := mkGame(
		[][]Card{
			{tcard("d2", ColorRed, RankDrawTwo), tcard("a", ColorRed, RankOne)},
			{tcard("b", ColorGreen, RankTwo)},
			{tcard("c", ColorYellow, RankThree)},
		},
		[]Card{tcard("q1", ColorGreen, RankOne), tcard("q2", ColorYellow, RankTwo)},
		tcard("top", ColorRed, RankFive), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.HouseRules.StackDrawTwo = true
	g.Config.HouseRules.StackDrawFour = true
	g.Config.Cards.ChallengeDrawFour = true
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d2"}); err != nil {
		t.Fatalf("p0 +2: %v", err)
	}
	if g.stackChallengeAvailable() {
		t.Fatalf("+2 heap must not offer a +4 challenge")
	}
	if g.LegalMoves("p1").CanChallengeStack {
		t.Fatal("LegalMoves must not offer challenge on a +2 heap")
	}
	// Also confirm the challenge action itself is rejected.
	if _, err := g.Apply("p1", Action{Type: ActionChallengeStack}); err == nil {
		t.Fatal("challenge_stack on a +2 heap should be rejected")
	}
}

func TestStack_Challenge_CleanDrawsWholePile(t *testing.T) {
	g := combinedStackGame(t, false) // resolver clean (no red held)
	res, err := g.Apply("p1", Action{Type: ActionChallengeStack})
	if err != nil {
		t.Fatalf("challenge clean +4: %v", err)
	}
	// Clean: challenger draws the whole pile (4) and is skipped.
	if len(g.Players[1].Hand) != 1+4 {
		t.Fatalf("p1 hand = %d, want 5 (1 + 4 drawn)", len(g.Players[1].Hand))
	}
	if g.Phase != PhaseTurn || g.CurrentIndex != 2 {
		t.Fatalf("phase=%s current=%d, want turn/2 (p1 skipped)", g.Phase, g.CurrentIndex)
	}
	if g.lastChallengeResult == nil || g.lastChallengeResult.Succeeded {
		t.Fatalf("clean challenge result = %+v, want failed", g.lastChallengeResult)
	}
	if _, ok := res.eventsContain(EventChallengeFailed); !ok {
		t.Fatalf("expected CHALLENGE_FAILED event, got %+v", res.Events)
	}
	_ = res
}

func TestStack_Challenge_ViolatedRollsBackFour(t *testing.T) {
	g := combinedStackGame(t, true) // resolver p0 held red (violated)
	beforeResolver := len(g.Players[0].Hand)
	res, err := g.Apply("p1", Action{Type: ActionChallengeStack})
	if err != nil {
		t.Fatalf("challenge violated +4: %v", err)
	}
	// Resolver drew 4.
	if len(g.Players[0].Hand) != beforeResolver+4 {
		t.Fatalf("p0 hand = %d, want %d (4 drawn)", len(g.Players[0].Hand), beforeResolver+4)
	}
	// Pile emptied (was only the +4) -> challenger continues on a clean board.
	if g.Phase != PhaseTurn || g.CurrentIndex != 1 {
		t.Fatalf("phase=%s current=%d, want turn/1 (challenger continues)", g.Phase, g.CurrentIndex)
	}
	if g.stackCredits != 0 || len(g.stackPile) != 0 {
		t.Fatalf("pile not cleared: credits=%d pile=%d", g.stackCredits, len(g.stackPile))
	}
	if g.lastChallengeResult == nil || !g.lastChallengeResult.Succeeded {
		t.Fatalf("violated challenge result = %+v, want succeeded", g.lastChallengeResult)
	}
	if _, ok := res.eventsContain(EventChallengeSucceeded); !ok {
		t.Fatalf("expected CHALLENGE_SUCCEEDED event, got %+v", res.Events)
	}
}

func TestStack_Challenge_ViolatedMidPileReduces(t *testing.T) {
	// p0 +2, then p1 +4 (violated), total 6. p2 challenges p1's +4.
	g := mkGame(
		[][]Card{
			{tcard("d2", ColorRed, RankDrawTwo), tcard("a0", ColorRed, RankOne)},
			{tcard("w4", ColorWild, RankWildDrawFour), tcard("held", ColorRed, RankOne)}, // p1 violates
			{tcard("c", ColorYellow, RankThree), tcard("c2", ColorYellow, RankFour)},
			{tcard("d", ColorBlue, RankFive), tcard("d2h", ColorBlue, RankSix)},
		},
		[]Card{tcard("q1", ColorGreen, RankOne), tcard("q2", ColorYellow, RankTwo),
			tcard("q3", ColorRed, RankThree), tcard("q4", ColorBlue, RankFour),
			tcard("q5", ColorGreen, RankFive), tcard("q6", ColorRed, RankSix)},
		tcard("top", ColorRed, RankFive), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.HouseRules.MixedStacking = true
	g.Config.Cards.ChallengeDrawFour = true
	red := ColorRed
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d2"}); err != nil {
		t.Fatalf("p0 +2: %v", err)
	}
	if _, err := g.Apply("p1", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: &red}); err != nil {
		t.Fatalf("p1 +4: %v", err)
	}
	if g.stackCredits != 6 {
		t.Fatalf("credits after +4 = %d, want 6", g.stackCredits)
	}
	// p1's +4 is the head and violated.
	if g.CurrentIndex != 2 {
		t.Fatalf("current = %d, want 2 (p2 at bat)", g.CurrentIndex)
	}
	// Suppress the resolver's winning/deferred path? p1 keeps 1 card -> fine.
	if _, err := g.Apply("p2", Action{Type: ActionChallengeStack}); err != nil {
		t.Fatalf("p2 challenges p1's +4: %v", err)
	}
	// The +4 is rolled back: credits 6-4=2 (only p0's +2 remains). p1 draws 4.
	if g.stackCredits != 2 || len(g.stackPile) != 1 {
		t.Fatalf("after rollback credits=%d pile=%d, want 2/1", g.stackCredits, len(g.stackPile))
	}
	// Challenger p2 stays at bat in PhaseStack facing 2 (a +2, not challengeable).
	if g.Phase != PhaseStack || g.CurrentIndex != 2 {
		t.Fatalf("phase=%s current=%d, want stack/2", g.Phase, g.CurrentIndex)
	}
	if g.stackChallengeAvailable() {
		t.Fatal("remaining +2 pile must not be challengeable")
	}
}

// TestStack_Challenge_OnlyInitialFour_Latched verifies that with two +4s in the
// pile the challengeable resolver is the FIRST (+4) one, never the most recent.
func TestStack_Challenge_OnlyInitialFour_Latched(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("w4a", ColorWild, RankWildDrawFour), tcard("held", ColorRed, RankOne)}, // p0 initial +4 (violated: holds red)
			{tcard("w4b", ColorWild, RankWildDrawFour), tcard("x", ColorGreen, RankTwo)},  // p1 later +4
			{tcard("c", ColorYellow, RankThree)},
			{tcard("d", ColorBlue, RankFive)},
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
	if g.stackCredits != 8 {
		t.Fatalf("credits = %d, want 8", g.stackCredits)
	}
	if g.CurrentIndex != 2 {
		t.Fatalf("current = %d, want 2", g.CurrentIndex)
	}
	// The initial +4 (p0), not the latest (p1), is the challengeable resolver.
	if g.stackResolverID != "p0" {
		t.Fatalf("stackResolverID = %q, want p0 (initial +4)", g.stackResolverID)
	}
	if !g.stackChallengeAvailable() {
		t.Fatal("double +4 pile should be challengeable (initial +4 pending)")
	}
}

// TestStack_Challenge_InitialViolated_RemainingFourNotChallengeable: after the
// initial +4 (violated) is rolled back, a later +4 still in the pile must NOT
// offer another challenge (only the initial one can be challenged, once).
func TestStack_Challenge_InitialViolated_RemainingFourNotChallengeable(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("w4a", ColorWild, RankWildDrawFour), tcard("held", ColorRed, RankOne)}, // p0 initial +4 (violated)
			{tcard("w4b", ColorWild, RankWildDrawFour), tcard("x", ColorGreen, RankTwo)},  // p1 later +4 (clean)
			{tcard("c", ColorYellow, RankThree)},
			{tcard("d", ColorBlue, RankFive)},
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
	beforeResolver := len(g.Players[0].Hand)
	if _, err := g.Apply("p2", Action{Type: ActionChallengeStack}); err != nil {
		t.Fatalf("p2 challenges initial +4: %v", err)
	}
	// p0 (initial, violated) drew 4; pile keeps only p1's +4 (credits 4).
	if len(g.Players[0].Hand) != beforeResolver+4 {
		t.Fatalf("p0 hand = %d, want %d", len(g.Players[0].Hand), beforeResolver+4)
	}
	if g.stackCredits != 4 || len(g.stackPile) != 1 {
		t.Fatalf("credits=%d pile=%d, want 4/1", g.stackCredits, len(g.stackPile))
	}
	if g.stackPile[0].playerID != "p1" {
		t.Fatalf("remaining pile resolver = %q, want p1", g.stackPile[0].playerID)
	}
	// p2 continues in PhaseStack facing the remaining +4, but NO second challenge.
	if g.Phase != PhaseStack || g.CurrentIndex != 2 {
		t.Fatalf("phase=%s current=%d, want stack/2", g.Phase, g.CurrentIndex)
	}
	if g.stackChallengeAvailable() {
		t.Fatal("remaining +4 must NOT be challengeable: only the initial +4 gets one challenge")
	}
	if g.LegalMoves("p2").CanChallengeStack {
		t.Fatal("LegalMoves must not expose challenge on the remaining +4")
	}
}

// TestStack_Challenge_InitialUnclearWithLaterFourOnTop: even when the top card is
// a +2 (not +4), the pending initial +4 keeps the challenge available.
func TestStack_Challenge_InitialUnclearWithLaterFourOnTop(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("d2a", ColorRed, RankDrawTwo), tcard("a", ColorRed, RankOne)},   // p0 +2
			{tcard("w4", ColorWild, RankWildDrawFour), tcard("held", ColorRed, RankOne)}, // p1 +4 (violated)
			{tcard("d2b", ColorRed, RankDrawTwo), tcard("c", ColorYellow, RankThree)},    // p2 +2 on top
			{tcard("d", ColorBlue, RankFive), tcard("d3", ColorBlue, RankSix)},
		},
		[]Card{tcard("q1", ColorGreen, RankOne), tcard("q2", ColorYellow, RankTwo),
			tcard("q3", ColorRed, RankThree), tcard("q4", ColorBlue, RankFour),
			tcard("q5", ColorGreen, RankFive), tcard("q6", ColorRed, RankSix),
			tcard("q7", ColorGreen, RankSeven), tcard("q8", ColorRed, RankEight)},
		tcard("top", ColorRed, RankFive), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.HouseRules.MixedStacking = true
	g.Config.Cards.ChallengeDrawFour = true
	red := ColorRed
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d2a"}); err != nil {
		t.Fatalf("p0 +2: %v", err)
	}
	if _, err := g.Apply("p1", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: &red}); err != nil {
		t.Fatalf("p1 +4: %v", err)
	}
	if _, err := g.Apply("p2", Action{Type: ActionPlayCard, CardID: "d2b"}); err != nil {
		t.Fatalf("p2 +2: %v", err)
	}
	if g.stackRank != RankDrawTwo {
		t.Fatalf("stackRank = %q, want draw_two (top card)", g.stackRank)
	}
	// Top is +2, yet the initial +4 (p1) is still pending and challengeable.
	if g.stackResolverID != "p1" {
		t.Fatalf("stackResolverID = %q, want p1", g.stackResolverID)
	}
	if !g.stackChallengeAvailable() {
		t.Fatal("initial +4 must remain challengeable even with a +2 on top")
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
