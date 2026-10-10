package game

import (
	"fmt"
	"math/rand"
	"sync/atomic"
)

// Phase is the authoritative game stage (readme §3.6).
type Phase string

const (
	PhaseLobby         Phase = "lobby"
	PhaseTurn          Phase = "turn"
	PhaseDrawnDecision Phase = "drawn_decision"
	PhasePaused        Phase = "paused"
	PhaseFinished      Phase = "finished"
)

// MinPlayers / MaxPlayers bound the supported player count.
const (
	MinPlayers = 2
	MaxPlayers = 4
	// CardsPerPlayer is the opening hand size.
	CardsPerPlayer = 7
)

// Player is a participant tracked by the engine. Hand privacy is preserved by
// the view layer; this struct is never broadcast directly.
type Player struct {
	ID        string
	Name      string
	Hand      []Card
	Connected bool
	HasUNO    bool
	Ready     bool
	IsHost    bool
}

// PlayerView is the privacy-safe, JSON-ready summary of a player. It contains
// no hand cards.
type PlayerView struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CardCount int    `json:"card_count"`
	UNO       bool   `json:"uno"`
	Connected bool   `json:"connected"`
	Ready     bool   `json:"ready"`
	IsHost    bool   `json:"is_host"`
}

// Game is the pure card game state. All state is confined to the owning room
// event loop and mutated only through Start/Apply (readme §5.2).
type Game struct {
	Players        []Player
	DrawPile       []Card
	DiscardPile    []Card
	CurrentIndex   int
	Direction      int // +1 or -1
	ActiveColor    Color
	Phase          Phase
	DrawnCardID    string
	WinnerPlayerID string
	EndReason      string
	PreviousPhase  Phase

	rng      rand.Source // injected RNG (never nil after Start)
	eventSeq uint64
}

// NewGame builds a fresh game from the standard 108-card deck, dealing opening
// hands and establishing the initial top card. On any error it returns a nil
// Game and leaves no partial state behind (readme §3.3 item 6).
func NewGame(playerNames []string, rng rand.Source) (*Game, error) {
	return newGameFromDeck(playerNames, StandardDeck(), rng)
}

// newGameFromDeck is the testable core of NewGame: it builds the game from an
// arbitrary deck, which lets tests exercise the "no initial number card" path.
func newGameFromDeck(playerNames []string, deck []Card, rng rand.Source) (*Game, error) {
	if rng == nil {
		return nil, newErr(ErrCodeInvalidArgument, "rng must not be nil")
	}
	if len(playerNames) < MinPlayers || len(playerNames) > MaxPlayers {
		return nil, newErr(ErrCodeInvalidPlayerCount, "player count %d outside [%d,%d]", len(playerNames), MinPlayers, MaxPlayers)
	}

	// Work on local copies so an error leaves the receiver unchanged.
	draw := make([]Card, len(deck))
	copy(draw, deck)
	shuffle(draw, rng)

	// The deck must hold all opening hands plus at least one possible top card.
	if len(draw) < len(playerNames)*CardsPerPlayer+1 {
		return nil, newErr(ErrCodeEmptyDeck, "deck too small: %d cards for %d players", len(draw), len(playerNames))
	}

	players := make([]Player, 0, len(playerNames))
	for i, nm := range playerNames {
		players = append(players, Player{
			ID:        playerID(i),
			Name:      sanitizeName(nm),
			Hand:      make([]Card, 0, CardsPerPlayer+1),
			Connected: true,
		})
	}
	// Deal opening hands.
	for i := 0; i < CardsPerPlayer; i++ {
		for j := range players {
			players[j].Hand = append(players[j].Hand, draw[0])
			draw = draw[1:]
		}
	}

	// Find the first number card from the top of the remaining pile.
	idx := -1
	for i, c := range draw {
		if IsNumber(c.Rank) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil, newErr(ErrCodeNoInitialNumberCard, "no initial number card in deck")
	}

	top := draw[idx]
	prefix := draw[:idx] // action cards skipped before the top card
	rest := draw[idx+1:] // remainder after the top card
	shuffle(prefix, rng) // reshuffle skipped action cards back into the pile
	draw = append(append([]Card{}, prefix...), rest...)

	g := &Game{
		Players:      players,
		DrawPile:     draw,
		DiscardPile:  []Card{top},
		CurrentIndex: 0,
		Direction:    1,
		ActiveColor:  top.Color,
		Phase:        PhaseTurn,
		rng:          rng,
	}
	return g, nil
}

// playerID maps a seat index to a stable player identifier.
func playerID(i int) string { return fmt.Sprintf("p%d", i) }

// shuffle performs an in-place Fisher-Yates shuffle using the injected RNG.
func shuffle(deck []Card, src rand.Source) {
	r := rand.New(src)
	for i := len(deck) - 1; i > 0; i-- {
		j := r.Intn(i + 1)
		deck[i], deck[j] = deck[j], deck[i]
	}
}

// NextEventID returns a unique, monotonically increasing event id for the
// current game. It is safe for any caller (atomic), but in practice events
// are only generated inside the single owner goroutine.
func (g *Game) NextEventID() uint64 { return atomic.AddUint64(&g.eventSeq, 1) }
