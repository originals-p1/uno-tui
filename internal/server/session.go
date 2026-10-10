package server

import (
	"crypto/rand"
	"encoding/base64"
)

// Session represents one authenticated client connection. The full auth
// lifecycle (tokens, reconnect) is completed in Task 6; this minimal form
// binds a session ID to a player seat and carries the idempotency cache.
type Session struct {
	ID       string
	PlayerID string

	// tokenHash stores the SHA-256 digest of the session token (never the raw
	// token). Populated once auth is complete.
	tokenHash []byte
}

// NewSessionID returns a cryptographically random session identifier.
func NewSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}
