package gocurrent

import (
	"errors"
	"sync"
)

// ErrAlreadyResolved is returned by Resolve when the entry at the
// given offset has already been resolved by a prior caller.
// Multi-resolver scenarios race here; first writer wins.
var ErrAlreadyResolved = errors.New("gocurrent: entry already resolved")

// ErrOffsetOutOfRange is returned by Resolve when offset is
// outside the queue's range.
var ErrOffsetOutOfRange = errors.New("gocurrent: offset out of range")

// Queue is an append-only event log with offset-based reads,
// per-subscriber wake-up channels, and an optional per-entry
// "barrier" resolution mechanism. Multi-producer, N-consumer.
//
// Consumers track their own read offset. Subscribe() returns a
// *Subscription whose Notify channel signals "there's new
// content past your offset" — drain it, then call ReadFrom to
// catch up. Per-subscription capacity-1 buffer coalesces burst
// notifies into single wake-ups (one drain per loop).
//
// Late subscribers see the full log from offset 0. The queue
// preserves every appended entry indefinitely (no eviction in
// this base type; bounded variants can layer on top).
//
// Barrier resolution: any entry can be tagged with a resolution
// value via Resolve(offset, data). Other callers can
// AwaitResolution(offset) to block until that happens. The
// queue is data-agnostic — it doesn't know which entries are
// "barriers"; the caller decides by choosing whether to await.
// The producer-side convenience AppendBarrier appends an entry
// and blocks waiting for its resolution in one call.
//
// Usage:
//
//	q := gocurrent.NewQueue[MyEvent]()
//	sub := q.Subscribe()
//	defer sub.Close()
//	go func() {
//	    for {
//	        <-sub.Notify()
//	        events, _ := q.ReadFrom(myOffset)
//	        for _, e := range events { handle(e) }
//	    }
//	}()
//	// Producer:
//	offset := q.Append(myEvent)       // async, returns offset
//	res := q.AppendBarrier(syncEvent) // blocks until resolved
type Queue[T any] struct {
	mu     sync.RWMutex
	events []T

	// subs is the live subscription list. Append fans out a wake-up
	// token to each subscription's channel (non-blocking). Snapshot
	// taken under mu and iterated outside the lock to avoid holding
	// the queue lock during channel sends.
	subs []*Subscription

	// resolutions maps offset → resolution data. Lazily populated
	// on Resolve. Separate from events so the log slice stays
	// effectively immutable (resolution metadata lives outside
	// the entries themselves).
	resolutions map[int]any

	// resolveSignals maps offset → channel closed on Resolve.
	// AwaitResolution waiters block on these; close broadcasts to
	// all waiters for the same offset.
	resolveSignals map[int]chan struct{}
}

// NewQueue returns an empty Queue ready to receive events.
func NewQueue[T any]() *Queue[T] {
	return &Queue[T]{
		resolutions:    map[int]any{},
		resolveSignals: map[int]chan struct{}{},
	}
}

// Append commits an event to the log, returns its offset, and
// signals every subscriber's notify channel (non-blocking).
// Safe to call concurrently with Read / Len / Resolve.
func (q *Queue[T]) Append(e T) int {
	q.mu.Lock()
	offset := len(q.events)
	q.events = append(q.events, e)
	// Snapshot subscribers under lock; signal outside.
	subs := make([]*Subscription, len(q.subs))
	copy(subs, q.subs)
	q.mu.Unlock()
	for _, s := range subs {
		s.signal()
	}
	return offset
}

// AppendBarrier appends e and blocks until Resolve has been called
// for the new entry's offset. Returns the resolution data.
// Convenience for the producer-side sync pattern; equivalent to
// q.AwaitResolution(q.Append(e)) but reads as a single
// "barrier append" intent.
//
// Multi-resolver: first writer wins; AppendBarrier returns
// whichever resolution data was committed.
func (q *Queue[T]) AppendBarrier(e T) any {
	return q.AwaitResolution(q.Append(e))
}

// ReadFrom returns the events at indices [offset, end) and the
// new offset (== Len() at call time). Callers track the returned
// offset and pass it as `offset` on the next call to drain only
// what's new.
//
// Returned slice is a copy of the queue's internal slice header
// for the requested range; events themselves are read-only
// values from the caller's perspective.
func (q *Queue[T]) ReadFrom(offset int) (events []T, newOffset int) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if offset < 0 {
		offset = 0
	}
	if offset >= len(q.events) {
		return nil, len(q.events)
	}
	out := make([]T, len(q.events)-offset)
	copy(out, q.events[offset:])
	return out, len(q.events)
}

// ReadAt returns the event at a specific offset, or the zero
// value + false if out of range.
func (q *Queue[T]) ReadAt(offset int) (T, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	var zero T
	if offset < 0 || offset >= len(q.events) {
		return zero, false
	}
	return q.events[offset], true
}

