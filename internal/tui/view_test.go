package tui

import (
	"fmt"
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
	// Stable original hand order: R2 then Y9 then W万能 stay in order.
	iR2, iY9, iW := strings.Index(out, "R2"), strings.Index(out, "Y9"), strings.Index(out, "W万能")
	if iR2 < 0 || iY9 < 0 || iW < 0 {
		t.Fatalf("hand cards not rendered: R2=%d Y9=%d W万能=%d out=%q", iR2, iY9, iW, out)
	}
	if !(iR2 < iY9 && iY9 < iW) {
		t.Fatalf("hand order not preserved: R2=%d Y9=%d W万能=%d", iR2, iY9, iW)
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

// Regression: pressing "v" must always show the rules view, even before the
// server's config snapshot has been loaded (m.Config == nil). Previously the
// view silently no-op'd because ModeRules required a non-nil Config.
func TestView_RulesKeyWorksWithoutConfig(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()}) // rules but no Config yet
	m = nm.(Model)
	if m.mode() != ModeTurn {
		t.Fatalf("precondition: want ModeTurn, got %v", m.mode())
	}
	nm, _ = m.Update(KeyMsg{Key: "v"})
	m = nm.(Model)
	if m.mode() != ModeRules {
		t.Fatalf("pressing v with nil Config: want ModeRules, got %v", m.mode())
	}
	out := m.render(RenderOpts{Color: false})
	if !strings.Contains(out, "当前规则") {
		t.Fatalf("rules view missing header: %q", out)
	}
	// Closing with q must return to the game.
	nm, _ = m.Update(KeyMsg{Key: "q"})
	if nm.(Model).mode() != ModeTurn {
		t.Fatalf("pressing q in rules should return to game, got %v", nm.(Model).mode())
	}
}

// Once the config snapshot is delivered, the rules view shows actual rules
// and the version footer.
func TestView_RulesKeyShowsLoadedConfig(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	m = nm.(Model)
	cfg := game.DefaultConfig()
	nm, _ = m.Update(ConfigMsg{Config: cfg, ConfigVersion: 3, Locked: false})
	m = nm.(Model)
	nm, _ = m.Update(KeyMsg{Key: "v"})
	out := nm.(Model).render(RenderOpts{Color: false})
	if !strings.Contains(out, "当前规则") || !strings.Contains(out, "版本：v3") {
		t.Fatalf("rules view missing loaded config: %q", out)
	}
}

// The hand is framed with a bright border on the player's own turn, drawing
// the eye to the need to act (readme §9). border is the rounded-rule glyphs
// used by scope/root lipgloss.
func TestView_HandFramedOnYourTurn(t *testing.T) {
	// Your turn: p0 is the current player in testSnap.
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	out := nm.(Model).render(RenderOpts{Color: false})
	for _, g := range []string{"╭", "╰", "│"} {
		if !strings.Contains(out, g) {
			t.Fatalf("your-turn hand not framed (missing %q): %q", g, out)
		}
	}

	// Not your turn: shift the current player so p0 is idle.
	snap := testSnap()
	snap.CurrentPlayer = "p1"
	m = newModel("p0")
	nm, _ = m.Update(SnapshotMsg{View: snap})
	out = nm.(Model).render(RenderOpts{Color: false})
	if strings.Contains(out, "╰") {
		t.Fatalf("idle player's hand should not be framed: %q", out)
	}
}

// A wild card's color pick can be cancelled: pressing q backs out of ModeColor
// without emitting any play action, and the card stays in hand.
func TestKey_WildColorCancelBack(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	m = nm.(Model)
	nm2, cmd := m.Update(KeyMsg{Key: "3"}) // wild c2 -> ModeColor
	if cmd != nil {
		t.Fatalf("wild press should not emit action yet")
	}
	m2 := nm2.(Model)
	if m2.mode() != ModeColor {
		t.Fatalf("want ModeColor, got %v", m2.mode())
	}
	// Cancel with q: no action, back to turn, hand still has the wild card.
	nm3, cmd := m2.Update(KeyMsg{Key: "q"})
	if cmd != nil {
		t.Fatalf("cancel should not emit an action, got %v", cmd())
	}
	m3 := nm3.(Model)
	if m3.mode() != ModeTurn {
		t.Fatalf("want ModeTurn after cancel, got %v", m3.mode())
	}
	if m3.picking.Active {
		t.Fatalf("picking should be cleared after cancel")
	}
	kept := false
	for _, c := range m3.Snap.YourHand {
		if c.ID == "c2" {
			kept = true
		}
	}
	if !kept {
		t.Fatalf("wild card should remain in hand after cancel")
	}
}

