package tui

import (
	tea "charm.land/bubbletea/v2"
	"terminal-uno/internal/game"
	"terminal-uno/internal/protocol"
)

// Update implements tea.Model: it folds messages into the model and returns a
// command that yields an ActionMsg for the network layer when the user makes
// a request.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case SnapshotMsg:
		m.applySnapshot(v.View)
		return m, nil
	case HelpMsg:
		m.help = !m.help
		return m, nil
	case ConfigMsg:
		c := v.Config
		m.Config, m.ConfigVersion, m.RulesLocked = &c, v.ConfigVersion, v.Locked
		if m.editMsg == "已提交，等待确认…" {
			// Server already confirmed via a fresh snapshot; leave editor.
			m.edit = false
			m.editMsg = ""
		}
		return m, nil
	case ErrorMsg:
		if v.Code == protocol.CodeConfigStale && m.edit {
			m.editMsg = "规则版本已过期（别人改过），请按 s 重新保存。"
		}
		return m, nil
	case KeyMsg:
		return m.onKey(v.Key)
	default:
		return m, nil
	}
}

func (m *Model) applySnapshot(v *game.GameView) {
	if v == nil {
		return
	}
	m.Snap = v
	// A new personal turn begins once we are current and no color pick needed.
	if v.CurrentPlayer == m.MyID {
		// (color pick persists until resolved by the player)
	}
}

func (m Model) onKey(k string) (tea.Model, tea.Cmd) {
	if k == "h" || k == "?" {
		m.help = !m.help
		return m, nil
	}
	if m.help {
		// In the help overlay, q (and h/?) closes it; all other keys are ignored.
		if k == "q" {
			m.help = false
		}
		return m, nil
	}
	// Route editor keys before the generic hotkeys so 'v'/'q'/navigation all
	// stay inside the config editor.
	if m.edit && m.Config != nil {
		return m.onConfigEditKey(k)
	}
	if k == "q" {
		// q is a context-aware back key: it closes the topmost overlay (help or
		// rules view) before ever quitting the app.
		if m.rules {
			m.rules = false
			return m, nil
		}
		return m, tea.Quit
	}
	if k == "l" {
		return m, actCmd(ActLeave)
	}
	if k == "v" {
		m.rules = !m.rules
		return m, nil
	}

	switch m.mode() {
	case ModeLobby:
		switch k {
		case "r", " ":
			return m, actCmd(ActReady)
		case "s":
			if m.IsHost {
				return m, actCmd(ActStartGame)
			}
		}
	case ModeColor:
		return m.onColorKey(k)
	case ModeDrawnDecision:
		switch k {
		case "p", "enter":
			return m, actCmd(ActPlayDrawn)
		case "g", " ":
			return m, actCmd(ActPassDrawn)
		}
	case ModeChallenge:
		switch k {
		case "c":
			return m, actCmd(ActChallengeFour)
		case "a":
			return m, actCmd(ActAcceptFour)
		}
	case ModeRules:
		if k == "e" && m.IsHost && !m.RulesLocked {
			m.edit = true
			m.editSel = 0
			m.editMsg = ""
		}
	case ModeConfigEdit:
		return m.onConfigEditKey(k)
	case ModeTurn:
		if k == "d" || k == " " {
			return m, actCmd(ActDraw)
		}
		return m.onTurnNumber(k)
	}
	return m, nil
}

func (m Model) onTurnNumber(k string) (tea.Model, tea.Cmd) {
	idx, ok := digitIndex(k)
	if !ok || m.Snap == nil || idx >= len(m.Snap.YourHand) {
		return m, nil
	}
	if m.mode() != ModeTurn {
		return m, nil
	}
	card := m.Snap.YourHand[idx]
	if game.IsWild(card.Rank) {
		// Wild card: enter color-pick before playing.
		m.picking = CardPick{Active: true, CardID: card.ID}
		return m, nil
	}
	return m, actCmdCol(ActPlayCard, card.ID, "")
}

func (m Model) onConfigEditKey(k string) (tea.Model, tea.Cmd) {
	switch k {
	case "up", "k":
		if m.editSel > 0 {
			m.editSel--
		}
	case "down", "j":
		if m.editSel < len(cfgItems)-1 {
			m.editSel++
		}
	case "left", "-":
		m.cfgStepValue(-1)
	case "right", "+", " ":
		m.cfgStepValue(1)
	case "s", "enter":
		if m.Config == nil {
			m.editMsg = "无配置可保存"
			break
		}
		cp := *m.Config
		m.editMsg = "已提交，等待确认…"
		return m, func() tea.Msg { return ActionMsg{Kind: ActApplyConfig, Config: &cp} }
	case "v", "c", "q", "esc":
		m.edit = false
		m.editMsg = ""
	}
	return m, nil
}

func (m Model) onColorKey(k string) (tea.Model, tea.Cmd) {
	if !m.picking.Active {
		return m, nil
	}
	col := colorKey(k)
	if col == "" {
		return m, nil
	}
	id := m.picking.CardID
	m.picking = CardPick{}
	return m, actCmdCol(ActPlayCard, id, col)
}

// digitIndex maps "1".."9","0" to hand index 0..9.
func digitIndex(k string) (int, bool) {
	if len(k) != 1 || k < "0" || k > "9" {
		return 0, false
	}
	n := int(k[0] - '0')
	if n == 0 {
		n = 10
	}
	return n - 1, true
}

func colorKey(k string) string {
	switch k {
	case "r":
		return "red"
	case "y":
		return "yellow"
	case "g":
		return "green"
	case "b":
		return "blue"
	}
	return ""
}

func actCmd(k ActionKind) tea.Cmd {
	return action(k, "", "")
}

func actCmdCol(k ActionKind, card, col string) tea.Cmd {
	return action(k, card, col)
}

func action(k ActionKind, card, col string) tea.Cmd {
	return func() tea.Msg {
		return ActionMsg{Kind: k, CardID: card, ChosenColor: col}
	}
}
