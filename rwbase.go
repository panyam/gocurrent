package gocurrent

import (
	"errors"
	"sync"
)

// Base of the Reader and Writer primitives
type RunnerBase[C any] struct {
	mu          sync.Mutex
	controlChan chan C
	isRunning   bool
	wg          sync.WaitGroup
	stopVal     C
}

// Creates a new base runner - called by the Reader and Writer primitives
func NewRunnerBase[C any](stopVal C) RunnerBase[C] {
	return RunnerBase[C]{
		controlChan: make(chan C, 1),
		stopVal:     stopVal,
	}
}

// Used for returning any debug information.
func (r *RunnerBase[R]) DebugInfo() any {
	r.mu.Lock()
	defer r.mu.Unlock()
	return map[string]any{
		"ctrlChan":  r.controlChan,
		"stopVal":   r.stopVal,
		"isRunning": r.isRunning,
	}
}

// Returns true if currently running otherwise false
func (r *RunnerBase[C]) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.isRunning
}

// Responsible for starting the runner.  This method is intentionally private.  It is to be inherited by child types and then called after their initialization is done.
func (r *RunnerBase[C]) start() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.isRunning {
		return errors.New("Channel already running")
	}
	r.isRunning = true
	r.wg.Add(1)
	return nil
}

// This method is called to stop the runner.  It is upto the child classes
// to listen to messages on the control channel and initiate the wind-down
// and cleanup process.
func (r *RunnerBase[C]) Stop() error {
	r.mu.Lock()
	if !r.isRunning || r.controlChan == nil {
		// Already stopped or cleaned up — nothing to do
		r.mu.Unlock()
		return nil
	}
	ch := r.controlChan
	// Mark stopped before releasing the lock so that recursive Stop()
	// calls (e.g. from cleanup → OnDone → removeAt → Stop) return early.
	r.isRunning = false
	r.mu.Unlock()

	// Send the stop signal. If cleanup() closes the channel concurrently
	// (goroutine self-terminated), recover from the panic on closed channel.
	func() {
		defer func() { recover() }()
		ch <- r.stopVal
	}()
	r.wg.Wait()
	return nil
}

// Cleanup method when the runner stops.  Will be called by the composing types
func (r *RunnerBase[C]) cleanup() {
	r.mu.Lock()
	if r.controlChan != nil {
		close(r.controlChan)
		r.controlChan = nil
	}
	r.isRunning = false
	r.mu.Unlock()
	r.wg.Done()
}
