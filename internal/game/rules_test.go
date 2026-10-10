package game

import (
	"math/rand"
	"reflect"
	"testing"
)

// --- test helpers ---

func tcard(id string, col Color, rank Rank) Card {
	return Card{ID: id, Color: col, Rank: rank}
}

func colp(c Color) *Color { return &c }

// mkGame builds a Game with fully controlled state for rule tests.
func mkGame(hands [][]Card, drawPile []Card, top Card, active Color, dir, current int, phase Phase) *Game {
	players := make([]Player, len(hands))
	for i, h := range hands {
		players[i] = Player{
			ID:        playerID(i),
			Name:      "P" + string(rune('0'+i)),
			Hand:      append([]Card{}, h...),
			Connected: true,
		}
	}
	return &Game{
		Players:      players,
		DrawPile:     append([]Card{}, drawPile...),
		DiscardPile:  []Card{top},
		CurrentIndex: current,
		Direction:    dir,
		ActiveColor:  active,
		Phase:        phase,
		Config:       DefaultConfig(),
		rng:          rand.NewSource(1),
	}
}

func setEq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]bool{}
	for _, x := range a {
		m[x] = true
	}
	for _, x := range b {
		if !m[x] {
			return false
		}
	}
	return true
}

func expectCode(t *testing.T, err error, want ErrorCode) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %s, got nil", want)
	}
	ge, ok := err.(*GameError)
	if !ok {
		t.Fatalf("expected *GameError, got %T: %v", err, err)
	}
	if ge.Code != want {
		t.Fatalf("error code = %s, want %s", ge.Code, want)
	}
}

// --- Task 2: matching ---

func TestLegalMoves_RedTwoTop(t *testing.T) {
	// Top red 2 -> red1/red3/blue2 playable, blue3 not (readme §4.1 correction).
	g := mkGame(
		[][]Card{
			{tcard("a", ColorRed, RankOne), tcard("b", ColorRed, RankThree), tcard("c", ColorBlue, RankTwo), tcard("d", ColorBlue, RankThree)},
		},
		nil,
		tcard("top", ColorRed, RankTwo),
		ColorRed, 1, 0, PhaseTurn,
	)
	lm := g.LegalMoves("p0")
	want := []string{"a", "b", "c"}
	if !setEq(lm.PlayableIDs, want) {
		t.Fatalf("legal = %v, want %v", lm.PlayableIDs, want)
	}
	if !lm.CanDraw {
		t.Fatal("expected CanDraw on own turn")
	}
}

func TestLegalMoves_NotCurrentPlayer(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("a", ColorRed, RankOne)}, {tcard("b", ColorBlue, RankTwo)}},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	lm := g.LegalMoves("p1")
	if len(lm.PlayableIDs) != 0 || lm.CanDraw {
		t.Fatalf("non-current player got legal moves %+v", lm)
	}
}

func TestPlay_NonCurrentPlayer_NotYourTurn(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("a", ColorRed, RankOne)}, {tcard("b", ColorBlue, RankTwo)}},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	before := *g
	_, err := g.Apply("p1", Action{Type: ActionPlayCard, CardID: "b"})
	expectCode(t, err, ErrCodeNotYourTurn)
	if !reflect.DeepEqual(stateOf(g), stateOf(&before)) {
		t.Fatal("state changed on NOT_YOUR_TURN")
	}
}

func stateOf(g *Game) Game {
	cp := *g
	cp.Players = append([]Player{}, g.Players...)
	return cp
}

func TestPlay_CardNotOwned(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("a", ColorRed, RankOne)}},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	_, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "ghost"})
	expectCode(t, err, ErrCodeCardNotOwned)
}

