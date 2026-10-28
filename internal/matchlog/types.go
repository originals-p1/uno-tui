package matchlog

import (
	"terminal-uno/internal/game"
	"time"
)

type MatchRecord struct {
	Seq           int64            `json:"seq"`
	Time          time.Time        `json:"time"`
	Winner        string           `json:"winner"` // winner's display name
	EndReason     string           `json:"end_reason,omitempty"`
	ConfigVersion uint64           `json:"config_version"`
	Plays         []PlayRecord     `json:"plays,omitempty"`
	Events        []game.GameEvent `json:"events,omitempty"`
	Players       []RecordPlayer   `json:"players"`
}

// RecordPlayer is one participant's final state in a completed game.
type RecordPlayer struct {
	Name      string `json:"name"`
	CardCount int    `json:"card_count"`
	IsHost    bool   `json:"is_host,omitempty"`
	Winner    bool   `json:"winner,omitempty"`
}

type HandSnapshot struct {
	PlayerID string      `json:"player_id"`
	Name     string      `json:"name"`
	Cards    []game.Card `json:"cards"`
}
type PlayRecord struct {
	EventID  uint64         `json:"event_id"`
	PlayerID string         `json:"player_id"`
	Card     *game.CardView `json:"card"`
	Color    game.Color     `json:"color,omitempty"`
	Before   []HandSnapshot `json:"hands_before"`
	After    []HandSnapshot `json:"hands_after"`
}

func Hands(g *game.Game) []HandSnapshot {
	out := []HandSnapshot{}
	for _, p := range g.Players {
		out = append(out, HandSnapshot{p.ID, p.Name, append([]game.Card{}, p.Hand...)})
	}
	return out
}
