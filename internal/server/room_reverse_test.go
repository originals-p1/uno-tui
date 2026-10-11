package server

import (
	"math/rand"
	"testing"

	"terminal-uno/internal/game"
)

// reverseRoom builds a started 3-player room where p0 holds a matching reverse.
func reverseRoom(t *testing.T) *Room {
	t.Helper()
	g, err := game.NewGame([]string{"Alice", "Bob", "Carol"}, rand.NewSource(1))
	if err != nil {
		t.Fatalf("NewGame: %v", err)
	}
	g.Players[0].Hand = []game.Card{{ID: "rv", Color: game.ColorRed, Rank: game.RankReverse}, {ID: "sp", Color: game.ColorGreen, Rank: game.RankSeven}}
	g.Players[1].Hand = []game.Card{{ID: "b", Color: game.ColorGreen, Rank: game.RankTwo}}
	g.Players[2].Hand = []game.Card{{ID: "c", Color: game.ColorRed, Rank: game.RankSeven}}
	g.DiscardPile = []game.Card{{ID: "top", Color: game.ColorRed, Rank: game.RankTwo}}
	g.ActiveColor = game.ColorRed
	g.CurrentIndex = 0
	g.Direction = 1
	g.Phase = game.PhaseTurn

	r := NewRoom(g, 0, NewRealClock())
	r.BindPlayer("s0", "p0")
	r.BindPlayer("s1", "p1")
	r.BindPlayer("s2", "p2")
	go r.Run()
	t.Cleanup(r.Close)
	return r
}

// TestRoom_ReverseFlipsDirectionInView verifies a reverse card played through
// the server flips the direction exposed in the resulting commit view.
func TestRoom_ReverseFlipsDirectionInView(t *testing.T) {
	r := reverseRoom(t)
	out := r.Submit(NewRequest(KindPlayCard, "s0", "rR1", exp(0), game.Action{Type: game.ActionPlayCard, CardID: "rv"}))
	if !out.OK() {
		t.Fatalf("play reverse failed: %+v", out)
	}
	v := out.Commit.Requester
	if v.Direction != -1 {
		t.Fatalf("view direction = %d, want -1 after reverse", v.Direction)
	}
	// Turn passes to the previous seat in the old clockwise order (counter-
	// clockwise): from Alice (0) going -1 -> Carol (2).
	if v.CurrentPlayer != "p2" {
		t.Fatalf("current player = %s, want p2", v.CurrentPlayer)
	}
}
