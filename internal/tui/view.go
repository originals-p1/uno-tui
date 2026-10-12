package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lip "charm.land/lipgloss/v2"
	"terminal-uno/internal/game"
)

// RenderOpts controls rendering (injected for tests).
type RenderOpts struct {
	Color   bool
	Unicode bool
	Narrow  bool
}

// defaultRenderOpts derives opts from the environment.
func defaultRenderOpts() RenderOpts {
	return RenderOpts{Color: colorEnabled(), Unicode: true}
}

// View implements tea.Model.
func (m Model) View() tea.View {
	// Full-screen: draw into the alternate screen buffer. Without it the inline
	// renderer rewrites the whole screen every frame, which on Windows (cmd /
	// legacy conhost) shows as constant reflushing and ghosted stacked copies
	// ("multiple terminals"). AltScreen gives a clean full-window buffer.
	v := tea.NewView(m.render(defaultRenderOpts()))
	v.AltScreen = true
	return v
}

// render produces the full screen for the current state.
func (m Model) render(o RenderOpts) string {
	var b strings.Builder
	if m.help {
		b.WriteString(m.renderHelp(o))
		return b.String()
	}
	b.WriteString(m.renderHeader(o))
	b.WriteString("\n\n")
	b.WriteString(m.renderBody(o))
	b.WriteString("\n\n")
	b.WriteString(m.renderFooter(o))
	return b.String()
}

func (m Model) renderHeader(o RenderOpts) string {
	title := "终端 UNO"
	if o.Color {
		title = titleStyle(title)
	}
	return title
}

func (m Model) renderBody(o RenderOpts) string {
	s := m.Snap
	if s == nil {
		return renderEmptyLobby(m, o)
	}
	banner := ""
	if m.mode() != ModeChallenge && m.mode() != ModeRules && m.mode() != ModeConfigEdit {
		banner = m.challengeResultBanner(o) + m.scoreBanner(o)
	}
	switch m.mode() {
	case ModeLobby:
		return banner + m.renderLobby(o)
	case ModePaused:
		return banner + m.renderPaused(o)
	case ModeFinished:
		return banner + m.renderFinished(o)
	case ModeColor:
		return banner + m.renderColorPanel(o)
	case ModeDrawnDecision:
		return banner + m.renderDrawDecision(o)
	case ModeChallenge:
		return m.renderChallenge(o)
	case ModeStack:
		return banner + m.renderStack(o)
	case ModeRoundOver:
		return m.renderRoundOver(o)
	case ModeRules:
		return m.renderRules(o)
	case ModeHistory:
		return m.renderHistory(o)
	case ModeConfigEdit:
		return m.renderConfigEdit(o)
	default:
		return banner + m.renderGame(o)
	}
}

func renderEmptyLobby(m Model, o RenderOpts) string {
	var b strings.Builder
	b.WriteString("正在连接大厅…\n")
	b.WriteString("  (r) 准备/就坐   (s) 开始（房主）   (l) 离开   (h) 帮助\n")
	return b.String()
}

// renderLobby shows the player roster plus ready/start actions while a game has
// not begun. The lobby snapshot carries no top card or hand, so it is rendered
// separately from the game view.
func (m Model) renderLobby(o RenderOpts) string {
	var b strings.Builder
	b.WriteString("玩家：\n")
	if m.Snap != nil {
		for _, p := range m.Snap.Players {
			b.WriteString("  " + playerLine(m, p, o) + "\n")
		}
	}
	b.WriteString("\n等待玩家准备…\n")
	b.WriteString("  (r) 准备   (s) 开始（房主）   (l) 离开\n")
	return b.String()
}

func (m Model) renderGame(o RenderOpts) string {
	var b strings.Builder
	s := m.Snap
	// Score-match header (rule.md R28).
	if s.WinMode == game.WinScore {
		b.WriteString(m.renderScoreLine(o))
	}
	// Players summary.
	b.WriteString("玩家：\n")
	for _, p := range s.Players {
		b.WriteString("  " + playerLine(m, p, o) + "\n")
	}
	// Board info.
	if s.Stack != nil && !s.Stack.YourDecision {
		b.WriteString(fmt.Sprintf("\n⚠ %s 正被叠加惩罚（累计 %d 张，等待其出牌或接受）\n",
			m.displayName(s.CurrentPlayer), s.Stack.Credits))
	}
	b.WriteString(fmt.Sprintf("\n轮到：%s （出牌方向 %s）\n", m.displayName(s.CurrentPlayer), dirArrow(s.Direction, o)))
	b.WriteString(m.renderTurnOrder(o))
	b.WriteString(fmt.Sprintf("桌面：%s   当前颜色：%s\n\n", cardText(s.TopCard), colorNameZh(s.ActiveColor)))
	// Own hand (stable order). Framed with a bright border on your turn so the
	// need to act is unmissable.
	hand := m.renderHand(o)
	if m.isMyTurn() {
		hand = m.highlightHand(hand, o)
	}
	b.WriteString(hand)
	return b.String()
}

