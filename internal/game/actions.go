package game

// ActionType enumerates the state-changing player actions the engine accepts.
type ActionType string

const (
	ActionPlayCard      ActionType = "play_card"      // discard a chosen hand card
	ActionDrawCard      ActionType = "draw_card"      // draw one card
	ActionPlayDrawn     ActionType = "play_drawn"     // play the just-drawn card
	ActionPassDrawn     ActionType = "pass_drawn"     // keep the drew card and end turn
	ActionAcceptFour    ActionType = "accept_four"    // +4 challenge: accept the +4 penalty (R13)
	ActionChallengeFour ActionType = "challenge_four" // +4 challenge: challenge the +4 player (R13)
)

// PlayCardAction carries a play_card request. ChosenColor is required for Wild
// and Wild Draw Four and must be one of the four colors; it may only be a wild
// color for wild ranks.
type Action struct {
	Type        ActionType `json:"type"`
	CardID      string     `json:"card_id,omitempty"`
	HandID      string     `json:"hand_id,omitempty"`  // back-compat alias for CardID
	DrawnID     string     `json:"drawn_id,omitempty"` // for play_drawn
	ChosenColor *Color     `json:"chosen_color,omitempty"`
}

// Result reports the outcome of an Apply, including generated events and the
// immediately following legal moves for the new current player.
type Result struct {
	Events       []Event
	Ended        bool
	WinnerID     string
	EndReason    string
	NextPlayerID string
	LegalMoves   []string // playable card IDs for the next current player
	// RoundPoints is the SCORE-mode points awarded to the round winner for this
	// round (sum of opponents' remaining hand-card points, rule.md R28). It is
	// only set when the round ended (Ended == true).
	RoundPoints int
}

// LegalMoves returns the set of playable card IDs for a player, or nil in a
// non-turn / finished phase. In the drawn_decision phase it returns only a
// pseudo card ID for playing the just-drawn card.
type LegalMoves struct {
	PlayableIDs []string // hand card IDs the player may currently play
	CanDraw     bool     // true during a normal own turn
}
