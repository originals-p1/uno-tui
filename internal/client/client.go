// Package client implements the terminal UNO Go client: dialing, hello auth,
// request/response correlation, snapshot maintenance, and reconnection.
package client

import (
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"terminal-uno/internal/game"
	"terminal-uno/internal/protocol"
)

// Config carries the client's connection parameters.
type Config struct {
	Address            string
	Name               string
	RoomKey            string
	ReconnectToken     string
	HostBootstrapToken string
	Logger             *slog.Logger
	ResponseTimeout    time.Duration
}

// DefaultTimeout is the default per-request response timeout.
const DefaultTimeout = 10 * time.Second

// pendingReq tracks an in-flight request (message kept for reconnect resend).
type pendingReq struct {
	ch  chan *Response
	msg *protocol.Message
}

// Response is the correlated result of one request.
type Response struct {
	OK       bool
	ErrCode  string
	Message  string
	Snapshot *game.GameView
}

// Client is a single connection to a server room.
type Client struct {
	cfg Config
	log *slog.Logger

	mu        sync.Mutex
	conn      net.Conn
	writeCh   chan *protocol.Message
	done      chan struct{}
	closeOnce sync.Once

	reqSeq  atomic.Int64
	pending sync.Map // request_id -> *pendingReq

	muSnap       sync.RWMutex
	snapshot     *game.GameView
	revision     uint64
	sessionID    string
	playerID     string
	sessionToken string
	reconnectTok string
	host         bool

	OnSnapshot   func(*game.GameView)
	OnEvent      func(typ string, rev uint64)
	OnError      func(code, message string)
	OnDisconnect func(err error)
}

// NewClient returns a client with the given config.
func NewClient(cfg Config) *Client {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.ResponseTimeout == 0 {
		cfg.ResponseTimeout = DefaultTimeout
	}
	return &Client{cfg: cfg, log: cfg.Logger, done: make(chan struct{})}
}

// Connect dials and performs the hello handshake (or a reconnect hello if a
// reconnect token is configured).
func (c *Client) Connect(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return err
	}
	return c.attach(conn)
}

// attach adopts an established connection and performs hello.
func (c *Client) attach(conn net.Conn) error {
	c.mu.Lock()
	c.conn = conn
	c.writeCh = make(chan *protocol.Message, 256)
	// Re-arm done only on the first connect.
	select {
	case <-c.done:
	default:
		c.done = make(chan struct{})
	}
	c.mu.Unlock()

	go c.writePump()
	// The readLoop must be live before hello so the welcome can be correlated
	// to the pending hello request.
	go c.readLoop()
	if err := c.doHello(c.cfg.ReconnectToken); err != nil {
		c.close()
		return err
	}
	return nil
}

// doHello sends hello using an optional reconnect token and awaits welcome.
func (c *Client) doHello(rtoken string) error {
	reqID := c.nextID()
	m := &protocol.Message{ProtocolVersion: protocol.ProtocolVersion, Type: protocol.TypeHello, RequestID: reqID}
	m.Payload = mustMarshal(protocol.Hello{
		PlayerName:         c.cfg.Name,
		RoomKey:            c.cfg.RoomKey,
		ReconnectToken:     rtoken,
		HostBootstrapToken: c.cfg.HostBootstrapToken,
	})
	ch := c.register(m)
	if err := c.send(m); err != nil {
		return err
	}
	select {
	case <-time.After(c.cfg.ResponseTimeout):
		return errors.New("hello timed out")
	case <-c.done:
		return errors.New("connection closed during hello")
	case resp := <-ch:
		if resp != nil && !resp.OK {
			return errors.New(resp.ErrCode + ": " + resp.Message)
		}
		return nil
	}
}

// Registered captures identity returned by the welcome message.
type Registered struct {
	SessionID      string
	PlayerID       string
	SessionToken   string
	ReconnectToken string
	IsHost         bool
	Snapshot       *game.GameView
}

