package gocurrent

import (
	"errors"
	"testing"
	"time"
)

// TestReaderRace_RapidStopDuringError rapidly creates and stops Readers
// where the Read function returns an error. This reproduces a race between
// cleanup() closing closedChan (reader.go:161) and the inner goroutine
// sending to closedChan (reader.go:137).
//
// See: https://github.com/panyam/gocurrent/issues/4
//
// The race occurs because:
//  1. Read() returns an error
//  2. Inner goroutine tries to send error to closedChan (line 137)
//  3. Concurrently, controlChan receives stop signal (line 145)
//  4. close(stopReading) is called (line 149)
//  5. cleanup() runs and closes closedChan (line 161)
//  6. Step 2 and step 5 race on closedChan
//
// Run with: go test -run TestReaderRace -race -count=1
func TestReaderRace_RapidStopDuringError(t *testing.T) {
	errFake := errors.New("simulated read error")

	for i := 0; i < 500; i++ {
		// Create a channel-based reader so we control exactly when errors happen
		errCh := make(chan struct{})

		reader := NewReader(func() (int, error) {
			select {
			case <-errCh:
				return 0, errFake
			}
		})

		// Trigger the error and stop nearly simultaneously to maximize
		// the window where closedChan send (line 137) races with
		// closedChan close (line 161)
		go func() { close(errCh) }()
		time.Sleep(time.Microsecond)
		reader.Stop()
	}
}

// TestReaderRace_StopDuringBlockingRead simulates the WebSocket pattern:
// Read() blocks on a channel, then the channel is closed (simulating
// connection close) causing Read() to return an error, while Stop() is
// called concurrently. The inner goroutine tries to send the error to
// closedChan while cleanup() closes it.
//
// See: https://github.com/panyam/gocurrent/issues/4
func TestReaderRace_StopDuringBlockingRead(t *testing.T) {
	for i := 0; i < 500; i++ {
		ch := make(chan int)

		reader := NewReader(func() (int, error) {
			val, ok := <-ch
			if !ok {
				return 0, errors.New("connection closed")
			}
			return val, nil
		})

		// Drain output so reader doesn't block on msgChannel send
		go func() {
			for range reader.OutputChan() {
			}
		}()

		// Send a few values to get the reader goroutine into a rhythm
		for j := 0; j < 3; j++ {
			ch <- j
		}

		// Now close the channel and stop nearly simultaneously.
		// The reader goroutine will see the close, get an error,
		// and try to send to closedChan — racing with cleanup().
		go func() {
			time.Sleep(time.Microsecond)
			close(ch)
		}()
		time.Sleep(2 * time.Microsecond)
		reader.Stop()
	}
}
