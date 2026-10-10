// Package tui implements the Bubble Tea terminal UI (readme §8). Network and
// game events arrive as tea.Msg and are turned into pure state transitions by
// Update; View renders the screen. All game legality is decided by the server:
// the TUI only *requests* actions.
package tui

import "terminal-uno/internal/game"

// SnapshotMsg carries a fresh personalized game view from the room.
type SnapshotMsg struct {
	View *game.GameView
}

// ErrorMsg is a server error surfaced to the user.
type ErrorMsg struct {
	Code    string
	Message string
}

// HelpMsg toggles the help overlay.
type HelpMsg struct{}

// KeyMsg is a normalized keyboard input ("1".."9", "d", "p", "g", "r", "s",
// "q", "h", "l", "ctrl_c", …). Keeping keys as strings decouples the model
// from the terminal decoder and makes hotkeys trivially testable.
type KeyMsg struct {
	Key string
}

// ActionKind enumerates the user-initiated requests the TUI can forward to the
// network layer. Legality is validated server-side.
type ActionKind int

const (
	ActNone ActionKind = iota
	ActPlayCard
	ActDraw
	ActPlayDrawn
	ActPassDrawn
	ActReady
	ActStartGame
	ActLeave
)

// ActionMsg is the outcome of a user hotkey: a request for the network layer
// to send. In a running program this Msg is intercepted by the runner and
// turned into a client call; it never decides legality itself.
type ActionMsg struct {
	Kind        ActionKind
	CardID      string
	ChosenColor string
	Ready       bool
}

// String returns a stable, machine-readable name for the action kind.
func (k ActionKind) String() string {
	switch k {
	case ActPlayCard:
		return "play_card"
	case ActDraw:
		return "draw"
	case ActPlayDrawn:
		return "play_drawn"
	case ActPassDrawn:
		return "pass_drawn"
	case ActReady:
		return "ready"
	case ActStartGame:
		return "start_game"
	case ActLeave:
		return "leave"
	default:
		return "none"
	}
}
