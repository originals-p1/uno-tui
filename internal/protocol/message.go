package protocol

import (
	"encoding/json"
	"fmt"
	"strings"

	"terminal-uno/internal/game"
)

// ProtocolVersion is the wire protocol version carried on every message.
const ProtocolVersion = 1

// Stable protocol-level error codes, distinct from game rule codes.
const (
	CodeInvalidMessage     = "INVALID_MESSAGE"
	CodeProtocolMismatch   = "PROTOCOL_MISMATCH"
	CodeMissingRequestID   = "MISSING_REQUEST_ID"
	CodeMissingExpectedRev = "MISSING_EXPECTED_REVISION"
)

// ProtocolError is a stable, typed protocol failure.
type ProtocolError struct {
	Code    string
	Message string
}

func (e *ProtocolError) Error() string { return e.Code + ": " + e.Message }

func newProto(code, format string, args ...interface{}) *ProtocolError {
	return &ProtocolError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Message type names (readme §6.2 table).
const (
	TypeHello         = "hello"
	TypeWelcome       = "welcome"
	TypeReady         = "ready"
	TypeStartGame     = "start_game"
	TypePlayCard      = "play_card"
	TypeDrawCard      = "draw_card"
	TypePlayDrawnCard = "play_drawn_card"
	TypePassDrawnCard = "pass_drawn_card"
	TypeLeave         = "leave"
	TypeStateSnapshot = "state_snapshot"
	TypeGameEvent     = "game_event"
	TypeError         = "error"
	TypeResponse      = "response"
	TypePing          = "ping"
	TypePong          = "pong"
)

// ChosenColor is the color chosen for a wild card (one of red/yellow/green/blue).
type ChosenColor = string

// --- typed payloads ---

// Hello is the first client->server message (readme §2.1/§6.2).
type Hello struct {
	PlayerName         string `json:"player_name"`
	RoomKey            string `json:"room_key"`
	ReconnectToken     string `json:"reconnect_token,omitempty"`
	HostBootstrapToken string `json:"host_bootstrap_token,omitempty"`
}

// Welcome is the server's session greeting carrying the private session token.
type Welcome struct {
	PlayerID  string         `json:"player_id"`
	SessionID string         `json:"session_id"`
	Token     string         `json:"session_token"`
	Snapshot  *game.GameView `json:"snapshot,omitempty"`
}

// Ready is the ready/pass toggle request.
type Ready struct {
	Ready bool `json:"ready"`
}

// StartGame is the host's start request (no payload).
type StartGame struct{}

// PlayCard is a play_card request.
type PlayCard struct {
	CardID      string  `json:"card_id"`
	ChosenColor *string `json:"chosen_color,omitempty"`
}

// PlayDrawnCard is a play_drawn_card request.
type PlayDrawnCard struct {
	ChosenColor *string `json:"chosen_color,omitempty"`
}

// ResponseAck is a success response payload.
type ResponseAck struct {
	Accepted bool `json:"accepted"`
}

// ErrorBody is an error response payload.
type ErrorBody struct {
	Code     string          `json:"code"`
	Message  string          `json:"message"`
	Snapshot json.RawMessage `json:"snapshot,omitempty"`
}

// Empty is used for actions with no payload.
type Empty struct{}

// Message is the wire envelope shared by all frames.
type Message struct {
	ProtocolVersion  int             `json:"protocol_version"`
	Type             string          `json:"type"`
	RequestID        string          `json:"request_id,omitempty"`
	ExpectedRevision *uint64         `json:"expected_revision,omitempty"`
	EventID          uint64          `json:"event_id,omitempty"`
	RoomRevision     uint64          `json:"room_revision,omitempty"`
	Payload          json.RawMessage `json:"payload,omitempty"`
}

// Decode parses a frame body, validates the protocol version and required
// fields, and decodes the payload into the matching typed struct. It returns
// the envelope plus the typed payload.
func Decode(raw []byte) (*Message, interface{}, error) {
	var m Message
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil, newProto(CodeInvalidMessage, "malformed JSON: %v", err)
	}
	if m.ProtocolVersion != ProtocolVersion {
		return nil, nil, newProto(CodeProtocolMismatch, "protocol version %d unsupported (want %d)", m.ProtocolVersion, ProtocolVersion)
	}

	switch m.Type {
	case TypeHello:
		p, err := decodeObject(m.Payload, func(p *Hello) error {
			if err := requireRequestID(&m); err != nil {
				return err
			}
			if p.PlayerName == "" {
				return newProto(CodeInvalidMessage, "hello: player_name required")
			}
			return nil
		})
		return &m, p, err
	case TypeReady:
		if err := requireRequestAndRev(&m); err != nil {
			return nil, nil, err
		}
		p, err := decodeObject(m.Payload, func(p *Ready) error { return nil })
		return &m, p, err
	case TypeStartGame, TypeDrawCard, TypePassDrawnCard, TypeLeave:
		if err := requireRequestAndRev(&m); err != nil {
			return nil, nil, err
		}
		if err := requireEmpty(m.Payload); err != nil {
			return nil, nil, err
		}
		return &m, &Empty{}, nil
	case TypePlayCard:
		if err := requireRequestAndRev(&m); err != nil {
			return nil, nil, err
		}
		p, err := decodeObject(m.Payload, func(p *PlayCard) error {
			if p.CardID == "" {
				return newProto(CodeInvalidMessage, "play_card: card_id required")
			}
			return nil
		})
		return &m, p, err
	case TypePlayDrawnCard:
		if err := requireRequestAndRev(&m); err != nil {
			return nil, nil, err
		}
		p, err := decodeObject(m.Payload, func(p *PlayDrawnCard) error { return nil })
		return &m, p, err
	case TypePing, TypePong:
		if err := requireEmpty(m.Payload); err != nil {
			return nil, nil, err
		}
		return &m, &Empty{}, nil
	default:
		return nil, nil, newProto(CodeInvalidMessage, "unknown message type %q", m.Type)
	}
}

