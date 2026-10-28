package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDailyHistory(t *testing.T) {
	old := historyFileOverride
	defer func() { historyFileOverride = old }()
	historyFileOverride = filepath.Join(t.TempDir(), "games.json")
	first := MatchRecord{Time: time.Date(2026, 10, 27, 23, 0, 0, 0, time.Local), Winner: "A"}
	second := MatchRecord{Time: first.Time.Add(2 * time.Hour), Winner: "B"}
	appendAndSave(nil, first)
	appendAndSave(nil, second)
	for _, day := range []string{"2026-10-27", "2026-10-28"} {
		if _, err := os.Stat(filepath.Join(filepath.Dir(historyFileOverride), "games", day+".json")); err != nil {
			t.Fatal(err)
		}
	}
	recs := loadHistory()
	if len(recs) != 2 || recs[0].Winner != "B" {
		t.Fatalf("unexpected history: %+v", recs)
	}
}