// Identity returns the current registered identity (may be zero if not yet
// connected).
func (c *Client) Identity() Registered {
	c.mu.Lock()
	defer c.mu.Unlock()
	return Registered{
		SessionID: c.sessionID, PlayerID: c.playerID,
		SessionToken: c.sessionToken, ReconnectToken: c.reconnectTok,
		IsHost: c.host, Snapshot: c.Snapshot(),
	}
}

// Snapshot returns the latest known snapshot.
func (c *Client) Snapshot() *game.GameView {
	c.muSnap.RLock()
	defer c.muSnap.RUnlock()
	return c.snapshot
}

// Revision returns the latest known room revision.
func (c *Client) Revision() uint64 {
	c.muSnap.RLock()
	defer c.muSnap.RUnlock()
	return c.revision
}

func (c *Client) applySnapshot(v *game.GameView) {
	if v == nil {
		return
	}
	c.muSnap.Lock()
	if v.RoomRevision > c.revision {
		c.revision = v.RoomRevision
		c.snapshot = v
	}
	c.muSnap.Unlock()
	if c.OnSnapshot != nil {
		c.OnSnapshot(v)
	}
}

func (c *Client) nextID() string {
	n := c.reqSeq.Add(1)
	return "c" + intToStr(n)
}

// register stores an in-flight request (msg kept for reconnect resend).
func (c *Client) register(m *protocol.Message) chan *Response {
	ch := make(chan *Response, 1)
	c.pending.Store(m.RequestID, &pendingReq{ch: ch, msg: m})
	return ch
}

// Send performs a request and waits for its correlated response.
func (c *Client) Send(typ string, expected *uint64, payload interface{}) (*Response, error) {
	m := &protocol.Message{ProtocolVersion: protocol.ProtocolVersion, Type: typ, RequestID: c.nextID(), ExpectedRevision: expected}
	if payload != nil {
		m.Payload = mustMarshal(payload)
	} else {
		m.Payload = []byte("{}")
	}
	ch := c.register(m)
	if err := c.send(m); err != nil {
		return nil, err
	}
	select {
	case <-time.After(c.cfg.ResponseTimeout):
		return nil, errors.New("response timed out")
	case <-c.done:
		return nil, errors.New("connection closed")
	case resp := <-ch:
		return resp, nil
	}
}

// SendAction is a convenience for game actions (sets the expected revision).
func (c *Client) SendAction(a game.Action, expected uint64) (*Response, error) {
	var typ string
	var payload interface{}
	switch a.Type {
	case game.ActionPlayCard:
		typ = protocol.TypePlayCard
		payload = protocol.PlayCard{CardID: a.CardID, ChosenColor: colorPtr(a.ChosenColor)}
	case game.ActionDrawCard:
		typ = protocol.TypeDrawCard
	case game.ActionPlayDrawn:
		typ = protocol.TypePlayDrawnCard
		payload = protocol.PlayDrawnCard{ChosenColor: colorPtr(a.ChosenColor)}
	case game.ActionPassDrawn:
		typ = protocol.TypePassDrawnCard
	default:
		return nil, errors.New("unsupported action")
	}
	return c.Send(typ, &expected, payload)
}

func colorPtr(c *game.Color) *string {
	if c == nil {
		return nil
	}
	s := string(*c)
	return &s
}

// Ready toggles readiness.
func (c *Client) Ready(value bool, expected uint64) (*Response, error) {
	return c.Send(protocol.TypeReady, &expected, protocol.Ready{Ready: value})
}

// StartGame requests the host to start the game.
func (c *Client) StartGame(expected uint64) (*Response, error) {
	return c.Send(protocol.TypeStartGame, &expected, protocol.StartGame{})
}

// Leave requests to leave the room.
func (c *Client) Leave(expected uint64) (*Response, error) {
	return c.Send(protocol.TypeLeave, &expected, protocol.Empty{})
}

