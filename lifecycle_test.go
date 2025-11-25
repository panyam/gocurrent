package gocurrent

import (
	"testing"
	"time"
)

// TestMapperClosedChan verifies that Mapper signals completion via ClosedChan
func TestMapperClosedChan(t *testing.T) {
	input := make(chan int)
	output := make(chan int)

	mapper := NewMapper(input, output, func(i int) (int, bool, bool) {
		return i * 2, false, false
	})

	// Close input to trigger mapper completion
	close(input)

	// Wait for completion signal
	select {
	case err := <-mapper.ClosedChan():
		if err != nil {
			t.Errorf("Expected nil error, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for Mapper to close")
	}
}

// TestMapperOnDoneCallback verifies that Mapper calls OnDone callback
func TestMapperOnDoneCallback(t *testing.T) {
	input := make(chan int)
	output := make(chan int)

	called := false
	mapper := NewMapper(input, output, func(i int) (int, bool, bool) {
		return i, false, false
	})
	mapper.OnDone = func(m *Mapper[int, int]) {
		called = true
	}

	// Close input to trigger completion
	close(input)

	// Wait for completion
	<-mapper.ClosedChan()

	if !called {
		t.Error("OnDone callback was not called")
	}
}

// TestFanInClosedChan verifies that FanIn signals completion via ClosedChan
func TestFanInClosedChan(t *testing.T) {
	fanin := NewFanIn[int]()

	// Add some input channels
	in1 := make(chan int)
	in2 := make(chan int)
	fanin.Add(in1, in2)

	// Close inputs
	close(in1)
	close(in2)

	// Stop FanIn
	fanin.Stop()

	// Wait for completion signal
	select {
	case err := <-fanin.ClosedChan():
		if err != nil {
			t.Errorf("Expected nil error, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for FanIn to close")
	}
}

// TestFanOutClosedChan verifies that FanOut signals completion via ClosedChan
func TestFanOutClosedChan(t *testing.T) {
	fanout := NewFanOut[int]()

	// Add some outputs
	out1 := fanout.New(nil)
	out2 := fanout.New(nil)

	// Drain outputs in background
	go func() {
		for range out1 {
		}
	}()
	go func() {
		for range out2 {
		}
	}()

	// Stop FanOut
	fanout.Stop()

	// Wait for completion signal
	select {
	case err := <-fanout.ClosedChan():
		if err != nil {
			t.Errorf("Expected nil error, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for FanOut to close")
	}
}

// TestReducerClosedChan verifies that Reducer signals completion via ClosedChan
func TestReducerClosedChan(t *testing.T) {
	reducer := NewIDReducer[int]()

	// Send some data
	reducer.Send(1)
	reducer.Send(2)
	reducer.Send(3)

	// Stop reducer
	reducer.Stop()

	// Wait for completion signal
	select {
	case err := <-reducer.ClosedChan():
		if err != nil {
			t.Errorf("Expected nil error, got: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Timeout waiting for Reducer to close")
	}
}

// TestStandardizedNaming verifies OutputChan and InputChan work correctly
func TestStandardizedNaming(t *testing.T) {
	// Test Reader.OutputChan()
	t.Run("Reader.OutputChan", func(t *testing.T) {
		calls := 0
		reader := NewReader(func() (int, error) {
			calls++
			return calls, nil
		})

		// Drain in background
		done := make(chan bool)
		go func() {
			count := 0
			for msg := range reader.OutputChan() {
				if count == 0 && msg.Value != 1 {
					t.Errorf("Expected 1, got %d", msg.Value)
				}
				count++
				if count >= 3 {
					close(done)
					return
				}
			}
		}()

		// Wait for a few reads
		<-done
		reader.Stop()
	})

	// Test Writer.InputChan()
	t.Run("Writer.InputChan", func(t *testing.T) {
		received := 0
		writer := NewWriter(func(val int) error {
			received = val
			return nil
		})
		defer writer.Stop()

		// Send via InputChan
		writer.InputChan() <- 42
		time.Sleep(50 * time.Millisecond)

		if received != 42 {
			t.Errorf("Expected 42, got %d", received)
		}
	})

	// Test Reducer.OutputChan() and InputChan()
	t.Run("Reducer.OutputChan/InputChan", func(t *testing.T) {
		reducer := NewIDReducer[int](
			WithFlushPeriod[int, []int, []int](50 * time.Millisecond))
		defer reducer.Stop()

		// Send via InputChan
		reducer.InputChan() <- 1
		reducer.InputChan() <- 2
		reducer.InputChan() <- 3

		// Receive via OutputChan
		batch := <-reducer.OutputChan()
		if len(batch) == 0 {
			t.Error("Expected non-empty batch")
		}
	})

	// Test FanIn.OutputChan()
	t.Run("FanIn.OutputChan", func(t *testing.T) {
		fanin := NewFanIn[int]()
		defer fanin.Stop()

		in1 := make(chan int, 1)
		fanin.Add(in1)

		in1 <- 100

		val := <-fanin.OutputChan()
		if val != 100 {
			t.Errorf("Expected 100, got %d", val)
		}
	})

	// Test FanOut.InputChan()
	t.Run("FanOut.InputChan", func(t *testing.T) {
		fanout := NewFanOut[int]()
		defer fanout.Stop()

		out := fanout.New(nil)

		fanout.InputChan() <- 200

		val := <-out
		if val != 200 {
			t.Errorf("Expected 200, got %d", val)
		}
	})
}
