package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"terminal-uno/internal/game"
)

// Persisted house rules (rule.md §0.5). The host's last-applied rules are saved
// to a JSON file so they survive a restart — the lobby re-seeds from it when
// hosting again, instead of always resetting to the defaults.

// rulesConfigFileOverride redirects the rules-file location (tests).
var rulesConfigFileOverride string

// envUnoRules overrides the rules-file location via environment (tests/advanced).
const envUnoRules = "UNO_RULES"

// rulesConfigFilePath returns the persisted rules path, a sibling of the running
// executable (travels with the program, like the match history).
func rulesConfigFilePath() string {
	if rulesConfigFileOverride != "" {
		return rulesConfigFileOverride
	}
	if p := os.Getenv(envUnoRules); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" {
			return filepath.Join(dir, "uno-rules.json")
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return filepath.Join(cwd, "uno-rules.json")
	}
	return "uno-rules.json"
}

// loadRulesConfig reads the persisted house rules. A missing, corrupt or invalid
// file yields the default config (all defaults apply).
func loadRulesConfig() game.Config {
	c := game.DefaultConfig()
	b, err := os.ReadFile(rulesConfigFilePath())
	if err != nil {
		return c
	}
	var loaded game.Config
	if json.Unmarshal(b, &loaded) != nil {
		return c
	}
	if loaded.Validate() != nil {
		return c
	}
	if loaded.Game.DeckType == game.DeckClassic108 {
		loaded.Game.DeckType = game.DeckAction140
	}
	return loaded
}

// saveRulesConfig persists the supplied rules owner-only.
func saveRulesConfig(c game.Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(rulesConfigFilePath()); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return os.WriteFile(rulesConfigFilePath(), b, 0o600)
}