func TestPlay_CardNotPlayable(t *testing.T) {
	// blue3 is not playable on red 2 (per the corrected rule).
	g := mkGame(
		[][]Card{{tcard("d", ColorBlue, RankThree)}},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	_, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d"})
	expectCode(t, err, ErrCodeCardNotPlayable)
}

func TestPlay_WildRequiresColor(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("w", ColorWild, RankWild)}},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	// Missing color -> error.
	_, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w"})
	expectCode(t, err, ErrCodeInvalidColor)
	// Invalid color -> error.
	_, err = g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w", ChosenColor: colp(ColorWild)})
	expectCode(t, err, ErrCodeInvalidColor)
	// Valid normal card with a color -> error.
	g2 := mkGame(
		[][]Card{{tcard("a", ColorRed, RankOne)}},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	_, err = g2.Apply("p0", Action{Type: ActionPlayCard, CardID: "a", ChosenColor: colp(ColorBlue)})
	expectCode(t, err, ErrCodeInvalidColor)
}

// --- Task 2: Wild Draw Four restriction ---

func TestPlay_WildDrawFour_InvalidWhenHoldingActiveColor(t *testing.T) {
	// Strict +4 (challenges disabled): active red, player holds a red card ->
	// +4 not allowed.
	g := mkGame(
		[][]Card{{tcard("w4", ColorWild, RankWildDrawFour), tcard("red", ColorRed, RankFive)}},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.Cards.ChallengeDrawFour = false
	if lm := g.LegalMoves("p0"); len(lm.PlayableIDs) != 1 || lm.PlayableIDs[0] != "red" {
		t.Fatalf("only the red card should be playable, got %v", lm.PlayableIDs)
	}
	_, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: colp(ColorBlue)})
	expectCode(t, err, ErrCodeWildDrawFourInvalid)
}

func TestPlay_WildDrawFour_ValidWithSameRankOtherColor(t *testing.T) {
	// active red, player holds blue 5 (same rank, different color) - still valid +4.
	g := mkGame(
		[][]Card{{tcard("w4", ColorWild, RankWildDrawFour), tcard("b5", ColorBlue, RankFive)}},
		[]Card{},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	res, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: colp(ColorBlue)})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if g.ActiveColor != ColorBlue {
		t.Fatalf("ActiveColor = %s, want blue", g.ActiveColor)
	}
	_ = res
}

// --- Task 2: turn effects ---

func TestPlay_Number_AdvancesTurn(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("a", ColorRed, RankOne), tcard("e", ColorRed, RankZero)}, {tcard("b", ColorBlue, RankTwo)}},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	res, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "a"})
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if res.NextPlayerID != "p1" || g.CurrentIndex != 1 {
		t.Fatalf("next = %d (%s), want p1", g.CurrentIndex, res.NextPlayerID)
	}
	if g.Phase != PhaseTurn {
		t.Fatalf("phase = %s, want turn", g.Phase)
	}
}

func TestPlay_Skip_ThreePlayers(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("sk", ColorRed, RankSkip), tcard("a", ColorRed, RankOne)},
			{tcard("b", ColorBlue, RankTwo)},
			{tcard("c", ColorGreen, RankTwo)},
		},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	_, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "sk"})
	if err != nil {
		t.Fatalf("play skip: %v", err)
	}
	// p1 skipped, p2 goes next.
	if g.CurrentIndex != 2 {
		t.Fatalf("current = %d, want 2 (p1 skipped)", g.CurrentIndex)
	}
}

func TestPlay_Reverse_TwoPlayers(t *testing.T) {
	// 2-player reverse == skip: the same player continues.
	g := mkGame(
		[][]Card{
			{tcard("rv", ColorRed, RankReverse), tcard("a", ColorRed, RankOne)},
			{tcard("b", ColorBlue, RankTwo)},
		},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	res, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "rv"})
	if err != nil {
		t.Fatalf("play reverse: %v", err)
	}
	if res.NextPlayerID != "p0" {
		t.Fatalf("2-player reverse should keep same player, got %s", res.NextPlayerID)
	}
	if g.CurrentIndex != 0 {
		t.Fatalf("current = %d, want 0", g.CurrentIndex)
	}
}

func TestPlay_Reverse_ThreePlayers(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("rv", ColorRed, RankReverse), tcard("a", ColorRed, RankOne)},
			{tcard("b", ColorBlue, RankTwo)},
			{tcard("c", ColorGreen, RankTwo)},
		},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	_, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "rv"})
	if err != nil {
		t.Fatalf("play reverse: %v", err)
	}
	// Direction flips; next is p2 (the last player before p0 in new direction).
	if g.Direction != -1 {
		t.Fatalf("direction = %d, want -1", g.Direction)
	}
	if g.CurrentIndex != 2 {
		t.Fatalf("current = %d, want 2", g.CurrentIndex)
	}
}

