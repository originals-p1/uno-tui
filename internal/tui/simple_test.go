package tui

import (
	tea "charm.land/bubbletea/v2"
	lip "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"terminal-uno/internal/game"
	"testing"
)

func TestSimpleTogglePreservesSelection(t *testing.T) {
	m := newModel("p0")
	m.Snap = testSnap()
	m.handSel = 2
	m.picking = CardPick{Active: true, CardID: "c2"}
	before := m.render(RenderOpts{})
	n, cmd := m.Update(KeyMsg{Key: "m"})
	m = n.(Model)
	if cmd != nil || !m.simple || m.handSel != 2 || m.picking.CardID != "c2" {
		t.Fatal("toggle changed pending action")
	}
	n, _ = m.Update(KeyMsg{Key: "m"})
	if n.(Model).render(RenderOpts{}) != before {
		t.Fatal("toggle did not restore full view")
	}
}

func TestSimpleChallengeAndScore(t *testing.T) {
	m := newModel("p0")
	m.simple = true
	m.Snap = testSnap(func(v *game.GameView) {
		v.WinMode = game.WinScore
		v.Scores = map[string]int{"p0": 160, "p1": 85}
		v.RoundNumber = 3
		v.TargetScore = 500
		v.TopCard = game.CardView{Color: game.ColorWild, Rank: game.RankWildDrawFour}
		v.ActiveColor = "blue"
		v.Challenge = &game.ChallengeView{ResolverPlayerID: "p1", TriggerColor: "red", YourDecision: true}
		v.Phase = string(game.PhaseChallenge)
		v.PlayableIDs = nil
	})
	out := m.render(RenderOpts{Unicode: true})
	for _, want := range []string{"(You):7(160)", "Bob:6(85)", "Round 3", "table BW+4", "color before R", "a accept 4", "c challenge", "┌"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %s", want, out)
		}
	}
	for _, absent := range []string{"当前颜色", "轮到我", "可出", "终端 UNO"} {
		if strings.Contains(out, absent) {
			t.Errorf("unexpected %q", absent)
		}
	}
	_, action := runKey(t, m, "c")
	if !strings.Contains(action, "challenge") {
		t.Fatal(action)
	}
}

func TestSimpleWidthAndBellSnapshots(t *testing.T) {
	m := newModel("p0")
	m.simple = true
	m.Snap = testSnap()
	n, _ := m.Update(tea.WindowSizeMsg{Width: 28, Height: 20})
	m = n.(Model)
	if m.width != 28 {
		t.Fatal("resize lost")
	}
	for _, line := range strings.Split(m.render(RenderOpts{Color: true, Unicode: true}), "\n") {
		if ansi.StringWidth(line) > 28 {
			t.Errorf("line exceeds terminal width: %q", line)
		}
	}
	if cmd := m.bellOnTurn(m.Snap); cmd == nil {
		t.Error("expected bell on my turn in simple mode")
	}
	m.knownPlayers = map[string]bool{"p0": true}
	m.Snap.Phase = string(game.PhaseLobby)
	if cmd := m.bellOnJoin(m.Snap); cmd == nil {
		t.Error("expected bell for a lobby arrival in simple mode")
	}
}

