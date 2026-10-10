package game

// PlayerIndex returns the seat index of playerID, or -1. It mirrors the
// unexported playerIndex for use by the server layer.
func (g *Game) PlayerIndex(id string) int { return g.playerIndex(id) }

// SetConnected updates the connected flag of a player in place (used by the
// room's disconnect/pause handling).
func (g *Game) SetConnected(id string, connected bool) {
	if i := g.playerIndex(id); i >= 0 {
		g.Players[i].Connected = connected
	}
}
