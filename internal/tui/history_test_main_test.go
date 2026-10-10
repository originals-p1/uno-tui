package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain redirects the match-history database to a temp file so the real
// per-user games.json is never touched by tests.
func TestMain(m *testing.M) {
	historyFileOverride = filepath.Join(os.TempDir(), "uno_test_games.json")
	_ = os.Remove(historyFileOverride)
	code := m.Run()
	_ = os.Remove(historyFileOverride)
	os.Exit(code)
}
