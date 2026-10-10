package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"math/rand"
	"strings"
	"sync"
	"time"

	"terminal-uno/internal/game"
	"terminal-uno/internal/protocol"
)

// Default idempotency cache capacity (readme §9: 256 entries per session).
const defaultIdemLimit = 256

// Room-level error codes.
const (
	CodeInvalidSession   = "INVALID_SESSION"
	CodeStaleRevision    = "STALE_REVISION"
	CodeRequestIDReused  = "REQUEST_ID_REUSED"
	CodeAuthFailed       = "AUTH_FAILED"
	CodeRoomFull         = "ROOM_FULL"
	CodeGameStarted      = "GAME_ALREADY_STARTED"
	CodeNotHost          = "NOT_HOST"
	CodeReconnectExpired = "RECONNECT_EXPIRED"
	CodeNameInvalid      = "NAME_INVALID"
	CodeUnexpected       = "ERR_INTERNAL"
	CodeConfigStale      = "CONFIG_STALE"
	CodeNotWaitingPhase  = "NOT_WAITING_PHASE"
	CodeConfigInvalid    = "CONFIG_INVALID"
)

// Kind categorizes a client request the room processes.
type Kind string

// Room action kinds.
const (
	KindHello      Kind = "hello"
	KindReady      Kind = "ready"
	KindStartGame  Kind = "start_game"
	KindLeave      Kind = "leave"
	KindDisconnect Kind = "disconnect"

	KindStatus Kind = "status"

	KindPlayCard      Kind = "play_card"
	KindDrawCard      Kind = "draw_card"
	KindPlayDrawnCard Kind = "play_drawn_card"
	KindPassDrawnCard Kind = "pass_drawn_card"

	KindUpdateConfig Kind = "update_config"
)

// Config carries the room's security and timing knobs (readme §9).
type Config struct {
	RoomKey            string
	HostBootstrapToken string
	MaxPlayers         int
	PingInterval       time.Duration
	IdleTimeout        time.Duration
	HelloTimeout       time.Duration
	ReconnectGrace     time.Duration
	WriteQueueSize     int
}

// DefaultConfig returns the v1 runtime defaults (readme §9).
func DefaultConfig() Config {
	return Config{
		MaxPlayers:     4,
		PingInterval:   10 * time.Second,
		IdleTimeout:    30 * time.Second,
		HelloTimeout:   10 * time.Second,
		ReconnectGrace: 60 * time.Second,
		WriteQueueSize: 64,
	}
}

// Member is a player in the room registry, alive in lobby and during a game.
type Member struct {
	ID        string
	Name      string
	Ready     bool
	IsHost    bool
	Connected bool
}

// HelloReq carries a hello authentication attempt.
type HelloReq struct {
	PlayerName         string
	RoomKey            string
	ReconnectToken     string
	HostBootstrapToken string
}

// Request is a single buffered unit of work handed to the room's single owner
// goroutine. All room/connection/timeout events funnel through here.
type Request struct {
	Kind      Kind
	SessionID string
	RequestID string
	Expected  *uint64
	Action    game.Action

	Hello      *HelloReq
	Ready      *Ready
	Disconnect *Disconnect
	ConfigUpdate *ConfigUpdateReq

	resp chan *Outcome
}

// ConfigUpdateReq carries a host's rules-configuration submission.
type ConfigUpdateReq struct {
	ExpectedConfigVersion uint64
	Config                game.Config
}

// Ready carries a ready toggle.
type Ready struct{ Value bool }

// Disconnect carries a player disconnect notice.
type Disconnect struct{ PlayerID string }

// NewRequest builds a game-action request (used by Task 5 and the server).
func NewRequest(k Kind, sessionID, requestID string, expected *uint64, a game.Action) *Request {
	return &Request{Kind: k, SessionID: sessionID, RequestID: requestID, Expected: expected, Action: a}
}

