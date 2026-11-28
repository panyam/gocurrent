package gocurrent

import (
	"bytes"
	"log"
	"net"
	"sync"
	"testing"
	"time"
)

// A program that stops a Writer or a Reader on its normal path gets nothing
// on the standard logger. Errors still reach the caller through ClosedChan.
func TestRoutineStopsWriteNothingToTheLog(t *testing.T) {
	var buf lockedBuffer
	defer log.SetOutput(log.Writer())
	log.SetOutput(&buf)

	w := NewWriter(func(int) error { return nil })
	w.Send(1)
	w.Stop()

	r := NewReader(func() (int, error) {
		time.Sleep(time.Millisecond)
		return 0, net.ErrClosed
	})
	time.Sleep(10 * time.Millisecond)
	r.Stop()

	if got := buf.String(); got != "" {
		t.Fatalf("the standard logger got:\n%s", got)
	}
}

// lockedBuffer collects log output. A Reader logs from its own goroutine.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
