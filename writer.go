package gocurrent

import (
	"log"
)

// WriterFunc is the type of the writer method used by the writer goroutine primitive to serialize its writes.
type WriterFunc[W any] func(W) error

// Writer is a typed Writer goroutine type which calls the Write method when it serializes its writes.
// It provides a way to serialize concurrent writes through a single goroutine.
type Writer[W any] struct {
	RunnerBase[string]
	msgChannel chan W
	Write      WriterFunc[W]
	closedChan chan error
}

// WriterOption is a functional option for configuring a Writer
type WriterOption[W any] func(*Writer[W])

// WithInputBuffer sets the buffer size for the input channel
func WithInputBuffer[W any](size int) WriterOption[W] {
	return func(w *Writer[W]) {
		w.msgChannel = make(chan W, size)
	}
}

// NewWriter creates a new writer instance with functional options.
// The writer function is required as the first parameter, with optional
// configuration via functional options.
//
// Examples:
//
//	// Simple usage (backwards compatible)
//	writer := NewWriter(myWriterFunc)
//
//	// With buffered input
//	writer := NewWriter(myWriterFunc, WithInputBuffer[int](100))
func NewWriter[W any](write WriterFunc[W], opts ...WriterOption[W]) *Writer[W] {
	out := &Writer[W]{
		RunnerBase: NewRunnerBase("stop"),
		Write:      write,
		msgChannel: make(chan W), // default unbuffered
		closedChan: make(chan error, 1),
	}

	// Apply options
	for _, opt := range opts {
		opt(out)
	}

	out.start()
	return out
}

func (w *Writer[W]) DebugInfo() any {
	return map[string]any{
		"base":    w.RunnerBase.DebugInfo(),
		"msgChan": w.msgChannel,
	}
}

func (ch *Writer[T]) cleanup() {
	log.Println("Cleaning up writer...")
	v := ch.msgChannel
	defer log.Println("Finished cleaning up writer: ", v)
	// msgChannel is NOT closed here — blocked Send() calls will see Done()
	// and return false, avoiding the concurrent close+send race.
	close(ch.closedChan)
	ch.RunnerBase.cleanup()
}

// InputChan returns the channel on which messages can be sent to the Writer.
// The returned channel is never nil after construction. Callers should prefer
// Send() for safe access that handles the writer being stopped.
func (wc *Writer[W]) InputChan() chan<- W {
	return wc.msgChannel
}

// Send sends a message to the Writer. Returns true if the message was accepted,
// false if the writer is stopped. Uses a select on Done() to safely unblock if
// the writer stops while the send is pending.
func (wc *Writer[W]) Send(req W) bool {
	if !wc.IsRunning() {
		return false
	}
	select {
	case wc.msgChannel <- req:
		return true
	case <-wc.Done():
		return false
	}
}

// ClosedChan returns the channel used to signal when the writer is done
func (wc *Writer[W]) ClosedChan() <-chan error {
	return wc.closedChan
}

// start launches the writer goroutine
func (wc *Writer[W]) start() {
	wc.RunnerBase.start()
	go func() {
		defer wc.cleanup()
		for {
			select {
			case newRequest := <-wc.msgChannel:
				err := wc.Write(newRequest)
				if err != nil {
					log.Println("Write Error: ", err)
					wc.closedChan <- err
					return
				}
			case controlRequest := <-wc.controlChan:
				log.Println("Received kill signal.  Quitting Writer.", controlRequest, wc.InputChan())
				return
			}
		}
	}()
}
