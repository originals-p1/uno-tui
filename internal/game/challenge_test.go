package game

import (
	"reflect"
	"testing"
)

// Catch checks against the newly chosen color, or against playable numbers,
// symbols and wilds instead of the color in effect before the +4 was played.
func TestChallenge_ColorEligibility(t *testing.T) {
	tests := []struct {
		name    string
		top     Card
		held    Card
		success bool
	}{
		{"same color number", tcard("top", ColorRed, RankFive), tcard("held", ColorRed, RankTwo), true},
		{"same color skip", tcard("top", ColorRed, RankFive), tcard("held", ColorRed, RankSkip), true},
		{"same color reverse", tcard("top", ColorRed, RankFive), tcard("held", ColorRed, RankReverse), true},
		{"same color draw two", tcard("top", ColorRed, RankFive), tcard("held", ColorRed, RankDrawTwo), true},
		{"other color same number", tcard("top", ColorRed, RankFive), tcard("held", ColorBlue, RankFive), false},
		{"other color same symbol", tcard("top", ColorRed, RankSkip), tcard("held", ColorBlue, RankSkip), false},
		{"ordinary wild", tcard("top", ColorRed, RankFive), tcard("held", ColorWild, RankWild), false},
		{"another draw four", tcard("top", ColorRed, RankFive), tcard("held", ColorWild, RankWildDrawFour), false},
		{"previous wild declared red", tcard("top", ColorWild, RankWild), tcard("held", ColorRed, RankTwo), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := fourChallengeFixture([]Card{tcard("w4", ColorWild, RankWildDrawFour), tt.held}, tt.top, 1)
			if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: colp(ColorBlue)}); err != nil {
				t.Fatal(err)
			}
			res, err := g.Apply("p1", Action{Type: ActionChallengeFour})
			if err != nil {
				t.Fatal(err)
			}
			wantHands, wantCurrent, wantEvent := []int{1, 7, 1, 1}, 2, EventChallengeFailed
			if tt.success {
				wantHands, wantCurrent, wantEvent = []int{5, 1, 1, 1}, 1, EventChallengeSucceeded
			}
			for i, want := range wantHands {
				if got := len(g.Players[i].Hand); got != want {
					t.Errorf("p%d hand=%d, want %d", i, got, want)
				}
			}
			if g.Phase != PhaseTurn || g.CurrentIndex != wantCurrent {
				t.Errorf("phase=%s current=%d, want turn/%d", g.Phase, g.CurrentIndex, wantCurrent)
			}
			if g.ActiveColor != ColorBlue || g.DiscardPile[len(g.DiscardPile)-1].ID != "w4" {
				t.Error("challenge must preserve the +4 and its chosen color")
			}
			if ok, _ := res.eventsContain(wantEvent); !ok {
				t.Errorf("missing %s: %+v", wantEvent, res.Events)
			}
			view := g.BuildViewFor("p1")
			if view.Challenge != nil || view.ChallengeRes == nil || view.ChallengeRes.Succeeded != tt.success {
				t.Errorf("unexpected settled challenge view: %+v", view)
			}
		})
	}
}

// Four players distinguish a reversed skip from an accidental clockwise move.
func TestChallenge_ReverseDirectionSettlement(t *testing.T) {
	for _, tt := range []struct {
		name    string
		action  ActionType
		color   Color
		current int
		hands   []int
	}{
		{"accept illegal four", ActionAcceptFour, ColorRed, 2, []int{1, 1, 1, 5}},
		{"challenge succeeds", ActionChallengeFour, ColorRed, 3, []int{5, 1, 1, 1}},
		{"challenge fails", ActionChallengeFour, ColorBlue, 2, []int{1, 1, 1, 7}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := fourChallengeFixture([]Card{tcard("w4", ColorWild, RankWildDrawFour), tcard("held", tt.color, RankTwo)}, tcard("top", ColorRed, RankFive), -1)
			if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: colp(ColorBlue)}); err != nil {
				t.Fatal(err)
			}
			if g.Phase != PhaseChallenge || g.CurrentIndex != 3 {
				t.Fatalf("challenge target: %s/%d", g.Phase, g.CurrentIndex)
			}
			res, err := g.Apply("p3", Action{Type: tt.action})
			if err != nil {
				t.Fatal(err)
			}
			if g.Phase != PhaseTurn || g.CurrentIndex != tt.current || res.NextPlayerID != playerID(tt.current) || g.Direction != -1 {
				t.Fatalf("incorrect turn after settlement: %+v", res)
			}
			for i, want := range tt.hands {
				if len(g.Players[i].Hand) != want {
					t.Errorf("p%d hand=%d, want %d", i, len(g.Players[i].Hand), want)
				}
			}
		})
	}
}

