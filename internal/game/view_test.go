package game

import (
	"encoding/json"
	"strings"
	"testing"
)

// --- Task 3: personalized views & privacy ---

func TestBuildView_TwoPlayers_HandPrivacy(t *testing.T) {
	g := mkGame(
		[][]Card{
			{tcard("a1", ColorRed, RankOne), tcard("a2", ColorRed, RankThree)},
			{tcard("b1", ColorBlue, RankTwo), tcard("b2", ColorGreen, RankFour)},
		},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)

	v0 := g.BuildViewFor("p0")
	v1 := g.BuildViewFor("p1")

	// Each player only sees their own hand.
	if len(v0.YourHand) != 2 || v0.YourHand[0].ID != "a1" || v0.YourHand[1].ID != "a2" {
		t.Fatalf("p0 hand view = %+v", v0.YourHand)
	}
	if len(v1.YourHand) != 2 || v1.YourHand[0].ID != "b1" || v1.YourHand[1].ID != "b2" {
		t.Fatalf("p1 hand view = %+v", v1.YourHand)
	}

	// PlayableIDs only reflect the requester's own cards.
	for _, id := range v0.PlayableIDs {
		if id != "a1" && id != "a2" {
			t.Fatalf("p0 playable includes non-own card %q", id)
		}
	}
	for _, id := range v1.PlayableIDs {
		if id != "b1" && id != "b2" {
			t.Fatalf("p1 playable includes non-own card %q", id)
		}
	}
}

func TestBuildView_RoomLevelIsSharedData(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("a1", ColorRed, RankOne)}, {tcard("b1", ColorBlue, RankTwo)}},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	v0 := g.BuildViewFor("p0")
	v1 := g.BuildViewFor("p1")
	// Shared public data identical across players.
	if v0.CurrentPlayer != v1.CurrentPlayer || v0.TopCard != v1.TopCard ||
		v0.ActiveColor != v1.ActiveColor || v0.Phase != v1.Phase || len(v0.Players) != len(v1.Players) {
		t.Fatalf("public data differs across player views")
	}
	// Both see each player's card count, not hands.
	for _, p := range v0.Players {
		if p.CardCount == 0 {
			t.Fatalf("player %s reported 0 cards", p.ID)
		}
	}
}

func TestDrawDecision_OnlyForActingPlayer(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("a1", ColorRed, RankOne)}, {tcard("b1", ColorBlue, RankTwo)}},
		[]Card{tcard("r3", ColorRed, RankThree)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	if _, err := g.Apply("p0", Action{Type: ActionDrawCard}); err != nil {
		t.Fatalf("draw: %v", err)
	}
	// p0 (acting) sees the draw decision with the drawn card.
	v0 := g.BuildViewFor("p0")
	if v0.DrawDecision == nil {
		t.Fatal("acting player should see DrawDecision")
	}
	if v0.DrawDecision.Card.ID != "r3" {
		t.Fatalf("draw decision card = %s, want r3", v0.DrawDecision.Card.ID)
	}
	// p1 sees no draw decision.
	v1 := g.BuildViewFor("p1")
	if v1.DrawDecision != nil {
		t.Fatal("non-acting player must not see DrawDecision")
	}
}

func TestBuildView_HistorySerialization_NoSecrets(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("a1", ColorRed, RankOne), tcard("a2", ColorRed, RankThree)}, {tcard("b1", ColorBlue, RankTwo), tcard("b2", ColorGreen, RankFour)}},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	// Commit a play by p0.
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "a1"}); err != nil {
		t.Fatalf("play: %v", err)
	}

	// p0's own remaining hand card a2 is legitimately visible only to p0;
	// the other player's cards (b1,b2) must never leak to anyone else.
	v0 := g.BuildViewFor("p0")
	v1 := g.BuildViewFor("p1")
	for _, p := range []struct {
		view   *GameView
		name   string
		secret []string
		want   []string
	}{
		{v0, "p0", []string{"b1", "b2"}, []string{"a1", "a2"}},
		{v1, "p1", []string{"a2"}, []string{"a1", "b1"}},
	} {
		raw, err := json.Marshal(p.view)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		js := string(raw)
		for _, secretID := range p.secret {
			if strings.Contains(js, secretID) {
				t.Fatalf("view for %s leaked secret id %q", p.name, secretID)
			}
		}
		for _, wantID := range p.want {
			if !strings.Contains(js, wantID) {
				t.Fatalf("view for %s missing id %q", p.name, wantID)
			}
		}
	}
	// History must not contain any hand-card IDs except the played one.
	for _, ev := range g.history {
		raw, _ := json.Marshal(ev)
		js := string(raw)
		for _, secretID := range []string{"b1", "b2", "a2"} {
			if strings.Contains(js, secretID) {
				t.Fatalf("history leaked secret id %q", secretID)
			}
		}
	}
}

func TestHistory_BoundedLimit(t *testing.T) {
	g := mkGame([][]Card{{tcard("a1", ColorRed, RankOne), tcard("a2", ColorRed, RankThree)}}, nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn)

	total := PublicHistoryLimit + 50
	events := make([]Event, 0, total)
	for i := 0; i < total; i++ {
		events = append(events, Event{ID: uint64(i + 1), Type: EventCardPlayed, PlayerID: "p0"})
	}
	g.commitEvents(events)

	if len(g.history) != PublicHistoryLimit {
		t.Fatalf("history = %d, want %d", len(g.history), PublicHistoryLimit)
	}
	// The oldest events are trimmed; the newest survive with correct IDs.
	if g.history[0].ID != uint64(total-PublicHistoryLimit+1) {
		t.Fatalf("first kept event id = %d, want %d", g.history[0].ID, total-PublicHistoryLimit+1)
	}
	if g.lastEventID() != uint64(total) {
		t.Fatalf("lastEventID = %d, want %d", g.lastEventID(), total)
	}
	// The view exposes the same bounded history.
	v := g.BuildViewFor("p0")
	if len(v.History) != PublicHistoryLimit || v.LastEventID != uint64(total) {
		t.Fatalf("view history = %d last=%d", len(v.History), v.LastEventID)
	}
}

func TestBuildView_Finished(t *testing.T) {
	g := mkGame([][]Card{{tcard("a1", ColorRed, RankOne)}}, nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn)
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "a1"}); err != nil {
		t.Fatalf("play: %v", err)
	}
	v := g.BuildViewFor("p0")
	if v.Phase != string(PhaseFinished) || v.WinnerPlayerID != "p0" {
		t.Fatalf("finished view = phase %s winner %s", v.Phase, v.WinnerPlayerID)
	}
}
