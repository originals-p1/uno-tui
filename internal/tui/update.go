package tui

import (
	"os"

	tea "charm.land/bubbletea/v2"
	"terminal-uno/internal/game"
	"terminal-uno/internal/protocol"
)

// Update implements tea.Model: it folds messages into the model and returns a
// command that yields an ActionMsg for the network layer when the user makes
// a request.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = v.Width
		return m, nil
	case SnapshotMsg:
		m.applySnapshot(v.View)
		m.recordFinished()
		return m, tea.Batch(m.bellOnTurn(v.View), m.bellOnJoin(v.View))
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
		m.lastError = v.Message
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
	m.simpleNotice = ""
	if m.Snap != nil && m.Snap.Challenge != nil && m.Snap.CurrentPlayer == m.MyID &&
		v.Challenge == nil && v.ChallengeRes == nil && len(v.YourHand) == len(m.Snap.YourHand)+4 {
		m.simpleNotice = "drew 4 · skip turn"
	}
	m.Snap = v
	// Keep the hand cursor within the (possibly shrunk/grown) hand.
	if m.handSel >= len(v.YourHand) {
		m.handSel = 0
	}
	// A new personal turn begins once we are current and no color pick needed.
	if v.CurrentPlayer == m.MyID {
		// (color pick persists until resolved by the player)
	}
}

// bellOnTurn rings the terminal bell (BEL, \a) exactly once when control moves
// to the local player — a new turn or a pending +4 challenge decision — and not
// while the game is over, paused, or still in the lobby. It writes the bell to
// stderr so it never corrupts the bubbletea-rendered view.
func (m *Model) bellOnTurn(v *game.GameView) tea.Cmd {
	if v == nil {
		return nil
	}
	if v.WinnerPlayerID != "" || v.Phase == string(game.PhaseFinished) ||
		v.Paused != nil || v.Phase == string(game.PhaseLobby) {
		m.rung = false
		return nil
	}
	if v.CurrentPlayer != m.MyID {
		m.rung = false
		return nil
	}
	if m.rung {
		return nil // already beeped for this turn
	}
	m.rung = true
	if m.simple {
		return nil
	}
	return bellCmd()
}

// bellCmd emits a terminal bell and returns no message.
func bellCmd() tea.Cmd {
	return func() tea.Msg {
		_, _ = os.Stderr.Write([]byte("\a"))
		return nil
	}
}

// bellOnJoin rings the terminal bell (BEL, \a) exactly once when a new player
// appears in the lobby, so a host hears an arrival without staring at the
// screen. The first snapshot just seeds the known-player set; subsequent joins
// chime once each. It only fires while the game is still in the lobby.
func (m *Model) bellOnJoin(v *game.GameView) tea.Cmd {
	if v == nil {
		return nil
	}
	present := make(map[string]bool, len(v.Players))
	for _, p := range v.Players {
		present[p.ID] = true
	}
	if m.knownPlayers == nil {
		m.knownPlayers = present
		return nil
	}
	newArrival := false
	for id := range present {
		if !m.knownPlayers[id] {
			newArrival = true
			break
		}
	}
	m.knownPlayers = present
	if m.simple || !newArrival || v.Phase != string(game.PhaseLobby) {
		return nil
	}
	return bellCmd()
}

// recordFinished persists the outcome of a completed game exactly once, keyed
// by the final event id (which is unique to the winning move). Silence on any
// error: history is best-effort and must never block rendering.
func (m *Model) recordFinished() {
	v := m.Snap
	if v == nil || v.Phase != string(game.PhaseFinished) || v.WinnerPlayerID == "" {
		return
	}
	if v.LastEventID != 0 && v.LastEventID == m.recordedLastEventID {
		return // already recorded this finish
	}
	m.matchSeq++
	rec := recordFromView(v, m.matchSeq, m.ConfigVersion)
	m.history = appendAndSave(m.history, rec)
	if v.LastEventID != 0 {
		m.recordedLastEventID = v.LastEventID
	}
}

