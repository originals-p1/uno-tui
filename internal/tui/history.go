package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"terminal-uno/internal/matchlog"
	"time"

	"terminal-uno/internal/game"
)

// MatchRecord is a persisted record of a completed game, written by every
// client that witnesses the game finish (readme §toast/log). History is kept
// locally per user so privacy (never share opponents' hands) is preserved.
type MatchRecord = matchlog.MatchRecord
type RecordPlayer = matchlog.RecordPlayer

// historyFileOverride redirects the history database location (used by tests
// to avoid writing into the executable's directory).
var historyFileOverride string

// historyFilePath is the match-history database: a games.json sibling of the
// running executable, so it travels with the program (works identically for
// Windows/macOS/Linux). Falls back to the current directory, then to a
// bare filename, if the executable location cannot be determined.
func historyFilePath() string {
	if historyFileOverride != "" {
		return historyFileOverride
	}
	return matchlog.LegacyPath()
}

// loadHistory reads the local match history, newest first. It never fails
// hard: a missing or corrupt file yields an empty history.
func loadHistory() []MatchRecord {
	recs := readHistoryFile(historyFilePath())
	paths, _ := filepath.Glob(filepath.Join(filepath.Dir(historyFilePath()), "games", "????-??-??.json"))
	for _, p := range paths {
		recs = append(recs, readHistoryFile(p)...)
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Time.After(recs[j].Time) })
	if len(recs) > maxHistoryRecords {
		recs = recs[:maxHistoryRecords]
	}
	return recs
}
func readHistoryFile(path string) []MatchRecord {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var recs []MatchRecord
	if err := json.Unmarshal(b, &recs); err != nil {
		return nil
	}
	return recs
}

// appendAndSave adds rec to the persisted history (newest first), capping the
// store at maxHistoryRecords, and writes it back. Returns the new history.
func appendAndSave(recs []MatchRecord, rec MatchRecord) []MatchRecord {
	recs = append([]MatchRecord{rec}, recs...)
	if len(recs) > maxHistoryRecords {
		recs = recs[:maxHistoryRecords]
	}
	_ = matchlog.Append(historyFilePath(), rec)
	return recs
}

func saveHistory(recs []MatchRecord) error {
	return saveHistoryFile(historyFilePath(), recs)
}
func saveHistoryFile(path string, recs []MatchRecord) error {
	b, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}

// recordFromView builds a MatchRecord from a finished game snapshot. The full
// privacy-safe event timeline (v.History) is captured so a recorded game can
// be replayed for bug triage.
func recordFromView(v *game.GameView, seq int64, configVersion uint64) MatchRecord {
	rec := MatchRecord{
		Seq:           seq,
		Time:          time.Now(),
		EndReason:     v.EndReason,
		ConfigVersion: configVersion,
		Events:        append([]game.GameEvent{}, v.History...),
	}
	for _, p := range v.Players {
		if p.ID == v.WinnerPlayerID {
			rec.Winner = p.Name
		}
		rec.Players = append(rec.Players, RecordPlayer{
			Name:      p.Name,
			CardCount: p.CardCount,
			IsHost:    p.IsHost,
			Winner:    p.ID == v.WinnerPlayerID,
		})
	}
	return rec
}

// maxHistoryRecords caps how many games are retained locally.
const maxHistoryRecords = 100
