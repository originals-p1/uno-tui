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

// StackView describes an in-flight stacking penalty (rule.md R19-R21). It is
// non-secret: it tells the UI how many cards the current player would draw on
// accepting, and that an accept action is available.
type StackView struct {
	Credits      int  `json:"credits"`
	YourDecision bool `json:"your_decision"` // true == it is your choice
	CanChallenge bool `json:"can_challenge"` // initial +4 can be challenged (combined rule R23)
}

// ChallengeResult is a transient outcome banner describing how a +4 challenge
// resolved (rule.md R13). It is non-secret and shown to all players until the
// next in-turn action clears it.
type ChallengeResult struct {
	Succeeded          bool   `json:"succeeded"` // challenger's challenge succeeded
	ResolverPlayerID   string `json:"resolver_player_id"`
	ChallengerPlayerID string `json:"challenger_player_id"`
}

// RoundResult is a transient banner describing the just-completed round of a
// SCORE-mode match (rule.md R28). It is attached to the view of the first state
// of the following round so clients can show "Player X earned N points" before
// play resumes, and cleared as soon as a new in-round action lands.
type RoundResult struct {
	RoundNumber    int                   `json:"round_number"`
	WinnerPlayerID string                `json:"winner_player_id"`
	Points         int                   `json:"points"`
	Deltas         map[string]int        `json:"deltas"` // per-player change this round (winner +, others -)
	Scores         map[string]int        `json:"scores"`
	Hands          map[string][]CardView `json:"hands"` // each player's cards at round end
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
	Stack          *StackView        `json:"stack,omitempty"`
	Paused         *PausedView       `json:"paused,omitempty"`
	WinnerPlayerID string            `json:"winner_player_id,omitempty"`
	EndReason      string            `json:"end_reason,omitempty"`
	LastEventID    uint64            `json:"last_event_id"`
	History        []GameEvent       `json:"history"`

	// SCORE-mode match fields (rule.md R28), populated by the room, not the
	// per-round engine. Empty in single-round mode.
	WinMode             string         `json:"win_mode,omitempty"`
	RoundNumber         int            `json:"round_number,omitempty"`
	TargetScore         int            `json:"target_score,omitempty"`
	Scores              map[string]int `json:"scores,omitempty"`
	MatchOver           bool           `json:"match_over,omitempty"`
	MatchWinnerPlayerID string         `json:"match_winner_player_id,omitempty"`
	RoundResult         *RoundResult   `json:"round_result,omitempty"`
	// RoundOver marks the pause after a SCORE-mode round ends (before the next):
	// the previous round's hands/scores are shown and a player presses "c" to
	// advance (rule.md R28).
	RoundOver bool `json:"round_over,omitempty"`
	// FinalHands holds every player's cards when the game is finished, so the
	// end screen can show everyone's hand (single-round and SCORE match-over).
	FinalHands map[string][]CardView `json:"final_hands,omitempty"`
}

// finalHands snapshots every player's remaining cards (used for the end-of-game
// summary screen, rule.md §15). It is not secret at this point: the game is over.
func (g *Game) finalHands() map[string][]CardView {
	if g == nil {
		return nil
	}
	out := make(map[string][]CardView, len(g.Players))
	for _, p := range g.Players {
		var cards []CardView
		for _, c := range p.Hand {
			cards = append(cards, c.View())
		}
		out[p.ID] = cards
	}
	return out
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
	if g.Phase == PhaseStack {
		v.Stack = &StackView{
			Credits:      g.stackCredits,
			YourDecision: playerID == g.currentPlayer().ID,
			CanChallenge: g.stackChallengeAvailable(),
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
	if g.Phase == PhaseFinished {
		v.FinalHands = g.finalHands()
	}
	return v
}