func TestPlay_DrawTwo_TargetDrawsAndSkips(t *testing.T) {
	// p0 plays +2; p1 draws 2 and is skipped; p2 goes next.
	g := mkGame(
		[][]Card{{tcard("d2", ColorRed, RankDrawTwo), tcard("a", ColorRed, RankOne)}},
		[]Card{tcard("x1", ColorBlue, RankOne), tcard("x2", ColorGreen, RankTwo), tcard("x3", ColorYellow, RankThree)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
		// build 3 players
	)
	// rebuild with 3 players
	g = mkGame(
		[][]Card{{tcard("d2", ColorRed, RankDrawTwo), tcard("a", ColorRed, RankOne)}, {tcard("b", ColorBlue, RankTwo)}, {tcard("c", ColorGreen, RankTwo)}},
		[]Card{tcard("x1", ColorBlue, RankOne), tcard("x2", ColorGreen, RankTwo), tcard("x3", ColorYellow, RankThree)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	before := len(g.Players[1].Hand)
	res, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d2"})
	if err != nil {
		t.Fatalf("play +2: %v", err)
	}
	if len(g.Players[1].Hand) != before+2 {
		t.Fatalf("p1 hand grew by %d, want 2", len(g.Players[1].Hand)-before)
	}
	if g.CurrentIndex != 2 {
		t.Fatalf("current = %d, want 2 (p1 skipped)", g.CurrentIndex)
	}
	_ = res
}

func TestPlay_WildDrawFour_TargetDrawsFour(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("w4", ColorWild, RankWildDrawFour)}},
		[]Card{tcard("x1", ColorBlue, RankOne), tcard("x2", ColorGreen, RankTwo), tcard("x3", ColorYellow, RankThree), tcard("x4", ColorRed, RankFour), tcard("x5", ColorBlue, RankFive)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	// need 2 players for the single result check; use 3 to assert skip logic.
	_ = g
	g = mkGame(
		[][]Card{{tcard("w4", ColorWild, RankWildDrawFour), tcard("b5", ColorBlue, RankFive)}, {tcard("b", ColorBlue, RankTwo)}, {tcard("c", ColorGreen, RankTwo)}},
		[]Card{tcard("x1", ColorBlue, RankOne), tcard("x2", ColorGreen, RankTwo), tcard("x3", ColorYellow, RankThree), tcard("x4", ColorRed, RankFour), tcard("x5", ColorBlue, RankFive)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	// Challenges disabled -> +4 applies its penalty immediately (no challenge).
	g.Config.Cards.ChallengeDrawFour = false
	before := len(g.Players[1].Hand)
	_, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: colp(ColorGreen)})
	if err != nil {
		t.Fatalf("play +4: %v", err)
	}
	if len(g.Players[1].Hand) != before+4 {
		t.Fatalf("p1 hand grew by %d, want 4", len(g.Players[1].Hand)-before)
	}
	if g.ActiveColor != ColorGreen {
		t.Fatalf("active = %s, want green", g.ActiveColor)
	}
	if g.CurrentIndex != 2 {
		t.Fatalf("current = %d, want 2", g.CurrentIndex)
	}
}

// --- Task 2: draw pile recycling ---

func TestDraw_RecyclesDiscard(t *testing.T) {
	// Draw pile has 1 card, discard has many -> drawing recycles discard (minus top).
	g := mkGame(
		[][]Card{{tcard("a", ColorRed, RankOne)}},
		[]Card{tcard("only", ColorBlue, RankNine)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	// discard pile: top + more cards to recycle.
	g.DiscardPile = []Card{
		tcard("c1", ColorYellow, RankOne),
		tcard("c2", ColorGreen, RankTwo),
		tcard("top", ColorRed, RankTwo),
	}
	res, err := g.Apply("p0", Action{Type: ActionDrawCard})
	if err != nil {
		t.Fatalf("draw: %v", err)
	}
	// Player drew the one card from the pile, which recycled the discard.
	if len(g.Players[0].Hand) != 2 {
		t.Fatalf("hand = %d, want 2 (drew 'only')", len(g.Players[0].Hand))
	}
	_ = res
}

// --- Task 2: win & UNO ---

func TestPlay_LastCard_Wins(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("a", ColorRed, RankOne)}},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	res, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "a"})
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if !res.Ended || g.Phase != PhaseFinished || g.WinnerPlayerID != "p0" {
		t.Fatalf("game not ended correctly: %+v phase=%s winner=%s", res, g.Phase, g.WinnerPlayerID)
	}
	// No UNO event on a win.
	for _, e := range res.Events {
		if e.Type == EventUNOCalled {
			t.Fatal("UNO event fired on winning play")
		}
	}
}

