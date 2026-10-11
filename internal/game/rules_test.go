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

// TestChallenge_TriggersColorAndResultBanner verifies the challenge view exposes
// the active color at the moment the +4 was played (not the chosen one), and
// that the resolve sets a transient result banner which the next action clears.
func TestChallenge_TriggersColorAndResultBanner(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("w4", ColorWild, RankWildDrawFour), tcard("r5", ColorRed, RankFive)}, // held red (active) -> violation
			{tcard("b", ColorBlue, RankTwo)}, {tcard("c", ColorGreen, RankTwo)}},
		[]Card{tcard("x1", ColorBlue, RankOne), tcard("x2", ColorGreen, RankTwo), tcard("x3", ColorYellow, RankThree), tcard("x4", ColorRed, RankFour)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: colp(ColorGreen)}); err != nil {
		t.Fatalf("play +4: %v", err)
	}
	// While pending, the trigger color is the pre-+4 active color (red), not the
	// chosen green.
	cv := g.BuildViewFor("p1")
	if cv.Challenge == nil {
		t.Fatalf("no challenge in view")
	}
	if cv.Challenge.TriggerColor != string(ColorRed) {
		t.Fatalf("trigger color = %q, want red (pre-+4 active)", cv.Challenge.TriggerColor)
	}
	if cv.ChallengeRes != nil {
		t.Fatalf("no result banner should be set while pending")
	}

	// p1 challenges and wins (p0 held red).
	if _, err := g.Apply("p1", Action{Type: ActionChallengeFour}); err != nil {
		t.Fatalf("challenge: %v", err)
	}
	tv := g.BuildViewFor("p1")
	if tv.ChallengeRes == nil || !tv.ChallengeRes.Succeeded {
		t.Fatalf("expected a successful challenge result banner, got %+v", tv.ChallengeRes)
	}
	if tv.ChallengeRes.ChallengerPlayerID != "p1" || tv.ChallengeRes.ResolverPlayerID != "p0" {
		t.Fatalf("result ids wrong: %+v", tv.ChallengeRes)
	}

	// The next legitimate turn action clears the banner.
	if _, err := g.Apply("p1", Action{Type: ActionDrawCard}); err != nil {
		t.Fatalf("draw after challenge: %v", err)
	}
	if g.BuildViewFor("p1").ChallengeRes != nil {
		t.Fatalf("result banner should be cleared by the next action")
	}
}

// TestAutoUNO_ClearedOnDrawEffect verifies that a player who had the UNO flag
// set at one card but then receives drawn cards (e.g. a +2) no longer shows
// the UNO mark (rule.md: the flag only applies while holding exactly one card).
func TestAutoUNO_ClearedOnDrawEffect(t *testing.T) {
	// p0 (index 0) has one card and the UNO flag; p1 (index 1, on turn)
	// plays a red +2, making p0 draw 2 cards -> flag must clear.
	g := mkGame(
		[][]Card{
			{tcard("own", ColorRed, RankOne)},
			{tcard("d2", ColorRed, RankDrawTwo), tcard("x", ColorBlue, RankTwo)},
		},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 1, PhaseTurn,
	)
	g.Players[0].HasUNO = true
	if _, err := g.Apply("p1", Action{Type: ActionPlayCard, CardID: "d2"}); err != nil {
		t.Fatalf("play +2: %v", err)
	}
	if g.Players[0].HasUNO {
		t.Fatal("UNO flag should be cleared after p0 drew 2 cards (no longer one card)")
	}
}

// TestAutoUNO_ClearedOnUnplayableDraw verifies that a player holding the flag
// who draws an unplayable card on their turn (going to 2 cards) loses it.
func TestAutoUNO_ClearedOnUnplayableDraw(t *testing.T) {
	// p0 has one card (not currently playable) and the flag; drawing an
	// unplayable card takes them to 2 cards -> flag clears, no drawn_decision.
	g := mkGame(
		[][]Card{
			{tcard("own", ColorGreen, RankOne)},
			{tcard("x", ColorBlue, RankTwo)},
		},
		[]Card{tcard("b7", ColorBlue, RankSeven)}, // not playable on red top
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	g.Players[0].HasUNO = true
	if _, err := g.Apply("p0", Action{Type: ActionDrawCard}); err != nil {
		t.Fatalf("draw: %v", err)
	}
	if g.Phase != PhaseTurn {
		t.Fatalf("phase = %s, want turn", g.Phase)
	}
	if g.Players[0].HasUNO {
		t.Fatal("UNO flag should be cleared after drawing an unplayable card (2 cards)")
	}
}

// TestAutoUNO_ClearedOnAcceptedFour verifies a +4 target accepting (drawing 4)
// clears a previously-set UNO flag.
func TestAutoUNO_ClearedOnAcceptedFour(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("own", ColorBlue, RankOne)},
			{tcard("d4", ColorWild, RankWildDrawFour)},
		},
		[]Card{tcard("c1", ColorRed, RankOne), tcard("c2", ColorRed, RankTwo),
			tcard("c3", ColorRed, RankThree), tcard("c4", ColorRed, RankFour)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 1, PhaseTurn,
	)
	g.Players[0].HasUNO = true
	red := ColorRed
	if _, err := g.Apply("p1", Action{Type: ActionPlayCard, CardID: "d4", ChosenColor: &red}); err != nil {
		t.Fatalf("play +4: %v", err)
	}
	if g.Phase != PhaseChallenge {
		t.Fatalf("phase = %s, want challenge", g.Phase)
	}
	if _, err := g.Apply("p0", Action{Type: ActionAcceptFour}); err != nil {
		t.Fatalf("accept four: %v", err)
	}
	if g.Players[0].HasUNO {
		t.Fatal("UNO flag should be cleared after accepting +4 (p0 drew 4 cards)")
	}
}

