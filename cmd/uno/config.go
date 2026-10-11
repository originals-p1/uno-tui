package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// wizardConfig is the persisted last-used startup configuration. It is loaded
// at launch so the guided wizard can prefill every prompt with the previous
// values — pressing Enter all the way then reuses your configuration. The room
// key itself is NEVER stored here; only the key-file path is remembered.
type wizardConfig struct {
	Name    string `json:"name,omitempty"`
	Mode    string `json:"mode,omitempty"` // "host" | "join"
	Port    int    `json:"port,omitempty"`
	Addr    string `json:"addr,omitempty"`
	UseKey  bool   `json:"use_key,omitempty"`
	KeyFile string `json:"key_file,omitempty"`
}

// wizardConfigFileOverride redirects the config location (tests).
var wizardConfigFileOverride string

// envUnoConfig overrides the config location via environment (used by tests,
// and handy for advanced users).
const envUnoConfig = "UNO_CONFIG"

// wizardConfigFilePath returns the persisted-config path, a sibling of the
// running executable (travels with the program, like the match history).
func wizardConfigFilePath() string {
	if wizardConfigFileOverride != "" {
		return wizardConfigFileOverride
	}
	if p := os.Getenv(envUnoConfig); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); dir != "" {
			return filepath.Join(dir, "uno-config.json")
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		return filepath.Join(cwd, "uno-config.json")
	}
	return "uno-config.json"
}

// loadWizardConfig reads the previous configuration; a missing or corrupt file
// yields an empty config (all defaults apply).
func loadWizardConfig() wizardConfig {
	var c wizardConfig
	b, err := os.ReadFile(wizardConfigFilePath())
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, &c)
	return c
}

// save persists the configuration owner-only. Never stores the room key.
func (c *wizardConfig) save() error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(wizardConfigFilePath()); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	return os.WriteFile(wizardConfigFilePath(), b, 0o600)
}