func TestAutoUNO_CalledOnce(t *testing.T) {
	// p0 plays down to exactly one card -> exactly one UNO_CALLED event.
	g := mkGame(
		[][]Card{{tcard("a", ColorRed, RankOne), tcard("b", ColorRed, RankThree)}},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	res, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "a"})
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if !g.Players[0].HasUNO {
		t.Fatal("player should have UNO flag set")
	}
	n := 0
	for _, e := range res.Events {
		if e.Type == EventUNOCalled {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("UNO_CALLED count = %d, want 1", n)
	}
	// Second play plays the last card -> win, UNO cleared, no new UNO event.
	res2, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "b"})
	if err != nil {
		t.Fatalf("second play: %v", err)
	}
	if !res2.Ended || g.WinnerPlayerID != "p0" {
		t.Fatalf("second play should be a win for p0, ended=%v winner=%s", res2.Ended, g.WinnerPlayerID)
	}
	if g.Players[0].HasUNO {
		t.Fatal("UNO flag should be cleared after winning")
	}
	for _, e := range res2.Events {
		if e.Type == EventUNOCalled {
			t.Fatal("UNO event should not re-fire on the winning play")
		}
	}
}

// --- Task 2: turn / drawn-decision transitions ---

func TestDraw_IllegalAutoAdvances(t *testing.T) {
	// Drawn card (blue7) not playable on red 2 -> auto advance, no drawn_decision.
	g := mkGame(
		[][]Card{{tcard("a", ColorRed, RankOne)}, {tcard("b", ColorBlue, RankTwo)}},
		[]Card{tcard("b7", ColorBlue, RankSeven)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	res, err := g.Apply("p0", Action{Type: ActionDrawCard})
	if err != nil {
		t.Fatalf("draw: %v", err)
	}
	if g.Phase != PhaseTurn {
		t.Fatalf("phase = %s, want turn", g.Phase)
	}
	if g.CurrentIndex != 1 {
		t.Fatalf("current = %d, want 1 (auto advance)", g.CurrentIndex)
	}
	if res.NextPlayerID != "p1" {
		t.Fatalf("next = %s, want p1", res.NextPlayerID)
	}
	if len(g.Players[0].Hand) != 2 {
		t.Fatalf("hand = %d, want 2", len(g.Players[0].Hand))
	}
}

func TestDraw_LegalEntersDrawnDecision(t *testing.T) {
	// Drawn card (red3) playable -> drawn_decision; only that card may be played.
	g := mkGame(
		[][]Card{{tcard("a", ColorRed, RankOne)}, {tcard("b", ColorBlue, RankTwo)}},
		[]Card{tcard("r3", ColorRed, RankThree)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	res, err := g.Apply("p0", Action{Type: ActionDrawCard})
	if err != nil {
		t.Fatalf("draw: %v", err)
	}
	if g.Phase != PhaseDrawnDecision {
		t.Fatalf("phase = %s, want drawn_decision", g.Phase)
	}
	if g.DrawnCardID != "r3" {
		t.Fatalf("drawn id = %s, want r3", g.DrawnCardID)
	}
	if res.NextPlayerID != "p0" {
		t.Fatalf("next = %s, want p0", res.NextPlayerID)
	}
	// LegalMoves only exposes the drawn card.
	lm := g.LegalMoves("p0")
	if !setEq(lm.PlayableIDs, []string{"r3"}) {
		t.Fatalf("legal = %v, want [r3]", lm.PlayableIDs)
	}
	// Cannot play an original hand card in this phase.
	_, err = g.Apply("p0", Action{Type: ActionPlayCard, CardID: "a"})
	expectCode(t, err, ErrCodeInvalidPhase)

	// pass advances
	res2, err := g.Apply("p0", Action{Type: ActionPassDrawn})
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	if g.Phase != PhaseTurn || g.CurrentIndex != 1 {
		t.Fatalf("after pass: phase=%s current=%d", g.Phase, g.CurrentIndex)
	}
	_ = res2
	if len(g.Players[0].Hand) != 2 {
		t.Fatalf("pass should keep drawn card, hand = %d", len(g.Players[0].Hand))
	}
}

func TestPlayDrawn_Card(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("a", ColorRed, RankOne)}, {tcard("b", ColorBlue, RankTwo)}},
		[]Card{tcard("r3", ColorRed, RankThree)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	g.Apply("p0", Action{Type: ActionDrawCard})
	res, err := g.Apply("p0", Action{Type: ActionPlayDrawn}) // no card id needed
	if err != nil {
		t.Fatalf("play drawn: %v", err)
	}
	if g.Phase != PhaseTurn || g.CurrentIndex != 1 {
		t.Fatalf("after play drawn: phase=%s current=%d", g.Phase, g.CurrentIndex)
	}
	if len(g.Players[0].Hand) != 1 {
		t.Fatalf("hand = %d, want 1", len(g.Players[0].Hand))
	}
	_ = res
}

func TestApply_FinishedRejectsAll(t *testing.T) {
	g := mkGame([][]Card{{tcard("a", ColorRed, RankOne)}}, nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseFinished)
	g.WinnerPlayerID = "p0"
	_, err := g.Apply("p0", Action{Type: ActionDrawCard})
	expectCode(t, err, ErrCodeGameFinished)
}

// --- +4 challenge (rule.md R13) ---

func TestChallenge_AcceptDrawsFourAndSkips(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("w4", ColorWild, RankWildDrawFour), tcard("b5", ColorBlue, RankFive)},
			{tcard("b", ColorBlue, RankTwo)},
			{tcard("c", ColorGreen, RankTwo)}},
		[]Card{tcard("x1", ColorBlue, RankOne), tcard("x2", ColorGreen, RankTwo), tcard("x3", ColorYellow, RankThree), tcard("x4", ColorRed, RankFour)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	// default: challenge on
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: colp(ColorGreen)}); err != nil {
		t.Fatalf("play +4: %v", err)
	}
	if g.Phase != PhaseChallenge || g.CurrentIndex != 1 {
		t.Fatalf("expected challenge phase on p1, got phase=%s idx=%d", g.Phase, g.CurrentIndex)
	}
	before := len(g.Players[1].Hand)
	res, err := g.Apply("p1", Action{Type: ActionAcceptFour})
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if len(g.Players[1].Hand) != before+4 {
		t.Fatalf("p1 hand grew by %d, want 4", len(g.Players[1].Hand)-before)
	}
	if g.Phase != PhaseTurn || g.CurrentIndex != 2 {
		t.Fatalf("after accept phase=%s idx=%d, want turn on p2", g.Phase, g.CurrentIndex)
	}
	_ = res
}