// While choosing a wild color (ModeColor), the panel shows the current color
// and a live copy of the hand for reference.
func TestView_ColorPanelShowsColorAndHand(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	m = nm.(Model)
	nm2, _ := m.Update(KeyMsg{Key: "3"}) // wild c2 -> ModeColor
	m = nm2.(Model)
	if m.mode() != ModeColor {
		t.Fatalf("want ModeColor, got %v", m.mode())
	}
	out := m.render(RenderOpts{Color: false})
	for _, want := range []string{"当前颜色", "你的手牌", "R2", "Y9", "W万能"} {
		if !strings.Contains(out, want) {
			t.Fatalf("color panel missing %q: %q", want, out)
		}
	}
}

// A finished game is persisted as a match record exactly once, and the
// history modal lists it.
func TestHistory_RecordsFinishedGame(t *testing.T) {
	historyFileOverride = t.TempDir() + "/games.json"
	m := newModel("p0")
	fin := testSnap(func(v *game.GameView) {
		v.Phase = string(game.PhaseFinished)
		v.WinnerPlayerID = "p0"
		v.LastEventID = 99
		v.EndReason = "先出完牌"
	})
	nm, cmd := m.Update(SnapshotMsg{View: fin})
	if cmd != nil {
		t.Fatalf("finished snapshot should not emit a command")
	}
	m = nm.(Model)
	if len(m.history) != 1 {
		t.Fatalf("want 1 record after finish, got %d", len(m.history))
	}
	r := m.history[0]
	if r.Winner != "Alice" || r.EndReason != "先出完牌" {
		t.Fatalf("unexpected record: %+v", r)
	}
	if len(r.Players) != 2 {
		t.Fatalf("want 2 players in record, got %d", len(r.Players))
	}

	// Re-delivering the same finish must not duplicate the record.
	nm, _ = m.Update(SnapshotMsg{View: fin})
	m = nm.(Model)
	if len(m.history) != 1 {
		t.Fatalf("finish recorded twice: got %d records", len(m.history))
	}

	// t opens the history modal, render shows the winner, q closes it.
	nm, _ = m.Update(KeyMsg{Key: "t"})
	m = nm.(Model)
	if m.mode() != ModeHistory {
		t.Fatalf("want ModeHistory, got %v", m.mode())
	}
	out := m.render(RenderOpts{Color: false})
	if !strings.Contains(out, "胜者：Alice") {
		t.Fatalf("history missing winner: %q", out)
	}
	nm, _ = m.Update(KeyMsg{Key: "q"})
	m = nm.(Model)
	if m.mode() != ModeFinished {
		t.Fatalf("q should close history back to finished, got %v", m.mode())
	}
}

// The finished screen previews the just-finished match summary.
func TestView_FinishedShowsSummary(t *testing.T) {
	m := newModel("p0")
	fin := testSnap(func(v *game.GameView) {
		v.Phase = string(game.PhaseFinished)
		v.WinnerPlayerID = "p0"
		v.LastEventID = 5
	})
	nm, _ := m.Update(SnapshotMsg{View: fin})
	out := nm.(Model).render(RenderOpts{Color: false})
	if !strings.Contains(out, "Alice 获胜") {
		t.Fatalf("finished screen missing winner: %q", out)
	}
	if !strings.Contains(out, "★ Alice") {
		t.Fatalf("finished screen missing winner star: %q", out)
	}
	return
}

