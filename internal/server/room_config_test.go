package server

import (
	"testing"

	"terminal-uno/internal/game"
)

// hostHello joins as host; guestHello joins as a normal player.
func hostHello() *Request { return hello("Alice", "topsecret", "", "bootstrap-token") }

func guestHello(name string) *Request { return hello(name, "topsecret", "", "") }

func updateReq(rev uint64, cfg game.Config) *Request {
	return &Request{
		Kind: KindUpdateConfig, SessionID: "s-host", RequestID: "uc-1", Expected: exp(rev),
		ConfigUpdate: &ConfigUpdateReq{ExpectedConfigVersion: 0, Config: cfg},
	}
}

// TestConfig_NonHostRejected verifies only the host may change the rules.
func TestConfig_NonHostRejected(t *testing.T) {
	r, _ := lobbyRoom(t, nil)
	r.Submit(hostHello())
	guest := r.Submit(guestHello("Bob"))
	_ = guest

	req := &Request{
		Kind: KindUpdateConfig, SessionID: guest.SessionID, RequestID: "uc-x",
		ConfigUpdate: &ConfigUpdateReq{ExpectedConfigVersion: 0, Config: game.DefaultConfig()},
	}
	out := r.Submit(req)
	if out.ErrCode != CodeNotHost {
		t.Fatalf("non-host config update: got %q, want %q", out.ErrCode, CodeNotHost)
	}
}

// TestConfig_InvalidRejected verifies the authoritative Validate gate rejects bad configs.
func TestConfig_InvalidRejected(t *testing.T) {
	r, _ := lobbyRoom(t, nil)
	host := r.Submit(hostHello())

	bad := game.DefaultConfig()
	bad.Room.MaxPlayers = 9 // out of v2 range
	req := &Request{
		Kind: KindUpdateConfig, SessionID: host.SessionID, RequestID: "uc-bad",
		ConfigUpdate: &ConfigUpdateReq{ExpectedConfigVersion: 0, Config: bad},
	}
	out := r.Submit(req)
	if out.ErrCode != CodeConfigInvalid {
		t.Fatalf("invalid config: got %q, want %q", out.ErrCode, CodeConfigInvalid)
	}
}

// TestConfig_StaleVersionRejects verifies optimistic-concurrency staleness.
func TestConfig_StaleVersionRejects(t *testing.T) {
	r, _ := lobbyRoom(t, nil)
	host := r.Submit(hostHello())

	// Commit a first config change -> configVersion becomes 1.
	first := game.DefaultConfig()
	first.Game.InitialHandSize = 5
	req1 := updateReq(0, first)
	req1.SessionID = host.SessionID
	out1 := r.Submit(req1)
	if !out1.OK() {
		t.Fatalf("first update failed: %+v", out1)
	}
	if !out1.ConfigChanged || out1.ConfigSnapshot == nil || out1.ConfigSnapshot.ConfigVersion != 1 {
		t.Fatalf("first update snapshot wrong: %+v", out1.ConfigSnapshot)
	}

	// Resubmit with expected version 0 (now stale) -> CONFIG_STALE.
	req2 := updateReq(0, first)
	req2.SessionID = host.SessionID
	out2 := r.Submit(req2)
	if out2.ErrCode != CodeConfigStale {
		t.Fatalf("stale update: got %q, want %q", out2.ErrCode, CodeConfigStale)
	}
	if out2.ConfigSnapshot == nil || out2.ConfigSnapshot.ConfigVersion != 1 {
		t.Fatalf("stale reply should carry current snapshot: %+v", out2.ConfigSnapshot)
	}
}

// TestConfig_LockedDuringGame verifies rules lock once a game starts.
func TestConfig_LockedDuringGame(t *testing.T) {
	r, _ := lobbyRoom(t, nil)
	host := r.Submit(hostHello())
	guest := r.Submit(guestHello("Bob"))
	startConfig := game.DefaultConfig()
	startConfig.Game.InitialHandSize = 5
	upd := updateReq(0, startConfig)
	upd.SessionID = host.SessionID
	if out := r.Submit(upd); !out.OK() {
		t.Fatalf("update before start failed: %+v", out)
	}

	// Ready guest, then host starts.
	r.Submit(&Request{Kind: KindReady, SessionID: guest.SessionID, RequestID: "rdy", Expected: exp(0), Ready: &Ready{Value: true}})
	st := r.Submit(&Request{Kind: KindStartGame, SessionID: host.SessionID, RequestID: "st", Expected: exp(0)})
	if !st.OK() {
		t.Fatalf("start failed: %+v", st)
	}
	if !st.RulesLocked || st.ConfigSnapshot == nil || !st.ConfigSnapshot.Locked {
		t.Fatalf("start should lock rules: %+v", st)
	}
	// Each player's hand should reflect initial_hand_size=5.
	for _, v := range st.Commit.Views {
		if len(v.YourHand) != 5 {
			t.Fatalf("started hand has %d cards, want 5 (config initial_hand_size)", len(v.YourHand))
		}
	}

	// Further config changes are rejected while locked.
	upd2 := updateReq(1, game.DefaultConfig())
	upd2.SessionID = host.SessionID
	if out := r.Submit(upd2); out.ErrCode != CodeNotWaitingPhase {
		t.Fatalf("update while locked: got %q, want %q", out.ErrCode, CodeNotWaitingPhase)
	}
}