// Commit is the result of a successful, committed transaction.
type Commit struct {
	Revision  uint64
	Requester *game.GameView
	Views     map[string]*game.GameView
}

// Outcome is the unified response of processing one request.
type Outcome struct {
	ErrCode string
	Message string

	Snapshot *game.GameView // requester's view (current on error, new on success)
	Commit   *Commit        // non-nil only on a committed transaction

	// Auth/lobby results for the server to relay.
	PlayerID             string
	SessionID            string
	SessionToken         string
	ReconnectToken       string
	IsHost               bool
	Name                 string
	WelcomeView          *game.GameView
	Joined               bool
	DisconnectedDeadline string

	// Config-sync (rule.md §0.5): set when the rules changed or were locked, so
	// the server can relay CONFIG_SNAPSHOT / RULES_LOCKED to the players.
	ConfigChanged  bool
	RulesLocked    bool
	ConfigSnapshot *protocol.ConfigSnapshot
}

// OK reports whether the request committed a game/room state change.
func (o *Outcome) OK() bool { return o != nil && o.Commit != nil }

// Room is the authoritative, single-owner state machine covering the full
// lobby -> turn -> ... lifecycle (readme §3.6). All mutation happens inside
// process(), called only from the Run goroutine.
type Room struct {
	inbox chan *Request
	done  chan struct{}
	once  sync.Once

	cfg   Config
	clock Clock
	rng   rand.Source

	game     *game.Game // nil until started
	revision uint64
	hostID   string
	players  map[string]*Member
	order    []string

	// room rule configuration (rule.md §0.5 C1). configVersion is optimistic-
	// concurrency guarded; configLocked turns true at game start and blocks
	// further UPDATE_CONFIG until the room returns to WAITING/FINISHED (v1
	// single-round: a finish ends the room, so lock is permanent).
	config        game.Config
	configVersion uint64
	configLocked  bool

	sessions       map[string]*Session
	playerSessions map[string]string
	reconnect      map[string][]byte // playerID -> reconnect token digest
	disconnected   map[string]time.Time
	prevPhase      game.Phase
	started        bool

	caches map[string]*idemCache

	// deadline scheduling
	afterCh     <-chan time.Time
	schedTarget time.Time
}

// NewRoom creates a Room over an already-started game (default config).
func NewRoom(g *game.Game, initialRevision uint64, clock Clock) *Room {
	r := newRoom(DefaultConfig(), clock)
	r.game = g
	g.RoomRevision = initialRevision
	r.revision = initialRevision
	r.started = true
	return r
}

// NewLobbyRoom creates a Room in the lobby phase (no game yet).
func NewLobbyRoom(cfg Config, clock Clock) *Room {
	return newRoom(cfg, clock)
}

func newRoom(cfg Config, clock Clock) *Room {
	if clock == nil {
		clock = NewRealClock()
	}
	return &Room{
		inbox:          make(chan *Request, 256),
		done:           make(chan struct{}),
		cfg:            cfg,
		clock:          clock,
		rng:            rand.NewSource(clock.Now().UnixNano()),
		players:        map[string]*Member{},
		sessions:       map[string]*Session{},
		playerSessions: map[string]string{},
		reconnect:      map[string][]byte{},
		disconnected:   map[string]time.Time{},
		caches:         map[string]*idemCache{},
		config:         game.DefaultConfig(),
	}
}

// ConfigState returns the current authoritative room configuration, its version,
// and whether the rules are locked (rule.md §0.5).
func (r *Room) ConfigState() (game.Config, uint64, bool) {
	return r.config, r.configVersion, r.configLocked
}

// Run starts the owner goroutine, driving both the inbox and deadline timers.
// It drains any already-fired deadline before selecting, so a fired timer is
// never silently dropped when a request is also pending.
func (r *Room) Run() {
	defer close(r.done)
	r.schedule()
	for {
		for r.afterCh != nil && len(r.afterCh) > 0 {
			select {
			case <-r.afterCh:
			default:
			}
			r.expireDeadlines()
			r.resetTimer()
		}
		select {
		case req, ok := <-r.inbox:
			if !ok {
				return
			}
			out := r.process(req)
			req.resp <- out
			r.schedule()
		case <-r.afterCh:
			r.expireDeadlines()
			r.resetTimer()
		}
	}
}

