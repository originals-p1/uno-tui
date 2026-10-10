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
	return tea.NewView(m.render(defaultRenderOpts()))
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
	if m.mode() != ModeChallenge && m.mode() != ModeRules {
		banner = m.challengeResultBanner(o)
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
	case ModeRules:
		return m.renderRules(o)
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
	// Players summary.
	b.WriteString("玩家：\n")
	for _, p := range s.Players {
		b.WriteString("  " + playerLine(m, p, o) + "\n")
	}
	// Board info.
	dir := "→"
	if !o.Unicode {
		dir = "->"
	}
	if s.Direction < 0 {
		dir = "←"
		if !o.Unicode {
			dir = "<-"
		}
	}
	b.WriteString(fmt.Sprintf("\n轮到：%s （%s）\n", m.displayName(s.CurrentPlayer), dir))
	b.WriteString(fmt.Sprintf("桌面：%s   当前颜色：%s\n\n", cardText(s.TopCard), colorNameZh(s.ActiveColor)))
	// Own hand (stable order).
	b.WriteString(m.renderHand(o))
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
	var b strings.Builder
	if m.mode() == ModeTurn {
		b.WriteString("你的手牌（按数字出牌，d 摸牌）：\n")
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
	for i, c := range hand {
		num := fmt.Sprintf("%d", i+1)
		if i == 9 {
			num = "0"
		}
		line := fmt.Sprintf("  %s [%s] %s", cardColorIcon(c, o.Color), num, cardText(c))
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
	b.WriteString("出 +4 前桌面颜色：" + colZh + "\n")
	b.WriteString("你被罚摸 4 张牌。你怀疑他出 +4 时手里其实有同色的 " + colZh + " 牌吗？\n\n")
	b.WriteString("  (c) 挑战 —— 若对方违规，他摸 4 张；若没有，你摸 6 张\n")
	b.WriteString("  (a) 接受 —— 你摸 4 张，跳过回合\n")
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
	var b strings.Builder
	if cr.Succeeded {
		line := "挑战成功：" + resolver + " 违规，摸了 4 张牌；" + challenger + " 回合继续。"
		if o.Color {
			b.WriteString(successStyle(line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	} else {
		line := "挑战失败：" + challenger + " 摸了 6 张牌并被跳过 （" + resolver + " 出 +4 合法）。"
		if o.Color {
			b.WriteString(failStyle(line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
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
	b.WriteString("  牌组：经典 108 张\n")
	b.WriteString(fmt.Sprintf("  万能牌：%s    万能+4：%s\n", onOff(c.Cards.EnableWild), onOff(c.Cards.EnableDrawFour)))
	b.WriteString(fmt.Sprintf("  +4 挑战：%s    跳牌：%s    反转：%s\n", onOff(c.Cards.ChallengeDrawFour), onOff(c.Cards.EnableSkip), onOff(c.Cards.EnableReverse)))
	b.WriteString(fmt.Sprintf("  +2：%s    自动 UNO：%s\n", onOff(c.Cards.EnableDrawTwo), onOff(c.HouseRules.AutoUNO)))
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

func (m Model) renderColorPanel(o RenderOpts) string {
	var b strings.Builder
	b.WriteString("为万能牌选择颜色：\n\n")
	for _, c := range []string{"red", "yellow", "green", "blue"} {
		k := map[string]string{"red": "r", "yellow": "y", "green": "g", "blue": "b"}[c]
		b.WriteString(fmt.Sprintf("  (%s) %s\n", k, colorNameZh(c)))
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
	w := m.displayName(m.Snap.WinnerPlayerID)
	if w == "" {
		w = "某人"
	}
	b.WriteString("游戏结束 — " + w + " 获胜！\n")
	if m.Snap.EndReason != "" {
		b.WriteString("  （" + m.Snap.EndReason + "）\n")
	}
	b.WriteString("\n  (l) 离开房间\n")
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
	case ModeRules:
		return "按键：v=返回 h=帮助 q=退出"
	case ModePaused:
		return "按键：h=帮助 q=退出"
	case ModeFinished:
		return "按键：l=离开 h=帮助 q=退出"
	default:
		return "按键：1-9=出牌 d=摸牌 h=帮助 q=退出"
	}
}

func (m Model) renderHelp(o RenderOpts) string {
	return `帮助
  h/?     开关本帮助
  q       退出
  l       离开房间

大厅等待：
  r/空格  准备/就坐
  s       开始游戏（仅房主）

你的回合：
  1-9/0   打出对应的那张牌
  d/空格  摸一张牌

摸牌后：
  p       打出刚摸的牌
  g       保留并过牌

万能牌选色：
  r/y/g/b 选择 红/黄/绿/蓝

被罚 +4（挑战）：
  c       挑战对方
  a       接受并摸 4 张

规则：
  v       查看当前规则

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
		parts = append(parts, "UNO!")
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
		rank = "SKIP"
	case game.RankReverse:
		rank = "REV"
	case game.RankDrawTwo:
		rank = "+2"
	case game.RankWild:
		rank = "*"
	case game.RankWildDrawFour:
		rank = "+4"
	}
	return col + rank
}

// ModeHelp is a pseudo-mode used only for footer lookup.
const ModeHelp Mode = 99