// --- Stacking (rule.md R19-R21) ---

func stackConfig() *Game {
	g := mkGame(
		[][]Card{
			{tcard("d2a", ColorRed, RankDrawTwo), tcard("x0", ColorBlue, RankThree)},
			{tcard("d2b", ColorBlue, RankDrawTwo), tcard("x1", ColorGreen, RankThree)},
			{tcard("n2", ColorYellow, RankSeven)},
		},
		nil, tcard("top", ColorRed, RankFive), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.HouseRules.StackDrawTwo = true
	return g
}

func TestStack_DrawTwoHandsOffAndAccumulates(t *testing.T) {
	g := stackConfig()
	// A (p0) plays +2 -> B (p1) must decide in the stack phase.
	res, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d2a"})
	if err != nil {
		t.Fatalf("p0 plays +2: %v", err)
	}
	if g.Phase != PhaseStack || g.CurrentIndex != 1 {
		t.Fatalf("phase=%s current=%d, want stack/1", g.Phase, g.CurrentIndex)
	}
	if g.stackCredits != 2 {
		t.Fatalf("credits = %d, want 2", g.stackCredits)
	}
	if res.LegalMoves == nil || len(res.LegalMoves) != 1 || res.LegalMoves[0] != "d2b" {
		t.Fatalf("stack playable = %v, want [d2b]", res.LegalMoves)
	}

	// B (p1) re-stacks with their +2 -> C (p2) now owes 4.
	if _, err := g.Apply("p1", Action{Type: ActionPlayCard, CardID: "d2b"}); err != nil {
		t.Fatalf("p1 stacks +2: %v", err)
	}
	if g.Phase != PhaseStack || g.CurrentIndex != 2 {
		t.Fatalf("phase=%s current=%d, want stack/2", g.Phase, g.CurrentIndex)
	}
	if g.stackCredits != 4 {
		t.Fatalf("accumulated credits = %d, want 4", g.stackCredits)
	}
	if g.Players[2].Hand[0].ID != "n2" {
		t.Fatalf("p2 hand disturbed before accepting")
	}
}

func TestStack_AcceptDrawsAccumulatedAndSkips(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("d2a", ColorRed, RankDrawTwo), tcard("x0", ColorBlue, RankThree)},
			{tcard("d2b", ColorBlue, RankDrawTwo), tcard("x1", ColorGreen, RankThree)},
			{tcard("n2", ColorYellow, RankSeven)},
		},
		[]Card{tcard("p1", ColorRed, RankOne), tcard("p2", ColorBlue, RankOne),
			tcard("p3", ColorGreen, RankOne), tcard("p4", ColorYellow, RankOne)},
		tcard("top", ColorRed, RankFive), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.HouseRules.StackDrawTwo = true
	_, _ = g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d2a"}) // B owes 2
	_, _ = g.Apply("p1", Action{Type: ActionPlayCard, CardID: "d2b"}) // C owes 4
	before := len(g.Players[2].Hand)
	res, err := g.Apply("p2", Action{Type: ActionAcceptStack})
	if err != nil {
		t.Fatalf("p2 accepts stack: %v", err)
	}
	if len(g.Players[2].Hand) != before+4 {
		t.Fatalf("p2 drew %d cards, want 4", len(g.Players[2].Hand)-before)
	}
	if g.Phase != PhaseTurn || g.CurrentIndex != 0 {
		t.Fatalf("phase=%s current=%d, want turn/0 (p2 skipped)", g.Phase, g.CurrentIndex)
	}
	if g.stackCredits != 0 {
		t.Fatalf("stack credits not cleared, = %d", g.stackCredits)
	}
	_ = res
}

