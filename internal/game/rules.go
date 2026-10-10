package game

// isRealColor reports whether col is one of the four playable colors (not wild).
func isRealColor(col Color) bool {
	return col == ColorRed || col == ColorYellow || col == ColorGreen || col == ColorBlue
}

// playerIndex returns the seat index of playerID or -1.
func (g *Game) playerIndex(id string) int {
	for i := range g.Players {
		if g.Players[i].ID == id {
			return i
		}
	}
	return -1
}

func (g *Game) currentPlayer() *Player {
	return &g.Players[g.CurrentIndex]
}

func (g *Game) topCard() Card {
	return g.DiscardPile[len(g.DiscardPile)-1]
}

func (g *Game) topRank() Rank { return g.topCard().Rank }

// wrap normalizes i into [0,n).
func wrap(i, n int) int {
	if n == 0 {
		return 0
	}
	i %= n
	if i < 0 {
		i += n
	}
	return i
}

// playableOn reports whether card matches the current active color or the top
// card's rank, or is itself a wild card.
func playableOn(c Card, activeColor Color, topRank Rank) bool {
	if c.Color == activeColor {
		return true
	}
	if c.Rank == topRank {
		return true
	}
	if IsWild(c.Rank) {
		return true
	}
	return false
}

func hasColor(hand []Card, col Color) bool {
	for _, c := range hand {
		if c.Color == col {
			return true
		}
	}
	return false
}

// handPlayableIDs computes the playable hand card IDs for the current rules.
// A Wild Draw Four is only playable when the player holds no card of the
// active color (readme §3.2).
func (g *Game) handPlayableIDs(hand []Card) []string {
	active := g.ActiveColor
	top := g.topRank()
	var ids []string
	for _, c := range hand {
		if !playableOn(c, active, top) {
			continue
		}
		if c.Rank == RankWildDrawFour && hasColor(hand, active) {
			continue
		}
		ids = append(ids, c.ID)
	}
	return ids
}

// LegalMoves returns the current legal actions for a player. It never mutates
// game state and never exposes another player's hand.
func (g *Game) LegalMoves(playerID string) LegalMoves {
	lm := LegalMoves{}
	if g.Phase == PhaseFinished {
		return lm
	}
	pi := g.playerIndex(playerID)
	if pi < 0 || g.Phase == PhasePaused {
		return lm
	}
	cur := g.Players[g.CurrentIndex]
	if playerID != cur.ID {
		return lm
	}
	switch g.Phase {
	case PhaseTurn:
		lm.PlayableIDs = g.handPlayableIDs(g.Players[pi].Hand)
		lm.CanDraw = true
	case PhaseDrawnDecision:
		if g.DrawnCardID != "" {
			// Only the just-drawn card may be played in this phase.
			lm.PlayableIDs = []string{g.DrawnCardID}
		}
	}
	return lm
}

// Apply executes a single validated action atomically. On error the game state
// is left unchanged and a stable error code is returned.
func (g *Game) Apply(playerID string, a Action) (*Result, error) {
	if g.Phase == PhaseFinished {
		return nil, gErr(ErrCodeGameFinished, "game already finished")
	}
	if g.Phase == PhasePaused {
		return nil, gErr(ErrCodeInvalidPhase, "game paused")
	}
	pi := g.playerIndex(playerID)
	if pi < 0 {
		return nil, gErr(ErrCodeInvalidPlayer, "unknown player %q", playerID)
	}
	cur := g.currentPlayer()
	if playerID != cur.ID {
		return nil, gErr(ErrCodeNotYourTurn, "it is not %s's turn", cur.ID)
	}

	var res *Result
	var err error
	switch a.Type {
	case ActionPlayCard:
		if g.Phase != PhaseTurn {
			return nil, gErr(ErrCodeInvalidPhase, "cannot play a hand card in phase %s", g.Phase)
		}
		res, err = g.applyPlayHandCard(pi, a)
	case ActionDrawCard:
		if g.Phase != PhaseTurn {
			return nil, gErr(ErrCodeInvalidPhase, "cannot draw in phase %s", g.Phase)
		}
		res, err = g.applyDrawCard(pi, a)
	case ActionPlayDrawn:
		if g.Phase != PhaseDrawnDecision {
			return nil, gErr(ErrCodeInvalidPhase, "no drawn card decision in phase %s", g.Phase)
		}
		res, err = g.applyPlayDrawn(pi, a)
	case ActionPassDrawn:
		if g.Phase != PhaseDrawnDecision {
			return nil, gErr(ErrCodeInvalidPhase, "no drawn card decision in phase %s", g.Phase)
		}
		res, err = g.applyPassDrawn(pi)
	default:
		return nil, gErr(ErrCodeInvalidArgument, "unknown action type %q", a.Type)
	}
	if err != nil {
		return nil, err
	}
	// Commit the transaction's public events onto the bounded history log.
	g.commitEvents(res.Events)
	return res, nil
}

