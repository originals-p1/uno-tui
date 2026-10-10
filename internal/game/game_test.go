package game

import (
	"fmt"
	"math/rand"
	"testing"
)

// --- Task 1: deck construction & determinism ---

func countRanks(deck []Card, r Rank) int {
	n := 0
	for _, c := range deck {
		if c.Rank == r {
			n++
		}
	}
	return n
}

func TestDeck_StandardComposition(t *testing.T) {
	deck := StandardDeck()
	if len(deck) != 108 {
		t.Fatalf("standard deck = %d cards, want 108", len(deck))
	}
	// Structure: 4 colors * 25 + 4 wild + 4 wild draw four.
	colored := []Color{ColorRed, ColorYellow, ColorGreen, ColorBlue}
	for _, col := range colored {
		num := 0
		for _, c := range deck {
			if c.Color == col {
				num++
			}
		}
		if num != 25 {
			t.Errorf("color %s = %d cards, want 25", col, num)
		}
		if countRanks(deck, RankZero) != 4 { // one per color
			t.Errorf("rank 0 count = %d, want 4", countRanks(deck, RankZero))
		}
	}
	for _, r := range []Rank{RankOne, RankTwo, RankThree, RankFour, RankFive, RankSix, RankSeven, RankEight, RankNine} {
		if countRanks(deck, r) != 8 { // two per color
			t.Errorf("rank %s count = %d, want 8", r, countRanks(deck, r))
		}
	}
	for _, r := range []Rank{RankSkip, RankReverse, RankDrawTwo} {
		if countRanks(deck, r) != 8 { // two per color
			t.Errorf("rank %s count = %d, want 8", r, countRanks(deck, r))
		}
	}
	if countRanks(deck, RankWild) != 4 {
		t.Errorf("wild count = %d, want 4", countRanks(deck, RankWild))
	}
	if countRanks(deck, RankWildDrawFour) != 4 {
		t.Errorf("wild draw four count = %d, want 4", countRanks(deck, RankWildDrawFour))
	}
}

func TestDeck_UniqueIDs(t *testing.T) {
	deck := StandardDeck()
	seen := make(map[string]bool, len(deck))
	for _, c := range deck {
		if c.ID == "" {
			t.Fatal("found empty card ID")
		}
		if seen[c.ID] {
			t.Fatalf("duplicate card ID %q", c.ID)
		}
		seen[c.ID] = true
	}
}

func TestDeck_DeterministicShuffle(t *testing.T) {
	names := []string{"Alice", "Bob", "Charlie"}
	a, err := NewGame(names, rand.NewSource(42))
	if err != nil {
		t.Fatalf("NewGame(a): %v", err)
	}
	b, err := NewGame(names, rand.NewSource(42))
	if err != nil {
		t.Fatalf("NewGame(b): %v", err)
	}
	// Independent sources with the same seed must produce identical hands.
	for i := range a.Players {
		for j := range a.Players[i].Hand {
			if a.Players[i].Hand[j] != b.Players[i].Hand[j] {
				t.Fatalf("determinism broken at player %d hand %d", i, j)
			}
		}
	}
	if a.DrawPile[0] != b.DrawPile[0] {
		t.Fatal("determinism broken in draw pile")
	}
}

// --- Task 1: initial top card ---

func TestStart_InitialNumberTopCard(t *testing.T) {
	g, err := NewGame([]string{"Alice", "Bob"}, rand.NewSource(7))
	if err != nil {
		t.Fatalf("NewGame: %v", err)
	}
	if len(g.DiscardPile) != 1 {
		t.Fatalf("discard pile = %d cards, want 1", len(g.DiscardPile))
	}
	top := g.DiscardPile[len(g.DiscardPile)-1]
	if !IsNumber(top.Rank) {
		t.Fatalf("initial top card rank %q is not a number", top.Rank)
	}
	if g.ActiveColor != top.Color {
		t.Fatalf("ActiveColor = %q, want top color %q", g.ActiveColor, top.Color)
	}
	if g.Phase != PhaseTurn {
		t.Fatalf("Phase = %q, want turn", g.Phase)
	}
	if g.Direction != 1 {
		t.Fatalf("Direction = %d, want 1", g.Direction)
	}
	// Opening hands: 7 cards each.
	for _, p := range g.Players {
		if len(p.Hand) != CardsPerPlayer {
			t.Errorf("hand size = %d, want %d", len(p.Hand), CardsPerPlayer)
		}
	}
	// Top card must not also be in any hand.
	for _, p := range g.Players {
		for _, c := range p.Hand {
			if c.ID == top.ID {
				t.Fatalf("top card %q also present in a player hand", top.ID)
			}
		}
	}
	// No duplicate IDs across hands/draw/discard.
	idseen := map[string]bool{}
	all := []Card{}
	for _, p := range g.Players {
		all = append(all, p.Hand...)
	}
	all = append(all, g.DrawPile...)
	all = append(all, g.DiscardPile...)
	for _, c := range all {
		if idseen[c.ID] {
			t.Fatalf("duplicate ID %q across game state", c.ID)
		}
		idseen[c.ID] = true
	}
}

func TestStart_NoInitialNumberCard(t *testing.T) {
	// A deck with no numeric cards must fail without leaving partial state.
	// It must still be large enough for two opening hands + a top card.
	ranks := []Rank{RankSkip, RankReverse, RankDrawTwo, RankWild, RankWildDrawFour}
	deck := make([]Card, 0, 25)
	for i := 0; i < 25; i++ {
		deck = append(deck, Card{
			ID:    fmt.Sprintf("x%02d", i),
			Color: ColorWild,
			Rank:  ranks[i%len(ranks)],
		})
	}
	g, err := newGameFromDeck([]string{"Alice", "Bob"}, deck, rand.NewSource(3))
	if err == nil {
		t.Fatal("expected error for deck with no number card")
	}
	if ge, ok := err.(*GameError); !ok || ge.Code != ErrCodeNoInitialNumberCard {
		t.Fatalf("error = %v, want ErrCodeNoInitialNumberCard", err)
	}
	if g != nil {
		t.Fatal("expected nil game on error")
	}
}

func TestStart_InvalidPlayerCount(t *testing.T) {
	for _, n := range []int{1, 5} {
		names := make([]string, n)
		for i := range names {
			names[i] = "P"
		}
		if g, err := NewGame(names, rand.NewSource(1)); err == nil || g != nil {
			t.Fatalf("count %d: want error+nil game, got %v / %v", n, g, err)
		}
	}
}

func TestRankHelpers(t *testing.T) {
	for _, r := range []Rank{RankZero, RankNine, RankFive} {
		if !IsNumber(r) || IsAction(r) {
			t.Errorf("rank %q misclassified", r)
		}
	}
	if !IsAction(RankSkip) || IsNumber(RankSkip) {
		t.Error("skip misclassified")
	}
	if !IsWild(RankWild) || !IsWild(RankWildDrawFour) {
		t.Error("wild misclassified")
	}
	if IsWild(RankSkip) {
		t.Error("skip should not be wild")
	}
}
