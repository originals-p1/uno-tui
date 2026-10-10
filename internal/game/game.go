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
	// PublicHistoryLimit bounds the number of public history events kept.
	PublicHistoryLimit = 100
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

	RoomRevision uint64      // authoritative room revision (owned by the room loop)
	rng          rand.Source // injected RNG (never nil after Start)
	eventSeq     uint64
	history      []GameEvent

	// Paused metadata, populated by the room on disconnect.
	disconnectedIDs  []string
	earliestDeadline string
}

// NewGame builds a fresh game from the standard 108-card deck, dealing opening
// hands and establishing the initial top card. On any error it returns a nil
// Game and leaves no partial state behind (readme §3.3 item 6).
func NewGame(playerNames []string, rng rand.Source) (*Game, error) {
	return newGameFromDeck(playerNames, StandardDeck(), rng)
}

// NewGameWithIDs is like NewGame but assigns the supplied stable player IDs
// (one per name, in seat order) instead of synthesized seat IDs. The server
// uses its own session-scoped player IDs so that reconnection, the player
// registry, and per-player views all reference one consistent identity
// (readme §3.6 stable player identity).
func NewGameWithIDs(playerNames, playerIDs []string, rng rand.Source) (*Game, error) {
	if len(playerIDs) != len(playerNames) {
		return nil, newErr(ErrCodeInvalidArgument, "player_ids length %d != names length %d", len(playerIDs), len(playerNames))
	}
	seen := map[string]bool{}
	for _, id := range playerIDs {
		if id == "" {
			return nil, newErr(ErrCodeInvalidArgument, "empty player id")
		}
		if seen[id] {
			return nil, newErr(ErrCodeInvalidArgument, "duplicate player id %q", id)
		}
		seen[id] = true
	}
	return newGameFromDeckIDs(playerNames, playerIDs, StandardDeck(), rng)
}

// newGameFromDeck is the testable core of NewGame: it builds the game from an
// arbitrary deck, which lets tests exercise the "no initial number card" path.
func newGameFromDeck(playerNames []string, deck []Card, rng rand.Source) (*Game, error) {
	return newGameFromDeckIDs(playerNames, nil, deck, rng)
}

func newGameFromDeckIDs(playerNames, playerIDs []string, deck []Card, rng rand.Source) (*Game, error) {
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
		id := playerID(i)
		if playerIDs != nil {
			id = playerIDs[i]
		}
		players = append(players, Player{
			ID:        id,
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

// lastEventID returns the ID of the most recently committed event, or 0 if
// no event has been committed yet.
func (g *Game) lastEventID() uint64 {
	if len(g.history) == 0 {
		return 0
	}
	return g.history[len(g.history)-1].ID
}

// commitEvents appends public events onto the bounded history log. The room
// loop must call this from the single owner goroutine after a successful
// Apply. Events carry the room revision current at commit time.
func (g *Game) commitEvents(events []Event) {
	for _, e := range events {
		g.history = append(g.history, GameEvent{
			ID:           e.ID,
			Revision:     g.RoomRevision,
			Type:         string(e.Type),
			PlayerID:     e.PlayerID,
			TargetPlayer: e.TargetPlayer,
			Card:         e.Card,
			Color:        string(e.Color),
			Payload:      e.Payload,
		})
	}
	if n := len(g.history); n > PublicHistoryLimit {
		g.history = g.history[n-PublicHistoryLimit:]
	}
}

// SetPaused marks the game as paused due to disconnects. It is called by the
// room loop with the exact set of offline players and the earliest deadline.
func (g *Game) SetPaused(ids []string, earliestDeadline string) {
	g.Phase = PhasePaused
	g.previousPhaseSnapshot()
	g.disconnectedIDs = append([]string{}, ids...)
	g.earliestDeadline = earliestDeadline
}

// ResumePaused restores the game to the phase it held before pausing.
func (g *Game) ResumePaused(prev Phase) {
	g.Phase = prev
	g.disconnectedIDs = nil
	g.earliestDeadline = ""
}

func (g *Game) previousPhaseSnapshot() {}

// pausedView renders the public paused summary.
func (g *Game) pausedView() *PausedView {
	return &PausedView{
		DisconnectedPlayerIDs: append([]string{}, g.disconnectedIDs...),
		EarliestDeadline:      g.earliestDeadline,
	}
}