// Close stops the owner goroutine after draining queued requests.
func (r *Room) Close() {
	r.once.Do(func() { close(r.inbox) })
	<-r.done
}

// Submit enqueues a request and waits for its outcome.
func (r *Room) Submit(req *Request) (out *Outcome) {
	req.resp = make(chan *Outcome, 1)
	defer func() {
		// If the room is shutting down the inbox may already be closed; return
		// a stable error instead of panicking out of a server goroutine.
		if recover() != nil {
			out = &Outcome{ErrCode: CodeInvalidSession, Message: "room closed"}
		}
	}()
	r.inbox <- req
	return <-req.resp
}

// RegisterConn associates a pre-auth connection ID with nothing yet; used to
// track a conn across hello. Returns nothing.
func (r *Room) RegisterConn(connID string) {}

// BindPlayer assigns a player seat to a pre-existing session and seeds the
// player's idempotency cache. Used by tests and by direct (non-hello) flow.
// It does not authenticate; for the real server prefer hello auth.
func (r *Room) BindPlayer(sessionID, playerID string) {
	r.sessions[sessionID] = &Session{ID: sessionID, PlayerID: playerID}
	r.playerSessions[playerID] = sessionID
	if _, ok := r.caches[playerID]; !ok {
		r.caches[playerID] = newIdemCache(defaultIdemLimit)
	}
	if _, ok := r.players[playerID]; !ok {
		r.players[playerID] = &Member{ID: playerID, Name: playerID, Connected: true}
		if !containsStr(r.order, playerID) {
			r.order = append(r.order, playerID)
		}
	}
}

// schedule arms a fresh deadline timer whenever the earliest pending deadline
// differs from the currently armed one. It never drops an already-fired timer;
// callers consume pending timers before rescheduling.
func (r *Room) schedule() {
	target, dur := r.nextDeadline()
	if r.afterCh != nil && target.Equal(r.schedTarget) {
		return
	}
	r.schedTarget = target
	if target.IsZero() {
		r.afterCh = nil
		return
	}
	r.afterCh = r.clock.After(dur)
}

// resetTimer clears the armed timer and re-arms for the next pending deadline.
func (r *Room) resetTimer() {
	r.afterCh = nil
	r.schedTarget = time.Time{}
	r.schedule()
}

func (r *Room) nextDeadline() (time.Time, time.Duration) {
	now := r.clock.Now()
	var earliest time.Time
	for _, d := range r.disconnected {
		if d.After(now) && (earliest.IsZero() || d.Before(earliest)) {
			earliest = d
		}
	}
	if earliest.IsZero() {
		return time.Time{}, 0
	}
	return earliest, earliest.Sub(now)
}

func (r *Room) expireDeadlines() {
	now := r.clock.Now()
	deadline := false
	for id, d := range r.disconnected {
		if !d.After(now) {
			deadline = true
			delete(r.disconnected, id)
			if m, ok := r.players[id]; ok {
				m.Connected = false
			}
			if r.game != nil {
				if pi := r.game.PlayerIndex(id); pi >= 0 {
					r.game.SetConnected(id, false)
				}
			}
		}
	}
	if deadline && r.game != nil && r.started {
		r.game.Phase = game.PhaseFinished
		r.game.EndReason = "PLAYER_RECONNECT_TIMEOUT"
		r.revision++
		r.game.RoomRevision = r.revision
	}
}