// The recorded match carries the full privacy-safe event timeline, and the
// history detail view replays it (for bug triage).
func TestHistory_ReplayDetail(t *testing.T) {
	historyFileOverride = t.TempDir() + "/games.json"
	m := newModel("p0")
	nm, _ := m.Update(ConfigMsg{Config: game.DefaultConfig(), ConfigVersion: 3})
	m = nm.(Model)

	ev := []game.GameEvent{
		{ID: 1, Type: string(game.EventTurnStarted), PlayerID: "p0"},
		{ID: 2, Type: string(game.EventCardPlayed), PlayerID: "p0", Card: &game.CardView{Color: game.ColorRed, Rank: game.RankTwo}},
		{ID: 3, Type: string(game.EventWinner), PlayerID: "p0"},
	}
	fin := testSnap(func(v *game.GameView) {
		v.Phase = string(game.PhaseFinished)
		v.WinnerPlayerID = "p0"
		v.LastEventID = 3
		v.History = ev
	})
	nm, _ = m.Update(SnapshotMsg{View: fin})
	m = nm.(Model)
	if len(m.history) != 1 {
		t.Fatalf("want 1 record, got %d", len(m.history))
	}
	if len(m.history[0].Events) != 3 {
		t.Fatalf("record missing event timeline: got %d events", len(m.history[0].Events))
	}
	if m.history[0].ConfigVersion != 3 {
		t.Fatalf("record config version = %d, want 3", m.history[0].ConfigVersion)
	}

	// Open history, select the (only) record with [1], and see the replay.
	nm, _ = m.Update(KeyMsg{Key: "t"})
	m = nm.(Model)
	if m.mode() != ModeHistory {
		t.Fatalf("want ModeHistory, got %v", m.mode())
	}
	nm, _ = m.Update(KeyMsg{Key: "1"})
	m = nm.(Model)
	if m.histSel != 0 {
		t.Fatalf("histSel = %d, want 0", m.histSel)
	}
	out := m.render(RenderOpts{Color: false})
	if !strings.Contains(out, string(game.EventTurnStarted)) || !strings.Contains(out, string(game.EventCardPlayed)) {
		t.Fatalf("replay detail missing events: %q", out)
	}
	if !strings.Contains(out, "R2") {
		t.Fatalf("replay detail missing played card: %q", out)
	}

	// q returns to the list, q again closes.
	nm, _ = m.Update(KeyMsg{Key: "q"})
	m = nm.(Model)
	if m.histSel != -1 || m.mode() != ModeHistory {
		t.Fatalf("q should return to list (histSel=%d mode=%v)", m.histSel, m.mode())
	}
	nm, _ = m.Update(KeyMsg{Key: "q"})
	m = nm.(Model)
	if m.mode() != ModeFinished {
		t.Fatalf("q should close history to finished, got %v", m.mode())
	}
}

// The rules page explains what each action card does.
func TestView_RulesExplainActionCards(t *testing.T) {
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: testSnap()})
	nm, _ = nm.(Model).Update(ConfigMsg{Config: game.DefaultConfig(), ConfigVersion: 1})
	nm, _ = nm.(Model).Update(KeyMsg{Key: "v"})
	out := nm.(Model).render(RenderOpts{Color: false})
	for _, want := range []string{"功能牌说明", "跳牌", "反转", "+2", "万能", "+4", "跳过下一个玩家", "下一个玩家摸 2 张", "改变出牌方向"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rules page missing %q: %q", want, out)
		}
	}
}

// TestBell_OnTurnOnce verifies the terminal bell rings exactly once per time
// the ball moves to the local player, never repeatedly on the same turn, and
// never while the game is over/paused/in the lobby.
func TestBell_OnTurnOnce(t *testing.T) {
	m := newModel("p0")
	v := testSnap() // CurrentPlayer = "p0" (my turn), PhaseTurn

	if cmd := (&m).bellOnTurn(v); cmd == nil {
		t.Fatal("expected bell on my turn")
	}
	if cmd := (&m).bellOnTurn(v); cmd != nil {
		t.Fatal("bell must not repeat on the same turn")
	}

	// Turn passes away -> resets the latch.
	v.CurrentPlayer = "p1"
	if cmd := (&m).bellOnTurn(v); cmd != nil {
		t.Fatal("no bell when it is not my turn")
	}
	// And back to me -> rings again.
	v.CurrentPlayer = "p0"
	if cmd := (&m).bellOnTurn(v); cmd == nil {
		t.Fatal("expected bell when control returns to me")
	}

	// Finished: no bell even if CurrentPlayer happens to be me.
	m2 := newModel("p0")
	v2 := testSnap()
	v2.Phase = string(game.PhaseFinished)
	if cmd := (&m2).bellOnTurn(v2); cmd != nil {
		t.Fatal("no bell on a finished game")
	}

	// Paused: no bell.
	m3 := newModel("p0")
	v3 := testSnap()
	v3.Paused = &game.PausedView{DisconnectedPlayerIDs: []string{"p1"}, EarliestDeadline: "x"}
	if cmd := (&m3).bellOnTurn(v3); cmd != nil {
		t.Fatal("no bell while paused")
	}

	// Lobby: no bell.
	m4 := newModel("p0")
	v4 := testSnap()
	v4.Phase = string(game.PhaseLobby)
	if cmd := (&m4).bellOnTurn(v4); cmd != nil {
		t.Fatal("no bell in the lobby")
	}
}