func requireRequestID(m *Message) error {
	if m.RequestID == "" {
		return newProto(CodeMissingRequestID, "%s: request_id required", m.Type)
	}
	return nil
}

func requireRequestAndRev(m *Message) error {
	if err := requireRequestID(m); err != nil {
		return err
	}
	if m.ExpectedRevision == nil {
		return newProto(CodeMissingExpectedRev, "%s: expected_revision required", m.Type)
	}
	return nil
}

func requireEmpty(raw json.RawMessage) error {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" || s == "{}" {
		return nil
	}
	return newProto(CodeInvalidMessage, "unexpected payload")
}

// decodeObject unmarshals raw into a fresh T and runs an optional validator.
func decodeObject[T any](raw json.RawMessage, validate func(*T) error) (*T, error) {
	p := new(T)
	if len(strings.TrimSpace(string(raw))) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, p); err != nil {
			return nil, newProto(CodeInvalidMessage, "invalid payload: %v", err)
		}
	}
	if validate != nil {
		if err := validate(p); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// --- builders for server->client messages ---

func newBase(typ string) *Message {
	return &Message{ProtocolVersion: ProtocolVersion, Type: typ}
}

func EncodeResponse(requestID string, roomRevision uint64, accepted bool) (*Message, error) {
	p, err := json.Marshal(ResponseAck{Accepted: accepted})
	if err != nil {
		return nil, err
	}
	m := newBase(TypeResponse)
	m.RequestID = requestID
	m.RoomRevision = roomRevision
	m.Payload = p
	return m, nil
}

func EncodeError(requestID string, roomRevision uint64, code, message string, snapshot interface{}) (*Message, error) {
	var snap json.RawMessage
	if snapshot != nil {
		b, err := json.Marshal(snapshot)
		if err != nil {
			return nil, err
		}
		snap = b
	}
	p, err := json.Marshal(ErrorBody{Code: code, Message: message, Snapshot: snap})
	if err != nil {
		return nil, err
	}
	m := newBase(TypeError)
	m.RequestID = requestID
	m.RoomRevision = roomRevision
	m.Payload = p
	return m, nil
}

func EncodeEvent(eventID, roomRevision uint64, typ string, payload interface{}) (*Message, error) {
	var raw json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		raw = b
	}
	m := newBase(TypeGameEvent)
	m.EventID = eventID
	m.RoomRevision = roomRevision
	// The event's inner type is carried in the payload envelope.
	type ev struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload,omitempty"`
	}
	inner, err := json.Marshal(ev{Type: typ, Payload: raw})
	if err != nil {
		return nil, err
	}
	m.Payload = inner
	return m, nil
}

func EncodeStateSnapshot(roomRevision uint64, view *game.GameView) (*Message, error) {
	p, err := json.Marshal(view)
	if err != nil {
		return nil, err
	}
	m := newBase(TypeStateSnapshot)
	m.RoomRevision = roomRevision
	m.Payload = p
	return m, nil
}

// Marshal serializes a Message into JSON bytes (the frame body).
func Marshal(m *Message) ([]byte, error) { return json.Marshal(m) }

// ParseGameEvent unwraps a game_event payload into its inner structure.
func ParseGameEvent(m *Message) (typ string, payload json.RawMessage, err error) {
	var inner struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload,omitempty"`
	}
	if err = json.Unmarshal(m.Payload, &inner); err != nil {
		return "", nil, err
	}
	return inner.Type, inner.Payload, nil
}