// process runs entirely on the owner goroutine.
func (r *Room) process(req *Request) *Outcome {
	switch req.Kind {
	case KindStatus:
		return &Outcome{Snapshot: r.snapshotForAny(req)}
	case KindHello:
		return r.processHello(req)
	case KindReady:
		return r.processReady(req)
	case KindStartGame:
		return r.processStartGame(req)
	case KindLeave:
		return r.processLeave(req)
	case KindDisconnect:
		return r.processDisconnect(req)
	case KindUpdateConfig:
		return r.processUpdateConfig(req)
	default:
		return r.processGameAction(req)
	}
}

// processHello performs authentication: room key, host bootstrap, or reconnect.
func (r *Room) processHello(req *Request) *Outcome {
	if req.Hello == nil {
		return &Outcome{ErrCode: CodeAuthFailed, Message: "invalid hello"}
	}
	h := req.Hello
	// 1. Room key (constant time).
	if r.cfg.RoomKey != "" && !tokenMatchesRaw(h.RoomKey, r.cfg.RoomKey) {
		return &Outcome{ErrCode: CodeAuthFailed, Message: "authentication failed"}
	}

	// 2. Host bootstrap.
	if h.HostBootstrapToken != "" && r.cfg.HostBootstrapToken != "" &&
		tokenMatchesRaw(h.HostBootstrapToken, r.cfg.HostBootstrapToken) {
		return r.admitHost(h.PlayerName)
	}

	// 3. Reconnect.
	if h.ReconnectToken != "" {
		for playerID, digest := range r.reconnect {
			if tokenMatches(h.ReconnectToken, digest) {
				return r.admitReconnect(playerID, h.ReconnectToken)
			}
		}
		return &Outcome{ErrCode: CodeAuthFailed, Message: "authentication failed"}
	}

	// 4. New join.
	return r.admitNew(h.PlayerName)
}

func (r *Room) admitHost(name string) *Outcome {
	if r.started {
		return &Outcome{ErrCode: CodeGameStarted, Message: "game already started"}
	}
	if _, ok := r.hostExists(); ok {
		return &Outcome{ErrCode: CodeAuthFailed, Message: "host already present"}
	}
	id := newPlayerID()
	r.players[id] = &Member{ID: id, Name: sanitize(name), IsHost: true, Connected: true, Ready: true}
	r.hostID = id
	if !containsStr(r.order, id) {
		r.order = append(r.order, id)
	}
	sid := NewSessionID()
	r.sessions[sid] = &Session{ID: sid, PlayerID: id}
	r.playerSessions[id] = sid
	if _, ok := r.caches[id]; !ok {
		r.caches[id] = newIdemCache(defaultIdemLimit)
	}
	token, digest := GenerateToken()
	r.sessions[sid].tokenHash = digest
	rtoken, rdigest := GenerateToken()
	r.reconnect[id] = rdigest
	r.revision++
	return &Outcome{
		PlayerID: id, SessionID: sid, IsHost: true, Name: r.players[id].Name,
		SessionToken: token, ReconnectToken: rtoken, Joined: true,
		WelcomeView: r.lobbyView(id),
	}
}

func (r *Room) admitNew(name string) *Outcome {
	if r.started {
		return &Outcome{ErrCode: CodeGameStarted, Message: "game already started"}
	}
	if len(r.players) >= r.cfg.MaxPlayers {
		return &Outcome{ErrCode: CodeRoomFull, Message: "room is full"}
	}
	clean := sanitize(name)
	if !validName(clean) {
		return &Outcome{ErrCode: CodeNameInvalid, Message: "invalid player name"}
	}
	for _, m := range r.players {
		if m.Name == clean {
			return &Outcome{ErrCode: CodeNameInvalid, Message: "duplicate player name"}
		}
	}
	id := newPlayerID()
	r.players[id] = &Member{ID: id, Name: clean, Connected: true}
	r.order = append(r.order, id)
	sid := NewSessionID()
	r.sessions[sid] = &Session{ID: sid, PlayerID: id}
	r.playerSessions[id] = sid
	if _, ok := r.caches[id]; !ok {
		r.caches[id] = newIdemCache(defaultIdemLimit)
	}
	token, digest := GenerateToken()
	r.sessions[sid].tokenHash = digest
	rtoken, rdigest := GenerateToken()
	r.reconnect[id] = rdigest
	r.revision++
	return &Outcome{
		PlayerID: id, SessionID: sid, Name: clean,
		SessionToken: token, ReconnectToken: rtoken, Joined: true,
		WelcomeView: r.lobbyView(id),
	}
}

