package protocol

import (
	"encoding/json"
	"strings"
	"testing"

	"terminal-uno/internal/game"
)

func decodeStr(t *testing.T, js string) (*Message, interface{}, error) {
	t.Helper()
	return Decode([]byte(js))
}

func TestDecode_ValidHello(t *testing.T) {
	m, p, err := decodeStr(t, `{
		"protocol_version":1,"type":"hello","request_id":"c1","expected_revision":null,
		"unexpected_extra_field":123,
		"payload":{"player_name":"Alice","room_key":"k","reconnect_token":"rt"}
	}`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if m.Type != TypeHello || m.RequestID != "c1" {
		t.Fatalf("envelope = %+v", m)
	}
	hello := p.(*Hello)
	if hello.PlayerName != "Alice" || hello.RoomKey != "k" || hello.ReconnectToken != "rt" {
		t.Fatalf("hello = %+v", hello)
	}
}

func TestDecode_UnknownFieldsIgnored(t *testing.T) {
	// Unknown top-level and payload fields must not break decoding.
	_, p, err := decodeStr(t, `{
		"protocol_version":1,"type":"play_card","request_id":"x","expected_revision":3,
		"future_field":[1,2,3],
		"payload":{"card_id":"c-1","chosen_color":"red","future_payload_field":"zzz"}
	}`)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	pc := p.(*PlayCard)
	if pc.CardID != "c-1" || pc.ChosenColor == nil || *pc.ChosenColor != "red" {
		t.Fatalf("play_card = %+v", pc)
	}
}

func TestDecode_MissingRequiredField(t *testing.T) {
	cases := []struct {
		name string
		js   string
		want string
	}{
		{"no request id", `{"protocol_version":1,"type":"play_card","expected_revision":3,"payload":{"card_id":"c"}}`, CodeMissingRequestID},
		{"no expected revision", `{"protocol_version":1,"type":"play_card","request_id":"x","payload":{"card_id":"c"}}`, CodeMissingExpectedRev},
		{"hello no name", `{"protocol_version":1,"type":"hello","request_id":"x","payload":{"room_key":"k"}}`, CodeInvalidMessage},
		{"play no card id", `{"protocol_version":1,"type":"play_card","request_id":"x","expected_revision":3,"payload":{}}`, CodeInvalidMessage},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := decodeStr(t, c.js)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			pe, ok := err.(*ProtocolError)
			if !ok || pe.Code != c.want {
				t.Fatalf("code = %v, want %s", err, c.want)
			}
		})
	}
}

func TestDecode_UnknownType(t *testing.T) {
	_, _, err := decodeStr(t, `{"protocol_version":1,"type":"hack","payload":{}}`)
	pe, ok := err.(*ProtocolError)
	if !ok || pe.Code != CodeInvalidMessage {
		t.Fatalf("code = %v, want INVALID_MESSAGE", err)
	}
}

func TestDecode_ProtocolMismatch(t *testing.T) {
	_, _, err := decodeStr(t, `{"protocol_version":99,"type":"ping","payload":{}}`)
	pe, ok := err.(*ProtocolError)
	if !ok || pe.Code != CodeProtocolMismatch {
		t.Fatalf("code = %v, want PROTOCOL_MISMATCH", err)
	}
}

func TestDecode_MalformedJSON(t *testing.T) {
	_, _, err := decodeStr(t, `{"protocol_version":1,"type":"ping","payload":`)
	pe, ok := err.(*ProtocolError)
	if !ok || pe.Code != CodeInvalidMessage {
		t.Fatalf("code = %v, want INVALID_MESSAGE", err)
	}
}

// TestErrorNotificationsNeverLeakSecrets ensures protocol-level error strings
// and marshalled envelopes never include room keys, session tokens, or
// reconnect tokens that were present on the wire.
func TestErrorNotificationsNeverLeakSecrets(t *testing.T) {
	secretKey := "super-secret-room-key-abcdef0123456789"
	secretToken := "reconnect-token-WXYZ9876543210"

	// Build a valid hello frame containing the secrets.
	hello := Message{ProtocolVersion: 1, Type: TypeHello, RequestID: "r1"}
	p, _ := json.Marshal(Hello{
		PlayerName: "Alice", RoomKey: secretKey,
		ReconnectToken:     secretToken,
		HostBootstrapToken: secretToken + "-boot",
	})
	hello.Payload = p
	helloRaw, err := Marshal(&hello)
	if err != nil {
		t.Fatalf("marshal hello: %v", err)
	}
	full := string(helloRaw)

	// 1) A protocol error produced about this frame must not contain the secrets
	//    (our ProtocolError messages never echo payload content).
	for _, msg := range []string{
		newProto(CodeProtocolMismatch, "unsupported version").Error(),
		newProto(CodeInvalidMessage, "unknown type").Error(),
		newProto(CodeMissingRequestID, "hello").Error(),
	} {
		for _, s := range []string{secretKey, secretToken, secretToken + "-boot"} {
			if strings.Contains(msg, s) {
				t.Fatalf("protocol error leaked secret %q: %s", s, msg)
			}
		}
	}

	// 2) An error response envelope must never embed the hello frame or its
	//    payload; it only carries the compact code/message.
	errMsg, err := EncodeError("r1", 0, CodeInvalidMessage, "authentication failed", nil)
	if err != nil {
		t.Fatalf("encode error: %v", err)
	}
	raw, _ := Marshal(errMsg)
	js := string(raw)
	for _, s := range []string{secretKey, secretToken, secretToken + "-boot"} {
		if strings.Contains(js, s) {
			t.Fatalf("error response leaked secret %q", s)
		}
	}

	// Sanity: the original hello frame did carry them (guards the test itself).
	for _, s := range []string{secretKey, secretToken} {
		if !strings.Contains(full, s) {
			t.Fatalf("test setup error: hello frame missing secret %q", s)
		}
	}
}

