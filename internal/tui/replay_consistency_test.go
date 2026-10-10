package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"terminal-uno/internal/game"
)

// ---------------------------------------------------------------------------
// Match replay consistency checker.
//
// validateReplay performs a self-contained consistency check on a recorded
// game's *public* event stream only — turn order, card matching, action-card
// effects, penalty draw counts, +4 challenge resolution and the winner — by
// cross-checking events against each other and against rule.md. It never needs
// the players' hidden hands, so it works purely from the privacy-safe history
// that MatchRecord captures.
// ---------------------------------------------------------------------------

func validateReplay(rec *MatchRecord) []string {
	var probs []string
	events := rec.Events
	n := len(events)
	consumed := make([]bool, n)

	// Player index = order of first appearance in events.
	pidx := map[string]int{}
	order := []string{}
	for _, e := range events {
		if e.PlayerID == "" {
			continue
		}
		if _, ok := pidx[e.PlayerID]; !ok {
			pidx[e.PlayerID] = len(order)
			order = append(order, e.PlayerID)
		}
	}
	nP := len(order)
	actor := func(e game.GameEvent) int { return pidx[e.PlayerID] }
	wrap := func(i int) int {
		m := i % nP
		if m < 0 {
			m += nP
		}
		return m
	}
	other := func(i int) int { return wrap(i + 1) }
	nameOf := func(i int) string {
		if i >= 0 && i < len(order) {
			return order[i]
		}
		return fmt.Sprintf("<idx%d>", i)
	}
	drawCount := func(e game.GameEvent) int {
		m, _ := e.Payload.(map[string]interface{})
		f, _ := m["count"].(float64)
		return int(f)
	}
	peekTurnAction := func(from int) *game.GameEvent {
		for j := from + 1; j < n; j++ {
			if consumed[j] {
				continue
			}
			switch events[j].Type {
			case "CARD_PLAYED", "CARD_DRAWN", "WINNER":
				return &events[j]
			}
		}
		return nil
	}
	add := func(cond bool, format string, args ...interface{}) {
		if !cond {
			probs = append(probs, fmt.Sprintf(format, args...))
		}
	}

	cur := -1
	hasTop := false
	var activeColor, topRank string

	// consumeSkip expects the immediately following PLAYER_SKIPPED(target) that
	// this play caused, and returns true when found.
	consumeSkip := func(from, by, target int) bool {
		ok := false
		for k := from + 1; k < n; k++ {
			if consumed[k] {
				continue
			}
			e2 := events[k]
			if e2.Type == "PLAYER_SKIPPED" && actor(e2) == by && pidx[e2.TargetPlayer] == target {
				consumed[k] = true
				ok = true
			}
			break
		}
		return ok
	}

	for pos := 0; pos < n; pos++ {
		if consumed[pos] {
			continue
		}
		ev := events[pos]
		switch ev.Type {
		case "CARD_PLAYED":
			a := actor(ev)
			if cur < 0 {
				cur = a
			} else if a != cur {
				add(false, "id%d %s 出牌: 应轮到 %s", ev.ID, ev.PlayerID, nameOf(cur))
			}
			c := ev.Card
			if c == nil {
				add(false, "id%d CARD_PLAYED 缺少 card", ev.ID)
				break
			}
			if hasTop {
				ok := c.Color == game.Color(activeColor) || string(c.Rank) == topRank ||
					c.Rank == game.RankWild || c.Rank == game.RankWildDrawFour
				add(ok, "id%d %s 打 %s/%s 不匹配(顶色=%s 顶rank=%s)",
					ev.ID, ev.PlayerID, c.Color, c.Rank, activeColor, topRank)
			}
			topRank = string(c.Rank)
			if c.Rank == game.RankWild || c.Rank == game.RankWildDrawFour {
				activeColor = ev.Color
			} else {
				activeColor = string(c.Color)
			}
			hasTop = true

			switch c.Rank {
			case game.RankDrawTwo:
				tgt := wrap(cur + 1)
				// A draw_two that finishes the game emits no consequence
				// (WINNER is decided before the effect applies).
				var nxt *game.GameEvent
				for k := pos + 1; k < n; k++ {
					if !consumed[k] {
						nxt = &events[k]
						break
					}
				}
				if nxt != nil && nxt.Type == "WINNER" {
					cur = wrap(tgt + 1)
					continue
				}
				seq := 0
				for k := pos + 1; k < n && seq < 2; k++ {
					if consumed[k] {
						continue
					}
					e2 := events[k]
					if e2.Type == "CARD_DRAWN" && actor(e2) == tgt && drawCount(e2) == 2 {
						consumed[k] = true
						seq = 1
					} else if e2.Type == "PLAYER_SKIPPED" && actor(e2) == a && pidx[e2.TargetPlayer] == tgt {
						consumed[k] = true
						seq = 2
					}
				}
				add(seq == 2, "id%d draw_two 副效果不完整(目标 %s 摸2+跳过)", ev.ID, nameOf(tgt))
				cur = wrap(tgt + 1)

			case game.RankSkip:
				tgt := wrap(cur + 1)
				consumeSkip(pos, a, tgt)
				cur = wrap(tgt + 1)

			case game.RankReverse:
				if nP == 2 {
					// reverse == skip in a 2-player game.
					tgt := wrap(cur + 1)
					consumeSkip(pos, a, tgt)
					cur = wrap(tgt + 1)
				} else {
					cur = wrap(cur - 1)
				}

			case game.RankWildDrawFour:
				tgt := wrap(cur + 1)
				resolved := false
				k := pos + 1
				for k < n {
					if consumed[k] {
						k++
						continue
					}
					e2 := events[k]
					switch e2.Type {
					case "UNO_CALLED":
						if actor(e2) == a {
							consumed[k] = true
						}
					case "FOUR_CHALLENGE":
						add(actor(e2) == a && pidx[e2.TargetPlayer] == tgt,
							"id%d FOUR_CHALLENGE 语义错: 应 +4方=%s 目标=%s", e2.ID, ev.PlayerID, nameOf(tgt))
						consumed[k] = true
						kk := k + 1
						for kk < n {
							e3 := events[kk]
							if consumed[kk] {
								kk++
								continue
							}
							if e3.Type == "CARD_DRAWN" && actor(e3) == tgt {
								consumed[kk] = true
							} else if e3.Type == "CHALLENGE_SUCCEEDED" || e3.Type == "CHALLENGE_FAILED" {
								add(pidx[e3.PlayerID] == tgt, "id%d %s 应归属质疑者 %s", e3.ID, e3.Type, nameOf(tgt))
								if e3.Type == "CHALLENGE_FAILED" {
									cur = wrap(tgt + 1)
								} else {
									cur = tgt
								}
								resolved = true
								consumed[kk] = true
								break
							}
							kk++
						}
						k = n
					default:
						k = n
					}
					if !resolved && k < n {
						k++
					}
				}
				add(resolved, "id%d +4 缺少质疑结果事件", ev.ID)

			case game.RankWild:
				cur = wrap(cur + 1)

			default: // numeric
				cur = wrap(cur + 1)
			}

		case "CARD_DRAWN":
			a := actor(ev)
			cnt := drawCount(ev)
			add(cur < 0 || a == cur, "id%d %s 摸牌: 应轮到 %s", ev.ID, ev.PlayerID, nameOf(cur))
			if cnt == 1 {
				nxt := peekTurnAction(pos)
				if !(nxt != nil && nxt.Type == "CARD_PLAYED" && actor(*nxt) == a) {
					cur = other(cur)
				}
				// else: drawn-decision, the player keeps the turn (played next).
			} else {
				add(false, "id%d 出现未归属罚摸 %d 张", ev.ID, cnt)
			}

		case "PLAYER_SKIPPED", "UNO_CALLED", "FOUR_CHALLENGE", "CHALLENGE_FAILED", "CHALLENGE_SUCCEEDED":
			add(false, "id%d %s 未与触发牌绑定为副效果", ev.ID, ev.Type)

		case "WINNER":
			add(ev.ID == events[n-1].ID, "WINNER 应为末条事件, 实际在 pos%d", pos)
			if len(rec.Players) > 0 {
				winners := 0
				for _, p := range rec.Players {
					if p.Winner {
						winners++
						add(p.CardCount == 0, "赢家 %s 终局手牌应为 0, 实际 %d", p.Name, p.CardCount)
					}
				}
				add(winners == 1, "Players 中应恰好有 1 名赢家, 实际 %d", winners)
			}
		}
	}
	return probs
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestReplayConsistency validates every record in the committed fixture.
func TestReplayConsistency(t *testing.T) {
	records := readRecords(t, filepath.Join("testdata", "example_game.json"))
	if len(records) == 0 {
		t.Fatal("fixture has no records")
	}
	for _, rec := range records {
		probs := validateReplay(&rec)
		if len(probs) > 0 {
			for _, p := range probs {
				t.Errorf("seq %d: %s", rec.Seq, p)
			}
		}
	}
}

// TestReplayConsistencyOptionalExternal, when UNO_GAMES_JSON points at a local
// (uncommitted, temporary) games.json, validates it too. It is a no-op when the
// env variable is unset, so CI never depends on the dev's temp file.
func TestReplayConsistencyOptionalExternal(t *testing.T) {
	path := os.Getenv("UNO_GAMES_JSON")
	if path == "" {
		t.Skip("UNO_GAMES_JSON not set; skipping optional validation of temporary history")
	}
	records := readRecords(t, path)
	for _, rec := range records {
		probs := validateReplay(&rec)
		if len(probs) > 0 {
			for _, p := range probs {
				t.Errorf("seq %d: %s", rec.Seq, p)
			}
		}
	}
}

func readRecords(t *testing.T, path string) []MatchRecord {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var recs []MatchRecord
	if err := json.Unmarshal(b, &recs); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return recs
}
