package server

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"terminal-uno/internal/game"
	"terminal-uno/internal/protocol"
)

// connState is the per-connection bookkeeping for one physical socket.
type connState struct {
	id       string
	conn     net.Conn
	sessID   string
	playerID string

	writeCh   chan *protocol.Message
	done      chan struct{}
	closeOnce sync.Once

	srv *Server

	// lastRead tracks the last successful frame read time (nanos since epoch).
	lastRead atomic.Int64
	lastSend atomic.Int64
	readErr  atomic.Bool
}

// Server is the TCP front-end. It owns a single Room and funnels every
// connection into it; per-connection read/write goroutines keep the room loop
// free of blocking I/O (readme §5.2, §9).
type Server struct {
	cfg   Config
	room  *Room
	clock Clock
	log   *slog.Logger

	mu      sync.Mutex
	conns   map[string]*connState
	players map[string]*connState // playerID -> current physical conn

	connWG sync.WaitGroup // tracks live serveConn goroutines for clean shutdown

	listener net.Listener
	closed   atomic.Bool
}

// NewServer builds a server with its own lobby Room.
func NewServer(cfg Config, clock Clock, log *slog.Logger) *Server {
	if clock == nil {
		clock = NewRealClock()
	}
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		cfg:     cfg,
		clock:   clock,
		log:     log,
		conns:   map[string]*connState{},
		players: map[string]*connState{},
	}
	s.room = NewLobbyRoom(cfg, clock)
	go s.room.Run()
	return s
}

// Room exposes the event-loop state for tests/integration.
func (s *Server) Room() *Room { return s.room }

// SetRoomRules seeds the lobby's rules from a persisted rules file before any
// game starts, so the host's last-used rules survive a restart.
func (s *Server) SetRoomRules(c game.Config) { s.room.setInitialRules(c) }

// Clock returns the server's clock.
func (s *Server) Clock() Clock { return s.clock }

// Close shuts down the listener, all connections, and the room loop.
func (s *Server) Close() {
	if s.closed.Swap(true) {
		return
	}
	s.mu.Lock()
	if s.listener != nil {
		_ = s.listener.Close()
	}
	conns := make([]*connState, 0, len(s.conns))
	for _, c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		c.close()
	}
	// Wait for every connection's read/write goroutines to fully exit before
	// closing the room, so no goroutine submits to a closing inbox (data race).
	s.connWG.Wait()
	s.room.Close()
}

// Listen starts accepting TCP connections on addr.
func (s *Server) Listen(addr string) (net.Listener, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	go s.acceptLoop(ln)
	return ln, nil
}

func (s *Server) acceptLoop(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			if s.closed.Load() {
				return
			}
			return
		}
		go s.serveConn(c)
	}
}

// serveConn handles one connection. It is also the entry point used by
// net.Pipe-based tests.
func (s *Server) serveConn(conn net.Conn) {
	s.connWG.Add(1)
	defer s.connWG.Done()
	cs := &connState{
		id:      newConnID(),
		conn:    conn,
		writeCh: make(chan *protocol.Message, s.cfg.WriteQueueSize),
		done:    make(chan struct{}),
		srv:     s,
	}
	cs.lastRead.Store(s.clock.Now().UnixNano())
	s.register(cs)
	defer func() {
		cs.close()
		s.unregister(cs)
	}()

	// Write pump: single goroutine owns all writes to the socket. Its exit is
	// awaited before this goroutine's connWG.Done() so Close never races the
	// room with a late failConn Submit from the pump.
	wpDone := make(chan struct{})
	go func() {
		defer close(wpDone)
		cs.writePump()
	}()
	defer func() { <-wpDone }()

	// Read pump: single goroutine decodes frames and dispatches into the loop.
	for {
		body, err := protocol.ReadFrame(conn)
		if err != nil {
			s.log.Debug("read error", "conn", cs.id, "err", err)
			s.failConn(cs)
			return
		}
		cs.lastRead.Store(s.clock.Now().UnixNano())
		msg, payload, derr := protocol.Decode(body)
		if derr != nil {
			s.handleProtocolError(cs, derr)
			continue
		}
		if !s.handleMessage(cs, msg, payload) {
			return
		}
	}
}

// handleProtocolError replies with a protocol error (closing the connection on
// a version mismatch per readme §6.2).
func (s *Server) handleProtocolError(cs *connState, derr error) {
	var pe *protocol.ProtocolError
	if !errors.As(derr, &pe) {
		return
	}
	m, err := protocol.EncodeError("", csLastRev(cs, s), pe.Code, pe.Message, nil)
	if err == nil && !cs.sendStrict(m) {
		s.failConn(cs)
		return
	}
	if pe.Code == protocol.CodeProtocolMismatch {
		s.failConn(cs)
	}
}

