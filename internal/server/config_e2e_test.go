package server

import (
	"testing"
	"time"

	"terminal-uno/internal/client"
	"terminal-uno/internal/game"
	"terminal-uno/internal/protocol"
)

// TestHostConfigFlow: host edits hand-size to 5, guest sees the broadcast, and
// the started game deals 5 cards — proving the host's rule change takes effect.
func TestHostConfigFlowEndToEnd(t *testing.T) {
	cfg := DefaultConfig()
	cfg.HostBootstrapToken = "host-tok"
	cfg.RoomKey = "sekret"
	s := NewServer(cfg, NewRealClock(), nil)
	t.Cleanup(s.Close)
	ln, err := s.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	hostConfig := make(chan protocol.ConfigSnapshot, 4)
	guestConfig := make(chan protocol.ConfigSnapshot, 4)
	host := client.NewClient(client.Config{Address: addr, Name: "Alice", RoomKey: "sekret", HostBootstrapToken: "host-tok", ResponseTimeout: 3 * time.Second})
	guest := client.NewClient(client.Config{Address: addr, Name: "Bob", RoomKey: "sekret", ResponseTimeout: 3 * time.Second})
	host.OnConfig = func(cs protocol.ConfigSnapshot) { hostConfig <- cs }
	guest.OnConfig = func(cs protocol.ConfigSnapshot) { guestConfig <- cs }
	if err := host.Connect(addr); err != nil {
		t.Fatalf("host connect: %v", err)
	}
	if err := guest.Connect(addr); err != nil {
		t.Fatalf("guest connect: %v", err)
	}
	drain := func(ch chan protocol.ConfigSnapshot) {
		for {
			select { case <-ch: default: return }
		}
	}
	drain(hostConfig)
	drain(guestConfig)

	nc := game.DefaultConfig()
	nc.Game.InitialHandSize = 5
	resp, err := host.UpdateConfig(nc, 0, host.Revision())
	if err != nil || !resp.OK {
		t.Fatalf("host update config failed: %v %+v", err, resp)
	}
	// Both see version 1 with hand size 5.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case cs := <-hostConfig:
			if cs.Config.Game.InitialHandSize == 5 && cs.ConfigVersion == 1 {
				goto hostGot
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("host did not receive updated config")
hostGot:
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case cs := <-guestConfig:
			if cs.Config.Game.InitialHandSize == 5 {
				goto guestGot
			}
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatal("guest did not receive broadcast config")
guestGot:

	// Guest ready, host starts -> game deals 5 cards per the new config.
	if _, err := guest.Ready(true, guest.Revision()); err != nil {
		t.Fatalf("guest ready: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := host.StartGame(host.Revision()); err != nil {
		t.Fatalf("start: %v", err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if gs := guest.Snapshot(); gs != nil && len(gs.YourHand) == 5 {
			t.Logf("OK: guest was dealt %d cards (config initial_hand_size=5)", len(gs.YourHand))
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	if gs := guest.Snapshot(); gs != nil {
		t.Fatalf("waned 5-card hands, got %d", len(gs.YourHand))
	}
	t.Fatal("game never started with revised config")
}
