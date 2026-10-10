package game

import (
	"math/rand"
	"strings"
	"testing"
)

func TestDefaultConfig_Classic(t *testing.T) {
	c := DefaultConfig()
	if err := c.Validate(); err != nil {
		t.Fatalf("default config must validate: %v", err)
	}
	if c.Game.InitialHandSize != 7 || c.Game.InitialDir != DirClockwise {
		t.Fatalf("bad defaults: %+v", c.Game)
	}
	if !c.Cards.ChallengeDrawFour || !c.HouseRules.AutoUNO {
		t.Fatalf("bad defaults: challenge/auto-uno should be on")
	}
}

func TestValidate_Ranges(t *testing.T) {
	if err := (Config{Room: struct {
		MaxPlayers           int    `json:"max_players"`
		RequireAllReady      bool   `json:"require_all_ready"`
		AllowSpectators      bool   `json:"allow_spectators"`
		AllowReconnect       bool   `json:"allow_reconnect"`
		DisconnectTimeoutSec int    `json:"disconnect_timeout_seconds"`
		LeavePolicy          string `json:"leave_policy"`
		HostLeavePolicy      string `json:"host_leave_policy"`
	}{MaxPlayers: 9}}).Validate(); err == nil {
		t.Fatalf("want error for max_players 9 (v2 scope 2-4)")
	}
	c := DefaultConfig()
	c.Game.InitialHandSize = 6
	if err := c.Validate(); err == nil {
		t.Fatalf("want error for hand size 6")
	}
	c = DefaultConfig()
	c.Game.InitialDir = "north"
	if err := c.Validate(); err == nil {
		t.Fatalf("want error for bad direction")
	}
}

func TestValidate_FutureFieldsRejected(t *testing.T) {
	c := DefaultConfig()
	c.HouseRules.SevenZeroRule = true
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "seven_zero_rule") {
		t.Fatalf("want future-field rejection mentioning seven_zero_rule, got %v", err)
	}
	c = DefaultConfig()
	c.Game.WinMode = WinScore
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "win_mode") {
		t.Fatalf("want rejection of SCORE (future), got %v", err)
	}
}

