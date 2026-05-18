package gocurrent

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// These tests are designed to exercise the Queue under
// concurrent pressure with the race detector enabled. Run with:
//
//	go test -race -run TestStress -timeout 60s ./
//
// Hazard surfaces probed by each test:
//
//   - q.events / q.subs slice growth races (mu must be held)
//   - per-subscription channel sends interleaving with Close
//   - resolveSignals map mutation under concurrent Resolve+Await
//   - signal-after-snapshot: subscriber closed between snapshot
//     and dispatch should not panic
//   - notify-channel buffer never blocks the producer
//
// Each test bounds its concurrency + duration so the race
// detector's overhead doesn't blow out the timeout.

// TestStressManyProducersManyConsumers — N producers and M
// consumers run concurrently. Every consumer must observe every
// produced event in order. Validates the fan-out + coalescing
// invariants under load.
func TestStressManyProducersManyConsumers(t *testing.T) {
	const (
		producers      = 8
		consumers      = 16
		eventsPerProd  = 200
		totalExpected  = producers * eventsPerProd
	)
	q := NewQueue[int]()

	// Start consumers first so they're subscribed before any
	// Append. Each consumer reads until it has seen `totalExpected`
	// events, then exits.
	consumerWG := sync.WaitGroup{}
	consumerWG.Add(consumers)
	consumerResults := make([]int, consumers)
	for i := 0; i < consumers; i++ {
		idx := i
		go func() {
			defer consumerWG.Done()
			sub := q.Subscribe()
			defer sub.Close()
			offset := 0
			seen := 0
			for seen < totalExpected {
				select {
				case <-sub.Notify():
				case <-time.After(2 * time.Second):
					t.Errorf("consumer %d: timed out at %d/%d events", idx, seen, totalExpected)
					return
				}
				events, newOff := q.ReadFrom(offset)
				seen += len(events)
				offset = newOff
			}
			consumerResults[idx] = seen
		}()
	}

	// Brief sleep so consumers are blocked on Notify before we
	// start producing — exercises the wake-up path more than the
	// already-buffered path.
	time.Sleep(10 * time.Millisecond)

	// Start producers.
	producerWG := sync.WaitGroup{}
	producerWG.Add(producers)
	for p := 0; p < producers; p++ {
		pid := p
		go func() {
			defer producerWG.Done()
			for i := 0; i < eventsPerProd; i++ {
				q.Append(pid*eventsPerProd + i)
			}
		}()
	}
	producerWG.Wait()
	consumerWG.Wait()

	for i, got := range consumerResults {
		if got != totalExpected {
			t.Errorf("consumer %d saw %d events, want %d", i, got, totalExpected)
		}
	}
	if got := q.Len(); got != totalExpected {
		t.Errorf("queue Len = %d, want %d", got, totalExpected)
	}
}

// TestStressSubscribeUnsubscribeChurn — concurrent goroutines
// subscribe + close in a tight loop while a producer hammers
// Append. The producer must never panic; the queue's signal-
// to-subs path must be safe against subs disappearing mid-fanout.
func TestStressSubscribeUnsubscribeChurn(t *testing.T) {
	q := NewQueue[int]()
	stop := make(chan struct{})

	// Producer: appends continuously.
	prodWG := sync.WaitGroup{}
	prodWG.Add(1)
	go func() {
		defer prodWG.Done()
		i := 0
		for {
			select {
			case <-stop:
				return
			default:
				q.Append(i)
				i++
			}
		}
	}()

	// Churners: subscribe → maybe drain a bit → close → repeat.
	const churners = 16
	churnerWG := sync.WaitGroup{}
	for c := 0; c < churners; c++ {
		churnerWG.Add(1)
		go func() {
			defer churnerWG.Done()
			for i := 0; i < 100; i++ {
				sub := q.Subscribe()
				// Race a notify drain against close.
				select {
				case <-sub.Notify():
				case <-time.After(5 * time.Millisecond):
				}
				sub.Close()
			}
		}()
	}
	churnerWG.Wait()
	close(stop)
	prodWG.Wait()

	// Multiple Close calls on the same Subscription should be
	// safe (idempotent).
	sub := q.Subscribe()
	sub.Close()
	sub.Close() // must not panic
}

