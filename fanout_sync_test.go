package gocurrent

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSyncFanOut_FIFOOrdering verifies that SyncFanOut delivers events in
// strict FIFO order. Since delivery is synchronous within the runner
// goroutine, event A completes delivery to ALL outputs before event B
// begins. Every output receives [0, 1, 2, ..., 99].
func TestSyncFanOut_FIFOOrdering(t *testing.T) {
	fanout := NewSyncFanOut[int]()
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

// TestSyncFanOut_AddRemoveFilter verifies Add, Remove, New, and per-output
// filters work correctly with SyncFanOut. Self-owned channels are closed
// immediately on Remove (unlike QueuedFanOut which defers closure to Stop).
func TestSyncFanOut_AddRemoveFilter(t *testing.T) {
	fanout := NewSyncFanOut[int]()
	defer fanout.Stop()

	out1 := fanout.New(nil)
	out2 := fanout.New(func(v *int) *int {
		d := *v * 2
		return &d
	})

	fanout.Send(5)

	assert.Equal(t, 5, <-out1)
	assert.Equal(t, 10, <-out2)

	<-fanout.Remove(out2, true)

	fanout.Send(7)
	assert.Equal(t, 7, <-out1)

	// SyncFanOut closes self-owned channels immediately on Remove
	_, ok := <-out2
	assert.False(t, ok, "out2 should be closed after Remove")
}
