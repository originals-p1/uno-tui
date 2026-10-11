package game

import "testing"

func TestCardPoints(t *testing.T) {
	cases := []struct {
		card Card
		want int
	}{
		{tcard("a", ColorRed, RankZero), 0},
		{tcard("a", ColorBlue, RankFive), 5},
		{tcard("a", ColorGreen, RankNine), 9},
		{tcard("a", ColorRed, RankSkip), 20},
		{tcard("a", ColorRed, RankReverse), 20},
		{tcard("a", ColorRed, RankDrawTwo), 20},
		{tcard("a", ColorWild, RankWild), 50},
		{tcard("a", ColorWild, RankWildDrawFour), 50},
	}
	for _, c := range cases {
		if got := CardPoints(c.card); got != c.want {
			t.Errorf("CardPoints(%s) = %d, want %d", c.card, got, c.want)
		}
	}
}

func TestRoundPoints_ExcludesWinner(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("w0", ColorRed, RankOne)}, // winner p0
			{tcard("r5", ColorRed, RankFive), tcard("g2", ColorGreen, RankTwo), tcard("wild", ColorWild, RankWild)}, // 5+2+50=57
			{tcard("skip", ColorBlue, RankSkip)}, // 20
		},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseFinished,
	)
	got := g.RoundPoints("p0")
	want := 5 + 2 + 50 + 20
	if got != want {
		t.Fatalf("RoundPoints = %d, want %d", got, want)
	}
}

func TestApplyWin_SetsRoundPoints(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("a1", ColorRed, RankOne)},
			{tcard("b5", ColorBlue, RankFive), tcard("b9", ColorBlue, RankNine)}, // 14
		},
		[]Card{}, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	res, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "a1"})
	if err != nil || !res.Ended {
		t.Fatalf("expected win: ended=%v err=%v", res.Ended, err)
	}
	if res.WinnerID != "p0" {
		t.Fatalf("winner = %s, want p0", res.WinnerID)
	}
	if res.RoundPoints != 14 {
		t.Fatalf("RoundPoints = %d, want 14", res.RoundPoints)
	}
}