// TestBell_OnPlayerJoin verifies the terminal bell rings exactly once when a
// new player joins the lobby, never on the initial snapshot, and never while
// the game has already started.
func TestBell_OnPlayerJoin(t *testing.T) {
	vLobby := testSnap(func(v *game.GameView) {
		v.Phase = string(game.PhaseLobby)
	})

	// No players present yet: seeds the known set, no bell.
	m := newModel("p0")
	vLobby.Players = nil
	if cmd := (&m).bellOnJoin(vLobby); cmd != nil {
		t.Fatal("first snapshot must not bell")
	}

	// Bob joins the lobby: bell once.
	vLobby.Players = []game.PlayerView{
		{ID: "p0", Name: "Alice"},
		{ID: "p1", Name: "Bob"},
	}
	if cmd := (&m).bellOnJoin(vLobby); cmd == nil {
		t.Fatal("expected bell when a player joins the lobby")
	}
	// No further change: no repeat.
	if cmd := (&m).bellOnJoin(vLobby); cmd != nil {
		t.Fatal("bell must not repeat without a new arrival")
	}

	// Carol joins: chimes again.
	vLobby.Players = []game.PlayerView{
		{ID: "p0", Name: "Alice"},
		{ID: "p1", Name: "Bob"},
		{ID: "p2", Name: "Carol"},
	}
	if cmd := (&m).bellOnJoin(vLobby); cmd == nil {
		t.Fatal("expected bell for each additional joiner")
	}

	// Game already started (players appear together): no join bell.
	m2 := newModel("p0")
	vTurn := testSnap() // PhaseTurn, both players already present
	if cmd := (&m2).bellOnJoin(vTurn); cmd != nil {
		t.Fatal("no bell for a game already in progress")
	}
}

func TestView_ScoreMode_RoundLineAndBanner(t *testing.T) {
	m := newModel("p0")
	v := testSnap()
	v.Phase = string(game.PhaseTurn)
	v.WinMode = game.WinScore
	v.RoundNumber = 2
	v.TargetScore = 500
	v.Scores = map[string]int{"p0": 4, "p1": 0}
	v.RoundResult = &game.RoundResult{RoundNumber: 1, WinnerPlayerID: "p0", Points: 4, Scores: map[string]int{"p0": 4, "p1": 0}}
	nm, _ := m.Update(SnapshotMsg{View: v})
	m = nm.(Model)
	out := m.render(RenderOpts{Color: false})
	if !strings.Contains(out, "积分赛 第 2 局") {
		t.Fatalf("missing score-match line: %q", out)
	}
	if !strings.Contains(out, "先到 500 分获胜") {
		t.Fatalf("missing target in score line: %q", out)
	}
	if !strings.Contains(out, "第 1 回合结束：Alice +4 分") {
		t.Fatalf("missing round-result banner: %q", out)
	}
	// A RoundResult carrying deltas also shows each loser's deduction.
	v.RoundResult.Deltas = map[string]int{"p0": 4, "p1": -6}
	nm, _ = m.Update(SnapshotMsg{View: v})
	m = nm.(Model)
	out2 := m.render(RenderOpts{Color: false})
	if !strings.Contains(out2, "Bob -6") {
		t.Fatalf("missing loser deduction in banner: %q", out2)
	}
}

func TestView_ScoreMode_MatchOverShowsChampion(t *testing.T) {
	m := newModel("p0")
	v := testSnap()
	v.Phase = string(game.PhaseFinished)
	v.WinMode = game.WinScore
	v.MatchOver = true
	v.TargetScore = 10
	v.RoundNumber = 3
	v.MatchWinnerPlayerID = "p0"
	v.Scores = map[string]int{"p0": 12, "p1": 3}
	v.WinnerPlayerID = "p0"
	nm, _ := m.Update(SnapshotMsg{View: v})
	m = nm.(Model)
	out := m.render(RenderOpts{Color: false})
	if !strings.Contains(out, "积分赛结束") || !strings.Contains(out, "率先达到 10 分夺冠") {
		t.Fatalf("missing match-over champion line: %q", out)
	}
	if !strings.Contains(out, "最终积分") {
		t.Fatalf("missing final scorebook: %q", out)
	}
	if !strings.Contains(out, "Alice  12 分") {
		t.Fatalf("missing champion score: %q", out)
	}
}

