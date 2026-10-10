// Package server implements the room event loop, sessions, and the TCP
// server (readme §5, §6).
package server

import "time"

// Clock abstracts time for deterministic tests (readme §15: injected clock).
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// NewRealClock returns the production clock backed by time.Now.
func NewRealClock() Clock { return realClock{} }
