package server

import (
	"encoding/json"
	"net"
	"testing"
	"time"

	"terminal-uno/internal/game"
	"terminal-uno/internal/protocol"
)

// probe is a single client-side socket hooked to a server's serveConn over
// net.Pipe, letting tests exercise the real read/write pumps and room loop.
type probe struct {
	t    *testing.T
	srv  *Server
	c    net.Conn
	last interface{} // last decoded payload
}

func newProbe(t *testing.T, s *Server) *probe {
	t.Helper()
	sconn, cconn := net.Pipe()
	sconn.SetDeadline(time.Now().Add(20 * time.Second))
	cconn.SetDeadline(time.Now().Add(20 * time.Second))
	go s.serveConn(sconn)
	return &probe{t: t, srv: s, c: cconn}
}

// newProbeOn attaches a probe socket to an existing, shared server via TCP
// loopback (readme permits loopback for tests; net.Pipe's synchronous semantics
// can deadlock the server write pump against recv ordering).
func newProbeOn(t *testing.T, s *Server) *probe {
	t.Helper()
	ln, err := s.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	return &probe{t: t, srv: s, c: conn}
}

func (p *probe) raw(m *protocol.Message) {
	p.t.Helper()
	raw, err := protocol.Marshal(m)
	if err != nil {
		p.t.Fatalf("marshal: %v", err)
	}
	if err := protocol.WriteFrame(p.c, raw); err != nil {
		p.t.Fatalf("write frame: %v", err)
	}
}

func (p *probe) recv() *protocol.Message {
	p.t.Helper()
	body, err := protocol.ReadFrame(p.c)
	if err != nil {
		p.t.Fatalf("read frame: %v", err)
	}
	m, payload, err := protocol.Decode(body)
	if err != nil {
		p.t.Fatalf("decode: %v", err)
	}
	p.last = payload
	return m
}

func helloMsg(reqID, name, roomKey, rtoken, htoken string) *protocol.Message {
	b, _ := json.Marshal(protocol.Hello{
		PlayerName: name, RoomKey: roomKey,
		ReconnectToken: rtoken, HostBootstrapToken: htoken,
	})
	return &protocol.Message{ProtocolVersion: protocol.ProtocolVersion, Type: protocol.TypeHello, RequestID: reqID, Payload: b}
}

func expRev(u uint64) *uint64 { return &u }

func startMsg(reqID string, rev uint64) *protocol.Message {
	return &protocol.Message{ProtocolVersion: protocol.ProtocolVersion, Type: protocol.TypeStartGame,
		RequestID: reqID, ExpectedRevision: expRev(rev), Payload: []byte("{}")}
}

func readyMsg(reqID string, rev uint64, ready bool) *protocol.Message {
	b, _ := json.Marshal(protocol.Ready{Ready: ready})
	return &protocol.Message{ProtocolVersion: protocol.ProtocolVersion, Type: protocol.TypeReady,
		RequestID: reqID, ExpectedRevision: expRev(rev), Payload: b}
}

func playMsg(reqID string, rev uint64, cardID string) *protocol.Message {
	b, _ := json.Marshal(protocol.PlayCard{CardID: cardID})
	return &protocol.Message{ProtocolVersion: protocol.ProtocolVersion, Type: protocol.TypePlayCard,
		RequestID: reqID, ExpectedRevision: expRev(rev), Payload: b}
}

func authServer(t *testing.T) *Server {
	t.Helper()
	cfg := DefaultConfig()
	cfg.HostBootstrapToken = "host-tok"
	cfg.RoomKey = "sekret"
	s := NewServer(cfg, NewRealClock(), nil)
	t.Cleanup(s.Close)
	return s
}

func TestServerNetpipe_HelloWelcomeRoundTrip(t *testing.T) {
	s := authServer(t)
	p := newProbeOn(t, s)

	p.raw(helloMsg("r1", "Alice", "sekret", "", "host-tok"))
	m := p.recv()
	if m.Type != protocol.TypeWelcome {
		t.Fatalf("type = %q, want welcome", m.Type)
	}
	w, ok := p.last.(*protocol.Welcome)
	if !ok {
		t.Fatalf("payload type %T, want *Welcome", p.last)
	}
	if !w.IsHost {
		t.Fatalf("first host hello should be host")
	}
	if w.PlayerID == "" || w.SessionID == "" || w.Token == "" || w.ReconnectToken == "" {
		t.Fatalf("welcome missing identity: %+v", w)
	}
}