// resolveChosenColor validates and returns the chosen color for a wild card,
// or nil for a non-wild card. It returns an error on invalid input.
func (g *Game) resolveChosenColor(card Card, a Action) (*Color, error) {
	if !IsWild(card.Rank) {
		if a.ChosenColor != nil {
			return nil, gErr(ErrCodeInvalidColor, "chosen color only allowed for wild cards")
		}
		return nil, nil
	}
	if a.ChosenColor == nil {
		return nil, gErr(ErrCodeInvalidColor, "chosen color required for wild card")
	}
	col := *a.ChosenColor
	if !isRealColor(col) {
		return nil, gErr(ErrCodeInvalidColor, "invalid chosen color %q", col)
	}
	c := col
	return &c, nil
}

// applyPlayHandCard plays a card already in the player's hand.
func (g *Game) applyPlayHandCard(pi int, a Action) (*Result, error) {
	cidx := IndexOf(g.Players[pi].Hand, a.CardID)
	if cidx < 0 {
		return nil, gErr(ErrCodeCardNotOwned, "player does not own card %q", a.CardID)
	}
	card := g.Players[pi].Hand[cidx]
	chosen, err := g.resolveChosenColor(card, a)
	if err != nil {
		return nil, err
	}
	if !playableOn(card, g.ActiveColor, g.topRank()) {
		return nil, gErr(ErrCodeCardNotPlayable, "card %s is not playable", card)
	}
	if card.Rank == RankWildDrawFour && hasColor(g.Players[pi].Hand, g.ActiveColor) {
		return nil, gErr(ErrCodeWildDrawFourInvalid, "wild draw four disallowed: player holds active color")
	}

	res := g.discardFromHand(pi, card, chosen)
	if res.Ended {
		return res, nil
	}
	g.applyEffectsAndAdvance(pi, card.Rank, res)
	return res, nil
}

// discardFromHand removes card from the player's hand, places it on the
// discard pile, updates ActiveColor, and performs win/UNO detection. It
// returns a non-nil Result whose Ended flag is set on a win.
func (g *Game) discardFromHand(pi int, card Card, chosen *Color) *Result {
	hand := g.Players[pi].Hand
	cidx := IndexOf(hand, card.ID)
	hand = append(append([]Card{}, hand[:cidx]...), hand[cidx+1:]...)
	g.Players[pi].Hand = hand

	g.DiscardPile = append(g.DiscardPile, card)
	if chosen != nil {
		g.ActiveColor = *chosen
	} else if IsWild(card.Rank) {
		g.ActiveColor = *chosen // defensive; caller guarantees non-nil for wild
	} else {
		g.ActiveColor = card.Color
	}

	res := &Result{}
	cv := card.View()
	res.Events = append(res.Events, g.event(EventCardPlayed, g.Players[pi].ID, &cv, g.ActiveColor))

	if len(hand) == 0 {
		g.Players[pi].HasUNO = false
		g.WinnerPlayerID = g.Players[pi].ID
		g.Phase = PhaseFinished
		g.EndReason = "win"
		res.Ended = true
		res.WinnerID = g.WinnerPlayerID
		res.Events = append(res.Events, g.event(EventWinner, g.Players[pi].ID, nil, ""))
		return res
	}
	if g.updateUNO(pi) {
		res.Events = append(res.Events, g.event(EventUNOCalled, g.Players[pi].ID, nil, ""))
	}
	return res
}