func (r *Room) admitReconnect(playerID, oldToken string) *Outcome {
	m, ok := r.players[playerID]
	if !ok {
		return &Outcome{ErrCode: CodeAuthFailed, Message: "authentication failed"}
	}
	deadline, wasDisconnected := r.disconnected[playerID]
	now := r.clock.Now()
	if wasDisconnected && !deadline.After(now) {
		delete(r.disconnected, playerID)
		// Grace already exceeded: session is dead.
		return &Outcome{ErrCode: CodeReconnectExpired, Message: "reconnect grace expired"}
	}

	// Rotate token: old digest invalid, new one issued.
	newToken, newDigest := GenerateToken()
	r.reconnect[playerID] = newDigest

	sid := NewSessionID()
	r.sessions[sid] = &Session{ID: sid, PlayerID: playerID}
	r.playerSessions[playerID] = sid
	if _, ok := r.caches[playerID]; !ok {
		r.caches[playerID] = newIdemCache(defaultIdemLimit)
	}
	stoken, sdigest := GenerateToken()
	r.sessions[sid].tokenHash = sdigest

	m.Connected = true

	// Resume from pause if this was the last disconnected player.
	wasPaused := false
	if wasDisconnected {
		wasPaused = true
		delete(r.disconnected, playerID)
		if r.game != nil {
			if pi := r.game.PlayerIndex(playerID); pi >= 0 {
				r.game.SetConnected(playerID, true)
			}
		}
	}
	if wasPaused && len(r.disconnected) == 0 && r.game != nil && r.game.Phase == game.PhasePaused {
		r.game.ResumePaused(r.prevPhase)
		r.prevPhase = ""
	}
	r.revision++
	view := r.buildView(playerID)
	if r.game == nil {
		view = r.lobbyView(playerID)
	}
	return &Outcome{
		PlayerID: playerID, SessionID: sid, Name: m.Name, IsHost: m.IsHost,
		SessionToken: stoken, ReconnectToken: newToken, Joined: false,
		WelcomeView: view,
	}
}

func (r *Room) processReady(req *Request) *Outcome {
	playerID := r.sessionPlayer(req)
	if playerID == "" {
		return &Outcome{ErrCode: CodeInvalidSession, Message: "unauthenticated session"}
	}
	m, ok := r.players[playerID]
	if !ok {
		return &Outcome{ErrCode: CodeInvalidSession, Message: "unauthenticated session"}
	}
	if m.IsHost {
		return &Outcome{ErrCode: CodeNotHost, Message: "host is always ready"} // host need not toggle
	}
	if req.Ready != nil {
		m.Ready = req.Ready.Value
	} else {
		m.Ready = !m.Ready
	}
	r.revision++
	view := r.lobbyView(playerID)
	return &Outcome{Commit: &Commit{Revision: r.revision, Requester: view, Views: r.lobbyViews()}}
}

