package game

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// DeckType identifiers (rule.md §2.3).
const (
	DeckAction140   = "ACTION_140"
	DeckClassic108  = "CLASSIC_108"
	DeckExtended112 = "EXTENDED_112"
)

// Direction identifiers (rule.md R03).
const (
	DirClockwise        = "CLOCKWISE"
	DirCounterClockwise = "COUNTER_CLOCKWISE"
	DirRandom           = "RANDOM"
)

// FirstPlayerMode identifiers (rule.md R04).
const (
	FirstAfterDealer = "AFTER_DEALER"
	FirstRandom      = "RANDOM"
	FirstHost        = "HOST"
)

// WinMode identifiers (rule.md R27/R28).
const (
	WinSingleRound = "SINGLE_ROUND"
	WinScore       = "SCORE"
)

// LeavePolicy / HostLeavePolicy identifiers (rule.md R37/R39).
const (
	LeaveBotReplace = "BOT_REPLACE"
	LeavePause      = "PAUSE"
	LeaveEndRound   = "END_ROUND"
	HostCloseRoom   = "CLOSE_ROOM"
	HostMigrate     = "MIGRATE"
)

// Config is the single host-authorised configuration for a room and its game
// (rule.md §0.2 / §13). Every field is serializable and validated by Validate.
// Fields marked "future" are parsed and validated but have no v2 behaviour.
type Config struct {
	SchemaVersion int `json:"schema_version"`

	Room struct {
		MaxPlayers           int    `json:"max_players"`
		RequireAllReady      bool   `json:"require_all_ready"`
		AllowSpectators      bool   `json:"allow_spectators"` // future
		AllowReconnect       bool   `json:"allow_reconnect"`
		DisconnectTimeoutSec int    `json:"disconnect_timeout_seconds"`
		LeavePolicy          string `json:"leave_policy"` // future
		HostLeavePolicy      string `json:"host_leave_policy"`
	} `json:"room"`

	Game struct {
		DeckType        string `json:"deck_type"`
		InitialHandSize int    `json:"initial_hand_size"`
		InitialDir      string `json:"initial_direction"`
		FirstPlayerMode string `json:"first_player_mode"`
		WinMode         string `json:"win_mode"`
		TargetScore     int    `json:"target_score"`         // future (SCORE only)
		TurnTimeoutSec  int    `json:"turn_timeout_seconds"` // future
	} `json:"game"`

	Cards struct {
		EnableSkip        bool `json:"enable_skip"`
		EnableReverse     bool `json:"enable_reverse"`
		EnableDrawTwo     bool `json:"enable_draw_two"`
		EnableWild        bool `json:"enable_wild"`
		EnableDrawFour    bool `json:"enable_draw_four"`
		ChallengeDrawFour bool `json:"challenge_draw_four"`
		AllowActionFinish bool `json:"allow_action_finish"`
	} `json:"cards"`

	HouseRules struct {
		JumpIn             bool `json:"jump_in"`              // future
		MultiCardPlay      bool `json:"multi_card_play"`      // future
		DrawUntilPlayable  bool `json:"draw_until_playable"`  // future
		ForcePlay          bool `json:"force_play"`           // future
		AllowVoluntaryPass bool `json:"allow_voluntary_pass"` // future
		StackDrawTwo       bool `json:"stack_draw_two"`       // future
		StackDrawFour      bool `json:"stack_draw_four"`      // future
		MixedStacking      bool `json:"mixed_stacking"`       // future
		SevenZeroRule      bool `json:"seven_zero_rule"`      // future
		WildSwapHands      bool `json:"wild_swap_hands"`      // future
		AutoUNO            bool `json:"auto_uno"`
		UNOPenaltyCards    int  `json:"uno_penalty_cards"` // future (manual UNO only)
	} `json:"house_rules"`
}

// DefaultConfig returns the v2 classic (CLASSIC_108) defaults (rule.md §0.2).
func DefaultConfig() Config {
	c := Config{SchemaVersion: 1}
	c.Room.MaxPlayers = 4
	c.Room.RequireAllReady = true
	c.Room.AllowReconnect = true
	c.Room.DisconnectTimeoutSec = 60 // v2 resolution 0.3-A2
	c.Room.LeavePolicy = LeavePause
	c.Room.HostLeavePolicy = HostCloseRoom
	c.Game.DeckType = DeckAction140
	c.Game.InitialHandSize = 7
	c.Game.InitialDir = DirClockwise
	c.Game.FirstPlayerMode = FirstAfterDealer
	c.Game.WinMode = WinSingleRound
	c.Game.TargetScore = 500
	c.Cards.EnableSkip = true
	c.Cards.EnableReverse = true
	c.Cards.EnableDrawTwo = true
	c.Cards.EnableWild = true
	c.Cards.EnableDrawFour = true
	c.Cards.ChallengeDrawFour = true
	c.Cards.AllowActionFinish = true
	c.HouseRules.AutoUNO = true
	c.HouseRules.UNOPenaltyCards = 2
	return c
}

