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

// NewReader creates a new reader instance. Just like time.Ticker, this initializer
// also starts the Reader loop. It is up to the caller to Stop this reader when
// done with it. Not doing so can risk the reader to run indefinitely.
func NewReader[R any](read ReaderFunc[R]) *Reader[R] {
	out := Reader[R]{
		RunnerBase: NewRunnerBase("stop"),
		Read:       read,
		closedChan: make(chan error, 1),
		msgChannel: make(chan Message[R]),
	}
	out.start()
	return &out
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

// The channel used to signal when the reader is done
func (rc *Reader[R]) ClosedChan() <-chan error {
	return rc.closedChan
}

func (rc *Reader[R]) start() {
	rc.RunnerBase.start()
	go func() {
		defer rc.cleanup()
		go func() {
			for {
				newMessage, err := rc.Read()
				timedOut := false
				if err != nil {
					nerr, ok := err.(net.Error)
					if ok {
						timedOut = nerr.Timeout()
					}
					log.Println("Net Error, TimedOut, Closed, errors.Is.ErrClosed: ", nerr, timedOut, errors.Is(err, net.ErrClosed), nil)
				}
				if rc.msgChannel != nil && !timedOut && !errors.Is(err, net.ErrClosed) {
					rc.msgChannel <- Message[R]{
						Value: newMessage,
						Error: err,
					}
				}
				if err != nil && !timedOut {
					slog.Debug("Read Error: ", "error", err)
					rc.closedChan <- err
					break
				}
			}
		}()

		// and the actual reader
		for {
			select {
			case <-rc.controlChan:
				// For now only a "kill" can be sent here
				return
			}
		}
	}()
}

func (r *Reader[T]) cleanup() {
	defer log.Println("Cleaned up reader...")
	if r.OnDone != nil {
		r.OnDone(r)
	}
	oldCh := r.msgChannel
	r.msgChannel = nil
	close(oldCh)
	close(r.closedChan)
	r.RunnerBase.cleanup()
}
