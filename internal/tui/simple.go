package tui

import (
	"fmt"
	"strings"

	lip "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"terminal-uno/internal/game"
)

func simpleColor(c string, o RenderOpts) string {
	var sym string
	switch c {
	case "red":
		sym = "R"
	case "yellow":
		sym = "Y"
	case "green":
		sym = "G"
	case "blue":
		sym = "B"
	default:
		return "?"
	}
	if o.Color {
		return lip.NewStyle().Foreground(unoColor(c)).Render("●") + sym
	}
	return sym
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
		rank = "skip"
	case game.RankReverse:
		rank = "rev"
	}
	if c.Color == game.ColorWild {
		return rank
	}
	return simpleColor(string(c.Color), o) + rank
}

func (m Model) simpleName(id string) string {
	if id == m.MyID {
		return "You"
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
		return "Connecting to lobby…\nm full · h help"
	}
	roster := []string{}
	for _, p := range s.Players {
		item := fmt.Sprintf("%s:%d", m.simpleName(p.ID), p.CardCount)
		if m.mode() == ModeLobby {
			item = m.simpleName(p.ID) + ":not ready"
			if p.Ready {
				item = m.simpleName(p.ID) + ":ready"
			}
		}
		if s.WinMode == game.WinScore {
			item += fmt.Sprintf("(%d)", s.Scores[p.ID])
		}
		if !p.Connected {
			item += "[offline]"
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
		lines = append(lines, ansi.Wrap("r ready · s start (host) · l leave", width, " "))
	} else {
		status := ""
		if s.WinMode == game.WinScore {
			status = fmt.Sprintf("Round %d · target %d pts | ", s.RoundNumber, s.TargetScore)
		}
		if !m.simpleDecision() && m.mode() == ModeTurn {
			status += "waiting for " + m.simpleName(s.CurrentPlayer) + " | "
		}
		top := s.TopCard
		top.Color = game.Color(s.ActiveColor)
		lines = append(lines, ansi.Wrap(status+"table "+simpleCard(top, o), width, " "))
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
					st = lip.NewStyle().Foreground(unoColor("green")).Bold(true)
				}
				num = st.Render(num)
			}
			cursor := ""
			if i == m.handSel {
				cursor = ">"
			}
			card := simpleCard(c, o)
			if playable {
				card = "[" + card + "]"
			}
			hand = append(hand, cursor+num+":"+card)
		}
		text := strings.Join(hand, "  ")
		if text == "" {
			text = "(empty)"
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
			result := "challenge failed · " + m.simpleName(cr.ChallengerPlayerID) + " draws 6 · skip turn"
			if cr.Succeeded {
				result = "challenge won · " + m.simpleName(cr.ResolverPlayerID) + " draws 4"
			}
			lines = append(lines, ansi.Wrap(result, width, " "))
		}
		if s.RoundResult != nil {
			rr := s.RoundResult
			lines = append(lines, ansi.Wrap(fmt.Sprintf("%s wins round · +%d pts → %d pts", m.simpleName(rr.WinnerPlayerID), rr.Points, rr.Scores[rr.WinnerPlayerID]), width, " "))
		}
		prompt := ""
		switch m.mode() {
		case ModeTurn:
			if m.simpleDecision() {
				prompt = "↑↓ pick · Enter play · d draw"
			}
			if s.Challenge != nil {
				prompt = "waiting for " + m.simpleName(s.CurrentPlayer) + " to decide +4 challenge"
			}
			if s.Stack != nil {
				prompt = fmt.Sprintf("waiting for %s to resolve +%d", m.simpleName(s.CurrentPlayer), s.Stack.Credits)
			}
		case ModeColor:
			prompt = "color: r " + simpleColor("red", o) + "  y " + simpleColor("yellow", o) + "  g " + simpleColor("green", o) + "  b " + simpleColor("blue", o) + " · Esc cancel"
		case ModeDrawnDecision:
			prompt = "drew " + simpleCard(s.DrawDecision.Card, o) + " · p play · g keep"
		case ModeChallenge:
			prompt = m.simpleName(s.Challenge.ResolverPlayerID) + " played +4 · color before " + simpleColor(s.Challenge.TriggerColor, o) + "\na accept 4 · c challenge"
			if len(s.PlayableIDs) > 0 {
				prompt += " · Enter stack"
			}
		case ModeStack:
			prompt = fmt.Sprintf("draw +%d · Enter stack · a accept", s.Stack.Credits)
		case ModePaused:
			names := []string{}
			for _, id := range s.Paused.DisconnectedPlayerIDs {
				names = append(names, m.simpleName(id))
			}
			prompt = "paused · waiting for " + strings.Join(names, ", ") + " to reconnect"
		case ModeRoundOver:
			prompt = "c next round"
		case ModeFinished:
			winner := s.WinnerPlayerID
			if s.MatchWinnerPlayerID != "" {
				winner = s.MatchWinnerPlayerID
			}
			prompt = m.simpleName(winner) + " wins · b back to lobby"
		}
		if prompt != "" {
			lines = append(lines, ansi.Wrap(prompt, width, " "))
		}
	}
	if m.lastError != "" {
		lines = append(lines, ansi.Wrap("error: "+m.lastError, width, " "))
	}
	if m.simpleNotice != "" {
		lines = append(lines, ansi.Wrap(m.simpleNotice, width, " "))
	}
	lines = append(lines, ansi.Wrap("m full · h help", width, " "))
	return strings.Join(lines, "\n")
}
