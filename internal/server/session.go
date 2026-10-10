package server

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
)

// Session represents one authenticated client binding. A logical player may be
// bound to a fresh physical Session after a reconnect; reconnect tokens are
// stored as digests only (readme §9 Identity).
type Session struct {
	ID       string
	PlayerID string

	// tokenHash is the SHA-256 digest of the current session token.
	tokenHash []byte
}

// NewSessionID returns a cryptographically random session identifier.
func NewSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// GenerateToken returns a fresh high-entropy raw token plus its SHA-256 digest.
// The raw value is handed to the client exactly once; only the digest is kept.
func GenerateToken() (raw string, digest []byte) {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, hashToken(raw)
}

func hashToken(raw string) []byte {
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

// tokenMatches compares a provided raw token against a stored digest using a
// constant-time comparison (readme §9 Identity / §2.2 key comparison).
func tokenMatches(raw string, digest []byte) bool {
	if digest == nil || raw == "" {
		return false
	}
	got := hashToken(raw)
	return subtle.ConstantTimeCompare(got, digest) == 1
}
