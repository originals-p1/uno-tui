package client

import (
	"testing"
	"time"

	"terminal-uno/internal/game"
	"terminal-uno/internal/protocol"
	"terminal-uno/internal/server"
)

func TestClient_HelloWelcomeAndStart(t *testing.T) {
	cfg := server.DefaultConfig()
	cfg.HostBootstrapToken = "host-tok"
	cfg.RoomKey = "sekret"
	s := server.NewServer(cfg, server.NewRealClock(), nil)
	t.Cleanup(s.Close)
	ln, err := s.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	// Host client.
	host := NewClient(Config{Address: addr, Name: "Alice", RoomKey: "sekret",
		HostBootstrapToken: "host-tok", Logger: nil, ResponseTimeout: 3 * time.Second})
	if err := host.Connect(addr); err != nil {
		t.Fatalf("host connect: %v", err)
	}
	id := host.Identity()
	if !id.IsHost || id.PlayerID == "" || id.SessionID == "" || id.ReconnectToken == "" {
		t.Fatalf("host identity incomplete: %+v", id)
	}

	// Guest client.
	guest := NewClient(Config{Address: addr, Name: "Bob", RoomKey: "sekret",
		ResponseTimeout: 3 * time.Second})
	if err := guest.Connect(addr); err != nil {
		t.Fatalf("guest connect: %v", err)
	}
	if guest.Identity().IsHost {
		t.Fatalf("guest should not be host")
	}

	rev := host.Revision()
	if _, err := guest.Ready(true, rev); err != nil {
		t.Fatalf("guest ready: %v", err)
	}
	// Wait briefly for snapshot broadcasts to propagate.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if host.Revision() >= rev+1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if _, err := host.StartGame(rev); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Game should begin: each client converges on a snapshot with 7-card hands.
	waitFor := func(c *Client, want uint64) bool {
		dl := time.Now().Add(3 * time.Second)
		for time.Now().Before(dl) {
			if c.Revision() >= want {
				return true
			}
			time.Sleep(10 * time.Millisecond)
		}
		return false
	}
	if !waitFor(host, 4) {
		t.Fatalf("host revision=%d, wanted >=4", host.Revision())
	}
	snap := host.Snapshot()
	if snap == nil || len(snap.YourHand) != 7 {
		t.Fatalf("host snapshot hand=%d, want 7", len(snap.YourHand))
	}
	if len(snap.Players) != 2 {
		t.Fatalf("host sees %d players, want 2", len(snap.Players))
	}
	if !waitFor(guest, 4) {
		t.Fatalf("guest revision=%d, wanted >=4", guest.Revision())
	}
	if gs := guest.Snapshot(); gs == nil || len(gs.YourHand) != 7 {
		t.Fatalf("guest snapshot hand not 7: %+v", gs)
	}

	host.Close()
	guest.Close()
}

func TestClient_ReconnectResumesSession(t *testing.T) {
	cfg := server.DefaultConfig()
	cfg.HostBootstrapToken = "host-tok"
	cfg.RoomKey = "sekret"
	s := server.NewServer(cfg, server.NewRealClock(), nil)
	t.Cleanup(s.Close)
	ln, err := s.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	host := NewClient(Config{Address: addr, Name: "Alice", RoomKey: "sekret",
		HostBootstrapToken: "host-tok", ResponseTimeout: 3 * time.Second})
	if err := host.Connect(addr); err != nil {
		t.Fatalf("host connect: %v", err)
	}
	firstID := host.Identity().PlayerID
	rtok := host.Identity().ReconnectToken
	host.Close() // drop the connection (session paused server-side)

	// Reconnect using the same identity pipeline: attach with the token.
	host2 := NewClient(Config{Address: addr, Name: "Alice", RoomKey: "sekret",
		ReconnectToken: rtok, ResponseTimeout: 3 * time.Second})
	if err := host2.Connect(addr); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	if got := host2.Identity().PlayerID; got != firstID {
		t.Fatalf("reconnected playerID=%q, want %q", got, firstID)
	}
	host2.Close()
}

func TestClient_UpdateConfigAndRelay(t *testing.T) {
	cfg := server.DefaultConfig()
	cfg.HostBootstrapToken = "host-tok"
	cfg.RoomKey = "sekret"
	s := server.NewServer(cfg, server.NewRealClock(), nil)
	t.Cleanup(s.Close)
	ln, err := s.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()

	host := NewClient(Config{Address: addr, Name: "Alice", RoomKey: "sekret",
		HostBootstrapToken: "host-tok", ResponseTimeout: 3 * time.Second})
	guest := NewClient(Config{Address: addr, Name: "Bob", RoomKey: "sekret",
		ResponseTimeout: 3 * time.Second})

	// Wire OnConfig before connecting so the welcome-time snapshot is captured.
	hostSnap := make(chan protocol.ConfigSnapshot, 2)
	guestSnap := make(chan protocol.ConfigSnapshot, 2)
	host.OnConfig = func(cs protocol.ConfigSnapshot) { hostSnap <- cs }
	guest.OnConfig = func(cs protocol.ConfigSnapshot) { guestSnap <- cs }

	if err := host.Connect(addr); err != nil {
		t.Fatalf("host connect: %v", err)
	}
	if err := guest.Connect(addr); err != nil {
		t.Fatalf("guest connect: %v", err)
	}

	// Both clients should receive the initial CONFIG_SNAPSHOT after welcome.
	select {
	case cs := <-hostSnap:
		if cs.ConfigVersion != 0 || cs.Config.Room.MaxPlayers != 4 {
			t.Fatalf("host initial config snapshot wrong: %+v", cs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("host did not receive initial config snapshot")
	}
	select {
	case cs := <-guestSnap:
		if cs.ConfigVersion != 0 || cs.Config.Room.MaxPlayers != 4 {
			t.Fatalf("guest initial config snapshot wrong: %+v", cs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("guest did not receive initial config snapshot")
	}

	// Host changes initial_hand_size to 5.
	nc := game.DefaultConfig()
	nc.Game.InitialHandSize = 5
	resp, err := host.UpdateConfig(nc, 0, host.Revision())
	if err != nil {
		t.Fatalf("update config: %v", err)
	}
	if !resp.OK {
		t.Fatalf("update config rejected: %s %s", resp.ErrCode, resp.Message)
	}
	// Every player receives a broadcast CONFIG_SNAPSHOT with version 1.
	select {
	case cs := <-hostSnap:
		if cs.ConfigVersion != 1 || cs.Config.Game.InitialHandSize != 5 {
			t.Fatalf("host updated config snapshot wrong: %+v", cs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("host did not receive updated config snapshot")
	}
	select {
	case cs := <-guestSnap:
		if cs.ConfigVersion != 1 || cs.Config.Game.InitialHandSize != 5 {
			t.Fatalf("guest updated config snapshot wrong: %+v", cs)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("guest did not receive updated config snapshot")
	}

	host.Close()
	guest.Close()
}
