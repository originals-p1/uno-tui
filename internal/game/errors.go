package game

import "fmt"

// Stable business error codes. These are part of the engine contract and
// must not change meaning; clients/UI must branch on these codes, never on
// human-readable text (readme §15).
type ErrorCode string

const (
	ErrCodeNoInitialNumberCard ErrorCode = "ERR_NO_INITIAL_NUMBER_CARD"
	ErrCodeInvalidPlayerCount  ErrorCode = "ERR_INVALID_PLAYER_COUNT"
	ErrCodeInvalidArgument     ErrorCode = "ERR_INVALID_ARGUMENT"
	ErrCodeEmptyDeck           ErrorCode = "ERR_EMPTY_DECK"
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
