package tui

import (
	tea "charm.land/bubbletea/v2"
	"terminal-uno/internal/game"
)

// Mode is the current high-level screen.
type Mode int

const (
	ModeLobby Mode = iota
	ModeTurn
	ModeDrawnDecision
	ModeColor
	ModePaused
	ModeFinished
)

// Model is the terminal UNO TUI model. It keeps only public, privacy-safe
// data (own hand from the personalized view) plus identity for hotkeys.
type Model struct {
	Name   string
	MyID   string
	IsHost bool

	Snap *game.GameView
	help bool

	// picking records a wild card awaiting a color choice (ModeColor).
	picking CardPick
}

// CardPick is a wild card that needs a color selection.
type CardPick struct {
	Active  bool
	CardID  string
	IsDrawn bool // color-for-playdrawn
}

// fmtCards returns the rendered own-hand cards.
type ownCard struct {
	ID       string
	Text     string
	Playable bool
}

// NewModel builds a fresh lobby TUI.
func NewModel(name string) Model {
	return Model{Name: name, help: false}
}

// Init satisfies tea.Model; no startup command.
func (m Model) Init() tea.Cmd { return nil }

// mode derives the current screen from the snapshot.
func (m Model) mode() Mode {
	s := m.Snap
	if s == nil {
		return ModeLobby
	}
	if s.WinnerPlayerID != "" || s.Phase == string(game.PhaseFinished) {
		return ModeFinished
	}
	if s.Paused != nil {
		return ModePaused
	}
	if m.picking.Active {
		return ModeColor
	}
	if s.DrawDecision != nil && s.CurrentPlayer == m.MyID {
		return ModeDrawnDecision
	}
	if s.CurrentPlayer == m.MyID {
		return ModeTurn
	}
	return ModeTurn
}

// started reports whether a game is in progress.
func (m Model) started() bool {
	return m.Snap != nil && (m.Snap.CurrentPlayer != "" || len(m.Snap.Players) > 0 && m.Snap.Phase != "")
}
