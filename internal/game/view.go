package game

// CardView is the JSON shape of a card exposed to clients.
type CardView struct {
	ID    string `json:"id"`
	Color Color  `json:"color"`
	Rank  Rank   `json:"rank"`
}

// View returns the CardView form of c.
func (c Card) View() CardView { return CardView{ID: c.ID, Color: c.Color, Rank: c.Rank} }

// GameEvent is a public, history-serializable event. It must never contain
// another player's hand cards or any secret field.
type GameEvent struct {
	ID           uint64      `json:"id"`
	Revision     uint64      `json:"room_revision"`
	Type         string      `json:"type"`
	PlayerID     string      `json:"player_id"`
	TargetPlayer string      `json:"target_player_id,omitempty"`
	Card         *CardView   `json:"card,omitempty"`
	Color        string      `json:"color,omitempty"`
	Payload      interface{} `json:"payload,omitempty"`
}

// DrawDecisionView is the private per-player view shown only to the acting
// player during the drawn_decision phase.
type DrawDecisionView struct {
	Card CardView `json:"card"`
}

// PausedView describes a paused game (disconnects and their deadline).
type PausedView struct {
	DisconnectedPlayerIDs []string `json:"disconnected_player_ids"`
	EarliestDeadline      string   `json:"earliest_deadline"` // RFC 3339 UTC
}

// ChallengeView describes an ongoing +4 challenge (rule.md R13). It is
// non-secret: it tells the UI who played the +4, whose decision is pending, and
// the active color that was in play when the +4 was dropped (the color the
// resolver needed to (not) hold) so the challenger can decide.
type ChallengeView struct {
	ResolverPlayerID string `json:"resolver_player_id"`
	YourDecision     bool   `json:"your_decision"` // true == it is your choice
	TriggerColor     string `json:"trigger_color,omitempty"`
}

// ChallengeResult is a transient outcome banner describing how a +4 challenge
// resolved (rule.md R13). It is non-secret and shown to all players until the
// next in-turn action clears it.
type ChallengeResult struct {
	Succeeded          bool   `json:"succeeded"` // challenger's challenge succeeded
	ResolverPlayerID   string `json:"resolver_player_id"`
	ChallengerPlayerID string `json:"challenger_player_id"`
}

// GameView is the full personalized view of the table.
type GameView struct {
	RoomRevision   uint64            `json:"room_revision"`
	Players        []PlayerView      `json:"players"`
	CurrentPlayer  string            `json:"current_player"`
	Direction      int               `json:"direction"`
	TopCard        CardView          `json:"top_card"`
	ActiveColor    string            `json:"active_color"`
	YourHand       []CardView        `json:"your_hand"`
	PlayableIDs    []string          `json:"playable_ids"`
	Phase          string            `json:"phase"`
	DrawDecision   *DrawDecisionView `json:"draw_decision,omitempty"`
	Challenge      *ChallengeView    `json:"challenge,omitempty"`
	ChallengeRes   *ChallengeResult  `json:"challenge_result,omitempty"`
	Paused         *PausedView       `json:"paused,omitempty"`
	WinnerPlayerID string            `json:"winner_player_id,omitempty"`
	EndReason      string            `json:"end_reason,omitempty"`
	LastEventID    uint64            `json:"last_event_id"`
	History        []GameEvent       `json:"history"`
}

// BuildViewFor returns a personalized view for playerID. Different players
// receive different views: YourHand and PlayableIDs only ever contain the
// requester's own cards, and DrawDecision is only populated for the acting
// player during drawn_decision (readme §5.3).
func (g *Game) BuildViewFor(playerID string) *GameView {
	pi := g.playerIndex(playerID)
	v := &GameView{
		RoomRevision:  g.RoomRevision,
		CurrentPlayer: g.currentPlayer().ID,
		Direction:     g.Direction,
		TopCard:       g.topCard().View(),
		ActiveColor:   string(g.ActiveColor),
		Phase:         string(g.Phase),
		LastEventID:   g.lastEventID(),
		History:       append([]GameEvent{}, g.history...),
	}

	v.Players = make([]PlayerView, 0, len(g.Players))
	for i := range g.Players {
		p := &g.Players[i]
		v.Players = append(v.Players, PlayerView{
			ID:        p.ID,
			Name:      p.Name,
			CardCount: len(p.Hand),
			UNO:       p.HasUNO,
			Connected: p.Connected,
			Ready:     p.Ready,
			IsHost:    p.IsHost,
		})
	}

	if pi >= 0 {
		for _, c := range g.Players[pi].Hand {
			v.YourHand = append(v.YourHand, c.View())
		}
		lm := g.LegalMoves(playerID)
		v.PlayableIDs = append([]string{}, lm.PlayableIDs...)

		if g.Phase == PhaseDrawnDecision && g.DrawnCardID != "" && playerID == g.currentPlayer().ID {
			if cidx := IndexOf(g.Players[pi].Hand, g.DrawnCardID); cidx >= 0 {
				c := g.Players[pi].Hand[cidx].View()
				v.DrawDecision = &DrawDecisionView{Card: c}
			}
		}
	}

	if g.Phase == PhaseChallenge {
		v.Challenge = &ChallengeView{
			ResolverPlayerID: g.challengeResolver,
			YourDecision:     playerID == g.currentPlayer().ID,
			TriggerColor:     g.challengeTriggerColor,
		}
	}
	if g.lastChallengeResult != nil {
		cr := *g.lastChallengeResult
		v.ChallengeRes = &cr
	}
	if g.Phase == PhasePaused {
		v.Paused = g.pausedView()
	}
	if g.WinnerPlayerID != "" {
		v.WinnerPlayerID = g.WinnerPlayerID
	}
	if g.EndReason != "" {
		v.EndReason = g.EndReason
	}
	return v
}