// handleMessage dispatches one decoded message. Returns false to close the conn.
func (s *Server) handleMessage(cs *connState, msg *protocol.Message, payload interface{}) bool {
	switch msg.Type {
	case protocol.TypeHello:
		return s.handleHello(cs, msg, payload.(*protocol.Hello))
	case protocol.TypePing:
		s.sendStrictOrFail(cs, pongMsg())
		return true
	case protocol.TypePong:
		// liveness acknowledged; nothing else to do.
		return true
	case protocol.TypeReady, protocol.TypeStartGame, protocol.TypeLeave, protocol.TypeRematch:
		return s.handleRoomAction(cs, msg, payload)
	case protocol.TypePlayCard, protocol.TypeDrawCard, protocol.TypePlayDrawnCard, protocol.TypePassDrawnCard,
		protocol.TypeAcceptFour, protocol.TypeChallengeFour:
		return s.handleRoomAction(cs, msg, payload)
	case protocol.TypeUpdateConfig:
		return s.handleRoomAction(cs, msg, payload)
	default:
		s.replyError(cs, "", false)
		return true
	}
}

func (s *Server) handleHello(cs *connState, msg *protocol.Message, h *protocol.Hello) bool {
	req := &Request{
		Kind:      KindHello,
		SessionID: cs.id,
		RequestID: msg.RequestID,
		Hello: &HelloReq{
			PlayerName:         h.PlayerName,
			RoomKey:            h.RoomKey,
			ReconnectToken:     h.ReconnectToken,
			HostBootstrapToken: h.HostBootstrapToken,
		},
	}
	out := s.room.Submit(req)
	if out.ErrCode != "" {
		s.replyErrCode(cs, msg.RequestID, out.ErrCode, out.Message)
		return true
	}
	cs.sessID = out.SessionID
	cs.playerID = out.PlayerID
	s.bindPlayer(cs)

	wm := &protocol.Message{ProtocolVersion: protocol.ProtocolVersion, Type: protocol.TypeWelcome, RequestID: msg.RequestID}
	wm.Payload = mustJSON(protocol.Welcome{
		PlayerID: out.PlayerID, SessionID: out.SessionID,
		Token: out.SessionToken, ReconnectToken: out.ReconnectToken,
		IsHost: out.IsHost, Snapshot: out.WelcomeView,
	})
	if !cs.sendStrict(wm) {
		s.failConn(cs)
		return false
	}
	// Relay the current authoritative rules to the new player (rule.md §0.5).
	cfg, ver, locked := s.room.ConfigState()
	if !s.sendConfigSnapshot(cs, protocol.ConfigSnapshot{Config: cfg, ConfigVersion: ver, Locked: locked}) {
		s.failConn(cs)
		return false
	}
	return true
}

func (s *Server) handleRoomAction(cs *connState, msg *protocol.Message, payload interface{}) bool {
	if cs.sessID == "" {
		s.replyErrCode(cs, msg.RequestID, CodeInvalidSession, "not authenticated")
		return true
	}
	req := s.buildRequest(cs, msg, payload)
	if req == nil {
		s.replyErrCode(cs, msg.RequestID, CodeUnexpected, "invalid request")
		return true
	}
	out := s.room.Submit(req)
	if out.ErrCode != "" {
		s.replyErrCode(cs, msg.RequestID, out.ErrCode, out.Message)
		// On a stale-config rejection the server also relays the current config
		// so the host can resend (rule.md §0.5 optimistic concurrency).
		if out.ErrCode == CodeConfigStale && out.ConfigSnapshot != nil {
			s.sendConfigSnapshot(cs, *out.ConfigSnapshot)
		}
		return true
	}
	// Success: respond to requester, broadcast personalized snapshots.
	if out.Commit != nil {
		if !s.sendOK(cs, msg.RequestID, out.Commit.Revision) {
			return false
		}
		s.broadcast(cs, out.Commit)
	} else if out.WelcomeView != nil {
		s.sendSnapshot(cs, out.WelcomeView)
	}
	// Relay config changes / rule locking to everyone.
	if out.ConfigChanged && out.ConfigSnapshot != nil {
		s.broadcastConfigSnapshot(cs, *out.ConfigSnapshot)
	}
	if out.RulesLocked && out.ConfigSnapshot != nil {
		s.broadcastRulesLocked(cs, *out.ConfigSnapshot)
	}
	return true
}