func TestConfigJSON_DefaultOverlay(t *testing.T) {
	// Partial JSON should inherit defaults (e.g., only changing hand size).
	cfg, err := ParseConfig([]byte(`{"game":{"initial_hand_size":5}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if cfg.Game.InitialHandSize != 5 || cfg.Room.MaxPlayers != 4 || cfg.Cards.EnableSkip != true {
		t.Fatalf("overlay failed: %+v", cfg.Game)
	}
}

func TestConfig_HandSizeDrive(t *testing.T) {
	g, err := NewGameWithConfig([]string{"A", "B"}, nil, DefaultConfig(), rand.NewSource(1))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if len(g.Players[0].Hand) != 7 {
		t.Fatalf("default hand size = %d, want 7", len(g.Players[0].Hand))
	}
	c := DefaultConfig()
	c.Game.InitialHandSize = 5
	g2, err := NewGameWithConfig([]string{"A", "B"}, nil, c, rand.NewSource(1))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if len(g2.Players[0].Hand) != 5 {
		t.Fatalf("hand size = %d, want 5", len(g2.Players[0].Hand))
	}
}

func TestConfig_InitialDirectionCounter(t *testing.T) {
	c := DefaultConfig()
	c.Game.InitialDir = DirCounterClockwise
	g, err := NewGameWithConfig([]string{"A", "B", "C"}, nil, c, rand.NewSource(1))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if g.Direction != -1 {
		t.Fatalf("direction = %d, want -1", g.Direction)
	}
}

func TestConfig_FirstPlayerAfterDealer(t *testing.T) {
	g, err := NewGameWithConfig([]string{"A", "B", "C"}, nil, DefaultConfig(), rand.NewSource(1))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if g.CurrentIndex != 1 {
		t.Fatalf("first player idx = %d, want 1 (after dealer seat 0)", g.CurrentIndex)
	}
}

func TestConfig_HostFirst(t *testing.T) {
	c := DefaultConfig()
	c.Game.FirstPlayerMode = FirstHost
	g, err := NewGameWithConfig([]string{"A", "B", "C"}, nil, c, rand.NewSource(1))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if g.CurrentIndex != 0 {
		t.Fatalf("first player idx = %d, want 0 (host)", g.CurrentIndex)
	}
}

func TestConfig_DisableSkipRemovesEffect(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("s", ColorRed, RankSkip), tcard("r5", ColorRed, RankFive)},
			{tcard("b", ColorBlue, RankTwo)}, {tcard("g", ColorGreen, RankThree)}},
		nil, tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.Cards.EnableSkip = false
	// Skip, with its effect disabled, matches as a normal red card and merely
	// advances the turn (no player skipped).
	res, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "s"})
	if err != nil {
		t.Fatalf("play skip(disabled): %v", err)
	}
	for _, e := range res.Events {
		if e.Type == EventSkipped {
			t.Fatalf("skip effect should be disabled, got skipped event")
		}
	}
	if g.CurrentIndex != 1 {
		t.Fatalf("current = %d, want 1 (no skip)", g.CurrentIndex)
	}
}

func TestConfig_DisableDrawTwoTrimEffect(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("d2", ColorRed, RankDrawTwo), tcard("r5", ColorRed, RankFive)},
			{tcard("b", ColorBlue, RankTwo)}, {tcard("g", ColorGreen, RankThree)}},
		[]Card{tcard("x", ColorYellow, RankThree), tcard("y", ColorYellow, RankFour)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.Cards.EnableDrawTwo = false
	if _, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "d2"}); err != nil {
		t.Fatalf("play d2(disabled): %v", err)
	}
	if len(g.Players[1].Hand) != 1 {
		t.Fatalf("target hand = %d, want unchanged 1 (no draw penalty)", len(g.Players[1].Hand))
	}
	if g.CurrentIndex != 1 {
		t.Fatalf("current = %d, want 1 (no skip)", g.CurrentIndex)
	}
}

func TestConfig_DisableWildMakesDead(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("w", ColorWild, RankWild), tcard("r5", ColorRed, RankFive)}},
		[]Card{tcard("x", ColorYellow, RankThree)},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.Cards.EnableWild = false
	lm := g.LegalMoves("p0")
	if len(lm.PlayableIDs) != 1 || lm.PlayableIDs[0] != "r5" {
		t.Fatalf("wild should be dead when disabled, playable=%v", lm.PlayableIDs)
	}
}

func TestConfig_ActionFinishDisallowed(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("s", ColorRed, RankSkip)}},
		[]Card{},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.Cards.AllowActionFinish = false
	_, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "s"})
	if err == nil || err.(*GameError).Code != ErrCodeActionFinishNotAllowed {
		t.Fatalf("want ACTION_FINISH_DISALLOWED, got %v", err)
	}
	// A number card may still finish.
	g2 := mkGame(
		[][]Card{{tcard("r5", ColorRed, RankFive)}},
		[]Card{},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	g2.Config.Cards.AllowActionFinish = false
	res, err := g2.Apply("p0", Action{Type: ActionPlayCard, CardID: "r5"})
	if err != nil || !res.Ended {
		t.Fatalf("number card should finish: ended=%v err=%v", res.Ended, err)
	}
}

func TestConfig_AutoUNODisabled(t *testing.T) {
	g := mkGame(
		[][]Card{{tcard("r5", ColorRed, RankFive), tcard("r6", ColorRed, RankSix)}},
		[]Card{},
		tcard("top", ColorRed, RankTwo), ColorRed, 1, 0, PhaseTurn,
	)
	g.Config.HouseRules.AutoUNO = false
	res, err := g.Apply("p0", Action{Type: ActionPlayCard, CardID: "r5"})
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	for _, e := range res.Events {
		if e.Type == EventUNOCalled {
			t.Fatalf("auto-uno should be disabled")
		}
	}
	if !g.Players[0].HasUNO {
		// flag state may still be tracked internally, but no event should fire
	}
}