func (r *Room) processStartGame(req *Request) *Outcome {
	playerID := r.sessionPlayer(req)
	if playerID == "" {
		return &Outcome{ErrCode: CodeInvalidSession, Message: "unauthenticated session"}
	}
	m, ok := r.players[playerID]
	if !ok {
		return &Outcome{ErrCode: CodeInvalidSession, Message: "unauthenticated session"}
	}
	if !m.IsHost {
		return &Outcome{ErrCode: CodeNotHost, Message: "only host can start"}
	}
	if r.started {
		return &Outcome{ErrCode: CodeGameStarted, Message: "game already started"}
	}
	if len(r.players) < game.MinPlayers || len(r.players) > game.MaxPlayers {
		return &Outcome{ErrCode: CodeUnexpected, Message: "need 2-4 players"}
	}
	// Host is always ready; all non-host players must be ready.
	for _, pid := range r.order {
		mem := r.players[pid]
		if !mem.IsHost && !mem.Ready {
			return &Outcome{ErrCode: CodeUnexpected, Message: "not all players ready"}
		}
		if !mem.Connected {
			return &Outcome{ErrCode: CodeUnexpected, Message: "player disconnected"}
		}
	}
	names := make([]string, 0, len(r.order))
	ids := make([]string, 0, len(r.order))
	for _, pid := range r.order {
		names = append(names, r.players[pid].Name)
		ids = append(ids, pid)
	}
	g, err := game.NewGameWithConfig(names, ids, r.config, r.rng)
	if err != nil {
		return &Outcome{ErrCode: CodeConfigInvalid, Message: err.Error()}
	}
	r.game = g
	r.started = true
	r.configLocked = true
	r.revision++
	r.game.RoomRevision = r.revision
	out := &Outcome{Commit: &Commit{Revision: r.revision, Requester: r.buildView(playerID), Views: r.buildViews()}}
	out.RulesLocked = true
	snap := r.configSnapshot()
	out.ConfigSnapshot = &snap
	return out
}

// processUpdateConfig handles a host's UPDATE_CONFIG request (rule.md §0.5).
// Only the host may change the rules, only while the game is not in progress,
// and the submitted config version must match the current one (optimistic
// concurrency -> CONFIG_STALE on a mismatch).
func (r *Room) processUpdateConfig(req *Request) *Outcome {
	playerID := r.sessionPlayer(req)
	if playerID == "" {
		return &Outcome{ErrCode: CodeInvalidSession, Message: "unauthenticated session"}
	}
	m, ok := r.players[playerID]
	if !ok || !m.IsHost {
		return &Outcome{ErrCode: CodeNotHost, Message: "only host can configure rules"}
	}
	if r.started || r.configLocked {
		return &Outcome{ErrCode: CodeNotWaitingPhase, Message: "rules are locked during a game"}
	}
	if req.ConfigUpdate == nil {
		return &Outcome{ErrCode: CodeUnexpected, Message: "invalid update"}
	}
	if req.ConfigUpdate.ExpectedConfigVersion != r.configVersion {
		snap := r.configSnapshot()
		return &Outcome{ErrCode: CodeConfigStale,
			Message:        "config version is stale; resend with current version",
			ConfigSnapshot: &snap,
		}
	}
	if err := req.ConfigUpdate.Config.Validate(); err != nil {
		return &Outcome{ErrCode: CodeConfigInvalid, Message: err.Error()}
	}
	r.config = req.ConfigUpdate.Config
	r.configVersion++
	out := &Outcome{Commit: &Commit{Revision: r.revision, Requester: r.buildView(playerID), Views: r.buildViews()}}
	out.ConfigChanged = true
	snap := r.configSnapshot()
	out.ConfigSnapshot = &snap
	return out
}

// configSnapshot captures the authoritative config state for relay.
func (r *Room) configSnapshot() protocol.ConfigSnapshot {
	return protocol.ConfigSnapshot{Config: r.config, ConfigVersion: r.configVersion, Locked: r.configLocked}
}

func (r *Room) processLeave(req *Request) *Outcome {
	playerID := r.sessionPlayer(req)
	if playerID == "" {
		return &Outcome{ErrCode: CodeInvalidSession, Message: "unauthenticated session"}
	}
	return r.minorLeave(playerID)
}

