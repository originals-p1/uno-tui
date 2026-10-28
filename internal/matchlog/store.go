package matchlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

var storeMu sync.Mutex

// LegacyPath locates the legacy history beside the executable.
func LegacyPath() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "games.json")
	}
	return "games.json"
}

// Append writes only the record's day. A corrupt existing file is preserved.
func Append(legacyPath string, rec MatchRecord) error {
	storeMu.Lock()
	defer storeMu.Unlock()
	path := filepath.Join(filepath.Dir(legacyPath), "games", rec.Time.Local().Format("2006-01-02")+".json")
	var records []MatchRecord
	b, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(b, &records); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	records = append([]MatchRecord{rec}, records...)
	b, err = json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".games-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
