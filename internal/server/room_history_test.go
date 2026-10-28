package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"terminal-uno/internal/game"
	"terminal-uno/internal/matchlog"
	"testing"
)

func TestPrivatePlaySnapshots(t *testing.T) {
	r := testRoom(t, 0)
	r.historyBase = filepath.Join(t.TempDir(), "games.json")
	out := r.Submit(NewRequest(KindPlayCard, "s0", "first", exp(0), game.Action{Type: game.ActionPlayCard, CardID: "a3"}))
	if !out.OK() {
		t.Fatal(out)
	}
	if len(r.plays) != 1 || len(r.plays[0].Before[0].Cards) != 2 || len(r.plays[0].After[0].Cards) != 1 {
		t.Fatalf("bad snapshots: %+v", r.plays)
	}
	b, _ := json.Marshal(out.Commit)
	if strings.Contains(string(b), "hands_before") {
		t.Fatal("private history leaked")
	}
	bad := r.Submit(NewRequest(KindPlayCard, "s1", "invalid", exp(1), game.Action{Type: game.ActionPlayCard, CardID: "missing"}))
	if bad.OK() || len(r.plays) != 1 {
		t.Fatal("invalid action recorded")
	}
	out = r.Submit(NewRequest(KindPlayCard, "s1", "last", exp(1), game.Action{Type: game.ActionPlayCard, CardID: "b4"}))
	if !out.OK() {
		t.Fatal(out)
	}
	if len(r.plays) != 2 || len(r.plays[0].Before[1].Cards) != 1 || len(r.plays[1].After[1].Cards) != 0 {
		t.Fatal("snapshots mutated or winning play omitted")
	}
	files, _ := filepath.Glob(filepath.Join(filepath.Dir(r.historyBase), "games", "*.json"))
	if len(files) != 1 {
		t.Fatal(files)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var records []matchlog.MatchRecord
	if err = json.Unmarshal(b, &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || len(records[0].Plays) != 2 || records[0].Winner != "Bob" {
		t.Fatalf("bad saved record: %+v", records)
	}
	r.saveMatch()
	b, _ = os.ReadFile(files[0])
	json.Unmarshal(b, &records)
	if len(records) != 1 {
		t.Fatal("duplicate record")
	}
}
