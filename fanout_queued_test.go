package gocurrent

import (
	"fmt"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ExampleQueuedFanOut demonstrates basic QueuedFanOut usage: create a fan-out,
// add output channels, send events, and receive them on all outputs in strict
// FIFO order.
func ExampleQueuedFanOut() {
	fanout := NewQueuedFanOut[int]()
	defer fanout.Stop()

	NUM_CHANS := 2
	NUM_MSGS := 3

	var outchans []chan int
	for i := 0; i < NUM_CHANS; i++ {
		outchan := fanout.New(nil)
		outchans = append(outchans, outchan)
	}

	var vals []int

	for i := 0; i < NUM_MSGS; i++ {
		fanout.Send(i)
	}

	for j := 0; j < NUM_MSGS; j++ {
		for i := 0; i < NUM_CHANS; i++ {
			val := <-outchans[i]
			vals = append(vals, val)
		}
	}

	sort.Ints(vals)
	for _, v := range vals {
		fmt.Println(v)
	}

	// Output:
	// 0
	// 0
	// 1
	// 1
	// 2
	// 2
}

// TestQueuedFanOut_Basic verifies that QueuedFanOut delivers all events to
// all registered outputs. 5 output channels each receive 10 events. All 50
// values are collected and verified.
func TestQueuedFanOut_Basic(t *testing.T) {
	fanout := NewQueuedFanOut[int]()
	var vals []int
	var wg sync.WaitGroup
	var m sync.Mutex
	for o := 0; o < 5; o++ {
		wg.Add(1)
		outch := fanout.New(nil)
		go func(o int, outch chan int) {
			defer fanout.Remove(outch, false)
			defer wg.Done()
			for count := 0; count < 10; count++ {
				i := <-outch
				m.Lock()
				vals = append(vals, i)
				m.Unlock()
			}
		}(o, outch)
	}

	for i := 0; i < 10; i++ {
		fanout.Send(i)
	}
	wg.Wait()

	sort.Ints(vals)
	for i := 0; i < 50; i++ {
		assert.Equal(t, vals[i], i/5, "Out vals dont match")
	}
}

// TestQueuedFanOut_FIFOOrdering verifies the core ordering guarantee of
// QueuedFanOut: when N events are sent sequentially, every output channel
// receives them in the exact same order. This is the property that
// AsyncFanOut does NOT provide — events A and B can arrive out of order
// with AsyncFanOut because independent goroutines race.
//
// The test sends 100 events to 5 outputs and checks that each output's
// received sequence is [0, 1, 2, ..., 99]. Run with -race to also verify
// no data races.
func TestQueuedFanOut_FIFOOrdering(t *testing.T) {
	fanout := NewQueuedFanOut[int]()
	defer fanout.Stop()

	const numOutputs = 5
	const numEvents = 100

	outchans := make([]chan int, numOutputs)
	for i := range outchans {
		outchans[i] = fanout.New(nil)
	}

	received := make([][]int, numOutputs)
	var wg sync.WaitGroup
	for i := 0; i < numOutputs; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			for j := 0; j < numEvents; j++ {
				received[idx] = append(received[idx], <-outchans[idx])
			}
		}(i)
	}

	for i := 0; i < numEvents; i++ {
		fanout.Send(i)
	}
	wg.Wait()

	for i := 0; i < numOutputs; i++ {
		assert.Len(t, received[i], numEvents, "output %d: wrong count", i)
		for j := 0; j < numEvents; j++ {
			assert.Equal(t, j, received[i][j], "output %d: event %d out of order", i, j)
		}
	}
}

// TestQueuedFanOut_SenderNonBlocking verifies that the sender does not block
// on slow outputs. A slow output channel (unbuffered, with a delayed reader)
// should not prevent the sender from enqueuing events into the dispatch queue.
// The sender should be able to fill the queue buffer without blocking per-output.
func TestQueuedFanOut_SenderNonBlocking(t *testing.T) {
	fanout := NewQueuedFanOut[int](WithQueueSize[int](32))
	defer fanout.Stop()

	slowOut := fanout.New(nil)
	go func() {
		for range slowOut {
			time.Sleep(10 * time.Millisecond)
		}
	}()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 20; i++ {
			fanout.Send(i)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Sender blocked — QueuedFanOut should buffer events in the dispatch queue")
	}
}

// TestQueuedFanOut_BackPressure verifies that when the dispatch queue is full
// AND outputs are blocked, the sender eventually blocks too. This ensures
// back-pressure propagates rather than events being silently dropped.
func TestQueuedFanOut_BackPressure(t *testing.T) {
	inCh := make(chan int)
	fanout := NewQueuedFanOut[int](WithFanOutInputChan[int](inCh), WithQueueSize[int](2))

	blockedOut := make(chan int)
	<-fanout.Add(blockedOut, nil, true)

	stopSender := make(chan struct{})
	sent := make(chan int, 10)
	go func() {
		for i := 0; i < 10; i++ {
			select {
			case inCh <- i:
				sent <- i
			case <-stopSender:
				return
			}
		}
	}()

	count := 0
	timeout := time.After(500 * time.Millisecond)
	for {
		select {
		case <-sent:
			count++
		case <-timeout:
			goto done
		}
	}
done:
	assert.Less(t, count, 10, "Sender should have blocked before sending all 10 events")

	close(stopSender)
	go func() {
		for range blockedOut {
		}
	}()
	fanout.Stop()
}