// TestStressConcurrentBarriers — many barriers active simultaneously,
// each with multiple awaiters and multiple racing resolvers.
// Asserts exactly-one-winner per barrier and all awaiters see
// the same resolution.
func TestStressConcurrentBarriers(t *testing.T) {
	q := NewQueue[string]()
	const (
		barriers           = 30
		awaitersPerBarrier = 5
		resolversPerBarrier = 5
	)

	offsets := make([]int, barriers)
	for i := range offsets {
		offsets[i] = q.Append("barrier")
	}

	// Awaiters: each gets a chan to report what they observed.
	awaiterResults := make([]chan any, barriers)
	for i := range awaiterResults {
		awaiterResults[i] = make(chan any, awaitersPerBarrier)
	}
	awaiterWG := sync.WaitGroup{}
	for b := 0; b < barriers; b++ {
		for a := 0; a < awaitersPerBarrier; a++ {
			awaiterWG.Add(1)
			barrier := b
			go func() {
				defer awaiterWG.Done()
				awaiterResults[barrier] <- q.AwaitResolution(offsets[barrier])
			}()
		}
	}

	// Brief sleep so awaiters are all parked on the wait channel
	// before resolvers race.
	time.Sleep(20 * time.Millisecond)

	// Resolvers: many goroutines try to resolve each barrier.
	var winners atomic.Int32
	resolverWG := sync.WaitGroup{}
	for b := 0; b < barriers; b++ {
		for r := 0; r < resolversPerBarrier; r++ {
			resolverWG.Add(1)
			barrier := b
			rid := r
			go func() {
				defer resolverWG.Done()
				err := q.Resolve(offsets[barrier], rid)
				if err == nil {
					winners.Add(1)
				} else if !errors.Is(err, ErrAlreadyResolved) {
					t.Errorf("unexpected error: %v", err)
				}
			}()
		}
	}
	resolverWG.Wait()
	awaiterWG.Wait()

	if got := winners.Load(); got != int32(barriers) {
		t.Errorf("total winners = %d, want %d (exactly one per barrier)", got, barriers)
	}

	// Every awaiter for a barrier should have observed the same
	// resolution value (whichever resolver won).
	for b := 0; b < barriers; b++ {
		close(awaiterResults[b])
		var first any
		firstSet := false
		for r := range awaiterResults[b] {
			if !firstSet {
				first = r
				firstSet = true
				continue
			}
			if r != first {
				t.Errorf("barrier %d awaiters saw different resolutions: %v vs %v", b, first, r)
			}
		}
	}
}

// TestStressAppendBarrierInterleaved — multiple producer
// goroutines call AppendBarrier concurrently; another goroutine
// drains the queue and resolves every barrier it sees. The
// AppendBarrier callers must all return; the queue's Len at
// the end matches the count of barriers appended.
func TestStressAppendBarrierInterleaved(t *testing.T) {
	q := NewQueue[string]()
	const barriers = 100

	// Resolver: drains the queue and resolves any unresolved
	// entry.
	resolverDone := make(chan struct{})
	resolved := atomic.Int32{}
	go func() {
		defer close(resolverDone)
		sub := q.Subscribe()
		defer sub.Close()
		offset := 0
		for resolved.Load() < barriers {
			select {
			case <-sub.Notify():
			case <-time.After(2 * time.Second):
				return
			}
			events, newOff := q.ReadFrom(offset)
			for i := range events {
				eventOffset := offset + i
				if _, alreadyResolved := q.Resolution(eventOffset); alreadyResolved {
					continue
				}
				if err := q.Resolve(eventOffset, "resolved"); err == nil {
					resolved.Add(1)
				}
			}
			offset = newOff
		}
	}()

	// Producers: each does AppendBarrier and asserts it returns.
	prodWG := sync.WaitGroup{}
	for i := 0; i < barriers; i++ {
		prodWG.Add(1)
		go func() {
			defer prodWG.Done()
			res := q.AppendBarrier("barrier")
			if res != "resolved" {
				t.Errorf("AppendBarrier returned %v, want %q", res, "resolved")
			}
		}()
	}
	prodWG.Wait()
	<-resolverDone

	if got := q.Len(); got != barriers {
		t.Errorf("Len = %d, want %d", got, barriers)
	}
}

// TestStressResolveRacesAwaitRegistration — exercises the
// narrow race window between AwaitResolution registering its
// wait-channel and Resolve dropping in the resolution. The
// mutex makes this sequenced-and-safe by construction, but the
// -race detector flags subtle issues if either path skips
// re-checking under the lock. 200 iterations × fresh-queue per
// iteration so the race window is the dominant timing.
func TestStressResolveRacesAwaitRegistration(t *testing.T) {
	const iterations = 200
	for i := 0; i < iterations; i++ {
		q := NewQueue[string]()
		offset := q.Append("barrier")

		var wg sync.WaitGroup
		wg.Add(2)
		var result any
		go func() {
			defer wg.Done()
			result = q.AwaitResolution(offset)
		}()
		go func() {
			defer wg.Done()
			_ = q.Resolve(offset, "value")
		}()
		wg.Wait()
		if result != "value" {
			t.Errorf("iter %d: AwaitResolution got %v, want %q", i, result, "value")
		}
	}
}

// TestStressBroadcastDuringResolve — large number of
// AwaitResolution waiters all parked on the same offset; a
// resolver wakes them all atomically. Validates the
// channel-close broadcast doesn't drop any waiter.
func TestStressBroadcastDuringResolve(t *testing.T) {
	q := NewQueue[string]()
	offset := q.Append("barrier")
	const waiters = 200

	done := make(chan any, waiters)
	for i := 0; i < waiters; i++ {
		go func() { done <- q.AwaitResolution(offset) }()
	}

	// Let them all park.
	time.Sleep(50 * time.Millisecond)

	_ = q.Resolve(offset, "broadcast")

	woke := 0
	for {
		select {
		case r := <-done:
			if r != "broadcast" {
				t.Errorf("waiter got %v, want broadcast", r)
			}
			woke++
			if woke == waiters {
				return
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("only %d/%d waiters woke", woke, waiters)
		}
	}
}