func TestStack_NonStackCardRejected(t *testing.T) {
	g := stackConfig()
	_, _ = g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d2a"}) // B in stack phase
	if _, err := g.Apply("p1", Action{Type: ActionPlayCard, CardID: "x1"}); err == nil {
		t.Fatal("playing a non-stack card in stack phase should be rejected")
	}
}

func TestStack_MixedAllowsSwitchToFour(t *testing.T) {
	// mixed_stacking: +2 heap may be re-stacked with a +4 (R21).
	g := mkGame(
		[][]Card{
			{tcard("d2a", ColorRed, RankDrawTwo), tcard("x0", ColorBlue, RankThree)},
			{tcard("w4", ColorWild, RankWildDrawFour), tcard("x1", ColorGreen, RankThree)},
			{tcard("n2", ColorYellow, RankSeven)},
		},
		nil, tcard("top", ColorRed, RankFive), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.HouseRules.MixedStacking = true
	g.Config.HouseRules.StackDrawTwo = true
	g.Config.HouseRules.StackDrawFour = true
	g.Config.Cards.ChallengeDrawFour = false

	_, _ = g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d2a"}) // B (p1) owes 2
	red := ColorRed
	if _, err := g.Apply("p1", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: &red}); err != nil {
		t.Fatalf("p1 stacks +4 onto +2 heap: %v", err)
	}
	if g.Phase != PhaseStack || g.CurrentIndex != 2 {
		t.Fatalf("phase=%s current=%d, want stack/2", g.Phase, g.CurrentIndex)
	}
	if g.stackCredits != 6 { // 2 + 4
		t.Fatalf("credits = %d, want 6", g.stackCredits)
	}
}

func TestStack_DrawFourRequiresNoChallenge(t *testing.T) {
	c := DefaultConfig()
	c.HouseRules.StackDrawFour = true
	c.Cards.ChallengeDrawFour = true // conflicting
	if err := c.Validate(); err == nil {
		t.Fatal("stack_draw_four with challenge_draw_four should be rejected")
	}
	c.Cards.ChallengeDrawFour = false
	if err := c.Validate(); err != nil {
		t.Fatalf("stack_draw_four with challenge off should pass, got %v", err)
	}
}

// TestStack_DrawTwoOnlyRejectsFour verifies that with +2 stacking on but +4
// off, a +4 cannot be stacked onto a +2 heap (R19).
func TestStack_DrawTwoOnlyRejectsFour(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("d2a", ColorRed, RankDrawTwo), tcard("x0", ColorBlue, RankThree)},
			{tcard("w4", ColorWild, RankWildDrawFour)},
			{tcard("n2", ColorYellow, RankSeven)},
		},
		nil, tcard("top", ColorRed, RankFive), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.HouseRules.StackDrawTwo = true
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d2a"}); err != nil {
		t.Fatalf("p0 plays +2: %v", err)
	}
	if g.Phase != PhaseStack || g.CurrentIndex != 1 {
		t.Fatalf("phase=%s current=%d, want stack/1", g.Phase, g.CurrentIndex)
	}
	lm := g.LegalMoves("p1")
	if len(lm.PlayableIDs) != 0 {
		t.Fatalf("p1 stackable = %v, want none (p1 only holds a +4, +4 stacking off)", lm.PlayableIDs)
	}
	red := ColorRed
	if _, err := g.Apply("p1", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: &red}); err == nil {
		t.Fatal("stacking a +4 onto a +2 heap with +4 stacking off should be rejected")
	}
	if g.Phase != PhaseStack || g.stackCredits != 2 {
		t.Fatalf("state changed after rejected stack: phase=%s credits=%d", g.Phase, g.stackCredits)
	}
}

// TestStack_DrawFourOnlyUsesFour verifies +4 stacking: a +4 heap uses 4-credit
// increments and a +2 cannot stack onto it (R20).
func TestStack_DrawFourOnlyUsesFour(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("w4", ColorWild, RankWildDrawFour), tcard("x0", ColorGreen, RankSeven)},
			{tcard("n2", ColorGreen, RankNine)},
			{tcard("p1", ColorBlue, RankOne), tcard("p2", ColorRed, RankSix)},
		},
		[]Card{tcard("q1", ColorGreen, RankOne), tcard("q2", ColorYellow, RankTwo),
			tcard("q3", ColorRed, RankThree), tcard("q4", ColorBlue, RankFour)},
		tcard("top", ColorRed, RankFive), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.HouseRules.StackDrawFour = true
	g.Config.Cards.ChallengeDrawFour = false
	red := ColorRed
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: &red}); err != nil {
		t.Fatalf("p0 plays +4: %v", err)
	}
	if g.Phase != PhaseStack || g.CurrentIndex != 1 {
		t.Fatalf("phase=%s current=%d, want stack/1", g.Phase, g.CurrentIndex)
	}
	if g.stackCredits != 4 {
		t.Fatalf("credits = %d, want 4", g.stackCredits)
	}
	// p1 has no stackable card (holds only a number): must accept.
	before := len(g.Players[1].Hand)
	if _, err := g.Apply("p1", Action{Type: ActionAcceptStack}); err != nil {
		t.Fatalf("p1 accepts: %v", err)
	}
	if len(g.Players[1].Hand) != before+4 {
		t.Fatalf("p1 drew %d cards, want 4", len(g.Players[1].Hand)-before)
	}
}

