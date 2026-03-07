package gocurrent

import (
	"errors"
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
