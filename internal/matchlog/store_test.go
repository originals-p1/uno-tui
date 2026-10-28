package matchlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAppendPreservesDayAndCorruptFiles(t *testing.T) {
	base := filepath.Join(t.TempDir(), "games.json")
	rec := MatchRecord{Time: time.Now()}
	for i := 0; i < 101; i++ {
		if err := Append(base, rec); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(filepath.Dir(base), "games", rec.Time.Local().Format("2006-01-02")+".json")
	b, _ := os.ReadFile(path)
	var records []MatchRecord
	if err := json.Unmarshal(b, &records); err != nil || len(records) != 101 {
		t.Fatalf("lost records: %v %d", err, len(records))
	}
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if Append(base, rec) == nil {
		t.Fatal("expected corruption error")
	}
	b, _ = os.ReadFile(path)
	if string(b) != "broken" {
		t.Fatal("overwrote corrupt file")
	}
}