func TestView_LargeHand_CursorPlaysBeyondNinth(t *testing.T) {
	m := newModel("p0")
	v := testSnap()
	hand := make([]game.CardView, 12)
	for i := range hand {
		hand[i] = game.CardView{ID: fmt.Sprintf("c%d", i), Color: game.ColorRed, Rank: game.RankTwo}
	}
	v.YourHand = hand
	nm, _ := m.Update(SnapshotMsg{View: v})
	m = nm.(Model)

	// Cursor starts at 0; two "down" presses select index 2 (>9 unreachable by
	// the single-digit shortcuts is index 10+; here we verify index 2 works too).
	m2, _ := runKey(t, m, "down")
	if m2.handSel != 1 {
		t.Fatalf("handSel after one down = %d, want 1", m2.handSel)
	}
	m3, _ := runKey(t, m2, "down")
	if m3.handSel != 2 {
		t.Fatalf("handSel after two downs = %d, want 2", m3.handSel)
	}
	// Enter plays the card at the cursor (c2).
	_, action := runKey(t, m3, "enter")
	if want := "play_card:c2:"; action != want {
		t.Fatalf("enter action = %q, want %q", action, want)
	}
}

func TestView_LargeHand_MovesToIndexTenAndWraps(t *testing.T) {
	m := newModel("p0")
	v := testSnap()
	hand := make([]game.CardView, 12)
	for i := range hand {
		hand[i] = game.CardView{ID: fmt.Sprintf("c%d", i), Color: game.ColorRed, Rank: game.RankTwo}
	}
	v.YourHand = hand
	nm, _ := m.Update(SnapshotMsg{View: v})
	m = nm.(Model)

	// Move down 10 times -> reache index 10 (the first card beyond 1-9/0 range).
	for i := 0; i < 10; i++ {
		m, _ = runKey(t, m, "down")
	}
	if m.handSel != 10 {
		t.Fatalf("handSel = %d, want 10", m.handSel)
	}
	_, action := runKey(t, m, "enter")
	if want := "play_card:c10:"; action != want {
		t.Fatalf("play index 10 action = %q, want %q", action, want)
	}
	// Move down again wraps to 11.
	m, _ = runKey(t, m, "down")
	if m.handSel != 11 {
		t.Fatalf("handSel = %d, want 11 (after wrap past end)", m.handSel)
	}
	// And once more wraps back to 0.
	m, _ = runKey(t, m, "down")
	if m.handSel != 0 {
		t.Fatalf("handSel = %d, want 0 (wrapped)", m.handSel)
	}
}

func TestRender_TurnOrderDirection(t *testing.T) {
	mk := func(dir int, current string) (Model, *game.GameView) {
		m := newModel("p0")
		v := testSnap()
		v.Players = []game.PlayerView{
			{ID: "p0", Name: "Alice"},
			{ID: "p1", Name: "Bob"},
			{ID: "p2", Name: "Carol"},
		}
		v.Direction = dir
		v.CurrentPlayer = current
		nm, _ := m.Update(SnapshotMsg{View: v})
		return nm.(Model), v
	}

	// Clockwise from Alice: Alice → Bob → Carol → Alice (rightward pass).
	m, _ := mk(1, "p0")
	if !strings.Contains(m.renderTurnOrder(RenderOpts{Unicode: true}), "▶Alice → Bob → Carol → ▶回到起点") {
		t.Fatalf("clockwise order wrong: %q", m.renderTurnOrder(RenderOpts{Unicode: true}))
	}
	if !strings.Contains(m.renderTurnOrder(RenderOpts{Unicode: true}), "顺时针") {
		t.Fatalf("clockwise label missing")
	}

	// Counter-clockwise from Bob: Bob → Alice → Carol → Bob. The arrow is always
	// rightward; the actual turn order (the sequence) carries the direction.
	m, _ = mk(-1, "p1")
	got := m.renderTurnOrder(RenderOpts{Unicode: true})
	if !strings.Contains(got, "▶Bob → Alice → Carol → ▶回到起点") {
		t.Fatalf("counterclockwise order wrong: %q", got)
	}
	if !strings.Contains(got, "逆时针") {
		t.Fatalf("counterclockwise label missing")
	}

	// Non-unicode downgrade must not leak unicode arrows.
	m2, _ := mk(1, "p0")
	plain := m2.renderTurnOrder(RenderOpts{Unicode: false})
	for _, bad := range []string{"↓", "↑", "→", "←", "▶"} {
		if strings.Contains(plain, bad) {
			t.Fatalf("unicode leaked %q in downgrade: %q", bad, plain)
		}
	}
}