func TestServerNetpipe_RoomKeyAuthFailure(t *testing.T) {
	s := authServer(t)
	p := newProbeOn(t, s)

	p.raw(helloMsg("r1", "Alice", "WRONG", "", "host-tok"))
	m := p.recv()
	if m.Type != protocol.TypeError {
		t.Fatalf("type = %q, want error", m.Type)
	}
	if eb, _ := p.last.(*protocol.ErrorBody); eb == nil || eb.Code != CodeAuthFailed {
		t.Fatalf("error body = %+v, want AUTH_FAILED", p.last)
	}
}

func TestServerNetpipe_TwoPlayersStartAndAct(t *testing.T) {
	s := authServer(t)
	pa := newProbeOn(t, s)

	// Host joins.
	pa.raw(helloMsg("r1", "Alice", "sekret", "", "host-tok"))
	pa.recv() // welcome (host)

	// Guest joins.
	pb := newProbeOn(t, s)
	pb.raw(helloMsg("r2", "Bob", "sekret", "", ""))
	m := pb.recv()
	if m.Type != protocol.TypeWelcome {
		t.Fatalf("guest got %q, want welcome", m.Type)
	}

	// Guest toggles ready -> revision 3.
	pb.raw(readyMsg("r3", 2, true))
	m = pb.recv() // ack
	if m.Type != protocol.TypeResponse {
		t.Fatalf("ready: got %q, want response", m.Type)
	}
	// Both peers receive a snapshot broadcast of the ready commit.
	m = pa.recv()
	if m.Type != protocol.TypeStateSnapshot {
		t.Fatalf("host after ready: got %q, want snapshot", m.Type)
	}
	m = pb.recv()
	if m.Type != protocol.TypeStateSnapshot {
		t.Fatalf("guest after ready: got %q, want snapshot", m.Type)
	}

	// Host starts -> revision 4.
	pa.raw(startMsg("s1", 3))
	m = pa.recv() // ack response
	if m.Type != protocol.TypeResponse {
		t.Fatalf("start: got %q, want response", m.Type)
	}
	m = pa.recv() // snapshot for host
	if m.Type != protocol.TypeStateSnapshot {
		t.Fatalf("start: got %q, want snapshot for host", m.Type)
	}
	// Guest also receives a snapshot broadcast.
	m = pb.recv()
	if m.Type != protocol.TypeStateSnapshot {
		t.Fatalf("guest got %q, want snapshot broadcast", m.Type)
	}
	var v game.GameView
	if err := json.Unmarshal(m.Payload, &v); err != nil {
		t.Fatalf("snapshot decode: %v", err)
	}
	if len(v.Players) != 2 {
		t.Fatalf("snapshot has %d players, want 2", len(v.Players))
	}
	for _, pl := range v.Players {
		if pl.CardCount != 7 {
			t.Fatalf("player %s has %d cards in snapshot, want 7", pl.ID, pl.CardCount)
		}
	}
	if len(v.YourHand) != 7 {
		t.Fatalf("guest own hand has %d cards, want 7", len(v.YourHand))
	}
}

func TestServerNetpipe_PingPong(t *testing.T) {
	s := authServer(t)
	p := newProbeOn(t, s)
	p.raw(helloMsg("r1", "Alice", "sekret", "", "host-tok"))
	p.recv()

	p.raw(&protocol.Message{ProtocolVersion: protocol.ProtocolVersion, Type: protocol.TypePing, Payload: []byte("{}")})
	m := p.recv()
	if m.Type != protocol.TypePong {
		t.Fatalf("got %q, want pong", m.Type)
	}
}

func TestServerNetpipe_CloseOnDisconnect(t *testing.T) {
	s := authServer(t)
	p := newProbeOn(t, s)
	p.raw(helloMsg("r1", "Alice", "sekret", "", "host-tok"))
	p.recv()

	// Closing the client socket should drop the connection cleanly.
	if err := p.c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}