func (s *Server) buildRequest(cs *connState, msg *protocol.Message, payload interface{}) *Request {
	switch msg.Type {
	case protocol.TypeReady:
		r := payload.(*protocol.Ready)
		return &Request{Kind: KindReady, SessionID: cs.sessID, RequestID: msg.RequestID,
			Expected: msg.ExpectedRevision, Ready: &Ready{Value: r.Ready}}
	case protocol.TypeStartGame:
		return &Request{Kind: KindStartGame, SessionID: cs.sessID, RequestID: msg.RequestID, Expected: msg.ExpectedRevision}
	case protocol.TypeLeave:
		return &Request{Kind: KindLeave, SessionID: cs.sessID, RequestID: msg.RequestID, Expected: msg.ExpectedRevision}
	case protocol.TypeRematch:
		return &Request{Kind: KindRematch, SessionID: cs.sessID, RequestID: msg.RequestID, Expected: msg.ExpectedRevision}
	case protocol.TypePlayCard:
		p := payload.(*protocol.PlayCard)
		a := game.Action{Type: game.ActionPlayCard, CardID: p.CardID}
		if p.ChosenColor != nil {
			c := game.Color(*p.ChosenColor)
			a.ChosenColor = &c
		}
		return &Request{Kind: KindPlayCard, SessionID: cs.sessID, RequestID: msg.RequestID, Expected: msg.ExpectedRevision, Action: a}
	case protocol.TypeDrawCard:
		return &Request{Kind: KindDrawCard, SessionID: cs.sessID, RequestID: msg.RequestID, Expected: msg.ExpectedRevision, Action: game.Action{Type: game.ActionDrawCard}}
	case protocol.TypePlayDrawnCard:
		p := payload.(*protocol.PlayDrawnCard)
		a := game.Action{Type: game.ActionPlayDrawn}
		if p.ChosenColor != nil {
			c := game.Color(*p.ChosenColor)
			a.ChosenColor = &c
		}
		return &Request{Kind: KindPlayDrawnCard, SessionID: cs.sessID, RequestID: msg.RequestID, Expected: msg.ExpectedRevision, Action: a}
	case protocol.TypePassDrawnCard:
		return &Request{Kind: KindPassDrawnCard, SessionID: cs.sessID, RequestID: msg.RequestID, Expected: msg.ExpectedRevision, Action: game.Action{Type: game.ActionPassDrawn}}
	case protocol.TypeAcceptFour:
		return &Request{Kind: KindPlayCard, SessionID: cs.sessID, RequestID: msg.RequestID, Expected: msg.ExpectedRevision, Action: game.Action{Type: game.ActionAcceptFour}}
	case protocol.TypeChallengeFour:
		return &Request{Kind: KindPlayCard, SessionID: cs.sessID, RequestID: msg.RequestID, Expected: msg.ExpectedRevision, Action: game.Action{Type: game.ActionChallengeFour}}
	case protocol.TypeUpdateConfig:
		p := payload.(*protocol.UpdateConfig)
		return &Request{Kind: KindUpdateConfig, SessionID: cs.sessID, RequestID: msg.RequestID, Expected: msg.ExpectedRevision,
			ConfigUpdate: &ConfigUpdateReq{ExpectedConfigVersion: p.ExpectedConfigVersion, Config: p.Config}}
	}
	return nil
}

// sendOK replies with an ack response to the requester.
func (s *Server) sendOK(cs *connState, requestID string, rev uint64) bool {
	m, err := protocol.EncodeResponse(requestID, rev, true)
	if err != nil {
		return false
	}
	return cs.sendStrict(m)
}

// broadcast pushes each other player's personalized snapshot to their conn.
func (s *Server) broadcast(from *connState, commit *Commit) {
	s.mu.Lock()
	conns := map[string]*connState{}
	for pid, c := range s.players {
		conns[pid] = c
	}
	s.mu.Unlock()
	for pid, view := range commit.Views {
		if pid == from.playerID {
			// Requester already got an ack; optionally send their snapshot too.
			if !s.sendSnapshot(from, view) {
				return
			}
			continue
		}
		c := conns[pid]
		if c == nil {
			continue
		}
		m, err := protocol.EncodeStateSnapshot(commit.Revision, view)
		if err != nil {
			continue
		}
		if !c.sendStrict(m) {
			s.failConn(c)
		}
	}
}

func (s *Server) sendSnapshot(cs *connState, view *game.GameView) bool {
	m, err := protocol.EncodeStateSnapshot(view.RoomRevision, view)
	if err != nil {
		return true
	}
	return cs.sendStrict(m)
}

