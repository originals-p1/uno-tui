package tui

import (
	"strings"
	"testing"

	"terminal-uno/internal/game"
)

// testSnap builds a personalized game view.
func testSnap(opts ...func(*game.GameView)) *game.GameView {
	v := &game.GameView{
		RoomRevision: 1,
		Players: []game.PlayerView{
			{ID: "p0", Name: "Alice", CardCount: 7, Ready: true, Connected: true, IsHost: true},
			{ID: "p1", Name: "Bob", CardCount: 6, Ready: true, Connected: true},
		},
		CurrentPlayer: "p0",
		Direction:     1,
		TopCard:       game.CardView{ID: "t", Color: game.ColorRed, Rank: game.RankTwo},
		ActiveColor:   string(game.ColorRed),
		Phase:         string(game.PhaseTurn),
		YourHand: []game.CardView{
			{ID: "c0", Color: game.ColorRed, Rank: game.RankTwo},
			{ID: "c1", Color: game.ColorYellow, Rank: game.RankNine},
			{ID: "c2", Color: game.ColorWild, Rank: game.RankWild},
		},
		PlayableIDs: []string{"c0", "c2"},
		History:     []game.GameEvent{},
	}
	for _, f := range opts {
		f(v)
	}
	return v
}

func newModel(myID string) Model {
	m := NewModel("Alice")
	m.MyID = myID
	m.IsHost = true
	return m
}

func runKey(t *testing.T, m Model, k string) (Model, string) {
	t.Helper()
	nm, cmd := m.Update(KeyMsg{Key: k})
	var action string
	if cmd != nil {
		if msg := cmd(); msg != nil {
			if a, ok := msg.(ActionMsg); ok {
				action = a.Kind.String() + ":" + a.CardID + ":" + a.ChosenColor
			}
		}
	}
	return nm.(Model), action
}

func TestView_Lobby(t *testing.T) {
	m := newModel("p0")
	out := m.render(RenderOpts{Color: false})
	if !strings.Contains(out, "Waiting to join") {
		t.Fatalf("lobby view missing waiting line: %q", out)
	}
}

func TestView_LobbySnapshotShowsPlayersAndNoTopcard(t *testing.T) {
	// A lobby snapshot has players but no top card / hand. Rendering it must not
	// panic on the empty TopCard (regression for #index-range in cardText) and
	// must show the player roster.
	lo := testSnap(func(v *game.GameView) {
		v.Phase = string(game.PhaseLobby)
		v.CurrentPlayer = ""
		v.TopCard = game.CardView{}
		v.YourHand = nil
		v.PlayableIDs = nil
		v.Players = []game.PlayerView{
			{ID: "p0", Name: "Alice", Ready: true, IsHost: true, Connected: true},
			{ID: "p1", Name: "Bob", Ready: false, Connected: true},
		}
	})
	m := newModel("p1")
	nm, _ := m.Update(SnapshotMsg{View: lo})
	out := nm.(Model).render(RenderOpts{Color: false})
	if !strings.Contains(out, "Players:") {
		t.Fatalf("lobby snapshot missing Players roster: %q", out)
	}
	if !strings.Contains(out, "Alice") || !strings.Contains(out, "Bob") {
		t.Fatalf("lobby roster missing players: %q", out)
	}
	if !strings.Contains(out, "(you)") {
		t.Fatalf("lobby roster missing (you) marker: %q", out)
	}
	if strings.Contains(out, "Your hand") {
		t.Fatalf("lobby should not render a hand: %q", out)
	}
}

func TestView_TurnRedTwoAndStableOrder(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	m = nm.(Model)
	out := m.render(RenderOpts{Color: false})
	if !strings.Contains(out, "R2") {
		t.Fatalf("turn view missing red 2: %q", out)
	}
	// Stable original hand order: R2 then Y9 then W* stay in order.
	iR2, iY9, iW := strings.Index(out, "R2"), strings.Index(out, "Y9"), strings.Index(out, "W*")
	if iR2 < 0 || iY9 < 0 || iW < 0 {
		t.Fatalf("hand cards not rendered: R2=%d Y9=%d W*=%d out=%q", iR2, iY9, iW, out)
	}
	if !(iR2 < iY9 && iY9 < iW) {
		t.Fatalf("hand order not preserved: R2=%d Y9=%d W*=%d", iR2, iY9, iW)
	}
	if !strings.Contains(out, "Your hand") {
		t.Fatalf("missing own-hand heading: %q", out)
	}
	if !strings.Contains(out, "playable") {
		t.Fatalf("missing playable hint: %q", out)
	}
}

