package gocurrent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// Interface compliance — compile-time checks
// ---------------------------------------------------------------------------

// TestFanOuter_InterfaceCompliance verifies at compile time that all three
// fan-out types satisfy the FanOuter[T] interface. If any method is missing
// or has the wrong signature, this test will fail to compile.
func TestFanOuter_InterfaceCompliance(t *testing.T) {
	var _ FanOuter[int] = (*SyncFanOut[int])(nil)
	var _ FanOuter[int] = (*AsyncFanOut[int])(nil)
	var _ FanOuter[int] = (*QueuedFanOut[int])(nil)
}

// ---------------------------------------------------------------------------
// Common option tests
// ---------------------------------------------------------------------------

// TestFanOut_WithInputBuffer verifies that WithFanOutInputBuffer creates a
// buffered input channel for all fan-out types.
func TestFanOut_WithInputBuffer(t *testing.T) {
	fo := NewQueuedFanOut[int](WithFanOutInputBuffer[int](10))
	defer fo.Stop()
	assert.Equal(t, 10, cap(fo.inputChan))

	fo2 := NewSyncFanOut[int](WithFanOutInputBuffer[int](5))
	defer fo2.Stop()
	assert.Equal(t, 5, cap(fo2.inputChan))
}

// TestFanOut_WithInputChan verifies that WithFanOutInputChan uses an
// existing channel and does not close it on Stop.
func TestFanOut_WithInputChan(t *testing.T) {
	ch := make(chan int, 3)
	fo := NewQueuedFanOut[int](WithFanOutInputChan[int](ch))
	out := fo.New(nil)

	ch <- 42
	assert.Equal(t, 42, <-out)

	fo.Stop()
	<-fo.ClosedChan()

	// Channel should still be open (not owned by fan-out)
	ch <- 99
	assert.Equal(t, 99, <-ch)
}
