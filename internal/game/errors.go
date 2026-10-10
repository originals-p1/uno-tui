package game

import "fmt"

// Stable business error codes. These are part of the engine contract and
// must not change meaning; clients/UI must branch on these codes, never on
// human-readable text (readme §15).
type ErrorCode string

const (
	ErrCodeNoInitialNumberCard    ErrorCode = "ERR_NO_INITIAL_NUMBER_CARD"
	ErrCodeInvalidPlayerCount     ErrorCode = "ERR_INVALID_PLAYER_COUNT"
	ErrCodeInvalidArgument        ErrorCode = "ERR_INVALID_ARGUMENT"
	ErrCodeEmptyDeck              ErrorCode = "ERR_EMPTY_DECK"
	ErrCodeInvalidPlayer          ErrorCode = "ERR_INVALID_PLAYER"
	ErrCodeNotYourTurn            ErrorCode = "NOT_YOUR_TURN"
	ErrCodeCardNotOwned           ErrorCode = "CARD_NOT_OWNED"
	ErrCodeCardNotPlayable        ErrorCode = "CARD_NOT_PLAYABLE"
	ErrCodeInvalidPhase           ErrorCode = "INVALID_PHASE"
	ErrCodeInvalidColor           ErrorCode = "INVALID_COLOR"
	ErrCodeGameFinished           ErrorCode = "ERR_GAME_FINISHED"
	ErrCodeWildDrawFourInvalid    ErrorCode = "WILD_DRAW_FOUR_NOT_ALLOWED"
	ErrCodeNoDrawnCard            ErrorCode = "NO_DRAWN_CARD"
	ErrCodeCardNotDrawn           ErrorCode = "CARD_NOT_DRAWN"
	ErrCodeInvalidConfig          ErrorCode = "CONFIG_INVALID"
	ErrCodeActionFinishNotAllowed ErrorCode = "ACTION_FINISH_DISALLOWED"
	ErrCodeChallengeNotAllowed    ErrorCode = "CHALLENGE_NOT_ALLOWED"
	ErrCodeChallengeNoFour        ErrorCode = "NO_WILD_DRAW_FOUR_TO_CHALLENGE"
)

// GameError is a stable, typed engine error carrying a machine-readable code
// and a human-oriented message.
type GameError struct {
	Code    ErrorCode
	Message string
}

func (e *GameError) Error() string { return string(e.Code) + ": " + e.Message }

func newErr(code ErrorCode, format string, args ...interface{}) *GameError {
	return &GameError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// gErr constructs a stable *GameError from a code and format message.
func gErr(code ErrorCode, format string, args ...interface{}) *GameError {
	return &GameError{Code: code, Message: fmt.Sprintf(format, args...)}
}
