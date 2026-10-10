package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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
	if !strings.Contains(out, "正在连接大厅") {
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
	if !strings.Contains(out, "玩家：") {
		t.Fatalf("lobby snapshot missing Players roster: %q", out)
	}
	if !strings.Contains(out, "Alice") || !strings.Contains(out, "Bob") {
		t.Fatalf("lobby roster missing players: %q", out)
	}
	if !strings.Contains(out, "（你）") {
		t.Fatalf("lobby roster missing (you) marker: %q", out)
	}
	if strings.Contains(out, "你的手牌") {
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
	if !strings.Contains(out, "你的手牌") {
		t.Fatalf("missing own-hand heading: %q", out)
	}
	if !strings.Contains(out, "可出") {
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

func TestKey_Challenge(t *testing.T) {
	snap := testSnap(func(v *game.GameView) {
		v.Phase = string(game.PhaseChallenge)
		v.Challenge = &game.ChallengeView{ResolverPlayerID: "p1", YourDecision: true}
	})
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: snap})
	m = nm.(Model)
	if m.mode() != ModeChallenge {
		t.Fatalf("expected challenge mode, got %v", m.mode())
	}
	_, a := runKey(t, m, "c")
	if !strings.HasPrefix(a, "challenge_four") {
		t.Fatalf("c should emit challenge_four, got %q", a)
	}
	_, a2 := runKey(t, m, "a")
	if !strings.HasPrefix(a2, "accept_four") {
		t.Fatalf("a should emit accept_four, got %q", a2)
	}
}

func TestView_Challenge(t *testing.T) {
	snap := testSnap(func(v *game.GameView) {
		v.Phase = string(game.PhaseChallenge)
		v.Challenge = &game.ChallengeView{ResolverPlayerID: "p1", YourDecision: true}
	})
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: snap})
	out := nm.(Model).render(RenderOpts{Color: false})
	if !strings.Contains(out, "+4") || !strings.Contains(out, "Bob") {
		t.Fatalf("challenge view wrong: %q", out)
	}
	// The on-screen copy must reflect the actual challenge criterion (whether the
	// +4 resolver held the active color), NOT a history of previous +4 plays.
	if strings.Contains(out, "之前出过 +4") {
		t.Fatalf("challenge copy is misleading about the criterion: %q", out)
	}
	if !strings.Contains(out, "同色") {
		t.Fatalf("challenge copy should mention having the same-color card: %q", out)
	}
}

func TestView_ChallengeShowsTriggerColor(t *testing.T) {
	snap := testSnap(func(v *game.GameView) {
		v.Phase = string(game.PhaseChallenge)
		v.Challenge = &game.ChallengeView{ResolverPlayerID: "p1", YourDecision: true, TriggerColor: string(game.ColorRed)}
	})
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: snap})
	out := nm.(Model).render(RenderOpts{Color: false})
	if !strings.Contains(out, "红") || !strings.Contains(out, "同色") {
		t.Fatalf("challenge view should show the trigger color: %q", out)
	}
}

func TestView_HandCardsShowColorIcon(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	out := nm.(Model).render(RenderOpts{Color: false, Narrow: false})
	// Wide hand should prefix each card with a pip icon ("●"/"◎").
	if !strings.Contains(out, "●") {
		t.Fatalf("hand cards lack color icon: %q", out)
	}
	// Wild cards use the hollow pip.
	if !strings.Contains(out, "◎") {
		t.Fatalf("wild card should use hollow pip icon: %q", out)
	}
}

func TestView_ChallengeResultBanner(t *testing.T) {
	for _, tt := range []struct {
		name string
		res  *game.ChallengeResult
		want string
	}{
		{"success", &game.ChallengeResult{Succeeded: true, ResolverPlayerID: "p1", ChallengerPlayerID: "p0"}, "挑战成功"},
		{"failure", &game.ChallengeResult{Succeeded: false, ResolverPlayerID: "p1", ChallengerPlayerID: "p0"}, "挑战失败"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			snap := testSnap(func(v *game.GameView) { v.ChallengeRes = tt.res })
			m := newModel("p0")
			nm, _ := m.Update(SnapshotMsg{View: snap})
			out := nm.(Model).render(RenderOpts{Color: false})
			if !strings.Contains(out, tt.want) {
				t.Fatalf("banner missing %q: %q", tt.want, out)
			}
		})
	}
}

func TestView_EachPlayerShowsHandCount(t *testing.T) {
	// During a game each player's row shows their live hand-card count.
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	out := nm.(Model).render(RenderOpts{Color: false})
	if !strings.Contains(out, "7 张") || !strings.Contains(out, "6 张") {
		t.Fatalf("game view should show each player's hand count: %q", out)
	}

	// The lobby must NOT show bogus 0-card counts.
	lo := testSnap(func(v *game.GameView) {
		v.Phase = string(game.PhaseLobby)
		v.CurrentPlayer = ""
	})
	m2 := newModel("p0")
	nm2, _ := m2.Update(SnapshotMsg{View: lo})
	lout := nm2.(Model).render(RenderOpts{Color: false})
	if strings.Contains(lout, "0 张") {
		t.Fatalf("lobby should not show hand counts: %q", lout)
	}
}

