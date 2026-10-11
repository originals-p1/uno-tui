package game

import "testing"

// TestReverse_FlipsDirectionAndAdvances3P verifies a reverse flips the
// direction and advances to the next player *in the new direction* (the
// previous seat in the old ordering).
func TestReverse_FlipsDirectionAndAdvances3P(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("rv", ColorRed, RankReverse), tcard("a", ColorRed, RankOne)},
			{tcard("b", ColorBlue, RankTwo)},
			{tcard("c", ColorGreen, RankTwo)},
		},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	res, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "rv"})
	if err != nil {
		t.Fatalf("play reverse: %v", err)
	}
	if g.Direction != -1 {
		t.Fatalf("direction = %d, want -1", g.Direction)
	}
	if g.CurrentIndex != 2 {
		t.Fatalf("current = %d, want 2", g.CurrentIndex)
	}
	// Ending at p2 means the previous player (in the old clockwise order) plays
	// next — i.e. play now flows counter-clockwise. Next player = p2's ID.
	if res.NextPlayerID != "p2" || g.currentPlayer().ID != "p2" {
		t.Fatalf("next = %s, want p2", res.NextPlayerID)
	}
}

// TestReverse_FromCounterClockwiseFlipsBack verifies a reverse played while
// going counter-clockwise flips back to clockwise.
func TestReverse_FromCounterClockwiseFlipsBack(t *testing.T) {
	// 4 players, current = p1, direction -1 (counter-clockwise).
	g := mkGame(
		[][]Card{
			{tcard("x", ColorBlue, RankTwo)},
			{tcard("rv", ColorRed, RankReverse), tcard("b1", ColorRed, RankOne)},
			{tcard("c", ColorGreen, RankTwo)},
			{tcard("d", ColorYellow, RankTwo)},
		},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, -1, 1, PhaseTurn,
	)
	_, err := g.Apply("p1", Action{Type: ActionPlayCard, CardID: "rv"})
	if err != nil {
		t.Fatalf("play reverse: %v", err)
	}
	if g.Direction != 1 {
		t.Fatalf("direction = %d, want +1 (flipped back to clockwise)", g.Direction)
	}
	// Old counter-clockwise next from p1 was p0; reversed, next is p2.
	if g.CurrentIndex != 2 {
		t.Fatalf("current = %d, want 2", g.CurrentIndex)
	}
}

// TestReverse_DoubleReverseRestoresOriginal verifies two reverses in a row by
// consecutive players restore the original direction.
func TestReverse_DoubleReverseRestoresOriginal(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("rv1", ColorRed, RankReverse), tcard("a", ColorRed, RankOne)},
			{tcard("rv2", ColorRed, RankReverse), tcard("b", ColorBlue, RankOne)},
			{tcard("c", ColorRed, RankSeven), tcard("sp", ColorGreen, RankNine)},
		},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	_, _ = g.Apply("p0", Action{Type: ActionPlayCard, CardID: "rv1"})
	if g.Direction != -1 || g.CurrentIndex != 2 {
		t.Fatalf("after 1st reverse dir=%d cur=%d, want -1/2", g.Direction, g.CurrentIndex)
	}
	// p2 plays reverse (p1 had none matching? p1 has a blue reverse — but p1 is
	// not next; next is p2 per counter-clockwise). p2 plays its only card first.
	_, err := g.Apply("p2", Action{Type: ActionPlayCard, CardID: "c"})
	if err != nil {
		t.Fatalf("p2 number: %v", err)
	}
	// Now counter-clockwise continues: p1 is next. p1 plays reverse.
	if g.CurrentIndex != 1 {
		t.Fatalf("current = %d, want 1 before p1 reverse", g.CurrentIndex)
	}
	_, err = g.Apply("p1", Action{Type: ActionPlayCard, CardID: "rv2"})
	if err != nil {
		t.Fatalf("p1 2nd reverse: %v", err)
	}
	if g.Direction != 1 {
		t.Fatalf("direction = %d, want +1 restored", g.Direction)
	}
	// After double reverse from p1, next in restored clockwise direction is p2.
	if g.CurrentIndex != 2 {
		t.Fatalf("current = %d, want 2", g.CurrentIndex)
	}
}

