package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
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
	title := "TERMINAL UNO"
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
	switch m.mode() {
	case ModePaused:
		return m.renderPaused(o)
	case ModeFinished:
		return m.renderFinished(o)
	case ModeColor:
		return m.renderColorPanel(o)
	case ModeDrawnDecision:
		return m.renderDrawDecision(o)
	default:
		return m.renderGame(o)
	}
}

func renderEmptyLobby(m Model, o RenderOpts) string {
	var b strings.Builder
	b.WriteString("Waiting to join…\n")
	b.WriteString("  (r) ready/sit   (s) start (host)   (l) leave   (h) help\n")
	return b.String()
}

func (m Model) renderGame(o RenderOpts) string {
	var b strings.Builder
	s := m.Snap
	// Players summary.
	b.WriteString("Players:\n")
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
	b.WriteString(fmt.Sprintf("\nTurn: %s (%s)\n", m.displayName(s.CurrentPlayer), dir))
	b.WriteString(fmt.Sprintf("Top: %s   Active color: %s\n\n", cardText(s.TopCard), s.ActiveColor))
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
		b.WriteString("Your hand (number to play, d to draw):\n")
	} else {
		b.WriteString("Your hand:\n")
	}
	if len(hand) == 0 {
		b.WriteString("  (empty)\n")
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
		line := fmt.Sprintf("  [%s] %s", num, cardText(c))
		if playable[c.ID] {
			line += "  <- playable"
			if o.Color {
				line = playableStyle(line)
			}
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func (m Model) renderDrawDecision(o RenderOpts) string {
	var b strings.Builder
	b.WriteString("You drew a card:\n\n")
	if m.Snap != nil && m.Snap.DrawDecision != nil {
		b.WriteString("  " + cardText(m.Snap.DrawDecision.Card) + "\n")
	}
	b.WriteString("\n  (p) play it   (g) keep & pass\n")
	return b.String()
}

func (m Model) renderColorPanel(o RenderOpts) string {
	var b strings.Builder
	b.WriteString("Choose a color for the wild card:\n\n")
	for _, c := range []string{"red", "yellow", "green", "blue"} {
		k := map[string]string{"red": "r", "yellow": "y", "green": "g", "blue": "b"}[c]
		b.WriteString(fmt.Sprintf("  (%s) %s\n", k, c))
	}
	return b.String()
}

func (m Model) renderPaused(o RenderOpts) string {
	var b strings.Builder
	b.WriteString("Paused — a player disconnected\n\n")
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
			b.WriteString("  deadline: " + m.Snap.Paused.EarliestDeadline + "\n")
		}
	}
	b.WriteString("\nWaiting for them to reconnect…\n")
	return b.String()
}

func (m Model) renderFinished(o RenderOpts) string {
	var b strings.Builder
	if m.Snap == nil {
		return ""
	}
	w := m.displayName(m.Snap.WinnerPlayerID)
	if w == "" {
		w = "someone"
	}
	b.WriteString("Game over — " + w + " wins!\n")
	if m.Snap.EndReason != "" {
		b.WriteString("  (" + m.Snap.EndReason + ")\n")
	}
	b.WriteString("\n  (l) leave room\n")
	return b.String()
}

func (m Model) renderFooter(o RenderOpts) string {
	switch m.mode() {
	case ModeHelp:
		return ""
	case ModeLobby:
		return "keys: r=ready s=start l=leave h=help q=quit"
	case ModeColor:
		return "keys: r/y/g/b=color h=help q=quit"
	case ModeDrawnDecision:
		return "keys: p=play-drawn g=pass h=help q=quit"
	case ModePaused:
		return "keys: h=help q=quit"
	case ModeFinished:
		return "keys: l=leave h=help q=quit"
	default:
		return "keys: 1-9=play d=draw h=help q=quit"
	}
}

func (m Model) renderHelp(o RenderOpts) string {
	return `HELP
  h/?     toggle this help
  q       quit
  l       leave room

While waiting (lobby):
  r/space ready   s start (host only)

On your turn:
  1-9/0   play that card
  d/space draw a card

After drawing (decision):
  p       play the drawn card
  g       keep it and pass

Choosing a wild color:
  r/y/g/b pick red/yellow/green/blue

All card legality is enforced by the server.`
}

func playerLine(m Model, p game.PlayerView, o RenderOpts) string {
	parts := []string{p.Name}
	if p.ID == m.MyID {
		parts = append(parts, "(you)")
	}
	if p.IsHost {
		parts = append(parts, "(host)")
	}
	if p.UNO {
		parts = append(parts, "UNO!")
	}
	if len(p.Name) == 0 || p.Name == p.ID {
		parts = append(parts, fmt.Sprintf("%d cards", p.CardCount))
	}
	status := "ready"
	if !p.Ready {
		status = "waiting"
	}
	if !p.Connected {
		status = "offline"
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
