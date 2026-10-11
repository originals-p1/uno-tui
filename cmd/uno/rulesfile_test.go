package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"terminal-uno/internal/game"
)

// setRulesPath redirects the persisted-rules location for the duration of a test.
func setRulesPath(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "uno-rules.json")
	t.Setenv(envUnoRules, path)
	return path
}

func TestRulesConfig_Roundtrip(t *testing.T) {
	path := setRulesPath(t)
	cfg := game.DefaultConfig()
	cfg.HouseRules.StackDrawTwo = true
	cfg.HouseRules.StackDrawFour = true
	cfg.HouseRules.MixedStacking = true
	cfg.Cards.ChallengeDrawFour = true
	cfg.Game.WinMode = game.WinScore
	cfg.Game.TargetScore = 300
	if err := saveRulesConfig(cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := loadRulesConfig()
	if !got.HouseRules.StackDrawTwo || !got.HouseRules.StackDrawFour || !got.HouseRules.MixedStacking {
		t.Fatalf("stacking flags not persisted: %+v", got.HouseRules)
	}
	if !got.Cards.ChallengeDrawFour {
		t.Fatal("combined challenge flag not persisted")
	}
	if got.Game.WinMode != game.WinScore || got.Game.TargetScore != 300 {
		t.Fatalf("score mode not persisted: %+v", got.Game)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("rules file not written: %v", err)
	}
}

func TestLoadRulesConfig_MissingFallsBackToDefault(t *testing.T) {
	setRulesPath(t) // no file written
	got := loadRulesConfig()
	def := game.DefaultConfig()
	if got.HouseRules != def.HouseRules || got.Game != def.Game {
		t.Fatal("missing rules file must fall back to defaults")
	}
}

func TestLoadRulesConfig_CorruptAndInvalidFallBack(t *testing.T) {
	path := setRulesPath(t)
	// Corrupt JSON.
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadRulesConfig(); got.Game.WinMode != game.DefaultConfig().Game.WinMode {
		t.Fatal("corrupt file must fall back to defaults")
	}
	// Valid JSON but invalid config (a value Validate rejects).
	bad := game.DefaultConfig()
	bad.Room.DisconnectTimeoutSec = -5 // Validate rejects negative timeouts
	b, _ := json.Marshal(bad)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadRulesConfig(); got.Room.DisconnectTimeoutSec == -5 {
		t.Fatal("invalid persisted config must be rejected and fall back to defaults")
	}
}