func (r *Room) processDisconnect(req *Request) *Outcome {
	if req.Disconnect == nil {
		return &Outcome{ErrCode: CodeInvalidSession, Message: "invalid disconnect"}
	}
	playerID := req.Disconnect.PlayerID
	m, ok := r.players[playerID]
	if !ok {
		return &Outcome{ErrCode: CodeInvalidSession, Message: "unknown player"}
	}
	m.Connected = false
	hostProcessEnd := false
	if m.IsHost {
		hostProcessEnd = true
	}
	if hostProcessEnd {
		// Host process exit ends the game (v1).
		if r.game != nil {
			r.game.Phase = game.PhaseFinished
			r.game.EndReason = "host_exit"
		}
		r.revision++
		return &Outcome{Commit: &Commit{Revision: r.revision}}
	}
	// Game disconnect.
	if r.game != nil && r.started && r.game.Phase != game.PhaseLobby {
		if pi := r.game.PlayerIndex(playerID); pi >= 0 {
			r.game.SetConnected(playerID, false)
		}
		if _, ok := r.disconnected[playerID]; !ok {
			deadline := r.clock.Now().Add(r.cfg.ReconnectGrace)
			r.disconnected[playerID] = deadline
			r.prevPhase = r.game.Phase
			r.game.SetPaused(collectIDs(r.disconnected), deadline.Format(time.RFC3339))
		}
		r.revision++
	}
	return &Outcome{Commit: &Commit{Revision: r.revision}}
}

func (r *Room) minorLeave(playerID string) *Outcome {
	m, ok := r.players[playerID]
	if !ok {
		return &Outcome{ErrCode: CodeInvalidSession, Message: "unknown player"}
	}
	m.Connected = false
	if m.IsHost {
		if r.game != nil {
			r.game.Phase = game.PhaseFinished
			r.game.EndReason = "host_exit"
		}
		r.revision++
		return &Outcome{Commit: &Commit{Revision: r.revision}}
	}
	// Count as a disconnect with pause.
	if r.game != nil && r.started {
		if pi := r.game.PlayerIndex(playerID); pi >= 0 {
			r.game.SetConnected(playerID, false)
		}
		if _, ok := r.disconnected[playerID]; !ok {
			deadline := r.clock.Now().Add(r.cfg.ReconnectGrace)
			r.disconnected[playerID] = deadline
			r.prevPhase = r.game.Phase
			r.game.SetPaused(collectIDs(r.disconnected), deadline.Format(time.RFC3339))
		}
	}
	r.revision++
	view := r.lobbyView(playerID)
	return &Outcome{Commit: &Commit{Revision: r.revision, Requester: view, Views: r.lobbyViews()}}
}

// processGameAction handles game-engine actions (play/draw/...).
func (r *Room) processGameAction(req *Request) *Outcome {
	playerID := r.sessionPlayer(req)
	if playerID == "" {
		return &Outcome{ErrCode: CodeInvalidSession, Message: "unauthenticated session"}
	}
	if r.game == nil || !r.started {
		return &Outcome{ErrCode: CodeGameStarted, Message: "game not started"}
	}

	// Idempotency.
	body := digestAction(req.Action)
	if entry, ok := r.caches[playerID].get(req.RequestID); ok {
		if entry.digest == body {
			return entry.outcome
		}
		return &Outcome{ErrCode: CodeRequestIDReused, Message: "request_id reused with different payload", Snapshot: r.buildView(playerID)}
	}

	// Revision guard.
	if req.Expected != nil && *req.Expected != r.revision {
		return &Outcome{ErrCode: CodeStaleRevision, Message: "expected_revision does not match", Snapshot: r.buildView(playerID)}
	}

	// Propose: bump revision, run engine, roll back on error.
	r.revision++
	r.game.RoomRevision = r.revision
	res, err := r.game.Apply(playerID, req.Action)
	if err != nil {
		r.revision--
		r.game.RoomRevision = r.revision
		code, msg := gameCode(err)
		return &Outcome{ErrCode: code, Message: msg, Snapshot: r.buildView(playerID)}
	}
	_ = res
	commit := &Commit{
		Revision:  r.revision,
		Requester: r.buildView(playerID),
		Views:     r.buildViews(),
	}
	out := &Outcome{Commit: commit}
	r.caches[playerID].put(req.RequestID, body, out)
	return out
}

