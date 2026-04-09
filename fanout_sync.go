package gocurrent

// SyncFanOut distributes events to all registered output channels
// synchronously within the runner goroutine.
//
// Ordering semantics — sender waits for everyone:
//
//	Time ──────────────────────────────────────────────────────────────►
//
//	Sender:  Send(A) ─────────────────────────────────► Send(B) ───────────────────────► ...
//	                  │                                          │
//	                  ├─ deliver A to out[0] (blocks if full)    ├─ deliver B to out[0]
//	                  ├─ deliver A to out[1] (blocks if full)    ├─ deliver B to out[1]
//	                  └─ deliver A to out[2] (blocks if full)    └─ deliver B to out[2]
//
//	Guarantee: A is fully delivered to ALL outputs before B begins.
//
//   - Sender blocks until ALL outputs have received the event.
//   - Strict FIFO: event A is delivered to every output before event B.
//   - Zero extra goroutines — everything runs in the single runner goroutine.
//   - A slow output blocks the sender AND all other outputs.
//
// Use SyncFanOut when the number of outputs is small, outputs are fast, and
// back-pressure to the sender is desirable.
type SyncFanOut[T any] struct {
	fanOutCore[T]
}

// NewSyncFanOut creates a SyncFanOut that delivers events to all outputs
// synchronously. The fan-out starts running immediately.
//
// Options:
//   - [WithFanOutInputChan]: use an existing input channel (caller-owned)
//   - [WithFanOutInputBuffer]: create a buffered input channel
//
// Example:
//
//	fo := NewSyncFanOut[int]()
//	defer fo.Stop()
//	out := fo.New(nil)
//	fo.Send(42)
//	val := <-out // 42
func NewSyncFanOut[T any](opts ...FanOutOption[T]) *SyncFanOut[T] {
	fo := &SyncFanOut[T]{}
	applyOpts(&fo.fanOutCore, opts)
	fo.initCore()
	fo.start()
	return fo
}

func (fo *SyncFanOut[T]) start() {
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
							outputChan <- *newevent
						}
					} else {
						outputChan <- event
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
