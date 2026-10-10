package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"

	"terminal-uno/internal/game"
)

// Default idempotency cache capacity (readme §9: 256 entries per session).
const defaultIdemLimit = 256

// Room-level error codes.
const (
	CodeInvalidSession  = "INVALID_SESSION"
	CodeStaleRevision   = "STALE_REVISION"
	CodeRequestIDReused = "REQUEST_ID_REUSED"
	CodeUnexpected      = "ERR_INTERNAL"
)

// Kind categorizes a client request the room processes.
type Kind string

// Room action kinds that map to the game engine.
const (
	KindPlayCard      Kind = "play_card"
	KindDrawCard      Kind = "draw_card"
	KindPlayDrawnCard Kind = "play_drawn_card"
	KindPassDrawnCard Kind = "pass_drawn_card"
)

// Request is a single buffered unit of work handed to the room's single owner
// goroutine. All room/connection/timeout events funnel through here.
type Request struct {
	Kind      Kind
	SessionID string
	RequestID string
	Expected  *uint64 // client's last-seen revision
	Action    game.Action
	// resp is written exactly once by the owner goroutine.
	resp chan *Outcome
}

// NewRequest builds a request to be submitted to the room.
func NewRequest(k Kind, sessionID, requestID string, expected *uint64, a game.Action) *Request {
	return &Request{Kind: k, SessionID: sessionID, RequestID: requestID, Expected: expected, Action: a}
}

// Commit is the result of a successful, committed transaction.
type Commit struct {
	Revision  uint64
	Requester *game.GameView            // requester's personalized view (new revision)
	Views     map[string]*game.GameView // every player's view for broadcast
}

// Outcome is the unified response of processing one request.
type Outcome struct {
	ErrCode  string
	Message  string
	Snapshot *game.GameView // requester's view (current on error, new on success)
	Commit   *Commit        // non-nil only on a committed transaction
}

// OK reports whether the request committed a state change.
func (o *Outcome) OK() bool { return o != nil && o.Commit != nil }

// Room is the authoritative, single-owner state machine for one game. All
// mutation happens inside process(), called only from the Run goroutine,
// guaranteeing no concurrent writes to the engine (readme §5.2).
type Room struct {
	inbox chan *Request
	done  chan struct{}
	once  sync.Once

	game     *game.Game
	revision uint64

	sessions       map[string]*Session // sessionID -> session
	playerSessions map[string]string   // playerID -> sessionID
	caches         map[string]*idemCache

	clock Clock
}

// NewRoom creates a Room over an already-started game with the given initial
// revision and clock. Sessions are not yet bound; use BindPlayer.
func NewRoom(g *game.Game, initialRevision uint64, clock Clock) *Room {
	r := &Room{
		inbox:          make(chan *Request, 256),
		done:           make(chan struct{}),
		game:           g,
		revision:       initialRevision,
		sessions:       map[string]*Session{},
		playerSessions: map[string]string{},
		caches:         map[string]*idemCache{},
		clock:          clock,
	}
	return r
}

// BindPlayer assigns a player seat to a session (done after its first hello).
// It must be called before the room goroutine starts or via Connect requests.
func (r *Room) BindPlayer(sessionID, playerID string) {
	r.sessions[sessionID] = &Session{ID: sessionID, PlayerID: playerID}
	r.playerSessions[playerID] = sessionID
	if _, ok := r.caches[playerID]; !ok {
		r.caches[playerID] = newIdemCache(defaultIdemLimit)
	}
}

// Run starts the owner goroutine and blocks until Close is called.
func (r *Room) Run() {
	defer close(r.done)
	for req := range r.inbox {
		out := r.process(req)
		req.resp <- out
	}
}

// Close stops the owner goroutine after draining queued requests.
func (r *Room) Close() {
	r.once.Do(func() { close(r.inbox) })
	<-r.done
}

