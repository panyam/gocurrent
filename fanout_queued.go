package gocurrent

import (
	"log"
	"sync"
)

// DefaultQueueSize is the default capacity of the dispatch queue used by
// [QueuedFanOut]. The queue acts as a buffer between the runner goroutine
// (which reads events from the input channel) and the dispatch goroutine
// (which delivers events to outputs). When the queue is full, the runner
// blocks — propagating back-pressure to the sender.
const DefaultQueueSize = 64

// outputSnapshot is an immutable point-in-time copy of the output channel
// and filter slices. It is rebuilt only when Add or Remove modifies the
// output list, avoiding per-event allocations. The dispatch goroutine
// works exclusively from snapshots, eliminating races with the runner
// goroutine that manages subscriber changes.
type outputSnapshot[T any] struct {
	chans   []chan<- T
	filters []FilterFunc[T]
}

// dispatchItem pairs a snapshot with an event. The runner goroutine sends
// these to the dispatch goroutine via the buffered dispatchChan.
type dispatchItem[T any] struct {
	snapshot outputSnapshot[T]
	event    T
}

// QueuedFanOut distributes events to all registered output channels via a
// persistent dispatch goroutine reading from a buffered queue.
//
// This is the recommended fan-out strategy for most use cases.
//
// Ordering semantics — ordered pipeline:
//
//	Time ──────────────────────────────────────────────────────────────►
//
//	Sender:  Send(A) ──► Send(B) ──► Send(C) ──►  ...   (never blocks unless queue full)
//	             │           │           │
//	             └──────► dispatchChan (buffered queue, default 64) ◄──┘
//	                           │
//	                      Dispatch goroutine (single, persistent):
//	                           ├─ read A: deliver to out[0], out[1], out[2]
//	                           ├─ read B: deliver to out[0], out[1], out[2]  ← only after A is done
//	                           └─ read C: deliver to out[0], out[1], out[2]
//
//	Guarantee: A is fully delivered to ALL outputs before B begins.
//	           Sender is decoupled — it only blocks when the queue is full.
//
//   - Sender blocks only when the dispatch queue is full (configurable,
//     default 64). This propagates back-pressure without silently dropping events.
//   - Strict FIFO: the single dispatch goroutine processes events sequentially.
//     Event A is delivered to ALL outputs before event B begins delivery.
//   - Two goroutines total (runner + dispatcher), regardless of event volume.
//   - A slow output blocks delivery of the current event to remaining outputs
//     AND delays delivery of subsequent events in the queue.
//
// The subscriber list is captured as an immutable [outputSnapshot] on each
// Add/Remove. The dispatch goroutine always works from the snapshot bundled
// with each event, so there is no race between subscriber management and
// event delivery, and zero per-event allocations for the subscriber list.
//
// When an output is removed, it is added to a concurrent "removed set"
// (sync.Map). The dispatch goroutine checks this set before each send and
// skips removed outputs. Self-owned channels are closed during Stop after
// the dispatch goroutine has fully exited.
type QueuedFanOut[T any] struct {
	fanOutCore[T]
	dispatchChan     chan dispatchItem[T]
	dispatchDone     chan struct{}
	stopDispatch     chan struct{} // closed by runner to unblock dispatch sends
	snapshot         outputSnapshot[T]
	queueSize        int
	removed          sync.Map    // chan<- T → struct{}: channels removed but maybe in old snapshots
	removedSelfOwned []chan<- T  // self-owned removed channels, closed during cleanup
}

// QueuedFanOutOption is a functional option specific to [QueuedFanOut].
type QueuedFanOutOption[T any] func(*QueuedFanOut[T])

// WithQueueSize sets the capacity of the dispatch queue (default 64).
// A larger queue allows more events to be buffered before the sender
// blocks, at the cost of higher memory usage.
func WithQueueSize[T any](size int) QueuedFanOutOption[T] {
	return func(fo *QueuedFanOut[T]) {
		fo.queueSize = size
	}
}

// NewQueuedFanOut creates a QueuedFanOut that delivers events via a
// persistent dispatch goroutine with strict FIFO ordering. The fan-out
// starts running immediately.
//
// Accepts both common [FanOutOption] and [QueuedFanOutOption] options:
//   - [WithFanOutInputChan]: use an existing input channel (caller-owned)
//   - [WithFanOutInputBuffer]: create a buffered input channel
//   - [WithQueueSize]: set the dispatch queue capacity (default 64)
//
// Example:
//
//	fo := NewQueuedFanOut[int]()
//	defer fo.Stop()
//	out := fo.New(nil)
//	fo.Send(42)
//	val := <-out // 42
func NewQueuedFanOut[T any](opts ...any) *QueuedFanOut[T] {
	fo := &QueuedFanOut[T]{
		queueSize: DefaultQueueSize,
	}

	for _, opt := range opts {
		switch o := opt.(type) {
		case FanOutOption[T]:
			o(&fo.fanOutCore)
		case QueuedFanOutOption[T]:
			o(fo)
		}
	}

	fo.initCore()
	fo.dispatchChan = make(chan dispatchItem[T], fo.queueSize)
	fo.dispatchDone = make(chan struct{})
	fo.stopDispatch = make(chan struct{})
	fo.start()
	return fo
}