// applyDrawCard draws one card for the current player. If the drawn card is
// playable the game enters drawn_decision (only that card may be played);
// otherwise the turn advances in the same transaction.
func (g *Game) applyDrawCard(pi int, a Action) (*Result, error) {
	drawn := g.drawCards(1)
	res := &Result{}
	if len(drawn) == 0 {
		// Nothing at all left to draw: pass and advance the turn.
		return g.passAndAdvance(pi, res)
	}
	card := drawn[0]
	g.Players[pi].Hand = append(g.Players[pi].Hand, card)
	g.cardDrawnEvent(res, g.Players[pi].ID, len(drawn))

	playable := playableOn(card, g.ActiveColor, g.topRank())
	if card.Rank == RankWildDrawFour && hasColor(g.Players[pi].Hand, g.ActiveColor) {
		playable = false
	}
	if playable {
		g.Phase = PhaseDrawnDecision
		g.DrawnCardID = card.ID
		if g.updateUNO(pi) {
			res.Events = append(res.Events, g.event(EventUNOCalled, g.Players[pi].ID, nil, ""))
		}
		res.NextPlayerID = g.Players[pi].ID
		res.LegalMoves = []string{g.DrawnCardID}
		return res, nil
	}
	return g.passAndAdvance(pi, res)
}

// applyPlayDrawn plays the just-drawn card (the engine, not the client,
// decides which card that is).
func (g *Game) applyPlayDrawn(pi int, a Action) (*Result, error) {
	if g.DrawnCardID == "" {
		return nil, gErr(ErrCodeNoDrawnCard, "no drawn card pending")
	}
	cidx := IndexOf(g.Players[pi].Hand, g.DrawnCardID)
	if cidx < 0 {
		return nil, gErr(ErrCodeCardNotDrawn, "drawn card %q not found in hand", g.DrawnCardID)
	}
	card := g.Players[pi].Hand[cidx]
	chosen, err := g.resolveChosenColor(card, a)
	if err != nil {
		return nil, err
	}
	g.DrawnCardID = ""
	g.Phase = PhaseTurn
	res := g.discardFromHand(pi, card, chosen)
	if res.Ended {
		return res, nil
	}
	g.applyEffectsAndAdvance(pi, card.Rank, res)
	return res, nil
}

// applyPassDrawn keeps the drawn card and advances the turn.
func (g *Game) applyPassDrawn(pi int) (*Result, error) {
	if g.DrawnCardID == "" {
		return nil, gErr(ErrCodeNoDrawnCard, "no drawn card pending")
	}
	res := &Result{}
	g.DrawnCardID = ""
	g.Phase = PhaseTurn
	return g.passAndAdvance(pi, res)
}

// passAndAdvance advances the turn one step (used after a draw that cannot be
// played, or after passing), carrying over UNO state.
func (g *Game) passAndAdvance(pi int, res *Result) (*Result, error) {
	g.CurrentIndex = wrap(g.CurrentIndex+g.Direction, len(g.Players))
	res.NextPlayerID = g.currentPlayer().ID
	res.LegalMoves = g.handPlayableIDs(g.currentPlayer().Hand)
	return res, nil
}

