package gocurrent

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Basic append / read / len ---

func TestQueueAppendReturnsOffset(t *testing.T) {
	q := NewQueue[string]()
	if o := q.Append("a"); o != 0 {
		t.Errorf("offset = %d, want 0", o)
	}
	if o := q.Append("b"); o != 1 {
		t.Errorf("offset = %d, want 1", o)
	}
}

func TestQueueReadFromDrainsIncrementally(t *testing.T) {
	q := NewQueue[string]()
	q.Append("a")
	first, off1 := q.ReadFrom(0)
	if len(first) != 1 || off1 != 1 {
		t.Fatalf("first drain: len=%d off=%d, want 1, 1", len(first), off1)
	}
	q.Append("b")
	q.Append("c")
	second, off2 := q.ReadFrom(off1)
	if len(second) != 2 || off2 != 3 {
		t.Errorf("second drain: len=%d off=%d, want 2, 3", len(second), off2)
	}
}

func TestQueueReadFromOutOfRangeReturnsEmpty(t *testing.T) {
	q := NewQueue[string]()
	q.Append("a")
	events, off := q.ReadFrom(99)
	if len(events) != 0 {
		t.Errorf("out-of-range returned %d events, want 0", len(events))
	}
	if off != 1 {
		t.Errorf("out-of-range offset = %d, want 1 (current Len)", off)
	}
}

func TestQueueReadAtBounds(t *testing.T) {
	q := NewQueue[string]()
	q.Append("hello")
	v, ok := q.ReadAt(0)
	if !ok || v != "hello" {
		t.Errorf("ReadAt(0) = (%q, %v), want (hello, true)", v, ok)
	}
	if _, ok := q.ReadAt(99); ok {
		t.Error("ReadAt(99) should be ok=false")
	}
}

// --- N-consumer subscribe / notify ---

func TestSubscribeIndependentWakeups(t *testing.T) {
	q := NewQueue[string]()
	subA := q.Subscribe()
	subB := q.Subscribe()
	defer subA.Close()
	defer subB.Close()
	q.Append("x")
	select {
	case <-subA.Notify():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subA did not wake on Append")
	}
	select {
	case <-subB.Notify():
	case <-time.After(100 * time.Millisecond):
		t.Fatal("subB did not wake on Append")
	}
}

func TestSubscribeBurstCoalesces(t *testing.T) {
	q := NewQueue[string]()
	sub := q.Subscribe()
	defer sub.Close()
	for i := 0; i < 5; i++ {
		q.Append("x")
	}
	<-sub.Notify()
	select {
	case <-sub.Notify():
		t.Error("5 appends should coalesce to one notify; got a second")
	case <-time.After(20 * time.Millisecond):
	}
}

func TestSubscribeCloseUnregisters(t *testing.T) {
	q := NewQueue[string]()
	sub := q.Subscribe()
	sub.Close()
	// After Close, q.subs is empty; new Appends shouldn't deadlock
	// trying to signal a closed/missing subscription.
	q.Append("x")
	if got := q.Len(); got != 1 {
		t.Errorf("Len = %d, want 1 (append should still succeed)", got)
	}
}

func TestLateSubscriberSeesAllPastEvents(t *testing.T) {
	q := NewQueue[string]()
	q.Append("a")
	q.Append("b")
	q.Append("c")
	sub := q.Subscribe()
	defer sub.Close()
	// Subscribe after the appends. Notify channel is empty (no
	// post-subscribe activity yet), but ReadFrom(0) returns all
	// past events.
	events, off := q.ReadFrom(0)
	if len(events) != 3 || off != 3 {
		t.Errorf("late subscriber drain: len=%d off=%d, want 3, 3", len(events), off)
	}
}

// --- Barrier resolution ---

