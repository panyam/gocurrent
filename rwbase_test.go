package gocurrent

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// TestWriterStopAfterWriteError reproduces the WebSocket close crash:
// Writer goroutine self-terminates because Write returns an error,
// then Stop() is called externally (e.g. from OnClose). Before the fix,
// Stop() would panic with "send on closed channel" because cleanup()
// already closed the control channel.
func TestWriterStopAfterWriteError(t *testing.T) {
	writeErr := make(chan struct{})
	writer := NewWriter(func(val int) error {
		close(writeErr) // signal that we're about to return an error
		return errors.New("websocket: close sent")
	})

	// Send a message that will trigger the write error
	writer.Send(1)

	// Wait for the writer to self-terminate
	<-writeErr
	select {
	case <-writer.ClosedChan():
		// Writer has self-terminated
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for writer to self-terminate")
	}

	// Now call Stop() — this simulates OnClose() calling Writer.Stop()
	// after the writer goroutine already cleaned up.
	// Before the fix, this panicked with "send on closed channel".
	err := writer.Stop()
	if err != nil {
		t.Errorf("Expected nil error from Stop(), got: %v", err)
	}
}

// TestWriterStopAfterWriteErrorConcurrent tests the race where Stop()
// is called while the writer goroutine is still in its cleanup phase.
func TestWriterStopAfterWriteErrorConcurrent(t *testing.T) {
	for i := 0; i < 100; i++ {
		writeErr := make(chan struct{})
		writer := NewWriter(func(val int) error {
			close(writeErr)
			return errors.New("connection closed")
		})

		writer.Send(1)
		<-writeErr

		// Don't wait for ClosedChan — call Stop() immediately to
		// maximize the chance of racing with cleanup().
		err := writer.Stop()
		if err != nil {
			t.Errorf("Iteration %d: expected nil error from Stop(), got: %v", i, err)
		}
	}
}

// TestDoubleStop verifies that calling Stop() twice doesn't panic or deadlock.
func TestDoubleStop(t *testing.T) {
	writer := NewWriter(func(val int) error {
		return nil
	})

	err := writer.Stop()
	if err != nil {
		t.Errorf("First Stop() returned error: %v", err)
	}

	err = writer.Stop()
	if err != nil {
		t.Errorf("Second Stop() returned error: %v", err)
	}
}

// TestReaderStopAfterSelfTerminate tests Reader where the read function
// returns an error, causing self-termination, then Stop() is called.
func TestReaderStopAfterSelfTerminate(t *testing.T) {
	reader := NewReader(func() (int, error) {
		return 0, errors.New("connection reset")
	})

	// Wait for the reader to self-terminate (read error)
	time.Sleep(50 * time.Millisecond)

	// Stop should not panic
	err := reader.Stop()
	if err != nil {
		t.Errorf("Expected nil error from Stop(), got: %v", err)
	}
}

// TestWriterSendDuringCleanup verifies that calling Send() while the writer
// goroutine is cleaning up (due to a write error) does not panic or race.
// This catches the TOCTOU race where Send() passes the IsRunning() check
// but cleanup() closes/nils msgChannel before the actual channel send.
// Run with: go test -race -run TestWriterSendDuringCleanup
func TestWriterSendDuringCleanup(t *testing.T) {
	for i := 0; i < 200; i++ {
		errored := make(chan struct{})
		writer := NewWriter(func(val int) error {
			if val == 1 {
				close(errored)
				return errors.New("write failed")
			}
			return nil
		})

		// First send triggers the error and self-termination
		writer.Send(1)
		<-errored

		// Second send races with cleanup — must not panic or race
		writer.Send(2)
		writer.Stop()
	}
}

// TestReaderStopDuringRead verifies that calling Stop() on a Reader while
// its read function is blocked does not cause a data race. The read function
// blocks until signaled, and Stop() is called concurrently to exercise the
// race between the reader goroutine and the stop path.
// Run with: go test -race -run TestReaderStopDuringRead
func TestReaderStopDuringRead(t *testing.T) {
	for i := 0; i < 100; i++ {
		unblock := make(chan struct{})
		reader := NewReader(func() (int, error) {
			<-unblock
			return 0, errors.New("done")
		})

		// Give the reader goroutine time to enter Read()
		time.Sleep(time.Millisecond)

		// Stop and unblock concurrently — exercises the race
		go func() { close(unblock) }()
		reader.Stop()
	}
}

// TestMapperSelfTerminateThenStop verifies that closing a Mapper's input
// channel (causing self-termination via the "ok" check) followed by an
// external Stop() call does not race. The Mapper's goroutine exits when
// the input channel is closed, and Stop() must coordinate with cleanup().
// Run with: go test -race -run TestMapperSelfTerminateThenStop
func TestMapperSelfTerminateThenStop(t *testing.T) {
	for i := 0; i < 200; i++ {
		input := make(chan int)
		output := make(chan int, 100)
		mapper := NewMapper(input, output, func(i int) (int, bool, bool) {
			return i, false, false
		})

		// Close input — mapper self-terminates
		close(input)

		// Immediate Stop() races with cleanup
		mapper.Stop()
	}
}

// TestConcurrentStopAndSelfTerminate is a stress test running 1000 iterations
// where a Writer self-terminates (write error) and Stop() is called from a
// separate goroutine simultaneously. This maximizes the probability of hitting
// the race between Stop()'s controlChan send and cleanup()'s controlChan close.
// Run with: go test -race -run TestConcurrentStopAndSelfTerminate
func TestConcurrentStopAndSelfTerminate(t *testing.T) {
	for i := 0; i < 1000; i++ {
		started := make(chan struct{})
		writer := NewWriter(func(val int) error {
			close(started)
			return errors.New("fail")
		})

		var wg sync.WaitGroup
		wg.Add(2)

		// Goroutine 1: send a message that triggers self-termination
		go func() {
			defer wg.Done()
			writer.Send(1)
		}()

		// Goroutine 2: call Stop() as soon as the write starts
		go func() {
			defer wg.Done()
			<-started
			writer.Stop()
		}()

		wg.Wait()
	}
}