// TestReverse_AtSeatBoundary verifies a reverse at index 0 while going
// counter-clockwise lands correctly (wrap-around of the flipped direction).
func TestReverse_AtSeatBoundary(t *testing.T) {
	// 4 players, current = p0, direction -1.
	g := mkGame(
		[][]Card{
			{tcard("rv", ColorRed, RankReverse), tcard("a", ColorRed, RankOne)},
			{tcard("b", ColorBlue, RankTwo)},
			{tcard("c", ColorGreen, RankTwo)},
			{tcard("d", ColorYellow, RankTwo)},
		},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, -1, 0, PhaseTurn,
	)
	_, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "rv"})
	if err != nil {
		t.Fatalf("play reverse: %v", err)
	}
	if g.Direction != 1 {
		t.Fatalf("direction = %d, want +1", g.Direction)
	}
	if g.CurrentIndex != 1 {
		t.Fatalf("current = %d, want 1", g.CurrentIndex)
	}
}

// TestReverse_WinningLastCard_3P verifies a reverse used as the final card is a
// valid winning move that ends the round immediately (no crash from the
// direction flip on an empty next-index computation).
func TestReverse_WinningLastCard_3P(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("rv", ColorRed, RankReverse)}, // only a reverse
			{tcard("b", ColorBlue, RankTwo)},
			{tcard("c", ColorGreen, RankTwo)},
		},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	res, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "rv"})
	if err != nil {
		t.Fatalf("winning reverse: %v", err)
	}
	if !res.Ended || res.WinnerID != "p0" {
		t.Fatalf("ended=%v winner=%s, want true/p0", res.Ended, res.WinnerID)
	}
	if g.Phase != PhaseFinished || g.WinnerPlayerID != "p0" {
		t.Fatalf("phase=%s winner=%s, want finished/p0", g.Phase, g.WinnerPlayerID)
	}
}

// TestReverse_WinningLastCard_2P verifies the 2-player reverse-as-skip still
// ends the round correctly when it is the last card.
func TestReverse_WinningLastCard_2P(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("rv", ColorRed, RankReverse)},
			{tcard("b", ColorBlue, RankTwo)},
		},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	res, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "rv"})
	if err != nil {
		t.Fatalf("winning reverse (2P): %v", err)
	}
	if !res.Ended || res.WinnerID != "p0" {
		t.Fatalf("ended=%v winner=%s, want true/p0", res.Ended, res.WinnerID)
	}
}

// TestReverse_DisabledActsAsNumber verifies when the reverse card is disabled in
// config it behaves as an ordinary matching card (just advances the turn,
// direction unchanged).
func TestReverse_DisabledActsAsNumber(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("rv", ColorRed, RankReverse), tcard("a", ColorRed, RankOne)},
			{tcard("b", ColorBlue, RankTwo)},
			{tcard("c", ColorGreen, RankTwo)},
		},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.Cards.EnableReverse = false
	_, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "rv"})
	if err != nil {
		t.Fatalf("play disabled reverse: %v", err)
	}
	if g.Direction != 1 {
		t.Fatalf("direction = %d, want +1 (unchanged)", g.Direction)
	}
	if g.CurrentIndex != 1 {
		t.Fatalf("current = %d, want 1 (normal advance)", g.CurrentIndex)
	}
}

// TestView_ReverseReflectsInBuildView verifies the view exposes the flipped
// direction after a reverse so the TUI can render the correct turn order.
func TestView_ReverseReflectsInBuildView(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("rv", ColorRed, RankReverse), tcard("a", ColorRed, RankOne)},
			{tcard("b", ColorBlue, RankTwo)},
			{tcard("c", ColorGreen, RankTwo)},
		},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "rv"}); err != nil {
		t.Fatalf("play reverse: %v", err)
	}
	v := g.BuildViewFor("p2")
	if v.Direction != -1 {
		t.Fatalf("view direction = %d, want -1", v.Direction)
	}
}
