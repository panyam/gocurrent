package gocurrent

import (
	"errors"
	"log"
	"log/slog"
	"net"
)

// ReaderFunc is the type of the reader method used by the Reader goroutine primitive.
type ReaderFunc[R any] func() (msg R, err error)

// Reader is a typed Reader goroutine which calls a Read method to return data
// over a channel. It continuously calls the reader function and sends results
// to a channel wrapped in Message structs.
type Reader[R any] struct {
	RunnerBase[string]
	msgChannel chan Message[R]
	Read       ReaderFunc[R]
	closedChan chan error
	OnDone     func(r *Reader[R])
}

// ReaderOption is a functional option for configuring a Reader
type ReaderOption[R any] func(*Reader[R])

// WithOutputBuffer sets the buffer size for the output channel
func WithOutputBuffer[R any](size int) ReaderOption[R] {
	return func(r *Reader[R]) {
		r.msgChannel = make(chan Message[R], size)
	}
}

// WithOnDone sets the callback to be called when the reader finishes
func WithOnDone[R any](fn func(*Reader[R])) ReaderOption[R] {
	return func(r *Reader[R]) {
		r.OnDone = fn
	}
}

// NewReader creates a new reader instance with functional options.
// The reader function is required as the first parameter, with optional
// configuration via functional options.
//
// Examples:
//
//	// Simple usage (backwards compatible)
//	reader := NewReader(myReaderFunc)
//
//	// With options
//	reader := NewReader(myReaderFunc, WithOutputBuffer[int](10))
//
//	// With multiple options
//	reader := NewReader(myReaderFunc,
//	    WithOutputBuffer[int](100),
//	    WithOnDone(func(r *Reader[int]) { log.Println("done") }))
func NewReader[R any](read ReaderFunc[R], opts ...ReaderOption[R]) *Reader[R] {
	out := &Reader[R]{
		RunnerBase: NewRunnerBase("stop"),
		Read:       read,
		closedChan: make(chan error, 1),
		msgChannel: make(chan Message[R]), // default unbuffered
	}

	// Apply options
	for _, opt := range opts {
		opt(out)
	}

	out.start()
	return out
}

func (r *Reader[R]) DebugInfo() any {
	return map[string]any{
		"base":    r.RunnerBase.DebugInfo(),
		"msgChan": r.msgChannel,
	}
}

// OutputChan returns the channel on which messages can be received.
func (rc *Reader[R]) OutputChan() <-chan Message[R] {
	return rc.msgChannel
}

// ClosedChan returns the channel used to signal when the reader is done.
func (rc *Reader[R]) ClosedChan() <-chan error {
	return rc.closedChan
}

func (rc *Reader[R]) start() {
	rc.RunnerBase.start()
	go func() {
		defer rc.cleanup()

		// Channel to signal the inner goroutine to stop
		stopReading := make(chan struct{})

		go func() {
			// Recover from any panics (e.g., send on closed closedChan).
			defer func() { recover() }()
			for {
				// Check if we should stop before calling Read
				select {
				case <-stopReading:
					return
				default:
				}

				newMessage, err := rc.Read()
				timedOut := false
				if err != nil {
					nerr, ok := err.(net.Error)
					if ok {
						timedOut = nerr.Timeout()
					}
					log.Println("Net Error, TimedOut, Closed, errors.Is.ErrClosed: ", nerr, timedOut, errors.Is(err, net.ErrClosed), nil)
				}

				// Try to send, but respect stop signal
				if !timedOut && !errors.Is(err, net.ErrClosed) {
					select {
					case <-stopReading:
						return
					case rc.msgChannel <- Message[R]{
						Value: newMessage,
						Error: err,
					}:
					}
				}

				if err != nil && !timedOut {
					slog.Debug("Read Error: ", "error", err)
					select {
					case <-stopReading:
						return
					case rc.closedChan <- err:
					}
					break
				}
			}
		}()

		// Wait for control signal to stop
		<-rc.controlChan
		// Signal the reading goroutine to stop. It will exit when Read()
		// returns and it sees stopReading closed. We don't wait for it
		// because Read() may block indefinitely (e.g., network read).
		close(stopReading)
	}()
}

func (r *Reader[T]) cleanup() {
	defer log.Println("Cleaned up reader...")
	if r.OnDone != nil {
		r.OnDone(r)
	}
	// msgChannel is NOT closed here to avoid racing with the inner goroutine
	// which may still be sending to it (Read() can block indefinitely).
	// Consumers should use ClosedChan() or Done() to detect completion.
	close(r.closedChan)
	r.RunnerBase.cleanup()
}