// applyEffectsAndAdvance resolves the effect of a played action card and
// advances the turn cursor to the next active player.
func (g *Game) applyEffectsAndAdvance(pi int, played Rank, res *Result) {
	n := len(g.Players)
	playerID := g.Players[pi].ID
	switch played {
	case RankSkip:
		skipped := g.Players[wrap(g.CurrentIndex+g.Direction, n)].ID
		g.CurrentIndex = wrap(g.CurrentIndex+2*g.Direction, n)
		res.Events = append(res.Events, g.event(EventSkipped, playerID, nil, "", skipped))
	case RankReverse:
		if n == 2 {
			// 2-player Reverse is equivalent to Skip (readme §3.2).
			skipped := g.Players[wrap(g.CurrentIndex+g.Direction, n)].ID
			g.CurrentIndex = wrap(g.CurrentIndex+2*g.Direction, n)
			res.Events = append(res.Events, g.event(EventSkipped, playerID, nil, "", skipped))
		} else {
			g.Direction = -g.Direction
			g.CurrentIndex = wrap(g.CurrentIndex+g.Direction, n)
			res.Events = append(res.Events, g.event(EventDirectionReversed, playerID, nil, ""))
		}
	case RankDrawTwo:
		target := wrap(g.CurrentIndex+g.Direction, n)
		drawn := g.drawCards(2)
		g.Players[target].Hand = append(g.Players[target].Hand, drawn...)
		g.cardDrawnEvent(res, g.Players[target].ID, len(drawn))
		g.CurrentIndex = wrap(g.CurrentIndex+2*g.Direction, n)
		res.Events = append(res.Events, g.event(EventSkipped, playerID, nil, "", g.Players[target].ID))
	case RankWildDrawFour:
		target := wrap(g.CurrentIndex+g.Direction, n)
		drawn := g.drawCards(4)
		g.Players[target].Hand = append(g.Players[target].Hand, drawn...)
		g.cardDrawnEvent(res, g.Players[target].ID, len(drawn))
		g.CurrentIndex = wrap(g.CurrentIndex+2*g.Direction, n)
		res.Events = append(res.Events, g.event(EventSkipped, playerID, nil, "", g.Players[target].ID))
	case RankWild:
		g.CurrentIndex = wrap(g.CurrentIndex+g.Direction, n)
	default: // numeric cards
		g.CurrentIndex = wrap(g.CurrentIndex+g.Direction, n)
	}
	res.NextPlayerID = g.currentPlayer().ID
	res.LegalMoves = g.handPlayableIDs(g.currentPlayer().Hand)
}

// updateUNO maintains the once-per-transition UNO flag. It returns true only
// when a player transitions into exactly one card and did not previously have
// the flag set.
func (g *Game) updateUNO(pi int) bool {
	p := &g.Players[pi]
	now1 := len(p.Hand) == 1
	if now1 && !p.HasUNO {
		p.HasUNO = true
		return true
	}
	if !now1 && p.HasUNO {
		p.HasUNO = false
	}
	return false
}

// drawCards pops up to n cards from the top of the draw pile, recycling the
// discard pile (keeping its top card) as needed. It returns the cards actually
// drawn, which may be fewer than requested when both piles are exhausted.
func (g *Game) drawCards(n int) []Card {
	var drawn []Card
	for len(drawn) < n {
		if len(g.DrawPile) == 0 {
			if len(g.DiscardPile) <= 1 {
				break
			}
			keep := g.DiscardPile[len(g.DiscardPile)-1]
			rest := g.DiscardPile[:len(g.DiscardPile)-1]
			g.DiscardPile = []Card{keep}
			shuffle(rest, g.rng)
			g.DrawPile = rest
		}
		c := g.DrawPile[0]
		g.DrawPile = g.DrawPile[1:]
		drawn = append(drawn, c)
	}
	return drawn
}

// cardDrawnEvent records a public CARD_DRAWN event. The actual card is never
// included (hand privacy, readme §6.2).
func (g *Game) cardDrawnEvent(res *Result, playerID string, n int) {
	res.Events = append(res.Events, Event{
		ID:       g.NextEventID(),
		Type:     EventCardDrawn,
		PlayerID: playerID,
		Payload:  map[string]interface{}{"count": n},
	})
}

// event builds a public Event with a fresh ID.
func (g *Game) event(t EventType, playerID string, card *CardView, color Color, target ...string) Event {
	e := Event{
		ID:       g.NextEventID(),
		Type:     t,
		PlayerID: playerID,
		Card:     card,
		Color:    color,
	}
	if len(target) > 0 {
		e.TargetPlayer = target[0]
	}
	return e
}