// Submit enqueues a request and waits for its outcome. It is safe to call from
// any goroutine; all processing is serialized by the owner goroutine.
func (r *Room) Submit(req *Request) *Outcome {
	req.resp = make(chan *Outcome, 1)
	r.inbox <- req
	return <-req.resp
}

// process runs entirely on the owner goroutine.
func (r *Room) process(req *Request) *Outcome {
	// 1. session identity.
	sess, ok := r.sessions[req.SessionID]
	if !ok || sess.PlayerID == "" {
		return &Outcome{ErrCode: CodeInvalidSession, Message: "unauthenticated session"}
	}
	playerID := sess.PlayerID

	// 2. idempotency: same request_id + same payload -> replay cached response.
	body := digestAction(req.Action)
	if entry, ok := r.caches[playerID].get(req.RequestID); ok {
		if entry.digest == body {
			return entry.outcome // replay, no state change, no revision bump
		}
		return &Outcome{
			ErrCode:  CodeRequestIDReused,
			Message:  "request_id reused with different payload",
			Snapshot: r.buildView(playerID),
		}
	}

	// 3. revision guard: the client must be building on the latest commit.
	if req.Expected != nil && *req.Expected != r.revision {
		return &Outcome{
			ErrCode:  CodeStaleRevision,
			Message:  "expected_revision does not match current revision",
			Snapshot: r.buildView(playerID),
		}
	}

	// 4. Atomically propose the transaction: bump the revision, then run the
	//    engine. On engine error we roll the revision back (Apply is atomic).
	r.revision++
	r.game.RoomRevision = r.revision
	res, err := r.game.Apply(playerID, req.Action)
	if err != nil {
		r.revision--
		r.game.RoomRevision = r.revision
		code, msg := gameCode(err)
		return &Outcome{ErrCode: code, Message: msg, Snapshot: r.buildView(playerID)}
	}

	// 5. Commit: build personalized views for every player and cache the
	//    response for idempotent replay.
	commit := &Commit{
		Revision:  r.revision,
		Requester: r.buildView(playerID),
		Views:     r.buildViews(),
	}
	_ = res
	out := &Outcome{Commit: commit}
	r.caches[playerID].put(req.RequestID, body, out)
	return out
}

// buildView returns the requester's personalized view at the current revision.
func (r *Room) buildView(playerID string) *game.GameView {
	return r.game.BuildViewFor(playerID)
}

// buildViews renders every player's personalized view for broadcasting.
func (r *Room) buildViews() map[string]*game.GameView {
	views := make(map[string]*game.GameView, len(r.game.Players))
	for _, p := range r.game.Players {
		views[p.ID] = r.game.BuildViewFor(p.ID)
	}
	return views
}

// gameCode maps a game engine error to a stable wire code.
func gameCode(err error) (string, string) {
	if ge, ok := err.(*game.GameError); ok {
		return string(ge.Code), ge.Message
	}
	return CodeUnexpected, err.Error()
}

// digestAction produces a normalized digest of an action, used to detect
// identical replays vs request_id reuse.
func digestAction(a game.Action) string {
	b, err := json.Marshal(a)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// idemEntry is a single cached response keyed by request_id.
type idemEntry struct {
	digest  string
	outcome *Outcome
}

// idemCache is a bounded per-session idempotency cache (readme §5.2).
type idemCache struct {
	limit   int
	entries map[string]*idemEntry
	order   []string
}

func newIdemCache(limit int) *idemCache {
	return &idemCache{limit: limit, entries: make(map[string]*idemEntry)}
}

func (c *idemCache) get(reqID string) (*idemEntry, bool) {
	e, ok := c.entries[reqID]
	return e, ok
}

func (c *idemCache) put(reqID, digest string, outcome *Outcome) {
	if _, exists := c.entries[reqID]; exists {
		return
	}
	c.entries[reqID] = &idemEntry{digest: digest, outcome: outcome}
	c.order = append(c.order, reqID)
	// Evict the oldest while over capacity.
	for len(c.order) > c.limit {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
}
