package main

import (
	tea "charm.land/bubbletea/v2"
	"terminal-uno/internal/client"
	"terminal-uno/internal/game"
	"terminal-uno/internal/protocol"
	"terminal-uno/internal/tui"
)

// appModel couples the Bubble Tea TUI to the connected client. Network events
// (client snapshots) are pushed in as tui.SnapshotMsg; user hotkeys become
// tui.ActionMsg which are forwarded to the client (whose legality the TUI never
// decides).
type appModel struct {
	tui tui.Model
	c   *client.Client
}

func newAppModel(c *client.Client, name string) appModel {
	id := c.Identity()
	tm := tui.NewModel(name)
	tm.MyID = id.PlayerID
	tm.IsHost = id.IsHost
	return appModel{tui: tm, c: c}
}

// Init satisfies tea.Model. It emits the snapshot already received during the
// handshake (Connect consumed the Welcome snapshot before OnSnapshot was wired
// up), so the lobby is rendered immediately instead of starting blank.
func (m appModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	if s := m.c.Snapshot(); s != nil {
		cmds = append(cmds, func() tea.Msg { return tui.SnapshotMsg{View: s} })
	}
	// CONFIG_SNAPSHOT is delivered right after welcome, before runInteractive
	// installs OnConfig, so it can be dropped. Re-emit the cached copy here so
	// the rules view ("v") always has a config to render.
	if cs, ok := m.c.ConfigSnapshot(); ok {
		cmds = append(cmds, func() tea.Msg {
			return tui.ConfigMsg{Config: cs.Config, ConfigVersion: cs.ConfigVersion, Locked: cs.Locked}
		})
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyPressMsg:
		return m.forward(tui.KeyMsg{Key: normalizeKey(v)})
	case tui.SnapshotMsg, tui.HelpMsg, tui.ErrorMsg, tui.ConfigMsg:
		return m.forward(v)
	case tui.ActionMsg:
		go m.dispatch(v)
		return m, nil
	default:
		return m, nil
	}
}

// forward folds a tui message into the embedded model, preserving any command.
func (m appModel) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	inner, cmd := m.tui.Update(msg)
	m.tui = inner.(tui.Model)
	return m, cmd
}

func (m appModel) View() tea.View { return m.tui.View() }

// normalizeKey translates a bubbletea key into the TUI's normalized key string.
func normalizeKey(k tea.KeyPressMsg) string {
	s := k.String()
	switch s {
	case "space":
		return " "
	case "ctrl+c":
		return "ctrl_c"
	}
	return s
}

// dispatch forwards a user action request to the client. All legality is
// enforced by the server; errors just fall through to the next snapshot.
func (m appModel) dispatch(a tui.ActionMsg) {
	c := m.c
	rev := c.Revision()
	col := func(s string) *game.Color {
		if s == "" {
			return nil
		}
		cc := game.Color(s)
		return &cc
	}
	switch a.Kind {
	case tui.ActPlayCard:
		_, _ = c.SendAction(game.Action{Type: game.ActionPlayCard, CardID: a.CardID, ChosenColor: col(a.ChosenColor)}, rev)
	case tui.ActDraw:
		_, _ = c.SendAction(game.Action{Type: game.ActionDrawCard}, rev)
	case tui.ActPlayDrawn:
		_, _ = c.SendAction(game.Action{Type: game.ActionPlayDrawn, ChosenColor: col(a.ChosenColor)}, rev)
	case tui.ActPassDrawn:
		_, _ = c.SendAction(game.Action{Type: game.ActionPassDrawn}, rev)
	case tui.ActAcceptFour:
		_, _ = c.SendAction(game.Action{Type: game.ActionAcceptFour}, rev)
	case tui.ActChallengeFour:
		_, _ = c.SendAction(game.Action{Type: game.ActionChallengeFour}, rev)
	case tui.ActAcceptStack:
		_, _ = c.SendAction(game.Action{Type: game.ActionAcceptStack}, rev)
	case tui.ActChallengeStack:
		_, _ = c.SendAction(game.Action{Type: game.ActionChallengeStack}, rev)
	case tui.ActAdvanceRound:
		_, _ = c.AdvanceRound(rev)
	case tui.ActReady:
		_, _ = c.Ready(true, rev)
	case tui.ActStartGame:
		_, _ = c.StartGame(rev)
	case tui.ActApplyConfig:
		if a.Config == nil {
			break
		}
		_, _ = c.UpdateConfig(*a.Config, m.tui.ConfigVersion, rev)
		// Persist the host's applied rules so they survive a restart.
		_ = saveRulesConfig(*a.Config)
	case tui.ActLeave:
		_, _ = c.Leave(rev)
	case tui.ActRematch:
		_, _ = c.Rematch(rev)
	}
}

// runInteractive launches the Bubble Tea program with the client wired in. It
// blocks until the user quits and then returns.
func runInteractive(c *client.Client, name string) error {
	m := newAppModel(c, name)
	p := tea.NewProgram(m)
	c.OnSnapshot = func(v *game.GameView) { p.Send(tui.SnapshotMsg{View: v}) }
	c.OnError = func(code, message string) { p.Send(tui.ErrorMsg{Code: code, Message: message}) }
	c.OnConfig = func(cs protocol.ConfigSnapshot) {
		p.Send(tui.ConfigMsg{Config: cs.Config, ConfigVersion: cs.ConfigVersion, Locked: cs.Locked})
	}
	_, err := p.Run()
	if err != nil {
		return err
	}
	return nil
}
