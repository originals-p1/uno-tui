package game

import "testing"

func TestActionDeckComposition(t *testing.T) {
	deck := ActionDeck()
	if len(deck) != 140 {
		t.Fatalf("got %d cards", len(deck))
	}
	seen := map[string]bool{}
	actions := 0
	for _, c := range deck {
		if seen[c.ID] {
			t.Fatal("duplicate ID", c.ID)
		}
		seen[c.ID] = true
		if IsAction(c.Rank) {
			actions++
		}
	}
	if actions != 64 {
		t.Fatalf("got %d actions", actions)
	}
	for _, r := range []Rank{RankSkip, RankReverse, RankDrawTwo} {
		if countRanks(deck, r) != 16 {
			t.Fatal("incorrect action count", r)
		}
	}
}
