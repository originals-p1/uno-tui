package server

import (
	"sync"
	"time"
)

// timerEntry tracks a scheduled After with its absolute deadline.
type timerEntry struct {
	due time.Time
	ch  chan time.Time
}

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers map[int]timerEntry
	nextID int
}

func newFakeClock(start time.Time) *fakeClock {
	return &fakeClock{now: start, timers: map[int]timerEntry{}}
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.nextID
	f.nextID++
	ch := make(chan time.Time, 1)
	f.timers[id] = timerEntry{due: f.now.Add(d), ch: ch}
	return ch
}

// Advance moves the clock forward and fires every timer whose deadline has
// passed, in deadline order.
func (f *fakeClock) Advance(d time.Duration) {
	f.mu.Lock()
	f.now = f.now.Add(d)
	due := []timerEntry{}
	for _, e := range f.timers {
		if !e.due.After(f.now) {
			due = append(due, e)
		}
	}
	f.mu.Unlock()
	now := f.Now()
	// Sort by due to fire in order.
	for i := 0; i < len(due); i++ {
		for j := i + 1; j < len(due); j++ {
			if due[j].due.Before(due[i].due) {
				due[i], due[j] = due[j], due[i]
			}
		}
	}
	for _, e := range due {
		select {
		case e.ch <- now:
		default:
		}
	}
}
