package gocurrent

import (
	"sync"
	"time"
)

// IdleTimer implements a ref-counted idle timeout. The timer starts counting
// when all activity ceases (active count drops to zero) and pauses while any
// activity is in progress.
//
// This is the "activity-gated idle timeout" pattern used for session/connection
// idle cleanup — any service managing sessions, connections, or pooled resources
// with automatic expiry needs this exact primitive.
//
// Usage:
//
//	timer := NewIdleTimer(30*time.Second, func() { removeSession(id) })
//	timer.Acquire()       // request starts — pause idle timer
//	defer timer.Release() // request ends — restart timer if no more activity
//	timer.Stop()          // explicit shutdown — cancel timer permanently
//
// All methods are nil-safe: calling Acquire/Release/Stop on a nil *IdleTimer
// is a no-op. This enables callers to use *IdleTimer as a struct field without
// nil checks at every call site.
type IdleTimer struct {
	mu       sync.Mutex
	active   int
	timer    *time.Timer
	timeout  time.Duration
	onExpire func()
	stopped  bool
}

// NewIdleTimer creates an IdleTimer that calls onExpire when the timeout
// elapses with no activity. The timer starts immediately — call Acquire()
// to pause it.
//
// If timeout is 0, returns a no-op timer: Acquire/Release/Stop do nothing
// and onExpire is never called. This is the default for "no timeout configured."
func NewIdleTimer(timeout time.Duration, onExpire func()) *IdleTimer {
	if timeout <= 0 {
		return &IdleTimer{}
	}
	return &IdleTimer{
		timer:    time.AfterFunc(timeout, onExpire),
		timeout:  timeout,
		onExpire: onExpire,
	}
}

// Acquire marks the start of an activity (e.g., an in-flight request).
// Stops the idle timer so it cannot fire while the activity is in progress.
// Must be paired with a corresponding Release().
//
// Safe to call on a nil receiver (no-op).
func (t *IdleTimer) Acquire() {
	if t == nil || t.timeout <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active++
	if t.timer != nil {
		t.timer.Stop()
	}
}

// Release marks the end of an activity. If the active count drops to zero
// and the timer has not been stopped, the idle timer is restarted with the
// full timeout duration.
//
// Safe to call on a nil receiver (no-op).
func (t *IdleTimer) Release() {
	if t == nil || t.timeout <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.active--
	if t.active == 0 && !t.stopped && t.timer != nil {
		t.timer.Reset(t.timeout)
	}
}

// Stop permanently cancels the idle timer. After Stop(), onExpire will never
// be called. Safe to call multiple times. Safe to call on a nil receiver.
func (t *IdleTimer) Stop() {
	if t == nil || t.timeout <= 0 {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.stopped = true
	if t.timer != nil {
		t.timer.Stop()
	}
}
