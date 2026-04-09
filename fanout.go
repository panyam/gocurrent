package gocurrent

import (
	"log"
)

// FilterFunc is an optional per-output transformation/filtering function.
// It receives a pointer to the event and returns a pointer to the (possibly
// modified) event. Return nil to skip delivery to this output.
type FilterFunc[T any] func(*T) *T

// FanOuter is the interface satisfied by all fan-out dispatch strategies.
//
// Three concrete implementations are provided, each with different trade-offs
// between sender blocking, event ordering, and goroutine usage:
//
//   - [SyncFanOut]:   Sender blocks until all outputs receive the event.
//     Strict FIFO ordering. Zero extra goroutines.
//   - [AsyncFanOut]:  Sender never blocks (one goroutine per output per event).
//     No ordering guarantee. Goroutine count can explode.
//   - [QueuedFanOut]: Sender blocks only when the dispatch queue is full.
//     Strict FIFO ordering via a persistent dispatch goroutine.
//     Two goroutines total (runner + dispatcher). Recommended default.
//
// Ordering semantics summary:
//
//	| Type           | Sender blocks?        | FIFO ordering? | Goroutines        |
//	|----------------|-----------------------|----------------|-------------------|
//	| SyncFanOut     | Yes (all outputs)     | Strict         | 0 extra           |
//	| AsyncFanOut    | No                    | None           | N per event       |
//	| QueuedFanOut   | No (until queue full) | Strict         | 2 total (bounded) |
type FanOuter[T any] interface {
	Component

	// Send delivers a value to the fan-out for distribution to all outputs.
	// Blocking behavior depends on the concrete implementation.
	Send(value T)

	// InputChan returns the write-only input channel. Callers may send
	// directly on this channel instead of calling Send.
	InputChan() chan<- T

	// Add registers an existing output channel with an optional per-channel
	// filter. If wait is true, the returned channel receives nil once the
	// registration is complete; otherwise the returned channel is nil.
	Add(output chan<- T, filter FilterFunc[T], wait bool) (callbackChan chan error)

	// New creates a new output channel owned by the fan-out (closed on Stop)
	// with an optional filter. The call blocks until registration is complete.
	New(filter FilterFunc[T]) chan T

	// Remove unregisters an output channel. If the channel was created by New,
	// it is also closed. If wait is true, the returned channel receives nil
	// once the removal is complete.
	Remove(output chan<- T, wait bool) (callbackChan chan error)

	// Count returns the current number of registered output channels.
	Count() int

	// ClosedChan returns a channel that receives nil (or an error) when the
	// fan-out has fully shut down.
	ClosedChan() <-chan error
}

// ---------------------------------------------------------------------------
// Internal command type used by all fan-out implementations
// ---------------------------------------------------------------------------

type fanOutCmd[T any] struct {
	Name           string
	Filter         FilterFunc[T]
	SelfOwned      bool
	AddedChannel   chan<- T
	RemovedChannel chan<- T
	CallbackChan   chan error
}

// ---------------------------------------------------------------------------
// fanOutCore — shared state and methods embedded by all implementations
// ---------------------------------------------------------------------------

// fanOutCore contains the state and methods common to every fan-out strategy.
// It is unexported; callers interact through [FanOuter] or the concrete types.
type fanOutCore[T any] struct {
	RunnerBase[fanOutCmd[T]]
	selfOwnIn       bool
	inputChan       chan T
	outputChans     []chan<- T
	outputSelfOwned []bool
	outputFilters   []FilterFunc[T]
	closedChan      chan error
}

// initCore sets up the shared state. Called by each concrete constructor.
func (c *fanOutCore[T]) initCore() {
	c.RunnerBase = NewRunnerBase(fanOutCmd[T]{Name: "stop"})
	c.closedChan = make(chan error, 1)
	if c.inputChan == nil {
		c.inputChan = make(chan T)
		c.selfOwnIn = true
	}
}

// ClosedChan returns the channel used to signal when the fan-out is done.
func (c *fanOutCore[T]) ClosedChan() <-chan error {
	return c.closedChan
}

// DebugInfo returns diagnostic information about the fan-out's state.
func (c *fanOutCore[T]) DebugInfo() any {
	return map[string]any{
		"inputChan":    c.inputChan,
		"outputChan":   c.outputChans,
		"outputChanSO": c.outputSelfOwned,
	}
}

// Count returns the number of registered output channels.
func (c *fanOutCore[T]) Count() int {
	return len(c.outputChans)
}

// InputChan returns the write-only input channel.
func (c *fanOutCore[T]) InputChan() chan<- T {
	return c.inputChan
}