// Len returns the number of events in the queue.
func (q *Queue[T]) Len() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.events)
}

// Subscribe registers a new consumer and returns a *Subscription
// with its own notify channel. Each Append fans out a wake-up
// token to every active subscription (non-blocking, capacity-1
// per sub → coalesced per consumer).
//
// Each subscription is independent: late subscribers don't miss
// past events (they read from offset 0); each subscriber drains
// at its own pace.
func (q *Queue[T]) Subscribe() *Subscription {
	s := &Subscription{
		ch: make(chan struct{}, 1),
		q:  any(q),
		unsub: func(target *Subscription) {
			q.mu.Lock()
			defer q.mu.Unlock()
			for i, ss := range q.subs {
				if ss == target {
					q.subs = append(q.subs[:i], q.subs[i+1:]...)
					return
				}
			}
		},
	}
	q.mu.Lock()
	q.subs = append(q.subs, s)
	q.mu.Unlock()
	return s
}

// Resolve attaches resolution data to the entry at offset.
// First-writer-wins: returns ErrAlreadyResolved if a prior call
// already resolved this offset. Returns ErrOffsetOutOfRange for
// invalid offsets.
//
// On success, wakes any AwaitResolution waiters for this offset
// and signals subscribers (resolution change is treated as
// "interesting" so subscribers can re-read and observe the new
// resolution).
//
// The queue is data-agnostic about which entries are "barriers."
// Resolve simply attaches a value to a position; callers that
// care about resolution (via AwaitResolution / Resolution)
// observe it. Other consumers just see the regular event in the
// log; whether they inspect resolution is their choice.
func (q *Queue[T]) Resolve(offset int, resolution any) error {
	q.mu.Lock()
	if offset < 0 || offset >= len(q.events) {
		q.mu.Unlock()
		return ErrOffsetOutOfRange
	}
	if _, exists := q.resolutions[offset]; exists {
		q.mu.Unlock()
		return ErrAlreadyResolved
	}
	q.resolutions[offset] = resolution
	ch, hasWaiters := q.resolveSignals[offset]
	if hasWaiters {
		delete(q.resolveSignals, offset)
	}
	subs := make([]*Subscription, len(q.subs))
	copy(subs, q.subs)
	q.mu.Unlock()
	if hasWaiters {
		close(ch)
	}
	for _, s := range subs {
		s.signal()
	}
	return nil
}

// AwaitResolution blocks until Resolve has been called for
// offset, then returns the resolution data. Returns nil
// immediately for out-of-range offsets. If the entry is already
// resolved when called, returns the data without blocking —
// replay paths that load a pre-resolved log won't deadlock.
//
// Multiple waiters on the same offset all wake when Resolve
// fires (broadcast via channel close).
func (q *Queue[T]) AwaitResolution(offset int) any {
	q.mu.Lock()
	if offset < 0 || offset >= len(q.events) {
		q.mu.Unlock()
		return nil
	}
	if r, ok := q.resolutions[offset]; ok {
		q.mu.Unlock()
		return r
	}
	ch, ok := q.resolveSignals[offset]
	if !ok {
		ch = make(chan struct{})
		q.resolveSignals[offset] = ch
	}
	q.mu.Unlock()

	<-ch

	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.resolutions[offset]
}

// Resolution returns (data, true) if offset has been resolved,
// or (nil, false) otherwise. Non-blocking peek. Useful for
// consumers that want to render "resolved" state in their UI
// without waiting on it.
func (q *Queue[T]) Resolution(offset int) (any, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	r, ok := q.resolutions[offset]
	return r, ok
}

// --- Subscription ---

// Subscription is a per-consumer wake-up channel on a Queue.
// Notify fires once per "burst" of Appends (coalesced via
// capacity-1 buffer). Call Close to unsubscribe when done.
type Subscription struct {
	ch    chan struct{}
	q     any                       // back-reference; opaque to avoid generic spread
	unsub func(target *Subscription) // bound closure that knows the typed queue
}

// Notify returns the wake-up channel. Each receive corresponds
// to "at least one new event was appended (or resolved) since
// the last receive." The consumer should then call the queue's
// ReadFrom to drain.
func (s *Subscription) Notify() <-chan struct{} {
	return s.ch
}

// Close removes this subscription from the queue's broadcast
// list. After Close, the notify channel may continue to hold a
// buffered token from a concurrent Append; reads return that
// stale token then block forever. Callers typically just stop
// reading after Close.
//
// Safe to call multiple times.
func (s *Subscription) Close() {
	if s.unsub != nil {
		s.unsub(s)
		s.unsub = nil
	}
}

// signal is the internal non-blocking notify. Capacity-1 buffer
// means a burst of signals collapses to one consumer wake-up.
func (s *Subscription) signal() {
	select {
	case s.ch <- struct{}{}:
	default:
	}
}