func (m Model) renderHand(o RenderOpts) string {
	if m.Snap == nil {
		return ""
	}
	hand := m.Snap.YourHand
	playable := map[string]bool{}
	for _, id := range m.Snap.PlayableIDs {
		playable[id] = true
	}
	big := len(hand) > 10
	var b strings.Builder
	if m.isMyTurn() {
		b.WriteString("你的手牌（按数字出牌，d 摸牌）：\n")
		if big {
			b.WriteString("  （手牌较多：↑/↓ 移动 ▶ 光标，回车打出选中牌）\n")
		}
	} else {
		b.WriteString("你的手牌：\n")
	}
	if len(hand) == 0 {
		b.WriteString("  （空）\n")
		return b.String()
	}
	if o.Narrow {
		// Compact single-line hand for narrow terminals.
		items := make([]string, 0, len(hand))
		for _, c := range hand {
			items = append(items, cardText(c))
		}
		b.WriteString("  " + strings.Join(items, "  ") + "\n")
		return b.String()
	}
	sel := "▶ "
	if !o.Unicode {
		sel = ">  "
	}
	for i, c := range hand {
		num := fmt.Sprintf("%d", i+1)
		if i == 9 {
			num = "0"
		}
		cursor := "  "
		if m.handSel == i {
			cursor = sel
		}
		line := fmt.Sprintf("%s%s [%s] %s", cursor, cardColorIcon(c, o.Color), num, cardText(c))
		if playable[c.ID] {
			line += "  <- 可出"
			if o.Color {
				line = playableStyle(line)
			}
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// isMyTurn reports whether it is this player's turn to act on their hand
// (not merely that a game is in progress).
func (m Model) isMyTurn() bool {
	if m.Snap == nil || m.picking.Active {
		return false
	}
	return m.Snap.CurrentPlayer == m.MyID
}

// highlightHand wraps the hand in a bright border so it is unmistakable that
// it is this player's turn. The border always shows (even without color) using
// ASCII corners; in color terminals the border glows bright green.
func (m Model) highlightHand(text string, o RenderOpts) string {
	if text == "" {
		return text
	}
	st := lip.NewStyle().Border(lip.RoundedBorder())
	if o.Color {
		st = st.BorderForeground(lip.Color("46")).Foreground(lip.Color("46")).Bold(true)
	}
	return st.Padding(0, 1).Render(text)
}

// cardColorIcon renders a small color icon (a filled pip) for a hand card,
// tinted to the card's color in color terminals, showing the card's color at a
// glance (readme §8).
func cardColorIcon(c game.CardView, colored bool) string {
	sym := "●"
	if c.Color == game.ColorWild {
		sym = "◎"
	}
	if colored {
		return lip.NewStyle().Foreground(unoColor(string(c.Color))).Render(sym)
	}
	return sym
}

func (m Model) renderDrawDecision(o RenderOpts) string {
	var b strings.Builder
	b.WriteString("你摸到一张牌：\n\n")
	if m.Snap != nil && m.Snap.DrawDecision != nil {
		b.WriteString("  " + cardText(m.Snap.DrawDecision.Card) + "\n")
	}
	b.WriteString("\n  (p) 打出该牌   (g) 保留并过\n")
	return b.String()
}

func (m Model) renderChallenge(o RenderOpts) string {
	var b strings.Builder
	resolver := "(未知)"
	trigger := ""
	if m.Snap != nil && m.Snap.Challenge != nil {
		if m.Snap.Challenge.ResolverPlayerID != "" {
			resolver = m.displayName(m.Snap.Challenge.ResolverPlayerID)
		}
		trigger = m.Snap.Challenge.TriggerColor
	}
	b.WriteString("对手 " + resolver + " 打出了 +4\n")
	colZh := colorNameZh(trigger)
	if trigger == "" {
		colZh = "未知"
	}
	b.WriteString("判定色（出 +4 前的桌面颜色）：【" + colZh + "】—— 他当时手上若有【" + colZh + "】即违规\n\n")
	b.WriteString("  (c) 挑战 —— 违规：他摸 4、你继续 / 清白：你摸 6、跳过\n")
	b.WriteString("  (a) 接受 —— 你摸 4 张，跳过回合\n")
	return b.String()
}

// renderStack renders the stacking-penalty decision screen (rule.md R19-R21):
// the live board plus the option to re-stack or accept the accumulated penalty.
func (m Model) renderStack(o RenderOpts) string {
	var b strings.Builder
	credits := 0
	if m.Snap != nil && m.Snap.Stack != nil {
		credits = m.Snap.Stack.Credits
	}
	b.WriteString(fmt.Sprintf("你被罚摸 %d 张（叠加中）。\n", credits))
	b.WriteString("  (1-9/0 选中可叠加牌打出) —— 传递 +2/+4 继续累加\n")
	b.WriteString("  (a) 接受惩罚 —— 摸 " + fmt.Sprintf("%d", credits) + " 张并跳过\n")
	b.WriteString("\n")
	b.WriteString(m.renderGame(o))
	return b.String()
}

// challengeResultBanner renders the transient outcome of a +4 challenge.
func (m Model) challengeResultBanner(o RenderOpts) string {
	if m.Snap == nil || m.Snap.ChallengeRes == nil {
		return ""
	}
	cr := m.Snap.ChallengeRes
	resolver := m.displayName(cr.ResolverPlayerID)
	challenger := m.displayName(cr.ChallengerPlayerID)
	colZh := colorNameZh(cr.TriggerColor)
	if cr.TriggerColor == "" {
		colZh = "未知"
	}
	var b strings.Builder
	if cr.Succeeded {
		line := "挑战成功：" + resolver + " 出 +4 时手里握有判定色【" + colZh + "】= 违规，摸 4 张；" + challenger + " 回合继续。"
		if o.Color {
			b.WriteString(successStyle(line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	} else {
		line := "挑战失败：" + challenger + " 摸了 6 张牌并被跳过（" + resolver + " 出 +4 时未握判定色【" + colZh + "】= 合法）。"
		if o.Color {
			b.WriteString(failStyle(line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
}

// renderTurnOrder renders a compact, arrow-joined line showing the cyclic order
// of play starting from the current player, in the current direction. It gives
// an at-a-glance "who comes next in which direction" hint during a game.
func (m Model) renderTurnOrder(o RenderOpts) string {
	s := m.Snap
	if s == nil || len(s.Players) < 2 || s.Phase == string(game.PhaseLobby) {
		return ""
	}
	arrow := dirArrow(s.Direction, o)
	cur := "▶"
	dirWord := "顺时针"
	if s.Direction < 0 {
		dirWord = "逆时针"
	}
	if !o.Unicode {
		cur = ">"
		dirWord = "顺时针"
		if s.Direction < 0 {
			dirWord = "逆时针"
		}
	}
	n := len(s.Players)
	// Locate the current player's seat index so the sequence starts there.
	start := 0
	for i, p := range s.Players {
		if p.ID == s.CurrentPlayer {
			start = i
			break
		}
	}
	seq := make([]string, 0, n)
	for k := 0; k < n; k++ {
		idx := (start + s.Direction*k) % n
		if idx < 0 {
			idx += n
		}
		seq = append(seq, m.displayName(s.Players[idx].ID))
	}
	line := "出牌方向 " + dirWord + "：" + cur + seq[0]
	for _, name := range seq[1:] {
		line += " " + arrow + " " + name
	}
	line += " " + arrow + " " + cur + "回到起点"
	return "\n" + line + "\n"
}

// dirArrow returns the play-direction arrow. The right arrow is always used
// (the player order sequence already reflects the actual clockwise /
// counter-clockwise direction), so it is never confused with leftward arrows.
func dirArrow(direction int, o RenderOpts) string {
	if o.Unicode {
		return "→"
	}
	return ">"
}

// scoreBanner renders the transient "round finished" banner shown at the very
// start of the following round in a SCORE-mode match (rule.md R28). It is
// suppressed on the final/match-over finished screen, which renderFinished
// covers in full.
func (m Model) scoreBanner(o RenderOpts) string {
	s := m.Snap
	if s == nil || s.RoundResult == nil || s.MatchOver {
		return ""
	}
	rr := s.RoundResult
	name := m.displayName(rr.WinnerPlayerID)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("第 %d 回合结束：%s +%d 分。", rr.RoundNumber, name, rr.Points))
	// Show each loser's deduction (based on cards remaining in hand).
	for pid, d := range rr.Deltas {
		if d >= 0 {
			continue
		}
		b.WriteString(fmt.Sprintf(" %s %d", m.displayName(pid), d))
	}
	b.WriteString("\n各家手牌：\n")
	for _, p := range s.Players {
		cards := rr.Hands[p.ID]
		if len(cards) == 0 {
			b.WriteString(fmt.Sprintf("  %s：（已出完，获胜，+%d 分）\n", m.displayName(p.ID), rr.Deltas[p.ID]))
			continue
		}
		items := make([]string, 0, len(cards))
		for _, c := range cards {
			items = append(items, cardText(c))
		}
		sum := 0
		if d, ok := rr.Deltas[p.ID]; ok {
			sum = d
		}
		b.WriteString(fmt.Sprintf("  %s（%+d 分）：%s\n", m.displayName(p.ID), sum, strings.Join(items, "  ")))
	}
	if o.Color {
		return successStyle(b.String()) + "\n"
	}
	return "  " + b.String() + "\n"
}

// renderRoundOver renders the SCORE-mode round-over pause: the just-finished
// round's hands and scores are shown to everyone, and any player presses "c" to
// start the next round (rule.md R28). The match's running score line is included.
func (m Model) renderRoundOver(o RenderOpts) string {
	var b strings.Builder
	s := m.Snap
	if s == nil {
		return ""
	}
	b.WriteString("第 " + fmt.Sprint(s.RoundNumber) + " 局结束，本局小结：\n")
	b.WriteString(m.renderScoreLine(o))
	b.WriteString("\n各家手牌与积分：\n")
	rr := s.RoundResult
	for _, p := range s.Players {
		mark := "  "
		if rr != nil && p.ID == rr.WinnerPlayerID {
			mark = "★ "
		}
		var cards []game.CardView
		if rr != nil {
			cards = rr.Hands[p.ID]
		}
		score := s.Scores[p.ID]
		if len(cards) == 0 {
			b.WriteString(fmt.Sprintf("%s%s（本局获胜）  累计 %d 分\n", mark, m.displayName(p.ID), score))
			continue
		}
		items := make([]string, 0, len(cards))
		for _, c := range cards {
			items = append(items, cardText(c))
		}
		b.WriteString(fmt.Sprintf("%s%s  累计 %d 分：%s\n", mark, m.displayName(p.ID), score, strings.Join(items, "  ")))
	}
	if rr != nil {
		b.WriteString("\n本局得分：")
		for pid, d := range rr.Deltas {
			b.WriteString(fmt.Sprintf(" %s %+d", m.displayName(pid), d))
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderScoreLine renders a one-line score-match status (round number, target,
// and each player's running total) above the roster (rule.md R28).
func (m Model) renderScoreLine(o RenderOpts) string {
	s := m.Snap
	if s == nil {
		return ""
	}
	line := fmt.Sprintf("积分赛 第 %d 局 —— 先到 %d 分获胜：", s.RoundNumber, s.TargetScore)
	parts := make([]string, 0, len(s.Players))
	for _, p := range s.Players {
		parts = append(parts, fmt.Sprintf("%s %d", m.displayName(p.ID), s.Scores[p.ID]))
	}
	line += strings.Join(parts, "   ")
	return line + "\n"
}

func (m Model) renderRules(o RenderOpts) string {
	var b strings.Builder
	b.WriteString("当前规则：\n")
	if m.Config == nil {
		b.WriteString("  （尚无配置）\n")
		return b.String()
	}
	c := m.Config
	b.WriteString(fmt.Sprintf("  版本：v%d%s\n", m.ConfigVersion, lockSuffix(m.RulesLocked)))
	b.WriteString(fmt.Sprintf("  初始手牌：%d 张\n", c.Game.InitialHandSize))
	b.WriteString(fmt.Sprintf("  初始方向：%s\n", dirNameZh(c.Game.InitialDir)))
	b.WriteString(fmt.Sprintf("  先手：%s\n", firstPlayerZh(c.Game.FirstPlayerMode)))
	b.WriteString(fmt.Sprintf("  胜负模式：%s\n", winModeZh(c.Game.WinMode)))
	if c.Game.WinMode == game.WinScore {
		b.WriteString(fmt.Sprintf("  目标积分：%d 分\n", c.Game.TargetScore))
	}
	b.WriteString("  牌组：经典 108 张\n")
	b.WriteString(fmt.Sprintf("  万能：%s\n", onOff(c.Cards.EnableWild)))
	b.WriteString(fmt.Sprintf("  +4：%s\n", onOff(c.Cards.EnableDrawFour)))
	b.WriteString(fmt.Sprintf("  +4 挑战：%s\n", onOff(c.Cards.ChallengeDrawFour)))
	b.WriteString(fmt.Sprintf("  跳牌：%s\n", onOff(c.Cards.EnableSkip)))
	b.WriteString(fmt.Sprintf("  反转：%s\n", onOff(c.Cards.EnableReverse)))
	b.WriteString(fmt.Sprintf("  +2：%s\n", onOff(c.Cards.EnableDrawTwo)))
	b.WriteString(fmt.Sprintf("  自动 UNO：%s\n", onOff(c.HouseRules.AutoUNO)))
	b.WriteString(fmt.Sprintf("  动作牌可收尾：%s\n", onOff(c.Cards.AllowActionFinish)))
	b.WriteString("\n叠加房规：\n")
	b.WriteString(fmt.Sprintf("  +2 叠加：%s\n", onOff(c.HouseRules.StackDrawTwo)))
	b.WriteString(fmt.Sprintf("  +4 叠加：%s\n", onOff(c.HouseRules.StackDrawFour)))
	b.WriteString(fmt.Sprintf("  混合叠加：%s\n", onOff(c.HouseRules.MixedStacking)))
	b.WriteString("\n功能牌说明：\n")
	b.WriteString(fmt.Sprintf("  跳牌（%s）：出牌后跳过下一个玩家的回合\n", onOff(c.Cards.EnableSkip)))
	b.WriteString(fmt.Sprintf("  反转（%s）：出牌后改变出牌方向\n", onOff(c.Cards.EnableReverse)))
	b.WriteString(fmt.Sprintf("  +2（%s）：下一个玩家摸 2 张并跳过回合\n", onOff(c.Cards.EnableDrawTwo)))
	b.WriteString(fmt.Sprintf("  万能（%s）：你指定下一种颜色\n", onOff(c.Cards.EnableWild)))
	b.WriteString(fmt.Sprintf("  +4（%s）：你指定颜色，且下一个玩家摸 4 张\n", onOff(c.Cards.EnableDrawFour)))
	b.WriteString(fmt.Sprintf("  +4 挑战（%s）：以「出 +4 前的桌面颜色」判定——对方当时手里仍握此色则违规\n", onOff(c.Cards.ChallengeDrawFour)))
	b.WriteString(fmt.Sprintf("  动作牌可收尾（%s）：开=动作牌可作为获胜的最后一手；关=最后一张不能是动作牌\n", onOff(c.Cards.AllowActionFinish)))
	if c.HouseRules.StackDrawTwo {
		b.WriteString("  +2 叠加：被 +2 攻击者可再出 +2 把惩罚传下去（张数累加）\n")
	}
	if c.HouseRules.StackDrawFour {
		b.WriteString("  +4 叠加：被 +4 攻击者可再出 +4 把惩罚传下去（每次累加 4 张）\n")
	}
	if c.HouseRules.MixedStacking {
		b.WriteString("  混合叠加：+2 与 +4 可互相叠加接力\n")
	}
	if c.HouseRules.MixedStacking || c.HouseRules.StackDrawFour {
		if c.Cards.ChallengeDrawFour {
			b.WriteString("  叠加+挑战：叠加与挑战互斥——累加中不可挑战，只能继续叠或接受\n")
		}
	}
	b.WriteString("\n  (v) 返回\n")
	return b.String()
}

func lockSuffix(locked bool) string {
	if locked {
		return "（已锁定）"
	}
	return ""
}

func onOff(b bool) string {
	if b {
		return "开"
	}
	return "关"
}

func dirNameZh(d string) string {
	switch d {
	case game.DirCounterClockwise:
		return "逆时针"
	case game.DirRandom:
		return "随机"
	default:
		return "顺时针"
	}
}

func firstPlayerZh(m string) string {
	switch m {
	case game.FirstRandom:
		return "随机"
	case game.FirstHost:
		return "房主先出"
	default:
		return "发牌者之后"
	}
}

// cfgEditItem describes one editable rule and how to cycle (forward/backward)
// among its values.
type cfgEditItem struct {
	label string
	show  func(c *game.Config) string
	next  func(c *game.Config)
	prev  func(c *game.Config)
}

// cfgItems is the host-configurable surface (the v2-CLASSIC supported fields,
// rule.md §13). Cycle order follows the order of the option slices below.
var cfgItems = []cfgEditItem{
	{
		label: "初始手牌数",
		show:  func(c *game.Config) string { return fmt.Sprintf("%d 张", c.Game.InitialHandSize) },
		next:  func(c *game.Config) { c.Game.InitialHandSize = stepInt(c.Game.InitialHandSize, []int{5, 7, 10}, 1) },
		prev:  func(c *game.Config) { c.Game.InitialHandSize = stepInt(c.Game.InitialHandSize, []int{5, 7, 10}, -1) },
	},
	{
		label: "初始方向",
		show:  func(c *game.Config) string { return dirNameZh(c.Game.InitialDir) },
		next: func(c *game.Config) {
			c.Game.InitialDir = stepChoice(c.Game.InitialDir, []string{game.DirClockwise, game.DirCounterClockwise, game.DirRandom}, 1)
		},
		prev: func(c *game.Config) {
			c.Game.InitialDir = stepChoice(c.Game.InitialDir, []string{game.DirClockwise, game.DirCounterClockwise, game.DirRandom}, -1)
		},
	},
	{
		label: "先手",
		show:  func(c *game.Config) string { return firstPlayerZh(c.Game.FirstPlayerMode) },
		next: func(c *game.Config) {
			c.Game.FirstPlayerMode = stepChoice(c.Game.FirstPlayerMode, []string{game.FirstAfterDealer, game.FirstRandom, game.FirstHost}, 1)
		},
		prev: func(c *game.Config) {
			c.Game.FirstPlayerMode = stepChoice(c.Game.FirstPlayerMode, []string{game.FirstAfterDealer, game.FirstRandom, game.FirstHost}, -1)
		},
	},
	toggleItem("跳牌", "skip"),
	toggleItem("反转", "reverse"),
	toggleItem("+2", "draw_two"),
	toggleItem("万能", "wild"),
	toggleItem("+4", "draw_four"),
	toggleItem("+4 挑战", "challenge"),
	toggleItem("动作牌可收尾", "finish"),
	toggleItem("+2 叠加", "stack_two"),
	toggleItem("+4 叠加", "stack_four"),
	toggleItem("混合叠加", "mix_stack"),
	toggleItem("自动 UNO", "auto_uno"),
	{
		label: "胜负模式",
		show:  func(c *game.Config) string { return winModeZh(c.Game.WinMode) },
		next: func(c *game.Config) {
			c.Game.WinMode = stepChoice(c.Game.WinMode, []string{game.WinSingleRound, game.WinScore}, 1)
		},
		prev: func(c *game.Config) {
			c.Game.WinMode = stepChoice(c.Game.WinMode, []string{game.WinSingleRound, game.WinScore}, -1)
		},
	},
	{
		label: "目标积分",
		show:  func(c *game.Config) string { return fmt.Sprintf("%d 分", c.Game.TargetScore) },
		next: func(c *game.Config) {
			c.Game.TargetScore = stepInt(c.Game.TargetScore, []int{100, 200, 500, 1000, 2000}, 1)
		},
		prev: func(c *game.Config) {
			c.Game.TargetScore = stepInt(c.Game.TargetScore, []int{100, 200, 500, 1000, 2000}, -1)
		},
	},
}

// winModeZh renders the win-mode label for the rules/config editor.
func winModeZh(m string) string {
	if m == game.WinScore {
		return "积分赛"
	}
	return "单局"
}

// toggleItem builds a boolean (开/关) editor row bound to one config alias.
func toggleItem(label, alias string) cfgEditItem {
	return cfgEditItem{
		label: label,
		show:  func(c *game.Config) string { return onOff(toggleGet(c, alias)) },
		next:  func(c *game.Config) { toggleSet(c, alias, !toggleGet(c, alias)) },
		prev:  func(c *game.Config) { toggleSet(c, alias, !toggleGet(c, alias)) },
	}
}

func stepInt(cur int, opts []int, dir int) int {
	for i, v := range opts {
		if v == cur {
			return opts[(i+dir+len(opts))%len(opts)]
		}
	}
	return opts[0]
}

func stepChoice(cur string, opts []string, dir int) string {
	for i, v := range opts {
		if v == cur {
			return opts[(i+dir+len(opts))%len(opts)]
		}
	}
	return opts[0]
}

func (m Model) renderConfigEdit(o RenderOpts) string {
	var b strings.Builder
	b.WriteString("房主规则设置：\n\n")
	b.WriteString(fmt.Sprintf("  规则版本 v%d%s\n\n", m.ConfigVersion, lockSuffix(m.RulesLocked)))
	for i, it := range cfgItems {
		cursor := "  "
		if i == m.editSel {
			cursor = "▶ "
		}
		line := fmt.Sprintf("%s%-12s %s", cursor, it.label, it.show(m.Config))
		if o.Color {
			if i == m.editSel {
				line = currentPlayerStyle(line)
			}
		}
		b.WriteString("  " + line + "\n")
	}
	if m.editMsg != "" {
		b.WriteString("\n  " + m.editMsg + "\n")
	}
	b.WriteString("\n  ↑/↓ 选择项目    ←/→ 改变值    s=保存并应用    v=返回\n")
	return b.String()
}

func (m *Model) cfgStepValue(dir int) {
	if m.Config == nil || m.editSel < 0 || m.editSel >= len(cfgItems) {
		return
	}
	it := cfgItems[m.editSel]
	if dir < 0 {
		it.prev(m.Config)
	} else {
		it.next(m.Config)
	}
	m.editMsg = "（未保存）"
}

func toggleGet(c *game.Config, alias string) bool {
	switch alias {
	case "skip":
		return c.Cards.EnableSkip
	case "reverse":
		return c.Cards.EnableReverse
	case "draw_two":
		return c.Cards.EnableDrawTwo
	case "wild":
		return c.Cards.EnableWild
	case "draw_four":
		return c.Cards.EnableDrawFour
	case "challenge":
		return c.Cards.ChallengeDrawFour
	case "finish":
		return c.Cards.AllowActionFinish
	case "stack_two":
		return c.HouseRules.StackDrawTwo
	case "stack_four":
		return c.HouseRules.StackDrawFour
	case "mix_stack":
		return c.HouseRules.MixedStacking
	case "auto_uno":
		return c.HouseRules.AutoUNO
	}
	return false
}

func toggleSet(c *game.Config, alias string, v bool) {
	switch alias {
	case "skip":
		c.Cards.EnableSkip = v
	case "reverse":
		c.Cards.EnableReverse = v
	case "draw_two":
		c.Cards.EnableDrawTwo = v
	case "wild":
		c.Cards.EnableWild = v
	case "draw_four":
		c.Cards.EnableDrawFour = v
	case "challenge":
		// The +4 challenge and stacking may now coexist under the combined
		// stacking + challenge rule (rule.md R22), so nothing is force-disabled.
		c.Cards.ChallengeDrawFour = v
	case "finish":
		c.Cards.AllowActionFinish = v
	case "stack_two":
		c.HouseRules.StackDrawTwo = v
	case "stack_four":
		c.HouseRules.StackDrawFour = v
	case "mix_stack":
		c.HouseRules.MixedStacking = v
		if v {
			// Mixed stacking implies both +2 and +4 stacking (R21).
			c.HouseRules.StackDrawTwo = true
			c.HouseRules.StackDrawFour = true
		}
		if !v && !c.HouseRules.StackDrawFour {
			c.HouseRules.StackDrawTwo = false
		}
	case "auto_uno":
		c.HouseRules.AutoUNO = v
	}
}

func (m Model) renderColorPanel(o RenderOpts) string {
	var b strings.Builder
	b.WriteString("为万能选择颜色：\n\n")
	for _, c := range []string{"red", "yellow", "green", "blue"} {
		k := map[string]string{"red": "r", "yellow": "y", "green": "g", "blue": "b"}[c]
		b.WriteString(fmt.Sprintf("  (%s) %s\n", k, colorNameZh(c)))
	}
	b.WriteString("\n  (q) 返回，取消出牌\n")
	// Show the current color and the live hand for reference while deciding.
	if m.Snap != nil {
		cur := "未知"
		if m.Snap.ActiveColor != "" {
			cur = colorNameZh(m.Snap.ActiveColor)
		}
		b.WriteString(fmt.Sprintf("\n当前颜色：%s\n", cur))
		b.WriteString("你的手牌：\n")
		for i, c := range m.Snap.YourHand {
			num := fmt.Sprintf("%d", i+1)
			if i == 9 {
				num = "0"
			}
			b.WriteString(fmt.Sprintf("  %s [%s] %s\n", cardColorIcon(c, o.Color), num, cardText(c)))
		}
	}
	return b.String()
}

func (m Model) renderPaused(o RenderOpts) string {
	var b strings.Builder
	b.WriteString("已暂停 —— 有玩家掉线\n\n")
	if m.Snap != nil && m.Snap.Paused != nil {
		who := m.Snap.Paused.DisconnectedPlayerIDs
		names := make([]string, 0, len(who))
		for _, id := range who {
			names = append(names, m.displayName(id))
		}
		if len(names) > 0 {
			b.WriteString("  " + strings.Join(names, ", ") + "\n")
		}
		if m.Snap.Paused.EarliestDeadline != "" {
			b.WriteString("  截止时间：" + m.Snap.Paused.EarliestDeadline + "\n")
		}
	}
	b.WriteString("\n等待其重新连接…\n")
	return b.String()
}

func (m Model) renderFinished(o RenderOpts) string {
	var b strings.Builder
	if m.Snap == nil {
		return ""
	}
	// SCORE-mode match over: show the match champion and final scorebook.
	if m.Snap.WinMode == game.WinScore && m.Snap.MatchOver {
		champ := m.displayName(m.Snap.MatchWinnerPlayerID)
		if champ == "" {
			champ = "某人"
		}
		b.WriteString("积分赛结束 — " + champ + " 率先达到 " + fmt.Sprint(m.Snap.TargetScore) + " 分夺冠！\n")
		b.WriteString("\n最终积分（共 " + fmt.Sprint(m.Snap.RoundNumber) + " 局）：\n")
		for _, p := range m.Snap.Players {
			mark := "  "
			if p.ID == m.Snap.MatchWinnerPlayerID {
				mark = "★ "
			}
			b.WriteString(fmt.Sprintf("%s%s  %d 分，余 %d 张\n", mark, p.Name, m.Snap.Scores[p.ID], p.CardCount))
		}
		b.WriteString(m.renderFinalHands(o))
		b.WriteString("\n  (b) 返回大厅，再开一局   (l) 离开房间\n")
		return b.String()
	}

	w := m.displayName(m.Snap.WinnerPlayerID)
	if w == "" {
		w = "某人"
	}
	b.WriteString("游戏结束 — " + w + " 获胜！\n")
	if m.Snap.EndReason != "" {
		b.WriteString("  （" + m.Snap.EndReason + "）\n")
	}
	b.WriteString("\n本局结果（第 " + fmt.Sprint(m.matchSeq) + " 局）：\n")
	for _, p := range m.Snap.Players {
		mark := "  "
		if p.ID == m.Snap.WinnerPlayerID {
			mark = "★ "
		}
		b.WriteString(fmt.Sprintf("%s%s  %d 张\n", mark, p.Name, p.CardCount))
	}
	b.WriteString(m.renderFinalHands(o))
	if m.history != nil {
		b.WriteString(fmt.Sprintf("\n已记录 %d 局对局，按 t 查看历史。\n", len(m.history)))
	}
	b.WriteString("\n  (b) 返回大厅，再开一局   (t) 对局记录   (l) 离开房间\n")
	return b.String()
}

// renderFinalHands renders every player's remaining cards after the game is over
// (single-round finish and SCORE match-over). Data comes from the committed view's
// FinalHands snapshot.
func (m Model) renderFinalHands(o RenderOpts) string {
	var b strings.Builder
	if m.Snap == nil || len(m.Snap.FinalHands) == 0 {
		return ""
	}
	b.WriteString("\n各家手牌：\n")
	for _, p := range m.Snap.Players {
		cards := m.Snap.FinalHands[p.ID]
		if len(cards) == 0 {
			b.WriteString(fmt.Sprintf("  %s：（已出完，获胜）\n", m.displayName(p.ID)))
			continue
		}
		items := make([]string, 0, len(cards))
		for _, c := range cards {
			items = append(items, cardText(c))
		}
		b.WriteString(fmt.Sprintf("  %s：%s\n", m.displayName(p.ID), strings.Join(items, "  ")))
	}
	return b.String()
}

// renderHistory lists locally persisted completed matches, newest first, or
// expands a selected record into a full event replay for bug triage.
func (m Model) renderHistory(o RenderOpts) string {
	if m.histSel >= 0 && m.histSel < len(m.history) {
		return renderHistoryDetail(m.history[m.histSel], o)
	}
	var b strings.Builder
	b.WriteString("对局记录（本机 " + historyFilePath() + "）：\n")
	if len(m.history) == 0 {
		b.WriteString("  还没有完成的对局。\n\n  (q) 返回\n")
		return b.String()
	}
	for i, rec := range m.history {
		num := fmt.Sprintf("%d", i+1)
		if i == 9 {
			num = "0"
		}
		b.WriteString(fmt.Sprintf("  [%s] 第 %d 局  %s  胜者：%s\n", num, rec.Seq, rec.Time.Format("01-02 15:04"), rec.Winner))
		b.WriteString(fmt.Sprintf("      规则 v%d  事件 %d 条\n", rec.ConfigVersion, len(rec.Events)))
		for _, p := range rec.Players {
			mark := "    "
			if p.Winner {
				mark = "    ★ "
			}
			b.WriteString(fmt.Sprintf("%s%s  %d 张\n", mark, p.Name, p.CardCount))
		}
		if i < len(m.history)-1 {
			b.WriteString("\n")
		}
	}
	b.WriteString("\n  (1-9/0) 展开回放   (q) 返回\n")
	return b.String()
}

// renderHistoryDetail dumps a selected record as a replayable event log.
func renderHistoryDetail(rec MatchRecord, o RenderOpts) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("第 %d 局  %s  胜者：%s\n", rec.Seq, rec.Time.Format("01-02 15:04"), rec.Winner))
	if rec.EndReason != "" {
		b.WriteString("  原因：" + rec.EndReason + "\n")
	}
	for _, p := range rec.Players {
		mark := "  "
		if p.Winner {
			mark = "★ "
		}
		b.WriteString(fmt.Sprintf("  %s%s  %d 张\n", mark, p.Name, p.CardCount))
	}
	b.WriteString(fmt.Sprintf("  规则 v%d，事件 %d 条：\n", rec.ConfigVersion, len(rec.Events)))
	for _, ev := range rec.Events {
		who := ev.PlayerID
		card := ""
		if ev.Card != nil {
			card = " " + cardText(*ev.Card)
		}
		color := ""
		if ev.Color != "" {
			color = "/" + colorNameZh(string(ev.Color))
		}
		target := ""
		if ev.TargetPlayer != "" {
			target = " 目标:" + ev.TargetPlayer
		}
		b.WriteString(fmt.Sprintf("  #%d %s %s%s%s%s\n", ev.ID, ev.Type, who, card, color, target))
	}
	b.WriteString("\n  (q) 返回列表\n")
	return b.String()
}

func (m Model) renderFooter(o RenderOpts) string {
	switch m.mode() {
	case ModeHelp:
		return ""
	case ModeLobby:
		return "按键：r=准备 s=开始 l=离开 h=帮助 q=退出"
	case ModeColor:
		return "按键：r/y/g/b=选色 h=帮助 q=退出"
	case ModeDrawnDecision:
		return "按键：p=打出该牌 g=保留 h=帮助 q=退出"
	case ModeChallenge:
		return "按键：c=挑战 a=接受 h=帮助 q=退出"
	case ModeStack:
		// Stacking excludes the +4 challenge, so only stacking/accept keys appear.
		return "按键：1-9/0=出+2/+4叠加 ↑/↓=选牌回车=打出 a=接受惩罚 h=帮助 q=退出"
	case ModeRoundOver:
		return "按键：c=开始下一局   (l) 离开房间   h=帮助"
	case ModeRules:
		return "按键：v=返回 e=编辑(房主) h=帮助 q=退出"
	case ModeConfigEdit:
		return "按键：↑/↓=选择 ←/→=改值 s=保存 v=返回 h=帮助 q=退出"
	case ModePaused:
		return "按键：h=帮助 q=退出"
	case ModeFinished:
		return "按键：b=返回大厅再开一局 t=对局记录 l=离开 h=帮助 q=退出"
	case ModeHistory:
		return "按键：1-9/0=展开回放 q=返回 h=帮助"
	default:
		return "按键：1-9=出牌 ↑/↓=选牌回车=打出 d=摸牌 h=帮助 q=退出"
	}
}

func (m Model) renderHelp(o RenderOpts) string {
	return `帮助
  h/?     开关本帮助
  q       返回（在帮助/规则界面上按 q 关闭；主界面按 q 退出）
  l       离开房间

大厅等待：
  r/空格  准备/就坐
  s       开始游戏（仅房主）

你的回合：
  1-9/0   打出对应的那张牌（手牌超过 10 张时改用 ↑/↓ + 回车）
  ↑/↓     在手牌中移动 ▶ 光标（大数量手牌）
  回车     打出光标选中的牌
  d/空格  摸一张牌
  轮到你 / 需作 +4 决定时，终端会响铃提醒 (\a)；有玩家加入大厅时也会响铃

摸牌后：
  p       打出刚摸的牌
  g       保留并过牌

万能选色：
  r/y/g/b 选择 红/黄/绿/蓝

被罚 +4（挑战）：
  c       挑战对方
  a       接受并摸 4 张

被抓叠加惩罚（+2/+4 叠加房规）：
  1-9/0   打出 +2/+4 继续叠加（传递惩罚）
  a       接受并摸累计张数、跳过

规则：
  v       查看当前规则（房主按 e 进入编辑）

对局记录：
  t       查看本机历史对局（q 返回）
  1-9/0   在历史里展开某一局的事件回放（q 返回列表）

对局结束：
  b       返回大厅，再开一局（无需退出程序）
  l       离开房间

房主设置规则（v 后按 e）：
  ↑/↓ (k/j)  选择项目
  ←/→ (+/-)  改变项目值
  s           保存并应用（开局前）
  v           返回

出牌的合法性均由服务端裁决。`
}

func playerLine(m Model, p game.PlayerView, o RenderOpts) string {
	parts := []string{p.Name}
	if p.ID == m.MyID {
		parts = append(parts, "（你）")
	}
	if p.IsHost {
		parts = append(parts, "（房主）")
	}
	if p.UNO {
		tag := "UNO!"
		if o.Unicode {
			tag = "★UNO!"
		}
		if o.Color {
			tag = unoStyle(tag)
		}
		parts = append(parts, tag)
	}
	if m.inGame() {
		parts = append(parts, fmt.Sprintf("%d 张", p.CardCount))
	}
	status := "已准备"
	if !p.Ready {
		status = "等待"
	}
	if !p.Connected {
		status = "离线"
	}
	mark := "·"
	if o.Unicode {
		mark = "◦"
	}
	if p.ID == m.SnapCurrentPlayer() {
		mark = "▶"
		if !o.Unicode {
			mark = ">"
		}
		line := "  " + mark + " " + strings.Join(parts, " ") + "  [" + status + "]"
		if o.Color {
			line = currentPlayerStyle(line)
		}
		return line
	}
	return "  " + mark + " " + strings.Join(parts, " ") + "  [" + status + "]"
}

func (m Model) SnapCurrentPlayer() string {
	if m.Snap == nil {
		return ""
	}
	return m.Snap.CurrentPlayer
}

// inGame reports whether a game is in progress (vs. the lobby), in which case
// each player's live hand-card count is meaningful.
func (m Model) inGame() bool {
	return m.Snap != nil && m.Snap.CurrentPlayer != "" && m.Snap.Phase != string(game.PhaseLobby)
}

func (m Model) displayName(id string) string {
	if m.Snap == nil {
		return id
	}
	for _, p := range m.Snap.Players {
		if p.ID == id {
			return p.Name
		}
	}
	return id
}

// colorNameZh maps an active-color value to its Chinese label.
func colorNameZh(c string) string {
	switch c {
	case "red":
		return "红"
	case "yellow":
		return "黄"
	case "green":
		return "绿"
	case "blue":
		return "蓝"
	case "wild":
		return "万能"
	default:
		if c == "" {
			return "-"
		}
		return c
	}
}

// cardText renders a single card as "R2" style text (red 2). Wild shows "(W)".
func cardText(c game.CardView) string {
	col := strings.ToUpper(string(c.Color[0]))
	if c.Color == game.ColorWild {
		col = "W"
	}
	rank := string(c.Rank)
	switch c.Rank {
	case game.RankSkip:
		rank = "跳牌"
	case game.RankReverse:
		rank = "反转"
	case game.RankDrawTwo:
		rank = "+2"
	case game.RankWild:
		rank = "万能"
	case game.RankWildDrawFour:
		rank = "+4"
	}
	return col + rank
}

// ModeHelp is a pseudo-mode used only for footer lookup.
const ModeHelp Mode = 99
