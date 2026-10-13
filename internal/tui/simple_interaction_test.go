package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"terminal-uno/internal/game"
)

// Exercise emitted actions as well as the rendered decision, so mode switching
// cannot silently turn a drawn wild into a normal hand-card play.
func TestSimpleWildDecisionAcrossToggle(t *testing.T) {
	for _, drawn := range []bool{false, true} {
		t.Run(fmt.Sprintf("drawn=%v", drawn), func(t *testing.T) {
			m := newModel("p0")
			m.simple, m.Snap = true, testSnap()
			key, want := "3", ActPlayCard
			if drawn {
				m.Snap.DrawDecision = &game.DrawDecisionView{Card: m.Snap.YourHand[2]}
				key, want = "p", ActPlayDrawn
			}
			m, action := runKey(t, m, key)
			if action != "" || !strings.Contains(m.render(RenderOpts{}), "选色") {
				t.Fatal("wild must request color before sending an action")
			}
			m, _ = runKey(t, m, "m")
			m, _ = runKey(t, m, "m")
			m, action = runKey(t, m, "b")
			if action != want.String()+":c2:blue" || m.picking.Active {
				t.Fatalf("wrong resolved action %q, picking=%+v", action, m.picking)
			}
		})
	}
}

func TestSimpleLargeHandNavigation(t *testing.T) {
	m := newModel("p0")
	m.simple, m.Snap = true, testSnap()
	m.Snap.YourHand = nil
	for i := 0; i < 12; i++ {
		m.Snap.YourHand = append(m.Snap.YourHand, game.CardView{ID: fmt.Sprintf("card%d", i), Color: game.ColorRed, Rank: game.RankTwo})
	}
	m, _ = runKey(t, m, "up")
	if !strings.Contains(m.render(RenderOpts{}), ">12:R2") {
		t.Fatal("cursor did not wrap to last card")
	}
	_, action := runKey(t, m, "enter")
	if action != ActPlayCard.String()+":card11:" {
		t.Fatalf("selected wrong card: %s", action)
	}
	_, action = runKey(t, m, "0")
	if action != ActPlayCard.String()+":card9:" {
		t.Fatalf("0 must play tenth card: %s", action)
	}
	next := testSnap()
	m.applySnapshot(next)
	if !strings.Contains(m.render(RenderOpts{}), ">[1]:R2") {
		t.Fatal("cursor not clamped after hand shrank")
	}
}

func TestSimpleScoreAndMatchWinner(t *testing.T) {
	m := newModel("p0")
	m.simple, m.Snap = true, testSnap()
	s := m.Snap
	s.WinMode, s.RoundOver = game.WinScore, true
	s.Scores = map[string]int{"p0": 196, "p1": -12}
	s.RoundResult = &game.RoundResult{WinnerPlayerID: "p0", Points: 36, Scores: s.Scores}
	out := m.render(RenderOpts{})
	for _, want := range []string{"我:7(196)", "Bob:6(-12)", "本局我获胜 · +36分 → 196分", "c 下一局"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	s.RoundOver, s.MatchOver = false, true
	s.WinnerPlayerID, s.MatchWinnerPlayerID = "p0", "p1"
	out = m.render(RenderOpts{})
	if !strings.Contains(out, "Bob获胜 · b 返回大厅") || strings.Contains(out, "c 下一局") {
		t.Fatal(out)
	}
}

func TestSimpleNoColorAndReverse(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := newModel("p0")
	m.simple, m.Snap = true, testSnap()
	m.Snap.Direction = -1
	m.Snap.YourHand = []game.CardView{
		{ID: "r", Color: game.ColorRed, Rank: game.RankSkip},
		{ID: "y", Color: game.ColorYellow, Rank: game.RankReverse},
		{ID: "g", Color: game.ColorGreen, Rank: game.RankDrawTwo},
		{ID: "b", Color: game.ColorBlue, Rank: game.RankNine},
		{ID: "w", Color: game.ColorWild, Rank: game.RankWildDrawFour},
	}
	m.Snap.PlayableIDs = []string{"r"}
	out := m.render(defaultRenderOpts())
	for _, want := range []string{"| ←", "[1]:R跳过", "2:Y反转", "3:G+2", "4:B9", "5:W+4"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") || strings.Contains(out, "目标") {
		t.Fatal("color escapes or score info leaked into ordinary no-color game")
	}
}

func TestSimpleResizeAcrossScreens(t *testing.T) {
	for _, width := range []int{20, 40, 80} {
		for _, state := range []string{"turn", "lobby", "challenge", "color"} {
			t.Run(fmt.Sprintf("%s/%d", state, width), func(t *testing.T) {
				m := newModel("p0")
				m.simple, m.Snap = true, testSnap()
				switch state {
				case "lobby":
					m.Snap.Phase = string(game.PhaseLobby)
				case "challenge":
					m.Snap.Challenge = &game.ChallengeView{ResolverPlayerID: "p1", TriggerColor: "red", YourDecision: true}
				case "color":
					m.picking = CardPick{Active: true, CardID: "c2"}
				}
				n, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
				m = n.(Model)
				for _, line := range strings.Split(m.render(RenderOpts{Color: true, Unicode: true}), "\n") {
					if ansi.StringWidth(line) > width {
						t.Errorf("line exceeds %d columns: %q", width, line)
					}
				}
			})
		}
	}
}