func TestKey_QuitIsBackInOverlays(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	m = nm.(Model)
	nm, _ = m.Update(ConfigMsg{Config: game.DefaultConfig(), ConfigVersion: 0, Locked: false})
	m = nm.(Model)

	// Help overlay: q closes it rather than quitting.
	nm, _ = m.Update(KeyMsg{Key: "h"})
	m = nm.(Model)
	if !m.help {
		t.Fatal("h should open help")
	}
	nm, cmd := m.Update(KeyMsg{Key: "q"})
	m = nm.(Model)
	if m.help {
		t.Fatal("q should close help")
	}
	if cmd != nil {
		t.Fatalf("q inside help must not quit (cmd=%v)", cmd)
	}

	// Rules overlay: q closes it rather than quitting.
	nm, _ = m.Update(KeyMsg{Key: "v"})
	m = nm.(Model)
	if m.mode() != ModeRules {
		t.Fatalf("expected rules mode, got %v", m.mode())
	}
	nm, cmd = m.Update(KeyMsg{Key: "q"})
	m = nm.(Model)
	if m.mode() == ModeRules {
		t.Fatal("q should close rules view")
	}
	if cmd != nil {
		t.Fatalf("q inside rules must not quit (cmd=%v)", cmd)
	}

	// Config editor: q backs out to rules view (still not quitting).
	nm, _ = m.Update(KeyMsg{Key: "v"})
	m = nm.(Model)
	nm, _ = m.Update(KeyMsg{Key: "e"})
	m = nm.(Model)
	if m.mode() != ModeConfigEdit {
		t.Fatalf("expected config edit mode, got %v", m.mode())
	}
	nm, cmd = m.Update(KeyMsg{Key: "q"})
	m = nm.(Model)
	if m.mode() == ModeConfigEdit {
		t.Fatal("q should exit config editor")
	}
	if cmd != nil {
		t.Fatalf("q inside editor must not quit (cmd=%v)", cmd)
	}

	// Normal game screen: q really quits.
	nm, cmd = m.Update(KeyMsg{Key: "v"}) // close rules to leave overlays
	m = nm.(Model)
	_, cmd = m.Update(KeyMsg{Key: "q"})
	if cmd == nil {
		t.Fatal("q in normal mode should quit")
	}
	if msg := cmd(); msg != nil {
		if _, ok := msg.(tea.QuitMsg); !ok {
			t.Fatalf("expected QuitMsg, got %T", msg)
		}
	}
}

func TestConfigEditor_NavigateCycleSave(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	m = nm.(Model)
	nm, _ = m.Update(ConfigMsg{Config: game.DefaultConfig(), ConfigVersion: 0, Locked: false})
	m = nm.(Model)
	nm, _ = m.Update(KeyMsg{Key: "v"})
	m = nm.(Model) // rules view
	nm, _ = m.Update(KeyMsg{Key: "e"})
	m = nm.(Model)
	if m.mode() != ModeConfigEdit {
		t.Fatalf("expected config edit mode, got %v", m.mode())
	}
	// Move down once to select initial_direction row (0-based: hand size is row 0,
	// direction is row 1). Cycle it forward (顺时针 -> 逆时针).
	nm, _ = m.Update(KeyMsg{Key: "j"})
	m = nm.(Model)
	if m.editSel != 1 {
		t.Fatalf("editSel = %d, want 1", m.editSel)
	}
	nm, _ = m.Update(KeyMsg{Key: "right"})
	m = nm.(Model)
	if m.Config == nil || m.Config.Game.InitialDir != game.DirCounterClockwise {
		t.Fatalf("direction did not cycle, got %q", m.Config.Game.InitialDir)
	}
	// Save -> an ActApplyConfig action carrying a copy of the edited config.
	_, cmd := m.Update(KeyMsg{Key: "s"})
	if cmd == nil {
		t.Fatal("s should emit a save command")
	}
	got := cmd().(ActionMsg)
	if got.Kind != ActApplyConfig || got.Config == nil {
		t.Fatalf("save action wrong: %+v", got)
	}
	if got.Config.Game.InitialDir != game.DirCounterClockwise {
		t.Fatalf("saved config should carry the edit, got %q", got.Config.Game.InitialDir)
	}
}

func TestView_Rules(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(ConfigMsg{Config: game.DefaultConfig(), ConfigVersion: 2, Locked: false})
	m = nm.(Model)
	// Toggle rules view on.
	nm, _ = m.Update(SnapshotMsg{View: testSnap()})
	m = nm.(Model)
	nm, _ = m.Update(KeyMsg{Key: "v"})
	m = nm.(Model)
	if m.mode() != ModeRules {
		t.Fatalf("expected rules mode, got %v", m.mode())
	}
	out := m.render(RenderOpts{Color: false})
	if !strings.Contains(out, "当前规则") || !strings.Contains(out, "v2") {
		t.Fatalf("rules view wrong: %q", out)
	}
	// Toggle back off.
	nm, _ = m.Update(KeyMsg{Key: "v"})
	m = nm.(Model)
	if m.mode() == ModeRules {
		t.Fatalf("rules should be toggled off, got %v", m.mode())
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
	if !strings.Contains(out, "获胜") || !strings.Contains(out, "Bob") {
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
	if !strings.Contains(out, "已暂停") || !strings.Contains(out, "Bob") {
		t.Fatalf("paused view wrong: %q", out)
	}
}

func TestView_Help(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(HelpMsg{})
	out := nm.(Model).render(RenderOpts{Color: false})
	if !strings.Contains(out, "帮助") {
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