func (c *Client) send(m *protocol.Message) error {
	c.mu.Lock()
	ch := c.writeCh
	c.mu.Unlock()
	if ch == nil {
		return errors.New("not connected")
	}
	select {
	case ch <- m:
		return nil
	case <-c.done:
		return errors.New("connection closed")
	}
}

func (c *Client) writePump() {
	c.mu.Lock()
	conn := c.conn
	ch := c.writeCh
	c.mu.Unlock()
	for {
		select {
		case m := <-ch:
			raw, err := protocol.Marshal(m)
			if err != nil {
				continue
			}
			if err := protocol.WriteFrame(conn, raw); err != nil {
				c.disconnect(err)
				return
			}
		case <-c.done:
			return
		}
	}
}

func (c *Client) readLoop() {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	for {
		body, err := protocol.ReadFrame(conn)
		if err != nil {
			c.disconnect(err)
			return
		}
		msg, payload, derr := protocol.Decode(body)
		if derr != nil {
			continue
		}
		c.handle(msg, payload)
	}
}

func (c *Client) handle(msg *protocol.Message, payload interface{}) {
	switch msg.Type {
	case protocol.TypeWelcome:
		w := payload.(*protocol.Welcome)
		c.mu.Lock()
		c.sessionID = w.SessionID
		c.playerID = w.PlayerID
		c.sessionToken = w.Token
		c.reconnectTok = w.ReconnectToken
		c.host = w.IsHost
		c.mu.Unlock()
		c.applySnapshot(w.Snapshot)
		if pr, ok := c.pending.Load(msg.RequestID); ok {
			c.pending.Delete(msg.RequestID)
			pr.(*pendingReq).ch <- &Response{OK: true, Snapshot: w.Snapshot}
		}
	case protocol.TypeResponse:
		if pr, ok := c.pending.Load(msg.RequestID); ok {
			c.pending.Delete(msg.RequestID)
			pr.(*pendingReq).ch <- &Response{OK: true}
		}
	case protocol.TypeError:
		eb := payload.(*protocol.ErrorBody)
		if pr, ok := c.pending.Load(msg.RequestID); ok {
			c.pending.Delete(msg.RequestID)
			pr.(*pendingReq).ch <- &Response{OK: false, ErrCode: eb.Code, Message: eb.Message}
		} else if c.OnError != nil {
			c.OnError(eb.Code, eb.Message)
		}
	case protocol.TypeStateSnapshot:
		v := &game.GameView{}
		if jsonUnmarshal(msg.Payload, v) == nil {
			c.applySnapshot(v)
		}
	case protocol.TypeGameEvent:
		if typ, _, err := protocol.ParseGameEvent(msg); err == nil && c.OnEvent != nil {
			c.OnEvent(typ, msg.RoomRevision)
		}
	case protocol.TypePing:
		c.send(&protocol.Message{ProtocolVersion: protocol.ProtocolVersion, Type: protocol.TypePong, Payload: []byte("{}")})
	}
}

func (c *Client) disconnect(err error) {
	if c.OnDisconnect != nil {
		c.OnDisconnect(err)
	}
	c.close()
}

// Close tears down the client and its connection.
func (c *Client) Close() { c.close() }

func (c *Client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
		c.mu.Lock()
		if c.conn != nil {
			_ = c.conn.Close()
		}
		c.mu.Unlock()
	})
}

// ConnectionLost returns a channel closed when the connection is lost.
func (c *Client) ConnectionLost() <-chan struct{} { return c.done }

func mustMarshal(v interface{}) []byte {
	b, _ := jsonMarshal(v)
	return b
}

// refreshStateForReconnect re-arms done for a fresh connection (called by
// reconnect after the old done was closed).
func (c *Client) refreshDone() {
	c.closeOnce = sync.Once{}
	c.done = make(chan struct{})
}
