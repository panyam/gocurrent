package gocurrent

import (
	"log"
	"sync"
	"time"
)

// Reducer is a way to collect messages of type T in some kind of window
// and reduce them to type U. For example this could be used to batch messages
// into a list every 10 seconds. Alternatively if a time based window is not
// used a reduction can be invoked manually.
type Reducer[T any, C any, U any] struct {
	FlushPeriod time.Duration
	// CollectFunc adds an input to the collection and returns the updated collection.
	// The bool return value indicates whether a flush should be triggered immediately.
	CollectFunc   func(input T, collection C) (C, bool)
	ReduceFunc    func(collectedItems C) (reducedOutputs U)
	pendingEvents C
	selfOwnIn     bool
	inputChan     chan T
	selfOwnOut    bool
	outputChan    chan U
	cmdChan       chan reducerCmd[U]
	wg            sync.WaitGroup
}

type reducerCmd[T any] struct {
	Name    string
	Channel chan T
}

// ReducerOption is a functional option for configuring a Reducer
type ReducerOption[T any, C any, U any] func(*Reducer[T, C, U])

// WithFlushPeriod sets the flush period for the reducer
func WithFlushPeriod[T any, C any, U any](period time.Duration) ReducerOption[T, C, U] {
	return func(r *Reducer[T, C, U]) {
		r.FlushPeriod = period
	}
}

// WithInputChan sets the input channel for the reducer
func WithInputChan[T any, C any, U any](ch chan T) ReducerOption[T, C, U] {
	return func(r *Reducer[T, C, U]) {
		r.inputChan = ch
		r.selfOwnIn = false
	}
}

// WithOutputChan sets the output channel for the reducer
func WithOutputChan[T any, C any, U any](ch chan U) ReducerOption[T, C, U] {
	return func(r *Reducer[T, C, U]) {
		r.outputChan = ch
		r.selfOwnOut = false
	}
}

// NewReducer creates a reducer over generic input and output types. Options can be
// provided to configure the input channel, output channel, flush period, etc.
// If channels are not provided via options, the reducer will create and own them.
// Just like other runners, the Reducer starts as soon as it is created.
func NewReducer[T any, C any, U any](opts ...ReducerOption[T, C, U]) *Reducer[T, C, U] {
	out := &Reducer[T, C, U]{
		FlushPeriod: 100 * time.Millisecond,
		cmdChan:     make(chan reducerCmd[U]),
		selfOwnIn:   true,
		selfOwnOut:  true,
	}
	// Apply options
	for _, opt := range opts {
		opt(out)
	}
	// Create channels if not provided via options
	if out.inputChan == nil {
		out.inputChan = make(chan T)
	}
	if out.outputChan == nil {
		out.outputChan = make(chan U)
	}
	out.start()
	return out
}

// NewIDReducer creates a Reducer that simply collects events of type T into a list (of type []T).
func NewIDReducer[T any](opts ...ReducerOption[T, []T, []T]) *Reducer[T, []T, []T] {
	out := NewReducer(opts...)
	out.ReduceFunc = IDFunc[[]T]
	out.CollectFunc = func(input T, collection []T) ([]T, bool) {
		return append(collection, input), false
	}
	return out
}

// A reducer that collects a list of items and concats them to a collection
// This allows producers to send events here in batch mode instead of 1 at a time
func NewListReducer[T any](opts ...ReducerOption[[]T, []T, []T]) *Reducer[[]T, []T, []T] {
	out := NewReducer(opts...)
	out.ReduceFunc = IDFunc[[]T]
	out.CollectFunc = func(input []T, collection []T) ([]T, bool) {
		return append(collection, input...), false
	}
	return out
}

// Gets the channel from which we can read "reduced" values from
func (fo *Reducer[T, C, U]) RecvChan() <-chan U {
	return fo.outputChan
}

// SendChan returns the channel onto which messages can be sent (to be reduced).
func (fo *Reducer[T, C, U]) SendChan() chan<- T {
	return fo.inputChan
}

// Send sends a message/value onto this reducer for (eventual) reduction.
func (fo *Reducer[T, C, U]) Send(value T) {
	fo.inputChan <- value
}

// Stop stops the reducer and closes all channels it owns.
func (fo *Reducer[T, C, U]) Stop() {
	fo.cmdChan <- reducerCmd[U]{Name: "stop"}
	fo.wg.Wait()
}

func (fo *Reducer[T, C, U]) start() {
	ticker := time.NewTicker(fo.FlushPeriod)
	fo.wg.Add(1)
	go func() {
		// keep reading from input and send to outputs
		defer func() {
			defer ticker.Stop()
			if fo.selfOwnIn {
				close(fo.inputChan)
				fo.inputChan = nil
			}
			fo.wg.Done()
		}()
		for {
			select {
			case event := <-fo.inputChan:
				var shouldFlush bool
				fo.pendingEvents, shouldFlush = fo.CollectFunc(event, fo.pendingEvents)
				if shouldFlush {
					fo.Flush()
				}
			case <-ticker.C:
				// Flush
				fo.Flush()
			case cmd := <-fo.cmdChan:
				if cmd.Name == "stop" {
					return
				}
			}
		}
	}()
}

// Flush immediately processes all pending events and sends the result to the output channel.
func (fo *Reducer[T, C, U]) Flush() {
	log.Printf("Flushing messages.")
	joinedEvents := fo.ReduceFunc(fo.pendingEvents)
	var zero C
	fo.pendingEvents = zero
	fo.outputChan <- joinedEvents
}