// Send writes a value to the input channel for fan-out distribution.
func (c *fanOutCore[T]) Send(value T) {
	c.inputChan <- value
}

// Add registers an output channel with an optional filter.
// If wait is true, the returned channel receives nil once registration is complete.
func (c *fanOutCore[T]) Add(output chan<- T, filter FilterFunc[T], wait bool) (callbackChan chan error) {
	if wait {
		callbackChan = make(chan error, 1)
	}
	c.controlChan <- fanOutCmd[T]{Name: "add", AddedChannel: output, Filter: filter, CallbackChan: callbackChan}
	return
}

// New creates a new owned output channel with an optional filter.
// The fan-out will close this channel on Remove or Stop.
func (c *fanOutCore[T]) New(filter FilterFunc[T]) chan T {
	output := make(chan T, 1)
	callbackChan := make(chan error, 1)
	c.controlChan <- fanOutCmd[T]{
		Name:         "add",
		AddedChannel: output,
		Filter:       filter,
		SelfOwned:    true,
		CallbackChan: callbackChan,
	}
	<-callbackChan
	return output
}

// Remove unregisters an output channel. If the channel was created by New,
// it is also closed.
func (c *fanOutCore[T]) Remove(output chan<- T, wait bool) (callbackChan chan error) {
	if wait {
		callbackChan = make(chan error)
	}
	c.controlChan <- fanOutCmd[T]{Name: "remove", RemovedChannel: output, CallbackChan: callbackChan}
	return
}

// cleanup releases resources common to all fan-out types.
func (c *fanOutCore[T]) cleanup() {
	if c.selfOwnIn {
		close(c.inputChan)
	}
	for index, ch := range c.outputChans {
		if c.outputSelfOwned[index] && ch != nil {
			close(ch)
		}
	}
	close(c.closedChan)
	c.RunnerBase.cleanup()
}

// handleCmd processes a control command (add/remove/stop) inside the
// goroutine's select loop. Returns true if the goroutine should exit.
func (c *fanOutCore[T]) handleCmd(cmd fanOutCmd[T]) (shouldStop bool) {
	if cmd.Name == "stop" {
		return true
	}

	if cmd.Name == "add" {
		found := false
		for _, oc := range c.outputChans {
			if oc == cmd.AddedChannel {
				found = true
				log.Println("Output Channel already exists. Will skip. Remove it first if you want to add again or change filter funcs", cmd.AddedChannel)
				break
			}
		}
		if !found {
			c.outputChans = append(c.outputChans, cmd.AddedChannel)
			c.outputSelfOwned = append(c.outputSelfOwned, cmd.SelfOwned)
			c.outputFilters = append(c.outputFilters, cmd.Filter)
		}
		if cmd.CallbackChan != nil {
			cmd.CallbackChan <- nil
		}
	} else if cmd.Name == "remove" {
		for index, ch := range c.outputChans {
			if ch == cmd.RemovedChannel {
				if c.outputSelfOwned[index] {
					close(ch)
				}
				last := len(c.outputChans) - 1
				c.outputSelfOwned[index] = c.outputSelfOwned[last]
				c.outputSelfOwned = c.outputSelfOwned[:last]
				c.outputChans[index] = c.outputChans[last]
				c.outputChans = c.outputChans[:last]
				c.outputFilters[index] = c.outputFilters[last]
				c.outputFilters = c.outputFilters[:last]
				break
			}
		}
		if cmd.CallbackChan != nil {
			cmd.CallbackChan <- nil
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Common functional options (work with all fan-out types)
// ---------------------------------------------------------------------------

// FanOutOption is a functional option for configuring any fan-out type.
type FanOutOption[T any] func(*fanOutCore[T])

// WithFanOutInputChan sets the input channel. The fan-out will NOT close this
// channel on Stop (caller retains ownership).
func WithFanOutInputChan[T any](ch chan T) FanOutOption[T] {
	return func(c *fanOutCore[T]) {
		c.inputChan = ch
		c.selfOwnIn = false
	}
}

// WithFanOutInputBuffer creates a buffered input channel of the given size.
// The fan-out owns and will close this channel on Stop.
func WithFanOutInputBuffer[T any](size int) FanOutOption[T] {
	return func(c *fanOutCore[T]) {
		c.inputChan = make(chan T, size)
		c.selfOwnIn = true
	}
}

// applyOpts applies common functional options to the core.
func applyOpts[T any](c *fanOutCore[T], opts []FanOutOption[T]) {
	for _, opt := range opts {
		opt(c)
	}
}