// DebugInfo returns diagnostic information including queue depth, which is
// useful for debugging back-pressure issues and understanding dispatch state.
func (fo *QueuedFanOut[T]) DebugInfo() any {
	return map[string]any{
		"inputChan":     fo.inputChan,
		"outputChans":   fo.outputChans,
		"queueSize":     fo.queueSize,
		"queueLen":      len(fo.dispatchChan),
		"queueCap":      cap(fo.dispatchChan),
		"snapshotChans": len(fo.snapshot.chans),
	}
}

// rebuildSnapshot creates a new immutable snapshot from the current output
// channel and filter slices. Called after Add/Remove in the runner goroutine.
func (fo *QueuedFanOut[T]) rebuildSnapshot() {
	chans := make([]chan<- T, len(fo.outputChans))
	copy(chans, fo.outputChans)
	filters := make([]FilterFunc[T], len(fo.outputFilters))
	copy(filters, fo.outputFilters)
	fo.snapshot = outputSnapshot[T]{chans: chans, filters: filters}
}

// handleCmd overrides the core handleCmd. For Remove, self-owned channels
// are NOT closed immediately — they are added to the removed set (so the
// dispatch goroutine skips them) and tracked for closure during cleanup.
func (fo *QueuedFanOut[T]) handleCmd(cmd fanOutCmd[T]) (shouldStop bool) {
	if cmd.Name == "stop" {
		return true
	}

	if cmd.Name == "add" {
		found := false
		for _, oc := range fo.outputChans {
			if oc == cmd.AddedChannel {
				found = true
				log.Println("Output Channel already exists. Will skip.", cmd.AddedChannel)
				break
			}
		}
		if !found {
			fo.outputChans = append(fo.outputChans, cmd.AddedChannel)
			fo.outputSelfOwned = append(fo.outputSelfOwned, cmd.SelfOwned)
			fo.outputFilters = append(fo.outputFilters, cmd.Filter)
		}
		if cmd.CallbackChan != nil {
			cmd.CallbackChan <- nil
		}
		fo.rebuildSnapshot()
	} else if cmd.Name == "remove" {
		for index, ch := range fo.outputChans {
			if ch == cmd.RemovedChannel {
				// Mark as removed so dispatch goroutine skips it in old snapshots
				fo.removed.Store(ch, struct{}{})
				if fo.outputSelfOwned[index] {
					fo.removedSelfOwned = append(fo.removedSelfOwned, ch)
				}
				// Remove from slices (swap with last element)
				last := len(fo.outputChans) - 1
				fo.outputSelfOwned[index] = fo.outputSelfOwned[last]
				fo.outputSelfOwned = fo.outputSelfOwned[:last]
				fo.outputChans[index] = fo.outputChans[last]
				fo.outputChans = fo.outputChans[:last]
				fo.outputFilters[index] = fo.outputFilters[last]
				fo.outputFilters = fo.outputFilters[:last]
				break
			}
		}
		if cmd.CallbackChan != nil {
			cmd.CallbackChan <- nil
		}
		fo.rebuildSnapshot()
	}
	return false
}

// enqueue tries to write item to dispatchChan. While the queue is full it
// continues to process control commands (Add/Remove/Stop) so that Remove
// can unblock the pipeline. Returns false if a stop command was received.
func (fo *QueuedFanOut[T]) enqueue(item dispatchItem[T]) bool {
	for {
		select {
		case fo.dispatchChan <- item:
			return true
		case cmd := <-fo.controlChan:
			if fo.handleCmd(cmd) {
				return false
			}
			item.snapshot = fo.snapshot
		}
	}
}

func (fo *QueuedFanOut[T]) start() {
	fo.RunnerBase.start()

	// Dispatch goroutine — reads from dispatchChan and delivers to outputs.
	// Checks the removed set before each send to skip outputs removed after
	// the snapshot was taken. Every send uses a select on Done() so that
	// Stop() can interrupt a blocked delivery (e.g., when a removed output's
	// reader has stopped but the dispatch goroutine was already mid-send).
	go func() {
		defer close(fo.dispatchDone)
		stop := fo.stopDispatch
		for item := range fo.dispatchChan {
			for index, outputChan := range item.snapshot.chans {
				if outputChan == nil {
					continue
				}
				if _, removed := fo.removed.Load(outputChan); removed {
					continue
				}
				var val T
				if item.snapshot.filters[index] != nil {
					newevent := item.snapshot.filters[index](&item.event)
					if newevent == nil {
						continue
					}
					val = *newevent
				} else {
					val = item.event
				}
				select {
				case outputChan <- val:
				case <-stop:
					return
				}
			}
		}
	}()

	// Runner goroutine — reads events from inputChan, enqueues dispatch items.
	go func() {
		defer func() {
			close(fo.stopDispatch) // unblock dispatch goroutine's blocked sends
			close(fo.dispatchChan) // tell dispatch goroutine to stop iterating
			<-fo.dispatchDone      // wait for dispatch goroutine to exit

			// Safe to close removed self-owned channels now
			for _, ch := range fo.removedSelfOwned {
				close(ch)
			}
			fo.cleanup()
		}()

		for {
			select {
			case event := <-fo.inputChan:
				item := dispatchItem[T]{
					snapshot: fo.snapshot,
					event:    event,
				}
				if !fo.enqueue(item) {
					return
				}
			case cmd := <-fo.controlChan:
				if fo.handleCmd(cmd) {
					return
				}
			}
		}
	}()
}