func (r *Room) sessionPlayer(req *Request) string {
	sess, ok := r.sessions[req.SessionID]
	if !ok || sess.PlayerID == "" {
		return ""
	}
	return sess.PlayerID
}

func (r *Room) hostExists() (string, bool) {
	if r.hostID != "" {
		return r.hostID, true
	}
	for _, m := range r.players {
		if m.IsHost {
			return m.ID, true
		}
	}
	return "", false
}

func (r *Room) lobbyView(playerID string) *game.GameView {
	v := &game.GameView{
		RoomRevision: r.revision,
		Phase:        "lobby",
		History:      []game.GameEvent{},
	}
	for _, pid := range r.order {
		m := r.players[pid]
		v.Players = append(v.Players, game.PlayerView{
			ID: m.ID, Name: m.Name, CardCount: 0, Connected: m.Connected, Ready: m.Ready, IsHost: m.IsHost,
		})
	}
	return v
}

func (r *Room) lobbyViews() map[string]*game.GameView {
	views := map[string]*game.GameView{}
	for _, pid := range r.order {
		views[pid] = r.lobbyView(pid)
	}
	return views
}

// snapshotForAny returns a lobby/game view for the given request's session, if
// it has one, used by the read-only Status synchronization request.
func (r *Room) snapshotForAny(req *Request) *game.GameView {
	pid := r.sessionPlayer(req)
	if pid == "" {
		if len(r.order) > 0 {
			pid = r.order[0]
		}
	}
	return r.buildView(pid)
}

func (r *Room) buildView(playerID string) *game.GameView {
	if r.game == nil {
		return r.lobbyView(playerID)
	}
	return r.game.BuildViewFor(playerID)
}

func (r *Room) buildViews() map[string]*game.GameView {
	views := map[string]*game.GameView{}
	if r.game == nil {
		for _, pid := range r.order {
			views[pid] = r.lobbyView(pid)
		}
		return views
	}
	for _, p := range r.game.Players {
		views[p.ID] = r.game.BuildViewFor(p.ID)
	}
	return views
}

func (r *Room) allPlayerIDs() []string {
	ids := []string{}
	if r.game != nil {
		for _, p := range r.game.Players {
			ids = append(ids, p.ID)
		}
	} else {
		ids = append(ids, r.order...)
	}
	return ids
}

func gameCode(err error) (string, string) {
	if ge, ok := err.(*game.GameError); ok {
		return string(ge.Code), ge.Message
	}
	return CodeUnexpected, err.Error()
}

func digestAction(a game.Action) string {
	b, err := json.Marshal(a)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// --- helpers ---

func sanitize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func validName(s string) bool {
	if s == "" {
		return false
	}
	n := 0
	for _, R := range s {
		if R < 0x20 || R == 0x7f {
			return false
		}
		n++
	}
	return n >= 1 && n <= 24
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func collectIDs(m map[string]time.Time) []string {
	out := []string{}
	for id := range m {
		out = append(out, id)
	}
	return out
}

func newPlayerID() string {
	return genID("p")
}

var idCounter uint64

func genID(prefix string) string {
	idCounter++
	return prefix + hex.EncodeToString([]byte{byte(idCounter >> 8), byte(idCounter)})
}

// idemEntry is a single cached response keyed by request_id.
type idemEntry struct {
	digest  string
	outcome *Outcome
}

// idemCache is a bounded per-player idempotency cache (readme §5.2).
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
	for len(c.order) > c.limit {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}
}

// tokenMatchesRaw compares a provided secret against the stored secret in
// constant time (readme §2.2).
func tokenMatchesRaw(provided, stored string) bool {
	if stored == "" || provided == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(stored)) == 1
}
