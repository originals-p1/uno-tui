package tui

import (
	"fmt"
	"strings"

	lip "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"terminal-uno/internal/game"
)

func simpleColor(c string, o RenderOpts) string {
	if o.Color && o.Unicode {
		return lip.NewStyle().Foreground(unoColor(c)).Render("●")
	}
	switch c {
	case "red":
		return "R"
	case "yellow":
		return "Y"
	case "green":
		return "G"
	case "blue":
		return "B"
	}
	return "?"
}

func simpleCard(c game.CardView, o RenderOpts) string {
	rank := string(c.Rank)
	switch c.Rank {
	case game.RankWild:
		rank = "W"
	case game.RankWildDrawFour:
		rank = "W+4"
	case game.RankDrawTwo:
		rank = "+2"
	case game.RankSkip:
		rank = "跳过"
	case game.RankReverse:
		rank = "反转"
	}
	if c.Color == game.ColorWild {
		return rank
	}
	return simpleColor(string(c.Color), o) + rank
}

func (m Model) simpleName(id string) string {
	if id == m.MyID {
		return "我"
	}
	return m.displayName(id)
}

func (m Model) simpleDecision() bool {
	if m.Snap == nil || m.Snap.CurrentPlayer != m.MyID {
		return false
	}
	switch m.mode() {
	case ModeTurn, ModeColor, ModeDrawnDecision, ModeChallenge, ModeStack:
		return true
	}
	return false
}

