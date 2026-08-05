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
// Late subscribers see the full log from offset 0. By default the
// queue preserves every appended entry indefinitely. NewBoundedQueue
// caps how many entries are retained, evicting the oldest to bound
// memory for long-lived logs (a token stream, say) — see the bounded
// retention note below.
//
// # Bounded retention (NewBoundedQueue)
//
// A bounded queue retains at most maxRetain entries. When a new
// Append would exceed that window the oldest retained entry is
// evicted. Eviction preserves offsets: offsets stay absolute and
// monotonically increasing (they are never renumbered), so a caller
// holding an offset from before an eviction still names the same
// logical position. Two things follow from that:
//
//   - ReadFrom / ReadAt address payloads, so they only serve the
//     retained window. ReadFrom(offset) with an offset below the
//     retained head degrades gracefully: it clamps up to the oldest
//     retained entry and returns that suffix (never panics, never
//     renumbers). ReadAt of an evicted offset returns ok=false.
//   - Barrier resolution (Resolve / AwaitResolution / Resolution)
//     addresses positions, not payloads, so it stays valid for any
//     offset that was ever appended — including an evicted one. A
//     pending barrier therefore survives eviction pressure: its
//     resolution metadata is kept independently of the payload
//     window. (Barriers are expected to resolve near the head, so
//     they are rarely evicted in practice; the guarantee holds either
//     way.)
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
	mu sync.RWMutex

	// events is the backing store. The retained window is events[head:];
	// events[:head] are evicted slots kept only to amortize compaction and
	// are zeroed on eviction so their payloads are collected. The absolute
	// offset of the oldest retained entry (events[head]) is base, so the
	// absolute offset of events[head+i] is base+i and the total number of
	// entries ever appended is base + (len(events) - head). For an unbounded
	// queue head and base stay 0 and this reduces to len(events).
	events []T
	head   int // index in events of the oldest retained entry
	base   int // absolute offset of events[head] (== entries evicted so far)

	// maxRetain caps the retained window (NewBoundedQueue). 0 means unbounded:
	// nothing is ever evicted.
	maxRetain int

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

// NewQueue returns an empty, unbounded Queue ready to receive events.
func NewQueue[T any]() *Queue[T] {
	return &Queue[T]{
		resolutions:    map[int]any{},
		resolveSignals: map[int]chan struct{}{},
	}
}

// NewBoundedQueue returns an empty Queue that retains at most maxRetain
// entries, evicting the oldest as newer ones arrive. It behaves exactly like
// NewQueue for the first maxRetain entries; past that it caps memory while
// keeping offsets absolute and monotonic (see the "Bounded retention" note on
// Queue). maxRetain <= 0 is treated as unbounded (identical to NewQueue).
func NewBoundedQueue[T any](maxRetain int) *Queue[T] {
	if maxRetain < 0 {
		maxRetain = 0
	}
	q := NewQueue[T]()
	q.maxRetain = maxRetain
	return q
}

// totalLocked returns the number of entries ever appended (== the next offset
// Append will assign). Caller holds q.mu (read or write).
func (q *Queue[T]) totalLocked() int {
	return q.base + (len(q.events) - q.head)
}

// Append commits an event to the log, returns its offset, and
// signals every subscriber's notify channel (non-blocking).
// Safe to call concurrently with Read / Len / Resolve.
func (q *Queue[T]) Append(e T) int {
	q.mu.Lock()
	offset := q.totalLocked()
	q.events = append(q.events, e)
	if q.maxRetain > 0 {
		q.evictLocked()
	}
	// Snapshot subscribers under lock; signal outside.
	subs := make([]*Subscription, len(q.subs))
	copy(subs, q.subs)
	q.mu.Unlock()
	for _, s := range subs {
		s.signal()
	}
	return offset
}

// evictLocked drops oldest entries until the retained window fits maxRetain.
// Since Append adds one entry at a time this evicts at most one per call.
// Eviction is amortized O(1): the head pointer advances in O(1), the evicted
// slot is zeroed so its payload is collected, and the backing array is
// compacted only once the dead prefix grows to a full window, which bounds the
// backing array at ~2*maxRetain. Offsets are untouched (base rises with head),
// so they stay absolute and monotonic. Caller holds q.mu; maxRetain > 0.
func (q *Queue[T]) evictLocked() {
	var zero T
	for len(q.events)-q.head > q.maxRetain {
		q.events[q.head] = zero // free the evicted payload
		q.head++
		q.base++
	}
	if q.head >= q.maxRetain {
		n := copy(q.events, q.events[q.head:])
		for i := n; i < len(q.events); i++ {
			q.events[i] = zero
		}
		q.events = q.events[:n]
		q.head = 0
	}
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
	total := q.totalLocked()
	// Below the retained head (evicted, or negative): clamp up to the oldest
	// retained offset and return that suffix. This is the graceful-degradation
	// contract for a bounded queue — never panic, never renumber.
	if offset < q.base {
		offset = q.base
	}
	if offset >= total {
		return nil, total
	}
	lo := q.head + (offset - q.base)
	out := make([]T, total-offset)
	copy(out, q.events[lo:])
	return out, total
}

// ReadAt returns the event at a specific offset, or the zero
// value + false if out of range.
func (q *Queue[T]) ReadAt(offset int) (T, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	var zero T
	// Only the retained window has payloads; an evicted (offset < base) or
	// future offset is not available.
	if offset < q.base || offset >= q.totalLocked() {
		return zero, false
	}
	return q.events[q.head+(offset-q.base)], true
}

// Len returns the number of events ever appended (== the next offset). For a
// bounded queue this keeps counting past evictions — it is the offset space,
// not the retained count — so offsets stay absolute and monotonic.
func (q *Queue[T]) Len() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.totalLocked()
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
	// A barrier is a position, not a payload: any offset that was ever
	// appended is resolvable, including one whose payload has since been
	// evicted (resolutions live independently of the retained window).
	if offset < 0 || offset >= q.totalLocked() {
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
	// Positions, not payloads: an evicted offset is still awaitable (see
	// Resolve), so a barrier pending across eviction pressure still unblocks.
	if offset < 0 || offset >= q.totalLocked() {
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