func TestResolveFillsResolution(t *testing.T) {
	q := NewQueue[string]()
	offset := q.Append("barrier")
	if err := q.Resolve(offset, "done"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	r, ok := q.Resolution(offset)
	if !ok || r != "done" {
		t.Errorf("Resolution = (%v, %v), want (done, true)", r, ok)
	}
}

func TestResolveFirstWriterWins(t *testing.T) {
	q := NewQueue[string]()
	offset := q.Append("barrier")
	if err := q.Resolve(offset, "first"); err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	if err := q.Resolve(offset, "second"); !errors.Is(err, ErrAlreadyResolved) {
		t.Errorf("second Resolve should return ErrAlreadyResolved; got %v", err)
	}
	r, _ := q.Resolution(offset)
	if r != "first" {
		t.Errorf("first-writer-wins violated: %v", r)
	}
}

func TestResolveOutOfRange(t *testing.T) {
	q := NewQueue[string]()
	q.Append("x")
	if err := q.Resolve(99, "anything"); !errors.Is(err, ErrOffsetOutOfRange) {
		t.Errorf("out-of-range Resolve should return ErrOffsetOutOfRange; got %v", err)
	}
}

func TestAwaitResolutionBlocksUntilResolved(t *testing.T) {
	q := NewQueue[string]()
	offset := q.Append("barrier")
	got := make(chan any, 1)
	go func() { got <- q.AwaitResolution(offset) }()

	select {
	case <-got:
		t.Fatal("AwaitResolution returned before Resolve")
	case <-time.After(30 * time.Millisecond):
	}

	_ = q.Resolve(offset, "value")
	select {
	case r := <-got:
		if r != "value" {
			t.Errorf("AwaitResolution = %v, want value", r)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("AwaitResolution did not return after Resolve")
	}
}

func TestAwaitResolutionReturnsImmediatelyIfAlreadyResolved(t *testing.T) {
	q := NewQueue[string]()
	offset := q.Append("barrier")
	_ = q.Resolve(offset, "value")
	done := make(chan any, 1)
	go func() { done <- q.AwaitResolution(offset) }()
	select {
	case r := <-done:
		if r != "value" {
			t.Errorf("got %v, want value", r)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("AwaitResolution blocked on already-resolved entry")
	}
}

func TestAwaitResolutionNilForOutOfRange(t *testing.T) {
	q := NewQueue[string]()
	if r := q.AwaitResolution(99); r != nil {
		t.Errorf("out-of-range AwaitResolution = %v, want nil", r)
	}
}

func TestAwaitResolutionBroadcastsToMultipleWaiters(t *testing.T) {
	q := NewQueue[string]()
	offset := q.Append("barrier")
	const n = 5
	results := make(chan any, n)
	for i := 0; i < n; i++ {
		go func() { results <- q.AwaitResolution(offset) }()
	}
	// Give them time to all block on the wait channel.
	time.Sleep(20 * time.Millisecond)
	_ = q.Resolve(offset, "shared")
	for i := 0; i < n; i++ {
		select {
		case r := <-results:
			if r != "shared" {
				t.Errorf("waiter %d got %v, want shared", i, r)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatalf("waiter %d never woke", i)
		}
	}
}

// --- AppendBarrier ---

func TestAppendBarrierBlocksUntilResolved(t *testing.T) {
	q := NewQueue[string]()
	got := make(chan any, 1)
	go func() { got <- q.AppendBarrier("barrier") }()

	// Give producer time to Append + start awaiting.
	time.Sleep(20 * time.Millisecond)
	if q.Len() != 1 {
		t.Fatalf("AppendBarrier should have appended; Len = %d", q.Len())
	}
	select {
	case <-got:
		t.Fatal("AppendBarrier returned before Resolve")
	default:
	}

	_ = q.Resolve(0, "resolution")
	select {
	case r := <-got:
		if r != "resolution" {
			t.Errorf("AppendBarrier = %v, want resolution", r)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("AppendBarrier did not return after Resolve")
	}
}

// --- Concurrency stress ---

func TestConcurrentResolveExactlyOneWinner(t *testing.T) {
	q := NewQueue[string]()
	offset := q.Append("barrier")
	const n = 50
	var successes atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := q.Resolve(offset, "winner"); err == nil {
				successes.Add(1)
			} else if !errors.Is(err, ErrAlreadyResolved) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := successes.Load(); got != 1 {
		t.Errorf("successes = %d, want exactly 1", got)
	}
}

func TestConcurrentAppendsLenStable(t *testing.T) {
	q := NewQueue[string]()
	const n = 100
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q.Append("x")
		}()
	}
	wg.Wait()
	if got := q.Len(); got != n {
		t.Errorf("Len = %d after %d concurrent appends, want %d", got, n, n)
	}
}

// --- Bounded retention (NewBoundedQueue) ---

// readErrs collects the string values in the retained window from offset 0.
func retained(q *Queue[string]) []string {
	evs, _ := q.ReadFrom(0)
	return evs
}

func TestBoundedQueueEvictsOldest(t *testing.T) {
	q := NewBoundedQueue[string](3)
	for _, s := range []string{"a", "b", "c", "d", "e"} {
		q.Append(s)
	}
	// Five appended, window of three: the last three survive.
	got := retained(q)
	if len(got) != 3 || got[0] != "c" || got[1] != "d" || got[2] != "e" {
		t.Fatalf("retained window = %v, want [c d e]", got)
	}
	// Len is the offset space (total ever appended), not the retained count.
	if l := q.Len(); l != 5 {
		t.Errorf("Len = %d, want 5 (absolute offset space)", l)
	}
}

func TestBoundedQueueOffsetsMonotonicAcrossEviction(t *testing.T) {
	q := NewBoundedQueue[string](4)
	const n = 100
	for i := 0; i < n; i++ {
		if off := q.Append("x"); off != i {
			t.Fatalf("Append #%d returned offset %d; offsets must stay absolute and monotonic", i, off)
		}
	}
	// ReadFrom advances to the absolute end, not the retained count.
	_, newOff := q.ReadFrom(q.Len() - 4)
	if newOff != n {
		t.Errorf("newOffset = %d, want %d", newOff, n)
	}
}

func TestBoundedQueueReadFromEvictedDegrades(t *testing.T) {
	q := NewBoundedQueue[string](3)
	for _, s := range []string{"a", "b", "c", "d", "e"} {
		q.Append(s) // window now holds offsets 2,3,4 == c,d,e
	}
	// An evicted offset does not panic or renumber: it clamps up to the oldest
	// retained entry and returns that suffix.
	evs, newOff := q.ReadFrom(0)
	if len(evs) != 3 || evs[0] != "c" {
		t.Fatalf("ReadFrom(evicted) = %v, want the retained suffix [c d e]", evs)
	}
	if newOff != 5 {
		t.Errorf("newOffset = %d, want 5", newOff)
	}
	// A live mid-window offset reads only what is new past it.
	tail, _ := q.ReadFrom(4)
	if len(tail) != 1 || tail[0] != "e" {
		t.Fatalf("ReadFrom(4) = %v, want [e]", tail)
	}
	// A negative offset also clamps to the retained head (no panic).
	if evs, _ := q.ReadFrom(-1); len(evs) != 3 {
		t.Fatalf("ReadFrom(-1) = %v, want the full retained window", evs)
	}
}

func TestBoundedQueueReadAtWindow(t *testing.T) {
	q := NewBoundedQueue[string](3)
	for _, s := range []string{"a", "b", "c", "d", "e"} {
		q.Append(s) // retained offsets 2,3,4
	}
	if _, ok := q.ReadAt(0); ok {
		t.Error("ReadAt(evicted offset 0) should be ok=false")
	}
	if v, ok := q.ReadAt(2); !ok || v != "c" {
		t.Errorf("ReadAt(2) = (%q, %v), want (c, true)", v, ok)
	}
	if v, ok := q.ReadAt(4); !ok || v != "e" {
		t.Errorf("ReadAt(4) = (%q, %v), want (e, true)", v, ok)
	}
	if _, ok := q.ReadAt(5); ok {
		t.Error("ReadAt(future offset 5) should be ok=false")
	}
}

// TestBoundedQueueBarrierSurvivesEviction pins the core invariant the mcpkit
// event log depends on: a barrier is resolvable by its offset even after that
// entry's payload has scrolled out of the retained window.
func TestBoundedQueueBarrierSurvivesEviction(t *testing.T) {
	q := NewBoundedQueue[string](2)
	off := q.Append("ask") // offset 0
	// Evict it out of the payload window.
	for i := 0; i < 5; i++ {
		q.Append("filler")
	}
	if _, ok := q.ReadAt(off); ok {
		t.Fatal("precondition: the ask's payload should have been evicted")
	}
	if err := q.Resolve(off, "answer"); err != nil {
		t.Fatalf("Resolve of an evicted-but-appended offset: %v", err)
	}
	if r := q.AwaitResolution(off); r != "answer" {
		t.Errorf("AwaitResolution(evicted) = %v, want answer", r)
	}
	// An offset that was never appended is still out of range.
	if err := q.Resolve(999, "x"); !errors.Is(err, ErrOffsetOutOfRange) {
		t.Errorf("Resolve(never-appended) = %v, want ErrOffsetOutOfRange", err)
	}
}

// TestBoundedQueuePendingBarrierSurvivesEvictionPressure is the concurrent
// form: a waiter blocks on an offset while a flood of appends evicts it, then a
// late Resolve still unblocks the waiter.
func TestBoundedQueuePendingBarrierSurvivesEvictionPressure(t *testing.T) {
	q := NewBoundedQueue[string](4)
	off := q.Append("ask") // offset 0

	got := make(chan any, 1)
	go func() { got <- q.AwaitResolution(off) }()

	// Hammer appends so offset 0's payload is long gone while the waiter blocks.
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q.Append("filler")
		}()
	}
	wg.Wait()

	select {
	case <-got:
		t.Fatal("AwaitResolution returned before Resolve")
	default:
	}
	if err := q.Resolve(off, "answer"); err != nil {
		t.Fatalf("Resolve under eviction pressure: %v", err)
	}
	select {
	case r := <-got:
		if r != "answer" {
			t.Errorf("AwaitResolution = %v, want answer", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pending barrier never unblocked after Resolve")
	}
}

// TestBoundedQueueSubscribeReplayRespectsWindow: a late subscriber replays only
// the retained window from offset 0, and still wakes on a live append.
func TestBoundedQueueSubscribeReplayRespectsWindow(t *testing.T) {
	q := NewBoundedQueue[string](2)
	q.Append("a")
	q.Append("b")
	q.Append("c") // evicts "a"; window == [b c]

	sub := q.Subscribe() // subscribes late
	defer sub.Close()

	evs, off := q.ReadFrom(0)
	if len(evs) != 2 || evs[0] != "b" || evs[1] != "c" {
		t.Fatalf("late replay = %v, want the retained window [b c]", evs)
	}

	q.Append("d")
	select {
	case <-sub.Notify():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("late subscriber was not notified of the live append")
	}
	live, _ := q.ReadFrom(off)
	if len(live) != 1 || live[0] != "d" {
		t.Fatalf("live drain = %v, want [d]", live)
	}
}

// TestBoundedQueueMemoryStaysBounded pins the whole point: the backing array
// does not grow without bound as entries stream in. It peeks internal state
// (an in-package test), asserting the backing store stays within a small
// constant factor of the window.
func TestBoundedQueueMemoryStaysBounded(t *testing.T) {
	const cap = 8
	q := NewBoundedQueue[string](cap)
	for i := 0; i < 10000; i++ {
		q.Append("x")
	}
	q.mu.RLock()
	backing := len(q.events)
	q.mu.RUnlock()
	if backing > 2*cap {
		t.Errorf("backing slice len = %d after 10000 appends, want <= %d (bounded)", backing, 2*cap)
	}
	if l := q.Len(); l != 10000 {
		t.Errorf("Len = %d, want 10000 (offsets keep counting)", l)
	}
}

func TestBoundedQueueZeroMeansUnbounded(t *testing.T) {
	q := NewBoundedQueue[string](0)
	for i := 0; i < 50; i++ {
		q.Append("x")
	}
	if evs := retained(q); len(evs) != 50 {
		t.Errorf("retained = %d, want 50 (maxRetain 0 is unbounded)", len(evs))
	}
}

func TestBoundedQueueConcurrentAppendsBounded(t *testing.T) {
	const cap = 16
	q := NewBoundedQueue[string](cap)
	const n = 500
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q.Append("x")
		}()
	}
	wg.Wait()
	if l := q.Len(); l != n {
		t.Errorf("Len = %d, want %d", l, n)
	}
	if evs := retained(q); len(evs) != cap {
		t.Errorf("retained window = %d, want %d", len(evs), cap)
	}
	q.mu.RLock()
	backing := len(q.events)
	q.mu.RUnlock()
	if backing > 2*cap {
		t.Errorf("backing slice len = %d, want <= %d", backing, 2*cap)
	}
}
