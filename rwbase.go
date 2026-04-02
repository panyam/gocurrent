package gocurrent

import (
	"errors"
	"sync"
	"sync/atomic"
)

// RunnerBase is the base of the Reader, Writer, Mapper, FanIn, and FanOut
// primitives. It provides lifecycle management (start/stop) and coordination
// between the owner goroutine and the worker goroutine.
//
// Key design: controlChan is created once and never closed or nilled. The done
// channel is closed by cleanup() to signal that the worker goroutine has exited.
// This eliminates the data race between Stop() sending on controlChan and
// cleanup() closing it that existed in the previous mutex+close design.
type RunnerBase[C any] struct {
	controlChan chan C
	done        chan struct{}
	isRunning   atomic.Bool
	wg          sync.WaitGroup
	stopVal     C
}

// NewRunnerBase creates a new base runner. Called by Reader, Writer, Mapper,
// FanIn, and FanOut constructors. The controlChan is buffered(1) to allow
// a single stop signal to be sent without blocking.
func NewRunnerBase[C any](stopVal C) RunnerBase[C] {
	return RunnerBase[C]{
		controlChan: make(chan C, 1),
		done:        make(chan struct{}),
		stopVal:     stopVal,
	}
}

// DebugInfo returns diagnostic information about the runner's state.
func (r *RunnerBase[R]) DebugInfo() any {
	return map[string]any{
		"stopVal":   r.stopVal,
		"isRunning": r.isRunning.Load(),
	}
}

// IsRunning returns true if the runner's worker goroutine is active.
func (r *RunnerBase[C]) IsRunning() bool {
	return r.isRunning.Load()
}

// start marks the runner as running and increments the WaitGroup. This method
// is intentionally private — it is called by composing types after their own
// initialization is complete.
func (r *RunnerBase[C]) start() error {
	if !r.isRunning.CompareAndSwap(false, true) {
		return errors.New("Channel already running")
	}
	r.wg.Add(1)
	return nil
}

// Stop sends a stop signal to the worker goroutine and waits for it to finish.
// It is safe to call Stop() concurrently, multiple times, or after the worker
// goroutine has already self-terminated. Only the first call that transitions
// isRunning from true to false will send the stop signal; subsequent calls
// return immediately.
func (r *RunnerBase[C]) Stop() error {
	if !r.isRunning.CompareAndSwap(true, false) {
		// Already stopped (by another Stop() call or by self-termination)
		return nil
	}

	// Either deliver the stop signal, or observe that the goroutine already
	// exited (done closed). This select eliminates the old race between
	// sending on controlChan and cleanup() closing it.
	select {
	case r.controlChan <- r.stopVal:
		// Stop signal delivered; goroutine will read it and exit.
	case <-r.done:
		// Goroutine already exited on its own (e.g. write error).
	}
	r.wg.Wait()
	return nil
}

// Done returns a channel that is closed when the runner's worker goroutine exits.
// Useful for coordinating with other goroutines that need to know when the runner
// has stopped (e.g., FanIn's pipeClosed callback uses this to avoid sending on
// controlChan after the FanIn goroutine has exited).
func (r *RunnerBase[C]) Done() <-chan struct{} {
	return r.done
}

// cleanup is called by composing types (via defer) when their worker goroutine
// exits. It signals completion via the done channel and decrements the WaitGroup.
// controlChan is intentionally NOT closed — it is left for garbage collection.
func (r *RunnerBase[C]) cleanup() {
	r.isRunning.Store(false)
	close(r.done)
	r.wg.Done()
}