func TestKey_PlayNumberCard(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	_, action := runKey(t, nm.(Model), "1") // first card is c0 (red 2, non-wild)
	if !strings.HasPrefix(action, "play_card:c0") {
		t.Fatalf("expected play_card:c0, got %q", action)
	}
}

func TestKey_WildEntersColorThenPicks(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	m = nm.(Model)
	// '3' selects the wild card c2 -> enters color mode.
	nm2, cmd := m.Update(KeyMsg{Key: "3"})
	if cmd != nil {
		t.Fatalf("wild press should not emit action yet")
	}
	m2 := nm2.(Model)
	if m2.mode() != ModeColor {
		t.Fatalf("expected ModeColor, got %v", m2.mode())
	}
	_, action := runKey(t, m2, "r")
	if !strings.HasPrefix(action, "play_card:c2") || !strings.Contains(action, ":red") {
		t.Fatalf("expected play_card:c2:red, got %q", action)
	}
}

func TestKey_DrawDecision(t *testing.T) {
	snap := testSnap(func(v *game.GameView) {
		v.Phase = string(game.PhaseDrawnDecision)
		v.DrawDecision = &game.DrawDecisionView{Card: game.CardView{ID: "d0", Color: game.ColorGreen, Rank: game.RankFive}}
	})
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: snap})
	m = nm.(Model)
	if m.mode() != ModeDrawnDecision {
		t.Fatalf("expected drawn decision mode, got %v", m.mode())
	}
	_, a := runKey(t, m, "p")
	if !strings.HasPrefix(a, "play_drawn") {
		t.Fatalf("p should emit play_drawn, got %q", a)
	}
	_, a2 := runKey(t, m, "g")
	if !strings.HasPrefix(a2, "pass_drawn") {
		t.Fatalf("g should emit pass_drawn, got %q", a2)
	}
}

func TestKey_Draw(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	_, a := runKey(t, nm.(Model), "d")
	if !strings.HasPrefix(a, "draw") {
		t.Fatalf("d should emit draw, got %q", a)
	}
}

func TestView_GameOver(t *testing.T) {
	snap := testSnap(func(v *game.GameView) {
		v.WinnerPlayerID = "p1"
		v.EndReason = "ran out of cards"
	})
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: snap})
	out := nm.(Model).render(RenderOpts{Color: false})
	if !strings.Contains(out, "wins") || !strings.Contains(out, "Bob") {
		t.Fatalf("game-over view wrong: %q", out)
	}
}

func TestView_PausedDisconnect(t *testing.T) {
	snap := testSnap(func(v *game.GameView) {
		v.Paused = &game.PausedView{DisconnectedPlayerIDs: []string{"p1"}, EarliestDeadline: "2026-01-01T00:00:00Z"}
	})
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: snap})
	out := nm.(Model).render(RenderOpts{Color: false})
	if !strings.Contains(out, "Paused") || !strings.Contains(out, "Bob") {
		t.Fatalf("paused view wrong: %q", out)
	}
}

func TestView_Help(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(HelpMsg{})
	out := nm.(Model).render(RenderOpts{Color: false})
	if !strings.Contains(out, "HELP") {
		t.Fatalf("help view missing: %q", out)
	}
}

func TestRender_NoColorMode(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	out := nm.(Model).render(RenderOpts{Color: false})
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("no-color mode emitted ANSI escapes: %q", out)
	}
}

func TestRender_UnicodeDowngrade(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	out := nm.(Model).render(RenderOpts{Color: false, Unicode: false})
	for _, bad := range []string{"→", "←", "▶"} {
		if strings.Contains(out, bad) {
			t.Fatalf("unicode downgrade leaked %q: %q", bad, out)
		}
	}
}

func TestRender_NarrowScreen(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	full := nm.(Model).render(RenderOpts{Color: false})
	narrow := nm.(Model).render(RenderOpts{Color: false, Narrow: true})
	if len(narrow) >= len(full) {
		t.Fatalf("narrow output not smaller: full=%d narrow=%d", len(full), len(narrow))
	}
	if strings.Contains(narrow, "[1]") {
		t.Fatalf("narrow mode should compact hand, got %q", narrow)
	}
}

func TestKey_MultipleHotkeys(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	m = nm.(Model)
	cases := []struct {
		key  string
		want string
	}{
		{"1", "play_card:c0"},
		{"2", "play_card:c1"},
		{"d", "draw"},
		{"r", ""}, // r during turn: no action
	}
	for _, c := range cases {
		_, a := runKey(t, m, c.key)
		if c.want == "" && a != "" {
			t.Fatalf("key %q unexpected action %q", c.key, a)
		}
		if !strings.HasPrefix(a, c.want) {
			t.Fatalf("key %q action = %q, want prefix %q", c.key, a, c.want)
		}
	}
}
