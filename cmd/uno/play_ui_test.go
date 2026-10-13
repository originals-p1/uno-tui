package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"terminal-uno/internal/game"
	"terminal-uno/internal/tui"
)

// Exercise the application adapter: testing only tui.KeyMsg would miss a
// dropped terminal key or resize message in appModel.Update.
func TestInteractiveSimpleToggleAndResize(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := appModel{tui: tui.Model{MyID: "p0", Snap: &game.GameView{
		Phase: string(game.PhaseTurn), CurrentPlayer: "p1", Direction: 1,
		Players: []game.PlayerView{{ID: "p0", Name: "Alice", CardCount: 1, Connected: true}, {ID: "p1", Name: "Bob", CardCount: 7, Connected: true}},
		TopCard: game.CardView{Color: game.ColorRed, Rank: game.RankTwo}, ActiveColor: "red",
		YourHand: []game.CardView{{ID: "c0", Color: game.ColorBlue, Rank: game.RankFive}},
	}}}
	full := m.View().Content
	n, cmd := m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if cmd != nil {
		t.Fatal("display toggle emitted command")
	}
	m = n.(appModel)
	if out := m.View().Content; strings.Contains(out, "终端 UNO") || !strings.Contains(out, "m full") {
		t.Fatal(out)
	}
	n, _ = m.Update(tea.WindowSizeMsg{Width: 20, Height: 24})
	m = n.(appModel)
	for _, line := range strings.Split(m.View().Content, "\n") {
		if ansi.StringWidth(line) > 20 {
			t.Errorf("resize did not reach renderer: %q", line)
		}
	}
	n, _ = m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	m = n.(appModel)
	if m.View().Content != full {
		t.Fatal("second m did not restore full view")
	}
}