func (m Model) renderSimple(o RenderOpts) string {
	width := m.width
	if width <= 0 {
		width = 80
	}
	if o.Narrow && width > 40 {
		width = 40
	}
	lines := []string{}
	s := m.Snap
	if s == nil {
		return "正在连接大厅…\nm 完整 · h 帮助"
	}
	roster := []string{}
	for _, p := range s.Players {
		item := fmt.Sprintf("%s:%d", m.simpleName(p.ID), p.CardCount)
		if m.mode() == ModeLobby {
			item = m.simpleName(p.ID) + ":未准备"
			if p.Ready {
				item = m.simpleName(p.ID) + ":已准备"
			}
		}
		if s.WinMode == game.WinScore {
			item += fmt.Sprintf("(%d)", s.Scores[p.ID])
		}
		if !p.Connected {
			item += "[离线]"
		}
		roster = append(roster, item)
	}
	arrow := "→"
	if s.Direction < 0 {
		arrow = "←"
	}
	if !o.Unicode {
		arrow = ">"
		if s.Direction < 0 {
			arrow = "<"
		}
	}
	lines = append(lines, ansi.Wrap(strings.Join(roster, "  ")+" | "+arrow, width, " "))
	if m.mode() == ModeLobby {
		lines = append(lines, ansi.Wrap("r 准备 · s 开始(房主) · l 离开", width, " "))
	} else {
		status := ""
		if s.WinMode == game.WinScore {
			status = fmt.Sprintf("第%d局 · 目标%d分 | ", s.RoundNumber, s.TargetScore)
		}
		if !m.simpleDecision() && m.mode() == ModeTurn {
			status += "等待" + m.simpleName(s.CurrentPlayer) + " | "
		}
		top := s.TopCard
		top.Color = game.Color(s.ActiveColor)
		lines = append(lines, ansi.Wrap(status+"桌面 "+simpleCard(top, o), width, " "))
		hand := []string{}
		for i, c := range s.YourHand {
			num := fmt.Sprint(i + 1)
			if i == 9 {
				num = "0"
			}
			playable := false
			if m.simpleDecision() && m.mode() != ModeColor && m.mode() != ModeDrawnDecision {
				for _, id := range s.PlayableIDs {
					if id == c.ID {
						playable = true
						break
					}
				}
			}
			if o.Color {
				st := lip.NewStyle().Foreground(lip.Color("240"))
				if playable {
					st = lip.NewStyle().Foreground(lip.Color("255")).Bold(true)
				}
				num = st.Render(num)
			} else if playable {
				num = "[" + num + "]"
			}
			cursor := ""
			if i == m.handSel {
				cursor = ">"
			}
			hand = append(hand, cursor+num+":"+simpleCard(c, o))
		}
		text := strings.Join(hand, "  ")
		if text == "" {
			text = "（空）"
		}
		if m.simpleDecision() && width >= 6 {
			border := lip.NormalBorder()
			if !o.Unicode {
				border = lip.ASCIIBorder()
			}
			st := lip.NewStyle().Border(border).Padding(0, 1)
			if o.Color {
				st = st.BorderForeground(unoColor("green"))
			}
			text = st.Render(ansi.Wrap(text, width-4, " "))
		} else {
			text = ansi.Wrap(text, width, " ")
		}
		lines = append(lines, text)
		if s.ChallengeRes != nil {
			cr := s.ChallengeRes
			result := "挑战失败 · " + m.simpleName(cr.ChallengerPlayerID) + "摸6张 · 跳过回合"
			if cr.Succeeded {
				result = "挑战成功 · " + m.simpleName(cr.ResolverPlayerID) + "摸4张"
			}
			lines = append(lines, ansi.Wrap(result, width, " "))
		}
		if s.RoundResult != nil {
			rr := s.RoundResult
			lines = append(lines, ansi.Wrap(fmt.Sprintf("本局%s获胜 · +%d分 → %d分", m.simpleName(rr.WinnerPlayerID), rr.Points, rr.Scores[rr.WinnerPlayerID]), width, " "))
		}
		prompt := ""
		switch m.mode() {
		case ModeTurn:
			if m.simpleDecision() {
				prompt = "↑↓ 选牌 · Enter 出牌 · d 摸牌"
			}
			if s.Challenge != nil {
				prompt = "等待" + m.simpleName(s.CurrentPlayer) + "决定 +4 挑战"
			}
			if s.Stack != nil {
				prompt = fmt.Sprintf("等待%s处理 +%d", m.simpleName(s.CurrentPlayer), s.Stack.Credits)
			}
		case ModeColor:
			prompt = "选色：r " + simpleColor("red", o) + "  y " + simpleColor("yellow", o) + "  g " + simpleColor("green", o) + "  b " + simpleColor("blue", o) + " · Esc 取消"
		case ModeDrawnDecision:
			prompt = "摸到 " + simpleCard(s.DrawDecision.Card, o) + " · p 打出 · g 保留"
		case ModeChallenge:
			prompt = m.simpleName(s.Challenge.ResolverPlayerID) + "出了 +4 · 出牌前颜色 " + simpleColor(s.Challenge.TriggerColor, o) + "\na 接受摸4张 · c 挑战"
			if len(s.PlayableIDs) > 0 {
				prompt += " · Enter 叠加"
			}
		case ModeStack:
			prompt = fmt.Sprintf("待摸 +%d · Enter 叠加 · a 接受", s.Stack.Credits)
		case ModePaused:
			names := []string{}
			for _, id := range s.Paused.DisconnectedPlayerIDs {
				names = append(names, m.simpleName(id))
			}
			prompt = "暂停 · 等待" + strings.Join(names, "、") + "重连"
		case ModeRoundOver:
			prompt = "c 下一局"
		case ModeFinished:
			winner := s.WinnerPlayerID
			if s.MatchWinnerPlayerID != "" {
				winner = s.MatchWinnerPlayerID
			}
			prompt = m.simpleName(winner) + "获胜 · b 返回大厅"
		}
		if prompt != "" {
			lines = append(lines, ansi.Wrap(prompt, width, " "))
		}
	}
	if m.lastError != "" {
		lines = append(lines, ansi.Wrap("错误："+m.lastError, width, " "))
	}
	if m.simpleNotice != "" {
		lines = append(lines, ansi.Wrap(m.simpleNotice, width, " "))
	}
	lines = append(lines, ansi.Wrap("m 完整 · h 帮助", width, " "))
	return strings.Join(lines, "\n")
}