func TestView_HandSelectionMarker_AllHandSizes(t *testing.T) {
	// Even a small (<=10) hand shows a visible ▶ marker and supports ↑/↓ + Enter.
	m := newModel("p0")
	v := testSnap() // 3-card hand
	nm, _ := m.Update(SnapshotMsg{View: v})
	m = nm.(Model)

	opts := RenderOpts{Color: false, Unicode: true}
	out := m.render(opts)
	if !strings.Contains(out, "▶ ● [1] R2") {
		t.Fatalf("default marker should point at card 1: %q", out)
	}
	// Move down to card 2 and confirm the marker follows.
	m2, _ := runKey(t, m, "down")
	if m2.handSel != 1 {
		t.Fatalf("handSel = %d, want 1", m2.handSel)
	}
	out2 := m2.render(opts)
	if !strings.Contains(out2, "▶ ● [2] Y9") || strings.Contains(out2, "▶ ● [1]") {
		t.Fatalf("marker should move to card 2: %q", out2)
	}
	// Enter plays the selected card (index 1 = c1).
	_, action := runKey(t, m2, "enter")
	if want := "play_card:c1:"; action != want {
		t.Fatalf("enter action = %q, want %q", action, want)
	}
}

// TestKey_DrawnWildOpensColorPicker verifies that pressing "p" on a just-drawn
// wild/+4 opens the color picker instead of sending a color-less play_drawn
// (which the server would reject).
func TestKey_DrawnWildOpensColorPicker(t *testing.T) {
	snap := testSnap(func(v *game.GameView) {
		v.Phase = string(game.PhaseDrawnDecision)
		v.DrawDecision = &game.DrawDecisionView{Card: game.CardView{ID: "d0", Color: game.ColorWild, Rank: game.RankWildDrawFour}}
	})
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: snap})
	m = nm.(Model)
	if m.mode() != ModeDrawnDecision {
		t.Fatalf("want drawn decision mode, got %v", m.mode())
	}
	nm, action := runKey(t, m, "p")
	m = nm.(Model)
	if action != "" {
		t.Fatalf("pressing p on a drawn wild must not send a direct play, got %q", action)
	}
	if m.mode() != ModeColor {
		t.Fatalf("pressing p on a drawn wild should open the color picker, got %v", m.mode())
	}
	// Choosing a color now emits play_drawn with that color.
	_, a2 := runKey(t, m, "r")
	if !strings.HasPrefix(a2, "play_drawn:") || !strings.HasSuffix(a2, ":red") {
		t.Fatalf("choosing color should emit play_drawn with red, got %q", a2)
	}
}

// TestKey_Stack verifies the stacking phase mode and the accept key.
func TestKey_Stack(t *testing.T) {
	snap := testSnap(func(v *game.GameView) {
		v.Phase = string(game.PhaseStack)
		v.Stack = &game.StackView{Credits: 4, YourDecision: true}
	})
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: snap})
	m = nm.(Model)
	if m.mode() != ModeStack {
		t.Fatalf("expected stack mode, got %v", m.mode())
	}
	_, a := runKey(t, m, "a")
	if !strings.HasPrefix(a, "accept_stack") {
		t.Fatalf("a should emit accept_stack, got %q", a)
	}
}

// TestView_NonDecisionStackBanner verifies a spectator/sibling player sees the
// "被叠加惩罚" banner in the normal game render while a stack is in flight.
func TestView_NonDecisionStackBanner(t *testing.T) {
	snap := testSnap(func(v *game.GameView) {
		v.Phase = string(game.PhaseStack)
		v.Stack = &game.StackView{Credits: 6, YourDecision: false}
		v.CurrentPlayer = "p1" // Bob is charged; this model is Alice (p0)
	})
	m := newModel("p0")
	nm, _ := m.Update(SnapshotMsg{View: snap})
	m = nm.(Model)
	if m.mode() == ModeStack {
		t.Fatalf("non-decision model must not be in stack-decision mode, got %v", m.mode())
	}
	out := m.render(RenderOpts{Color: false})
	if !strings.Contains(out, "被叠加惩罚") || !strings.Contains(out, "累计 6 张") {
		t.Fatalf("non-decision stack banner missing: %q", out)
	}
}
