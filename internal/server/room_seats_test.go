package server

import (
	"fmt"
	"math/rand"
	"reflect"
	"terminal-uno/internal/game"
	"testing"
)

func seatRoom(seed int64, mode string) *Room {
	r := NewLobbyRoom(DefaultConfig(), nil)
	r.rng = rand.NewSource(seed)
	r.config.Game.FirstPlayerMode = mode
	r.config.Game.WinMode = game.WinScore
	for i := 0; i < 4; i++ {
		id := fmt.Sprintf("p%d", i)
		r.order = append(r.order, id)
		r.players[id] = &Member{ID: id, Name: "name-" + id, Ready: true, Connected: true, IsHost: i == 0}
	}
	r.hostID = "p0"
	r.sessions["host"] = &Session{PlayerID: "p0"}
	return r
}

func TestRoom_RandomSeatsEachRoundPreserveIdentity(t *testing.T) {
	for _, mode := range []string{game.FirstHost, game.FirstAfterDealer, game.FirstRandom} {
		t.Run(mode, func(t *testing.T) {
			r := seatRoom(42, mode)
			out := r.processStartGame(&Request{SessionID: "host"})
			if !out.OK() {
				t.Fatalf("start: %+v", out)
			}
			orders := map[string]bool{}
			r.scores = map[string]int{"p0": 120, "p1": -30, "p2": 60, "p3": 15}
			for round := 0; round < 12; round++ {
				ids := []string{}
				seen := map[string]bool{}
				for _, p := range r.game.Players {
					if seen[p.ID] || r.players[p.ID] == nil || p.Name != "name-"+p.ID {
						t.Fatalf("invalid identity: %+v", p)
					}
					seen[p.ID] = true
					ids = append(ids, p.ID)
					if len(p.Hand) != 7 {
						t.Fatal("incorrect fresh hand")
					}
				}
				if len(seen) != 4 {
					t.Fatal("lost players")
				}
				orders[fmt.Sprint(ids)] = true
				if mode == game.FirstHost && r.game.Players[r.game.CurrentIndex].ID != "p0" {
					t.Fatal("host first no longer refers to actual host")
				}
				if mode == game.FirstAfterDealer && r.game.CurrentIndex != 1 {
					t.Fatal("dealer successor changed")
				}
				for id, v := range r.buildViews() {
					if !reflect.DeepEqual(v.Scores, map[string]int{"p0": 120, "p1": -30, "p2": 60, "p3": 15}) {
						t.Fatal("scores moved with seats")
					}
					viewIDs := []string{}
					for _, p := range v.Players {
						viewIDs = append(viewIDs, p.ID)
					}
					if !reflect.DeepEqual(ids, viewIDs) {
						t.Fatal("clients disagree on seats")
					}
					for _, p := range r.game.Players {
						if p.ID == id && v.YourHand[0].ID != p.Hand[0].ID {
							t.Fatal("private hand assigned to wrong identity")
						}
					}
				}
				if !r.players["p0"].IsHost || r.sessionPlayer(&Request{SessionID: "host"}) != "p0" {
					t.Fatal("host/session identity changed")
				}
				r.startNextRound()
			}
			if len(orders) < 3 {
				t.Fatalf("seats not randomized each round: %v", orders)
			}
		})
	}
}

func TestRoom_OpeningSeatsVaryAcrossSeeds(t *testing.T) {
	orders := map[string]bool{}
	for seed := int64(0); seed < 16; seed++ {
		r := seatRoom(seed, game.FirstHost)
		if out := r.processStartGame(&Request{SessionID: "host"}); !out.OK() {
			t.Fatal(out)
		}
		ids := []string{}
		for _, p := range r.game.Players {
			ids = append(ids, p.ID)
		}
		orders[fmt.Sprint(ids)] = true
	}
	if len(orders) < 3 {
		t.Fatalf("opening seats remain fixed: %v", orders)
	}
}