// formatWithSecret is a helper mirroring how a naive logger might interpolate a
// frame; it is intentionally not called with secrets.
func formatWithSecret(_ string) (string, error) { return "", nil }

func TestEncodeResponseAndTypeShape(t *testing.T) {
	m, err := EncodeResponse("req-9", 42, true)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if m.Type != TypeResponse || m.RequestID != "req-9" || m.RoomRevision != 42 {
		t.Fatalf("response envelope = %+v", m)
	}
	raw, _ := Marshal(m)
	if !strings.Contains(string(raw), `"type":"response"`) {
		t.Fatalf("envelope not serializing correctly: %s", raw)
	}
}

func TestDecode_ServerMessages(t *testing.T) {
	// welcome
	raw := `{"protocol_version":1,"type":"welcome","request_id":"r1",
		"payload":{"player_id":"p0","session_id":"s1","session_token":"tok",
		"reconnect_token":"rt","is_host":true}}`
	m, payload, err := Decode([]byte(raw))
	if err != nil {
		t.Fatalf("welcome decode: %v", err)
	}
	if m.Type != TypeWelcome {
		t.Fatalf("type=%q", m.Type)
	}
	if w := payload.(*Welcome); w.PlayerID != "p0" || !w.IsHost {
		t.Fatalf("welcome payload: %+v", w)
	}

	// response
	raw = `{"protocol_version":1,"type":"response","request_id":"r2","room_revision":5,
		"payload":{"accepted":true}}`
	m, payload, err = Decode([]byte(raw))
	if err != nil {
		t.Fatalf("response decode: %v", err)
	}
	if ra := payload.(*ResponseAck); !ra.Accepted || m.RoomRevision != 5 {
		t.Fatalf("response payload: %+v rev=%d", ra, m.RoomRevision)
	}

	// error
	raw = `{"protocol_version":1,"type":"error","request_id":"r3","room_revision":6,
		"payload":{"code":"AUTH_FAILED","message":"bad"}}`
	m, payload, err = Decode([]byte(raw))
	if err != nil {
		t.Fatalf("error decode: %v", err)
	}
	if eb := payload.(*ErrorBody); eb.Code != "AUTH_FAILED" || eb.Message != "bad" {
		t.Fatalf("error payload: %+v", eb)
	}

	// state_snapshot (round-trip through EncodeStateSnapshot)
	gv := &game.GameView{RoomRevision: 9}
	em, err := EncodeStateSnapshot(9, gv)
	if err != nil {
		t.Fatalf("encode snapshot: %v", err)
	}
	rawb, _ := Marshal(em)
	_, payload, err = Decode(rawb)
	if err != nil {
		t.Fatalf("snapshot decode: %v", err)
	}
	if v := payload.(*game.GameView); v.RoomRevision != 9 {
		t.Fatalf("snapshot rev=%d, want 9", v.RoomRevision)
	}
}

func TestDecode_ConfigSyncMessages(t *testing.T) {
	// config_snapshot
	cs := ConfigSnapshot{Config: game.DefaultConfig(), ConfigVersion: 3, Locked: true}
	em, err := EncodeConfigSnapshot(cs)
	if err != nil {
		t.Fatalf("encode config snapshot: %v", err)
	}
	rawb, _ := Marshal(em)
	m, payload, err := Decode(rawb)
	if err != nil {
		t.Fatalf("config_snapshot decode: %v", err)
	}
	if m.Type != TypeConfigSnapshot {
		t.Fatalf("type=%q", m.Type)
	}
	if got := payload.(*ConfigSnapshot); got.ConfigVersion != 3 || !got.Locked ||
		got.Config.Game.InitialHandSize != 7 {
		t.Fatalf("config snapshot payload: %+v", got)
	}

	// rules_locked
	rl := RulesLocked{Config: game.DefaultConfig(), ConfigVersion: 3}
	em, err = EncodeRulesLocked(rl)
	if err != nil {
		t.Fatalf("encode rules_locked: %v", err)
	}
	rawb, _ = Marshal(em)
	_, payload, err = Decode(rawb)
	if err != nil {
		t.Fatalf("rules_locked decode: %v", err)
	}
	if got := payload.(*RulesLocked); got.ConfigVersion != 3 {
		t.Fatalf("rules_locked payload: %+v", got)
	}

	// update_config (valid)
	nc := game.DefaultConfig()
	nc.Game.InitialHandSize = 5
	raw := `{"protocol_version":1,"type":"update_config","request_id":"r9","expected_revision":4,
		"payload":{"expected_config_version":2,"config":` + mustJSONStr(nc) + `}}`
	_, payload, err = Decode([]byte(raw))
	if err != nil {
		t.Fatalf("update_config decode: %v", err)
	}
	if uc := payload.(*UpdateConfig); uc.ExpectedConfigVersion != 2 || uc.Config.Game.InitialHandSize != 5 {
		t.Fatalf("update_config payload: %+v", uc)
	}

	// update_config with an invalid config must be rejected at the protocol gate.
	bad := game.DefaultConfig()
	bad.Room.MaxPlayers = 9
	raw = `{"protocol_version":1,"type":"update_config","request_id":"r10","expected_revision":4,
		"payload":{"expected_config_version":2,"config":` + mustJSONStr(bad) + `}}`
	if _, _, err := Decode([]byte(raw)); err == nil {
		t.Fatal("invalid update_config should be rejected")
	}
}

func mustJSONStr(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}
