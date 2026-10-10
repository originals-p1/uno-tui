package game

// EventType enumerates public game events.
type EventType string

const (
	EventTurnStarted        EventType = "TURN_STARTED"
	EventCardPlayed         EventType = "CARD_PLAYED"
	EventCardDrawn          EventType = "CARD_DRAWN"
	EventUNOCalled          EventType = "UNO_CALLED"
	EventWinner             EventType = "WINNER"
	EventGameEnded          EventType = "GAME_ENDED"
	EventSkipped            EventType = "PLAYER_SKIPPED"
	EventDirectionReversed  EventType = "DIRECTION_REVERSED"
	EventDiscardToDraw      EventType = "DISCARD_RECYCLED"
	EventDrawDecision       EventType = "DRAW_DECISION"
	EventChallenge          EventType = "FOUR_CHALLENGE"
	EventChallengeSucceeded EventType = "CHALLENGE_SUCCEEDED"
	EventChallengeFailed    EventType = "CHALLENGE_FAILED"
)

// Event is a public, privacy-safe game event. It never contains another
// player's hand cards or any secret.
type Event struct {
	ID           uint64      `json:"id"`
	Revision     uint64      `json:"room_revision"`
	Type         EventType   `json:"type"`
	PlayerID     string      `json:"player_id"`
	TargetPlayer string      `json:"target_player_id,omitempty"`
	Card         *CardView   `json:"card,omitempty"`
	Color        Color       `json:"color,omitempty"`
	Payload      interface{} `json:"payload,omitempty"`
}
