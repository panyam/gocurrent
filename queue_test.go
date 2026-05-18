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