// TestQueuedFanOut_BoundedGoroutines verifies that QueuedFanOut uses a
// bounded number of goroutines regardless of how many events are sent.
// Unlike AsyncFanOut which spawns N goroutines per event, QueuedFanOut
// should only use 2 goroutines (runner + dispatcher) plus whatever the
// test itself creates.
func TestQueuedFanOut_BoundedGoroutines(t *testing.T) {
	baseline := runtime.NumGoroutine()

	fanout := NewQueuedFanOut[int]()
	defer fanout.Stop()

	out := fanout.New(nil)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			<-out
		}
	}()

	for i := 0; i < 100; i++ {
		fanout.Send(i)
	}
	wg.Wait()

	current := runtime.NumGoroutine()
	assert.Less(t, current, baseline+10,
		"Goroutine count should be bounded; got %d (baseline %d)", current, baseline)
}

// TestQueuedFanOut_SnapshotConsistency verifies that adding and removing
// outputs while events are being dispatched does not cause races, panics,
// or sends to closed channels. Run with -race to verify.
//
// Transient outputs drain continuously until their Remove completes, which
// is the correct usage pattern. Stop() at the end interrupts any dispatch
// goroutine sends that may be blocked on stale snapshot references.
func TestQueuedFanOut_SnapshotConsistency(t *testing.T) {
	fanout := NewQueuedFanOut[int]()

	drain := fanout.New(nil)
	go func() { for range drain {} }()

	stopSender := make(chan struct{})
	var senderWg sync.WaitGroup

	senderWg.Add(1)
	go func() {
		defer senderWg.Done()
		i := 0
		for {
			select {
			case fanout.InputChan() <- i:
				i++
				runtime.Gosched()
			case <-stopSender:
				return
			}
		}
	}()

	var subWg sync.WaitGroup
	for round := 0; round < 10; round++ {
		subWg.Add(1)
		go func() {
			defer subWg.Done()
			ch := fanout.New(nil)
			removeDone := make(chan struct{})

			go func() {
				for {
					select {
					case <-ch:
					case <-removeDone:
						return
					}
				}
			}()

			runtime.Gosched()
			<-fanout.Remove(ch, true)
			close(removeDone)
		}()
	}

	subWg.Wait()
	close(stopSender)
	senderWg.Wait()
	fanout.Stop()
}

// TestQueuedFanOut_ClosedChan verifies that QueuedFanOut signals completion
// via ClosedChan when stopped.
func TestQueuedFanOut_ClosedChan(t *testing.T) {
	fanout := NewQueuedFanOut[int]()

	out1 := fanout.New(nil)
	out2 := fanout.New(nil)

	go func() { for range out1 {} }()
	go func() { for range out2 {} }()

	fanout.Stop()

	select {
	case err := <-fanout.ClosedChan():
		if err != nil {
			t.Errorf("Expected nil error, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for QueuedFanOut to close")
	}
}

// TestQueuedFanOut_Filter verifies that per-output filters work correctly
// with QueuedFanOut. A filter that doubles values should only affect its
// output, while a nil-returning filter should suppress delivery.
//
// Because QueuedFanOut delivers to ALL outputs for event N before moving to
// event N+1, outputs must be drained concurrently to avoid blocking the
// dispatch goroutine.
func TestQueuedFanOut_Filter(t *testing.T) {
	fanout := NewQueuedFanOut[int]()
	defer fanout.Stop()

	plain := fanout.New(nil)
	doubled := fanout.New(func(v *int) *int {
		d := *v * 2
		return &d
	})
	evensOnly := fanout.New(func(v *int) *int {
		if *v%2 == 0 {
			return v
		}
		return nil
	})

	var plainVals, doubledVals, evensVals []int
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); for i := 0; i < 3; i++ { plainVals = append(plainVals, <-plain) } }()
	go func() { defer wg.Done(); for i := 0; i < 3; i++ { doubledVals = append(doubledVals, <-doubled) } }()
	go func() {
		defer wg.Done()
		select {
		case v := <-evensOnly:
			evensVals = append(evensVals, v)
		case <-time.After(2 * time.Second):
			t.Error("Timeout waiting for evensOnly value")
		}
	}()

	fanout.Send(1)
	fanout.Send(2)
	fanout.Send(3)

	wg.Wait()

	assert.Equal(t, []int{1, 2, 3}, plainVals)
	assert.Equal(t, []int{2, 4, 6}, doubledVals)
	assert.Equal(t, []int{2}, evensVals)

	select {
	case v := <-evensOnly:
		t.Errorf("Unexpected value on evensOnly: %d", v)
	case <-time.After(50 * time.Millisecond):
	}
}