func TestChallenge_LastCardFailedChallengeSettlesBeforeScoring(t *testing.T) {
	g := fourChallengeFixture([]Card{tcard("w4", ColorWild, RankWildDrawFour)}, tcard("top", ColorRed, RankFive), 1)
	res, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: colp(ColorBlue)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Ended || g.Phase != PhaseChallenge || g.WinnerPlayerID != "" {
		t.Fatal("winner declared before challenge settled")
	}
	res, err = g.Apply("p1", Action{Type: ActionChallengeFour})
	if err != nil {
		t.Fatal(err)
	}
	// Three opponents each hold a 2, and the target draws six 1s: 6 + 6.
	if !res.Ended || res.WinnerID != "p0" || g.Phase != PhaseFinished || res.RoundPoints != 12 || len(g.Players[1].Hand) != 7 {
		t.Fatalf("incorrect final penalty/scoring: %+v", res)
	}
	if ok, _ := res.eventsContain(EventChallengeFailed); !ok {
		t.Fatal("missing failed challenge event")
	}
	if ok, _ := res.eventsContain(EventWinner); !ok {
		t.Fatal("missing winner event")
	}
}

// Rejected decisions must neither consume the pending challenge nor draw cards.
func TestChallenge_RejectedActionsPreservePendingDecision(t *testing.T) {
	for _, tt := range []struct {
		name   string
		player string
		action Action
		code   ErrorCode
	}{
		{"resolver challenges", "p0", Action{Type: ActionChallengeFour}, ErrCodeNotYourTurn},
		{"bystander accepts", "p2", Action{Type: ActionAcceptFour}, ErrCodeNotYourTurn},
		{"unknown player", "unknown", Action{Type: ActionChallengeFour}, ErrCodeInvalidPlayer},
		{"target draws normally", "p1", Action{Type: ActionDrawCard}, ErrCodeInvalidPhase},
		{"target passes", "p1", Action{Type: ActionPassDrawn}, ErrCodeInvalidPhase},
	} {
		t.Run(tt.name, func(t *testing.T) {
			g := fourChallengeFixture([]Card{tcard("w4", ColorWild, RankWildDrawFour), tcard("held", ColorRed, RankTwo)}, tcard("top", ColorRed, RankFive), 1)
			if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "w4", ChosenColor: colp(ColorBlue)}); err != nil {
				t.Fatal(err)
			}
			before := make([]*GameView, 4)
			for i := range before {
				before[i] = g.BuildViewFor(playerID(i))
			}
			_, err := g.Apply(tt.player, tt.action)
			expectCode(t, err, tt.code)
			for i := range before {
				if !reflect.DeepEqual(before[i], g.BuildViewFor(playerID(i))) {
					t.Errorf("rejected action changed p%d's view", i)
				}
			}
			if _, err := g.Apply("p1", Action{Type: ActionChallengeFour}); err != nil {
				t.Fatal(err)
			}
			if len(g.Players[0].Hand) != 5 || len(g.Players[1].Hand) != 1 || g.CurrentIndex != 1 || g.Phase != PhaseTurn {
				t.Fatal("rejected action corrupted subsequent challenge settlement")
			}
		})
	}
}

func fourChallengeFixture(hand []Card, top Card, direction int) *Game {
	return mkGame([][]Card{hand,
		{tcard("p1-card", ColorBlue, RankTwo)},
		{tcard("p2-card", ColorGreen, RankTwo)},
		{tcard("p3-card", ColorYellow, RankTwo)},
	}, []Card{
		tcard("d1", ColorBlue, RankOne), tcard("d2", ColorBlue, RankOne),
		tcard("d3", ColorBlue, RankOne), tcard("d4", ColorBlue, RankOne),
		tcard("d5", ColorBlue, RankOne), tcard("d6", ColorBlue, RankOne),
	}, top, ColorRed, direction, 0, PhaseTurn)
}