func TestChallenge_LastCardFourDefersWinUntilAccept(t *testing.T) {
	// Resolver plays +4 as their final card: win is deferred until the challenge
	// settles (rule.md R27 / §16). Accepting still grants the resolver the win.
	g := mkGame(
		[][]Card{{tcard("w4", ColorWild, RankWildDrawFour)},
			{tcard("b", ColorBlue, RankTwo)}},
		[]Card{tcard("x1", ColorBlue, RankOne), tcard("x2", ColorGreen, RankTwo), tcard("x3", ColorYellow, RankThree), tcard("x4", ColorRed, RankFour)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: colp(ColorGreen)}); err != nil {
		t.Fatalf("play +4: %v", err)
	}
	if g.Phase != PhaseChallenge {
		t.Fatalf("expected challenge phase even though resolver emptied hand, got %s", g.Phase)
	}
	res, err := g.Apply("p1", Action{Type: ActionAcceptFour})
	if err != nil {
		t.Fatalf("accept: %v", err)
	}
	if !res.Ended || res.WinnerID != "p0" {
		t.Fatalf("resolver should win after accepted +4, ended=%v winner=%q", res.Ended, res.WinnerID)
	}
	if g.Phase != PhaseFinished {
		t.Fatalf("phase = %s, want finished", g.Phase)
	}
}