// sendConfigSnapshot delivers a CONFIG_SNAPSHOT to one connection.
func (s *Server) sendConfigSnapshot(cs *connState, snap protocol.ConfigSnapshot) bool {
	m, err := protocol.EncodeConfigSnapshot(snap)
	if err != nil {
		return true
	}
	return cs.sendStrict(m)
}

// broadcastControl pushes one CONFIG_SNAPSHOT / RULES_LOCKED to every player.
func (s *Server) broadcastControl(from *connState, typ string, snap protocol.ConfigSnapshot) {
	m, err := s.encodeControl(typ, snap)
	if err != nil {
		return
	}
	s.mu.Lock()
	conns := map[string]*connState{}
	for pid, c := range s.players {
		conns[pid] = c
	}
	s.mu.Unlock()
	for _, c := range conns {
		if !c.sendStrict(m) {
			s.failConn(c)
		}
	}
}

func (s *Server) encodeControl(typ string, snap protocol.ConfigSnapshot) (*protocol.Message, error) {
	switch typ {
	case protocol.TypeConfigSnapshot:
		return protocol.EncodeConfigSnapshot(snap)
	case protocol.TypeRulesLocked:
		return protocol.EncodeRulesLocked(protocol.RulesLocked{Config: snap.Config, ConfigVersion: snap.ConfigVersion})
	default:
		return nil, fmt.Errorf("unknown control type %q", typ)
	}
}

func (s *Server) broadcastConfigSnapshot(from *connState, snap protocol.ConfigSnapshot) {
	s.broadcastControl(from, protocol.TypeConfigSnapshot, snap)
}

func (s *Server) broadcastRulesLocked(from *connState, snap protocol.ConfigSnapshot) {
	s.broadcastControl(from, protocol.TypeRulesLocked, snap)
}

func (s *Server) replyError(cs *connState, requestID string, drop bool) {
	s.replyErrCode(cs, requestID, CodeUnexpected, "error")
}

func (s *Server) replyErrCode(cs *connState, requestID, code, message string) {
	m, err := protocol.EncodeError(requestID, csLastRev(cs, s), code, message, nil)
	if err != nil {
		return
	}
	if !cs.sendStrict(m) {
		s.failConn(cs)
	}
}

func (s *Server) failConn(cs *connState) {
	if cs.readErr.Swap(true) {
		return
	}
	// Notify the room so it can pause/timeout appropriately.
	if cs.playerID != "" {
		s.room.Submit(&Request{Kind: KindDisconnect, SessionID: cs.sessID, Disconnect: &Disconnect{PlayerID: cs.playerID}})
	}
	cs.close()
}

// --- registration / routing ---

func (s *Server) register(cs *connState) {
	s.mu.Lock()
	s.conns[cs.id] = cs
	s.mu.Unlock()
}

func (s *Server) unregister(cs *connState) {
	s.mu.Lock()
	delete(s.conns, cs.id)
	if cs.playerID != "" && s.players[cs.playerID] == cs {
		delete(s.players, cs.playerID)
	}
	s.mu.Unlock()
}

func (s *Server) bindPlayer(cs *connState) {
	s.mu.Lock()
	s.players[cs.playerID] = cs
	s.mu.Unlock()
}

func (cs *connState) close() {
	cs.closeOnce.Do(func() {
		close(cs.done)
		_ = cs.conn.Close()
	})
}

func (cs *connState) writePump() {
	for {
		select {
		case <-cs.done:
			return
		case m := <-cs.writeCh:
			raw, err := protocol.Marshal(m)
			if err != nil {
				continue
			}
			if err := protocol.WriteFrame(cs.conn, raw); err != nil {
				cs.srv.failConn(cs)
				return
			}
		}
	}
}

// sendStrict enqueues a message for the write pump. If the bounded queue is
// full (slow client), the client is disconnected without blocking the room.
func (cs *connState) sendStrict(m *protocol.Message) bool {
	select {
	case cs.writeCh <- m:
		return true
	default:
		cs.srv.log.Debug("write queue full; disconnecting", "conn", cs.id)
		cs.srv.failConn(cs)
		return false
	}
}

func (s *Server) sendStrictOrFail(cs *connState, m *protocol.Message) {
	cs.sendStrict(m)
}

func pongMsg() *protocol.Message {
	return &protocol.Message{ProtocolVersion: protocol.ProtocolVersion, Type: protocol.TypePong, Payload: []byte("{}")}
}

func csLastRev(cs *connState, s *Server) uint64 {
	return s.room.revision
}

func mustJSON(v interface{}) []byte {
	b, _ := jsonMarshal(v)
	return b
}

func newConnID() string { return NewSessionID() }
