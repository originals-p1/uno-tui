// Package game implements the pure UNO rule engine. It must not depend on
// networking or the TUI (see readme §5.1 and §15).
package game

import (
	"fmt"
	"strings"
)

// Color is the color of a card. "wild" is used for Wild and Wild Draw Four,
// whose active color is chosen by the player and tracked separately.
type Color string

const (
	ColorRed    Color = "red"
	ColorYellow Color = "yellow"
	ColorGreen  Color = "green"
	ColorBlue   Color = "blue"
	ColorWild   Color = "wild"
)

// Rank is the face value/action kind of a card.
type Rank string

const (
	RankZero         Rank = "0"
	RankOne          Rank = "1"
	RankTwo          Rank = "2"
	RankThree        Rank = "3"
	RankFour         Rank = "4"
	RankFive         Rank = "5"
	RankSix          Rank = "6"
	RankSeven        Rank = "7"
	RankEight        Rank = "8"
	RankNine         Rank = "9"
	RankSkip         Rank = "skip"
	RankReverse      Rank = "reverse"
	RankDrawTwo      Rank = "draw_two"
	RankWild         Rank = "wild"
	RankWildDrawFour Rank = "wild_draw_four"
)

// numberRanks is the set of numeric ranks used to find the initial top card.
var numberRanks = map[Rank]bool{
	RankZero: true,
	RankOne:  true, RankTwo: true, RankThree: true, RankFour: true,
	RankFive: true, RankSix: true, RankSeven: true, RankEight: true,
	RankNine: true,
}

// IsNumber reports whether r is a numeric rank (0..9).
func IsNumber(r Rank) bool { return numberRanks[r] }

// IsAction reports whether r is a non-numeric action rank.
func IsAction(r Rank) bool { return !IsNumber(r) }

// IsWild reports whether r is a wild rank (wild or wild_draw_four).
func IsWild(r Rank) bool { return r == RankWild || r == RankWildDrawFour }

// Card is a single UNO card. The ID is unique within a game; the same
// color+rank may appear multiple times, so IDs must not be derived from the
// display value alone (readme §5.3).
type Card struct {
	ID    string `json:"id"`
	Color Color  `json:"color"`
	Rank  Rank   `json:"rank"`
}

// String returns a compact human-readable form such as "red_2" or "wild".
// It is used for debugging/logs and must never leak a private card in a
// public broadcast payload.
func (c Card) String() string { return string(c.Color) + "_" + string(c.Rank) }

// StandardDeck returns the 108 classic UNO cards, each with a unique ID.
// Per color: 0x1, 1-9x2, Skip x2, Reverse x2, Draw Two x2 (25 cards/color),
// plus Wild x4 and Wild Draw Four x4.
func StandardDeck() []Card {
	deck := make([]Card, 0, 108)
	id := 0
	next := func() int { i := id; id++; return i }

	colored := []Color{ColorRed, ColorYellow, ColorGreen, ColorBlue}
	for _, col := range colored {
		deck = append(deck, Card{ID: cardID(next()), Color: col, Rank: RankZero})
		for d := 1; d <= 9; d++ {
			deck = append(deck, Card{ID: cardID(next()), Color: col, Rank: numberRank(d)})
			deck = append(deck, Card{ID: cardID(next()), Color: col, Rank: numberRank(d)})
		}
		for _, r := range []Rank{RankSkip, RankReverse, RankDrawTwo} {
			deck = append(deck, Card{ID: cardID(next()), Color: col, Rank: r})
			deck = append(deck, Card{ID: cardID(next()), Color: col, Rank: r})
		}
	}
	for i := 0; i < 4; i++ {
		deck = append(deck, Card{ID: cardID(next()), Color: ColorWild, Rank: RankWild})
	}
	for i := 0; i < 4; i++ {
		deck = append(deck, Card{ID: cardID(next()), Color: ColorWild, Rank: RankWildDrawFour})
	}
	return deck
}

func cardID(n int) string { return fmt.Sprintf("card-%03d", n) }

// numberRank maps the digits 1..9 to their Rank constant.
func numberRank(d int) Rank {
	switch d {
	case 1:
		return RankOne
	case 2:
		return RankTwo
	case 3:
		return RankThree
	case 4:
		return RankFour
	case 5:
		return RankFive
	case 6:
		return RankSix
	case 7:
		return RankSeven
	case 8:
		return RankEight
	case 9:
		return RankNine
	default:
		return RankZero
	}
}

// CardPoints returns the score value of a card in a SCORE-mode round
// (rule.md R28): number cards are worth their face value, Skip/Reverse/DrawTwo
// are 20, Wild and Wild Draw Four are 50.
func CardPoints(c Card) int {
	switch c.Rank {
	case RankSkip, RankReverse, RankDrawTwo:
		return 20
	case RankWild, RankWildDrawFour:
		return 50
	}
	for _, d := range "0123456789" {
		if c.Rank == Rank(d) {
			return int(d - '0')
		}
	}
	return 0
}

// RoundPoints computes the SCORE-mode points awarded to winnerID at the end of
// the round: the sum of the point values of every OTHER player's remaining hand
// cards (rule.md R28). Callers should only call this on a finished game.
func (g *Game) RoundPoints(winnerID string) int {
	total := 0
	for i := range g.Players {
		p := &g.Players[i]
		if p.ID == winnerID {
			continue
		}
		for _, c := range p.Hand {
			total += CardPoints(c)
		}
	}
	return total
}

// HandPoints returns the total point value of a hand (rule.md R28 card values).
func HandPoints(hand []Card) int {
	total := 0
	for _, c := range hand {
		total += CardPoints(c)
	}
	return total
}

// IndexOf returns the index of the first card in hand with the given ID, or
// -1 if not found.
func IndexOf(hand []Card, id string) int {
	for i, c := range hand {
		if c.ID == id {
			return i
		}
	}
	return -1
}

// sanitizeName trims surrounding whitespace and collapses internal runs of
// whitespace to a single space, used when constructing players. (Room-level
// checks enforce the 1-24 code point limit and control characters.)
func sanitizeName(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