func TestSimpleDecisionScreens(t *testing.T) {
	for _, tt := range []struct {
		name   string
		setup  func(*Model)
		want   string
		framed bool
	}{
		{"waiting", func(m *Model) { m.Snap.CurrentPlayer = "p1" }, "waiting for Bob", false},
		{"color", func(m *Model) { m.picking = CardPick{Active: true, CardID: "c2"} }, "color: r R", true},
		{"draw", func(m *Model) {
			m.Snap.DrawDecision = &game.DrawDecisionView{Card: game.CardView{Color: game.ColorBlue, Rank: game.RankFive}}
		}, "drew B5 · p play · g keep", true},
		{"stack", func(m *Model) { m.Snap.Stack = &game.StackView{Credits: 8, YourDecision: true} }, "draw +8", true},
		{"paused", func(m *Model) { m.Snap.Paused = &game.PausedView{DisconnectedPlayerIDs: []string{"p1"}} }, "waiting for Bob to reconnect", false},
		{"finished", func(m *Model) { m.Snap.WinnerPlayerID = "p1" }, "Bob wins", false},
		{"round over", func(m *Model) { m.Snap.RoundOver = true }, "c next round", false},
		{"failed challenge", func(m *Model) {
			m.Snap.CurrentPlayer = "p1"
			m.Snap.ChallengeRes = &game.ChallengeResult{ChallengerPlayerID: "p0"}
		}, "challenge failed · You draws 6", false},
		{"successful challenge", func(m *Model) { m.Snap.ChallengeRes = &game.ChallengeResult{Succeeded: true, ResolverPlayerID: "p1"} }, "challenge won · Bob draws 4", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel("p0")
			m.simple = true
			m.Snap = testSnap()
			tt.setup(&m)
			out := m.render(RenderOpts{Unicode: true})
			if !strings.Contains(out, tt.want) || strings.Contains(out, "┌") != tt.framed {
				t.Fatalf("unexpected screen: %s", out)
			}
		})
	}
}

func TestSimpleOnlyPlayableNumbersHighlighted(t *testing.T) {
	m := newModel("p0")
	m.simple = true
	m.Snap = testSnap()
	o := RenderOpts{Color: true, Unicode: true}
	bright := lip.NewStyle().Foreground(unoColor("green")).Bold(true)
	dim := lip.NewStyle().Foreground(lip.Color("240"))
	out := m.render(o)
	if !strings.Contains(out, bright.Render("1")+":") || !strings.Contains(out, dim.Render("2")+":") || !strings.Contains(out, bright.Render("3")+":") {
		t.Fatalf("incorrect number styles: %q", out)
	}
	m.Snap.CurrentPlayer = "p1"
	out = m.render(o)
	if strings.Contains(out, bright.Render("1")+":") || strings.Contains(out, "┌") {
		t.Fatal("waiting hand highlighted")
	}
}

func TestSimpleAcceptNoticeAndOverlayReturn(t *testing.T) {
	m := newModel("p0")
	m.simple = true
	m.Snap = testSnap()
	m.Snap.Challenge = &game.ChallengeView{YourDecision: true}
	next := testSnap()
	next.CurrentPlayer = "p1"
	next.YourHand = append(next.YourHand, make([]game.CardView, 4)...)
	m.applySnapshot(next)
	if !strings.Contains(m.render(RenderOpts{}), "drew 4 · skip turn") {
		t.Fatal("missing accept result")
	}
	m, _ = runKey(t, m, "h")
	if !strings.Contains(m.render(RenderOpts{}), "帮助") {
		t.Fatal("help missing")
	}
	m, _ = runKey(t, m, "q")
	if !m.simple || strings.Contains(m.render(RenderOpts{}), "终端 UNO") {
		t.Fatal("did not return to simple mode")
	}
	m.applySnapshot(testSnap())
	if m.simpleNotice != "" {
		t.Fatal("stale notice")
	}
}

func TestSimpleNextPlayer(t *testing.T) {
	for _, tt := range []struct {
		name, current, want string
		direction           int
	}{
		{"forward", "p0", "→ next(Bob)", 1},
		{"reverse wraps", "p0", "← next(Carol)", -1},
		{"forward wraps", "p2", "→ next(You)", 1},
		{"reverse", "p2", "← next(Bob)", -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := newModel("p0")
			m.simple, m.Snap = true, testSnap()
			m.Snap.Players = append(m.Snap.Players, game.PlayerView{ID: "p2", Name: "Carol", Connected: true})
			m.Snap.CurrentPlayer, m.Snap.Direction = tt.current, tt.direction
			if out := m.render(RenderOpts{Unicode: true}); !strings.Contains(out, tt.want) {
				t.Fatalf("missing %q in %s", tt.want, out)
			}
		})
	}
}