// TestStack_MixedFourThenTwo verifies mixed stacking lets a +4 follow a +2 and
// a +2 follow a +4, accumulating 2/4 each step (R21).
func TestStack_MixedFourThenTwo(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("d2a", ColorRed, RankDrawTwo), tcard("s0", ColorGreen, RankSeven)},
			{tcard("w4", ColorWild, RankWildDrawFour), tcard("s1", ColorYellow, RankFive)},
			{tcard("d2c", ColorBlue, RankDrawTwo), tcard("s2", ColorGreen, RankTwo)},
			{tcard("p1", ColorGreen, RankNine), tcard("p2", ColorRed, RankSix)},
		},
		[]Card{tcard("q1", ColorGreen, RankOne), tcard("q2", ColorYellow, RankTwo),
			tcard("q3", ColorRed, RankThree), tcard("q4", ColorBlue, RankFour),
			tcard("q5", ColorGreen, RankThree), tcard("q6", ColorRed, RankFive),
			tcard("q7", ColorYellow, RankFour), tcard("q8", ColorBlue, RankSix)},
		tcard("top", ColorRed, RankFive), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.HouseRules.MixedStacking = true
	g.Config.Cards.ChallengeDrawFour = false
	red := ColorRed
	_, _ = g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d2a"})                   // p1 owes 2
	_, _ = g.Apply("p1", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: &red}) // p2 owes 6
	if g.stackCredits != 6 {
		t.Fatalf("credits after +4 = %d, want 6", g.stackCredits)
	}
	_, _ = g.Apply("p2", Action{Type: ActionPlayCard, CardID: "d2c"}) // p3 owes 8
	if g.CurrentIndex != 3 || g.Phase != PhaseStack || g.stackCredits != 8 {
		t.Fatalf("after mixed stack current=%d phase=%s credits=%d, want 3/stack/8",
			g.CurrentIndex, g.Phase, g.stackCredits)
	}
	before := len(g.Players[3].Hand)
	if _, err := g.Apply("p3", Action{Type: ActionAcceptStack}); err != nil {
		t.Fatalf("p3 accepts: %v", err)
	}
	if len(g.Players[3].Hand) != before+8 {
		t.Fatalf("p3 drew %d cards, want 8", len(g.Players[3].Hand)-before)
	}
}

// TestStack_FirstStepAcceptDrawsTwo verifies a lone +2 that is accepted at once
// only draws 2 (no over-charging).
func TestStack_FirstStepAcceptDrawsTwo(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("d2a", ColorRed, RankDrawTwo), tcard("s0", ColorGreen, RankSeven)},
			{tcard("p1", ColorGreen, RankNine)},
			{tcard("p2", ColorRed, RankSix)},
		},
		[]Card{tcard("q1", ColorGreen, RankOne), tcard("q2", ColorYellow, RankTwo)},
		tcard("top", ColorRed, RankFive), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.HouseRules.StackDrawTwo = true
	_, _ = g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d2a"})
	if g.Phase != PhaseStack || g.CurrentIndex != 1 || g.stackCredits != 2 {
		t.Fatalf("setup: phase=%s current=%d credits=%d", g.Phase, g.CurrentIndex, g.stackCredits)
	}
	before := len(g.Players[1].Hand)
	if _, err := g.Apply("p1", Action{Type: ActionAcceptStack}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if got := len(g.Players[1].Hand) - before; got != 2 {
		t.Fatalf("p1 drew %d, want 2", got)
	}
}

// TestStack_InvalidActionsRejected verifies draw and accept are phase-guarded.
func TestStack_InvalidActionsRejected(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("d2a", ColorRed, RankDrawTwo)},
			{tcard("p1", ColorGreen, RankNine)},
		},
		[]Card{tcard("q1", ColorGreen, RankOne), tcard("q2", ColorYellow, RankTwo)},
		tcard("top", ColorRed, RankFive), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.HouseRules.StackDrawTwo = true
	// Accept while in a normal turn phase is rejected.
	if _, err := g.Apply("p0", Action{Type: ActionAcceptStack}); err == nil {
		t.Fatal("accept_stack outside a stack phase should be rejected")
	}
	_, _ = g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d2a"})
	// Drawing during the stack phase is rejected.
	if _, err := g.Apply("p1", Action{Type: ActionDrawCard}); err == nil {
		t.Fatal("draw during stack phase should be rejected")
	}
}
