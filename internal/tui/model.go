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
	ModeChallenge
	ModeRules
	ModeConfigEdit
	ModeHistory
)

// Model is the terminal UNO TUI model. It keeps only public, privacy-safe
// data (own hand from the personalized view) plus identity for hotkeys.
type Model struct {
	Name   string
	MyID   string
	IsHost bool

	Snap  *game.GameView
	help  bool
	rules bool

	Config        *game.Config
	ConfigVersion uint64
	RulesLocked   bool

	// host rule editor (ModeConfigEdit)
	edit    bool // editing the config (host)
	editSel int  // selected row in the editor
	editMsg string

	// picking records a wild card awaiting a color choice (ModeColor).
	picking CardPick

	// match history (persisted locally, ModeHistory).
	history             []MatchRecord
	hist                bool // history modal open
	histSel             int  // selected record index (>=0 => detail/replay view)
	matchSeq            int64
	recordedLastEventID uint64

	// rung tracks whether the terminal bell has fired for the current *active*
	// ownership (your turn / your +4 decision). It is cleared whenever the ball
	// is no longer in your court, so the bell rings exactly once per transition.
	rung bool

	// knownPlayers is the set of player IDs present in the last snapshot. It lets
	// the banner/audio layer detect when someone new joins the lobby so the
	// terminal bell can chime once per arrival.
	knownPlayers map[string]bool

	// handSel is the cursor index into YourHand, used to select and play cards
	// when the hand is too large for the single-digit (1-9/0) shortcuts.
	handSel int
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

// NewModel builds a fresh lobby TUI, loading any local match history.
func NewModel(name string) Model {
	return Model{Name: name, help: false, history: loadHistory(), histSel: -1, knownPlayers: map[string]bool{}}
}

// Init satisfies tea.Model; no startup command.
func (m Model) Init() tea.Cmd { return nil }

// mode derives the current screen from the snapshot.
func (m Model) mode() Mode {
	s := m.Snap
	if s == nil {
		return ModeLobby
	}
	if m.edit && m.Config != nil {
		return ModeConfigEdit
	}
	// Rules view does not depend on a loaded config: renderRules defensively
	// shows "尚无配置" so pressing "v" always gives visible feedback.
	if m.rules {
		return ModeRules
	}
	if m.hist {
		return ModeHistory
	}
	if s.Phase == string(game.PhaseLobby) {
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
	if s.Challenge != nil && s.CurrentPlayer == m.MyID {
		return ModeChallenge
	}
	if s.DrawDecision != nil && s.CurrentPlayer == m.MyID {
		return ModeDrawnDecision
	}
	return ModeTurn
}

// started reports whether a game is in progress.
func (m Model) started() bool {
	return m.Snap != nil && (m.Snap.CurrentPlayer != "" || len(m.Snap.Players) > 0 && m.Snap.Phase != "")
}