func TestChallenge_ViolationResolverDrawsFour(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("w4", ColorWild, RankWildDrawFour), tcard("r5", ColorRed, RankFive)}, // resolver held red (active)
			{tcard("b", ColorBlue, RankTwo)},
			{tcard("c", ColorGreen, RankTwo)}},
		[]Card{tcard("x1", ColorBlue, RankOne), tcard("x2", ColorGreen, RankTwo), tcard("x3", ColorYellow, RankThree), tcard("x4", ColorRed, RankFour)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: colp(ColorGreen)}); err != nil {
		t.Fatalf("play +4: %v", err)
	}
	beforeResolver := len(g.Players[0].Hand) // held r5 + the drawn +4? after discard = 1 (r5). plus draws.
	beforeChallenger := len(g.Players[1].Hand)
	res, err := g.Apply("p1", Action{Type: ActionChallengeFour})
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	if len(g.Players[0].Hand) != beforeResolver+4 {
		t.Fatalf("resolver hand grew by %d, want 4 (held active color)", len(g.Players[0].Hand)-beforeResolver)
	}
	if len(g.Players[1].Hand) != beforeChallenger {
		t.Fatalf("challenger hand should be unchanged, grow %d", len(g.Players[1].Hand)-beforeChallenger)
	}
	// Challenger continues (not skipped).
	if g.CurrentIndex != 1 || g.Phase != PhaseTurn {
		t.Fatalf("challenger should continue: phase=%s idx=%d", g.Phase, g.CurrentIndex)
	}
	_ = res
}

func TestChallenge_CleanTargetDrawsSixAndSkipped(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("w4", ColorWild, RankWildDrawFour), tcard("b5", ColorBlue, RankFive)}, // no red
			{tcard("b", ColorBlue, RankTwo)},
			{tcard("c", ColorGreen, RankTwo)}},
		[]Card{tcard("x1", ColorBlue, RankOne), tcard("x2", ColorGreen, RankTwo), tcard("x3", ColorYellow, RankThree), tcard("x4", ColorRed, RankFour), tcard("x5", ColorRed, RankFive), tcard("x6", ColorGreen, RankSix)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: colp(ColorGreen)}); err != nil {
		t.Fatalf("play +4: %v", err)
	}
	beforeChallenger := len(g.Players[1].Hand)
	_, err := g.Apply("p1", Action{Type: ActionChallengeFour})
	if err != nil {
		t.Fatalf("challenge: %v", err)
	}
	if len(g.Players[1].Hand) != beforeChallenger+6 {
		t.Fatalf("challenger hand grew by %d, want 6", len(g.Players[1].Hand)-beforeChallenger)
	}
	// Challenger skipped -> resolver (p0) is current again.
	if g.CurrentIndex != 2 {
		t.Fatalf("current idx = %d, want 2 (p1 skipped)", g.CurrentIndex)
	}
}

func TestChallenge_NonTargetCannotAct(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("w4", ColorWild, RankWildDrawFour), tcard("b5", ColorBlue, RankFive)},
			{tcard("b", ColorBlue, RankTwo)},
			{tcard("c", ColorGreen, RankTwo)}},
		[]Card{tcard("x1", ColorBlue, RankOne)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: colp(ColorGreen)}); err != nil {
		t.Fatalf("play +4: %v", err)
	}
	if _, err := g.Apply("p0", Action{Type: ActionChallengeFour}); err == nil {
		t.Fatalf("resolver must not be able to challenge")
	}
}
