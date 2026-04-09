package gocurrent

// AsyncFanOut distributes events to all registered output channels by
// spawning a separate goroutine for each output on every event.
//
// Ordering semantics — fire and forget per output:
//
//	Time ──────────────────────────────────────────────────────────────►
//
//	Sender:  Send(A) ──► Send(B) ──► ...
//	             │           │
//	             │           ├─ goroutine: B → out[0]  ← may arrive BEFORE A!
//	             │           ├─ goroutine: B → out[1]
//	             │           └─ goroutine: B → out[2]
//	             │
//	             ├─ goroutine: A → out[0]  ← may arrive AFTER B!
//	             ├─ goroutine: A → out[1]
//	             └─ goroutine: A → out[2]
//
//	NO guarantee: A and B goroutines race. B can arrive before A on any output.
//
//   - Sender never blocks (goroutines are spawned immediately).
//   - NO ordering guarantee: goroutines for event B may execute before
//     goroutines for event A. The Go scheduler does not guarantee FIFO
//     ordering of goroutine execution.
//   - Goroutine count: N (outputs) per event. Can explode under high
//     throughput with many outputs.
//   - A slow output does NOT affect delivery to other outputs.
//
// Use AsyncFanOut only when event ordering does not matter and per-output
// independence is more important than resource usage. In most cases,
// [QueuedFanOut] is a better choice.
type AsyncFanOut[T any] struct {
	fanOutCore[T]
}

// NewAsyncFanOut creates an AsyncFanOut that spawns a goroutine per output
// per event. The fan-out starts running immediately.
//
// Options:
//   - [WithFanOutInputChan]: use an existing input channel (caller-owned)
//   - [WithFanOutInputBuffer]: create a buffered input channel
//
// Example:
//
//	fo := NewAsyncFanOut[int]()
//	defer fo.Stop()
//	out := fo.New(nil)
//	fo.Send(42)
//	val := <-out // 42
func NewAsyncFanOut[T any](opts ...FanOutOption[T]) *AsyncFanOut[T] {
	fo := &AsyncFanOut[T]{}
	applyOpts(&fo.fanOutCore, opts)
	fo.initCore()
	fo.start()
	return fo
}

func (fo *AsyncFanOut[T]) start() {
	fo.RunnerBase.start()

	go func() {
		defer fo.cleanup()

		for {
			select {
			case event := <-fo.inputChan:
				for index, outputChan := range fo.outputChans {
					if outputChan == nil {
						continue
					}
					if fo.outputFilters[index] != nil {
						if newevent := fo.outputFilters[index](&event); newevent != nil {
							go func(ch chan<- T, evt T) { ch <- evt }(outputChan, *newevent)
						}
					} else {
						go func(ch chan<- T, evt T) { ch <- evt }(outputChan, event)
					}
				}
			case cmd := <-fo.controlChan:
				if fo.handleCmd(cmd) {
					return
				}
			}
		}
	}()
}