func (m Model) onKey(k string) (tea.Model, tea.Cmd) {
	if k == "m" {
		m.simple = !m.simple
		return m, nil
	}
	m.lastError = ""
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
	// While choosing a wild card color, q/esc cancels the choice (keeps the
	// card) instead of the global q-as-back / quit behaviour.
	if m.picking.Active && (k == "q" || k == "esc") {
		m.picking = CardPick{}
		return m, nil
	}
	if k == "t" {
		if !m.hist {
			m.histSel = -1 // open at the list
		}
		m.hist = !m.hist
		return m, nil
	}
	if k == "q" {
		// q is a context-aware back key: it closes the topmost overlay (help,
		// rules view, or history) before ever quitting the app.
		if m.hist {
			if m.histSel >= 0 {
				m.histSel = -1 // back to history list first
				return m, nil
			}
			m.hist = false
			return m, nil
		}
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
			// A drawn wild / +4 needs a chosen color before it can be played.
			if m.Snap != nil && m.Snap.DrawDecision != nil && game.IsWild(m.Snap.DrawDecision.Card.Rank) {
				m.picking = CardPick{Active: true, CardID: m.Snap.DrawDecision.Card.ID, IsDrawn: true}
				return m, nil
			}
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
		// Combined stacking + challenge: the first +4 may be re-stacked with a
		// +2/+4 (starting an accumulating, non-challengeable pile). Route the
		// same hand-selection hotkeys used in the stack phase.
		switch k {
		case "up", "k":
			nm := m
			nm.moveHandSel(-1)
			return nm, nil
		case "down", "j":
			nm := m
			nm.moveHandSel(1)
			return nm, nil
		case "enter":
			return m.playHandAt(m.handSel)
		}
		return m.onTurnNumber(k)
	case ModeStack:
		// During a stacking penalty you may play a +2/+4 to keep stacking or
		// accept with "a". The +4 challenge is disabled while stacking, but we
		// keep the "c" hook guarded by CanChallenge (always false) defensively.
		if k == "a" {
			return m, actCmd(ActAcceptStack)
		}
		if k == "c" && m.Snap != nil && m.Snap.Stack != nil && m.Snap.Stack.CanChallenge {
			return m, actCmd(ActChallengeStack)
		}
		switch k {
		case "up", "k":
			nm := m
			nm.moveHandSel(-1)
			return nm, nil
		case "down", "j":
			nm := m
			nm.moveHandSel(1)
			return nm, nil
		case "enter":
			return m.playHandAt(m.handSel)
		}
		return m.onTurnNumber(k)
	case ModeRoundOver:
		// SCORE round finished: any player presses "c" to start the next round.
		if k == "c" {
			return m, actCmd(ActAdvanceRound)
		}
	case ModeRules:
		if k == "e" && m.IsHost && !m.RulesLocked {
			m.edit = true
			m.editSel = 0
			m.editMsg = ""
		}
	case ModeHistory:
		// In the list view, digits select a record to expand its replay log.
		if m.histSel < 0 {
			if idx, ok := digitIndex(k); ok && idx < len(m.history) {
				m.histSel = idx
			}
		}
	case ModeConfigEdit:
		return m.onConfigEditKey(k)
	case ModeTurn:
		if k == "d" || k == " " {
			return m, actCmd(ActDraw)
		}
		// Hand-cursor navigation for large hands (>10 cards).
		switch k {
		case "up", "k":
			nm := m
			nm.moveHandSel(-1)
			return nm, nil
		case "down", "j":
			nm := m
			nm.moveHandSel(1)
			return nm, nil
		case "enter":
			return m.playHandAt(m.handSel)
		}
		return m.onTurnNumber(k)
	case ModeFinished:
		// b / back: return the finished room to a fresh lobby for another round.
		if k == "b" || k == "backspace" {
			return m, actCmd(ActRematch)
		}
	}
	return m, nil
}

func (m Model) onTurnNumber(k string) (tea.Model, tea.Cmd) {
	idx, ok := digitIndex(k)
	if !ok {
		return m, nil
	}
	return m.playHandAt(idx)
}

// playHandAt starts playing the hand card at index idx (0-based) if it is
// within range and it is the player's turn. Wild cards enter the color-picker
// instead of being played directly.
func (m Model) playHandAt(idx int) (tea.Model, tea.Cmd) {
	if !m.isMyTurn() || m.Snap == nil || idx < 0 || idx >= len(m.Snap.YourHand) {
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

// moveHandSel moves the hand-cursor by delta (wrapping) so the selected card
// always has a visible ▶ marker, regardless of hand size.
func (m *Model) moveHandSel(delta int) {
	if m.Snap == nil || len(m.Snap.YourHand) == 0 {
		return
	}
	n := len(m.Snap.YourHand)
	m.handSel = ((m.handSel+delta)%n + n) % n
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
	isDrawn := m.picking.IsDrawn
	m.picking = CardPick{}
	// A just-drawn wild card is played with play_drawn, not a hand play.
	if isDrawn {
		return m, action(ActPlayDrawn, id, col)
	}
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
