package gocurrent

import (
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAsyncFanOut_Basic verifies that AsyncFanOut delivers all events to all
// registered outputs. Event ordering is NOT checked because AsyncFanOut
// provides no ordering guarantee — goroutines for different events can
// interleave. Only completeness is verified.
func TestAsyncFanOut_Basic(t *testing.T) {
	fanout := NewAsyncFanOut[int]()
	defer fanout.Stop()

	const numOutputs = 3
	const numEvents = 50

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

	// Verify completeness: each output got all 50 events (in any order)
	for i := 0; i < numOutputs; i++ {
		assert.Len(t, received[i], numEvents, "output %d: wrong count", i)
		sorted := make([]int, numEvents)
		copy(sorted, received[i])
		sort.Ints(sorted)
		for j := 0; j < numEvents; j++ {
			assert.Equal(t, j, sorted[j], "output %d: missing event %d", i, j)
		}
	}
}

// TestAsyncFanOut_AddRemoveFilter verifies Add, Remove, New, and per-output
// filters work correctly with AsyncFanOut. Self-owned channels are closed
// immediately on Remove (same as SyncFanOut).
func TestAsyncFanOut_AddRemoveFilter(t *testing.T) {
	fanout := NewAsyncFanOut[int]()
	defer fanout.Stop()

	out1 := fanout.New(nil)
	out2 := fanout.New(func(v *int) *int {
		d := *v * 2
		return &d
	})

	fanout.Send(5)

	v1 := <-out1
	v2 := <-out2

	assert.Equal(t, 5, v1)
	assert.Equal(t, 10, v2)

	<-fanout.Remove(out2, true)

	fanout.Send(7)
	assert.Equal(t, 7, <-out1)

	// AsyncFanOut closes self-owned channels immediately on Remove
	_, ok := <-out2
	assert.False(t, ok, "out2 should be closed after Remove")
}
