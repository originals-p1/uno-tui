package game

// CardView is the JSON shape of a card exposed to clients. It carries the
// same fields as Card and is used in views and events.
type CardView struct {
	ID    string `json:"id"`
	Color Color  `json:"color"`
	Rank  Rank   `json:"rank"`
}

// View returns the CardView form of c.
func (c Card) View() CardView { return CardView{ID: c.ID, Color: c.Color, Rank: c.Rank} }