// ParseConfig decodes a Config from JSON (rule.md §13), overlaying the provided
// fields on top of the defaults so that a partial document is valid.
func ParseConfig(b []byte) (Config, error) {
	d := DefaultConfig()
	if err := json.Unmarshal(b, &d); err != nil {
		return d, err
	}
	return d, nil
}

// Marshal returns the JSON encoding of the config.
func (c Config) Marshal() ([]byte, error) { return json.Marshal(c) }

// Validate checks ranges, dependencies, and conflicts (rule.md §0.4 + §12).
// It is the single authoritative config gate; the room rejects invalid configs.
// stackingDrawTwo reports whether +2 stacking is enabled: explicitly via
// stack_draw_two, or implied by mixed_stacking (rule.md R19/R21).
func (c Config) stackingDrawTwo() bool {
	return c.HouseRules.StackDrawTwo || c.HouseRules.MixedStacking
}

// stackingDrawFour reports whether +4 stacking is enabled: explicitly via
// stack_draw_four, or implied by mixed_stacking (rule.md R20/R21).
func (c Config) stackingDrawFour() bool {
	return c.HouseRules.StackDrawFour || c.HouseRules.MixedStacking
}

func (c Config) Validate() error {
	if c.Room.MaxPlayers < 2 || c.Room.MaxPlayers > 4 {
		return fmt.Errorf("max_players must be 2-4 (v2 scope), got %d", c.Room.MaxPlayers)
	}
	if c.Game.DeckType != DeckClassic108 && c.Game.DeckType != DeckAction140 {
		return fmt.Errorf("deck_type: v2 only supports %s, got %q", DeckClassic108, c.Game.DeckType)
	}
	switch c.Game.InitialHandSize {
	case 5, 7, 10:
	default:
		return fmt.Errorf("initial_hand_size must be 5, 7 or 10, got %d", c.Game.InitialHandSize)
	}
	switch c.Game.InitialDir {
	case DirClockwise, DirCounterClockwise, DirRandom:
	default:
		return fmt.Errorf("initial_direction must be one of %q, got %q", []string{DirClockwise, DirCounterClockwise, DirRandom}, c.Game.InitialDir)
	}
	switch c.Game.FirstPlayerMode {
	case FirstAfterDealer, FirstRandom, FirstHost:
	default:
		return fmt.Errorf("first_player_mode must be one of %q", []string{FirstAfterDealer, FirstRandom, FirstHost})
	}
	switch c.Game.WinMode {
	case WinSingleRound, WinScore:
	default:
		return fmt.Errorf("win_mode must be one of %q", []string{WinSingleRound, WinScore})
	}
	if c.Game.WinMode == WinScore && (c.Game.TargetScore < 100 || c.Game.TargetScore > 2000) {
		return fmt.Errorf("target_score must be 100-2000 for SCORE mode, got %d", c.Game.TargetScore)
	}
	if c.Room.DisconnectTimeoutSec < 0 {
		return fmt.Errorf("disconnect_timeout_seconds must be >= 0")
	}

	// Conflicts from rule.md §12, restricted to the implemented surface.
	// +4 stacking and the +4 challenge are now combinable under the combined
	// stacking+challenge rule (rule.md R22): the target of a pile whose most
	// recent card is a +4 may challenge that +4. So no conflict is rejected here.
	used := c.futureFieldsEnabled()
	if len(used) > 0 {
		sort.Strings(used)
		return fmt.Errorf("config fields not supported in v2: %s", strings.Join(used, ", "))
	}
	return nil
}

// futureFieldsEnabled reports which future-only config fields are turned on.
// v2 rejects these rather than silently ignoring them (rule.md §13 note).
func (c Config) futureFieldsEnabled() []string {
	var on []string
	if c.Room.AllowSpectators {
		on = append(on, "allow_spectators")
	}
	if c.Room.LeavePolicy != "" && c.Room.LeavePolicy != "PAUSE" {
		on = append(on, "leave_policy="+c.Room.LeavePolicy)
	}
	if c.Room.HostLeavePolicy != "" && c.Room.HostLeavePolicy != HostCloseRoom {
		on = append(on, "host_leave_policy="+c.Room.HostLeavePolicy)
	}
	if c.Game.TurnTimeoutSec != 0 {
		on = append(on, "turn_timeout_seconds")
	}
	if c.HouseRules.JumpIn {
		on = append(on, "jump_in")
	}
	if c.HouseRules.MultiCardPlay {
		on = append(on, "multi_card_play")
	}
	if c.HouseRules.DrawUntilPlayable {
		on = append(on, "draw_until_playable")
	}
	if c.HouseRules.ForcePlay {
		on = append(on, "force_play")
	}
	if c.HouseRules.AllowVoluntaryPass {
		on = append(on, "allow_voluntary_pass")
	}
	if c.HouseRules.SevenZeroRule {
		on = append(on, "seven_zero_rule")
	}
	if c.HouseRules.WildSwapHands {
		on = append(on, "wild_swap_hands")
	}
	if c.HouseRules.UNOPenaltyCards != 0 && c.HouseRules.UNOPenaltyCards != 2 {
		on = append(on, "uno_penalty_cards")
	}
	if !c.HouseRules.AutoUNO {
		on = append(on, "auto_uno=false(manual UNO is future)")
	}
	return on
}
