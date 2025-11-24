package gocurrent

import (
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

const testTimeout = 5 * time.Second

// withTimeout wraps a channel receive with a timeout
func withTimeout[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case val := <-ch:
		return val
	case <-time.After(testTimeout):
		t.Fatal("Test timed out waiting for channel receive")
		var zero T
		return zero
	}
}

func ExampleReducer() {
	inputChan := make(chan int)
	outputChan := make(chan []int)

	// Create a reducer that collects integers into slices
	reducer := NewIDReducer(inputChan, outputChan)
	reducer.FlushPeriod = 50 * time.Millisecond
	defer reducer.Stop()

	// Send data
	go func() {
		for i := range 3 {
			inputChan <- i
		}
	}()

	// After FlushPeriod, receive the collected batch
	batch := <-outputChan
	fmt.Println(batch)

	// Output:
	// [0 1 2]
}

func TestIDReducer(t *testing.T) {
	log.Println("============== TestIDReducer ================")
	inputChan := make(chan int)
	outputChan := make(chan []int)

	reducer := NewIDReducer(inputChan, outputChan)
	reducer.FlushPeriod = 50 * time.Millisecond
	defer reducer.Stop()

	// Send 5 values
	go func() {
		for i := range 5 {
			inputChan <- i
		}
	}()

	// Wait for flush
	batch := withTimeout(t, outputChan)

	assert.Equal(t, 5, len(batch), "Should have collected 5 items")
	for i := range 5 {
		assert.Equal(t, i, batch[i], "Values should match")
	}
}

func TestReducerManualFlush(t *testing.T) {
	log.Println("============== TestReducerManualFlush ================")
	inputChan := make(chan int)
	outputChan := make(chan []int)

	reducer := NewIDReducer(inputChan, outputChan)
	// Set a very long flush period so auto-flush doesn't trigger
	reducer.FlushPeriod = 10 * time.Second
	defer reducer.Stop()

	// Send values
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for i := range 3 {
			inputChan <- i
		}
		// Give time for events to be collected
		time.Sleep(10 * time.Millisecond)
		reducer.Flush()
		wg.Done()
	}()

	batch := withTimeout(t, outputChan)
	wg.Wait()

	assert.Equal(t, 3, len(batch), "Should have collected 3 items")
	assert.Equal(t, []int{0, 1, 2}, batch)
}

func TestReducerMultipleBatches(t *testing.T) {
	log.Println("============== TestReducerMultipleBatches ================")
	inputChan := make(chan int)
	outputChan := make(chan []int, 10)

	reducer := NewIDReducer(inputChan, outputChan)
	reducer.FlushPeriod = 30 * time.Millisecond
	defer reducer.Stop()

	// Send first batch
	for i := range 3 {
		inputChan <- i
	}

	// Wait for first flush
	batch1 := withTimeout(t, outputChan)
	assert.Equal(t, []int{0, 1, 2}, batch1, "First batch should contain 0,1,2")

	// Send second batch
	for i := 10; i < 13; i++ {
		inputChan <- i
	}

	// Wait for second flush
	batch2 := withTimeout(t, outputChan)
	assert.Equal(t, []int{10, 11, 12}, batch2, "Second batch should contain 10,11,12")
}

func TestReducerCustomCollectFunc(t *testing.T) {
	log.Println("============== TestReducerCustomCollectFunc ================")
	inputChan := make(chan int)
	outputChan := make(chan int)

	// Custom reducer that sums integers
	reducer := NewReducer[int, int, int](inputChan, outputChan)
	reducer.FlushPeriod = 50 * time.Millisecond
	reducer.CollectFunc = func(input int, sum int) (int, bool) {
		return sum + input, true
	}
	reducer.ReduceFunc = func(sum int) int {
		return sum
	}
	defer reducer.Stop()

	// Send values 1+2+3+4+5 = 15
	go func() {
		for i := 1; i <= 5; i++ {
			inputChan <- i
		}
	}()

	result := withTimeout(t, outputChan)
	assert.Equal(t, 15, result, "Sum should be 15")
}

func TestReducerWithMapCollection(t *testing.T) {
	log.Println("============== TestReducerWithMapCollection ================")
	type WordCount map[string]int

	inputChan := make(chan string)
	outputChan := make(chan int)

	reducer := NewReducer[string, WordCount, int](inputChan, outputChan)
	reducer.FlushPeriod = 50 * time.Millisecond
	reducer.CollectFunc = func(word string, counts WordCount) (WordCount, bool) {
		if counts == nil {
			counts = make(WordCount)
		}
		counts[word]++
		return counts, true
	}
	reducer.ReduceFunc = func(counts WordCount) int {
		return len(counts) // Return unique word count
	}
	defer reducer.Stop()

	// Send words with duplicates
	go func() {
		words := []string{"hello", "world", "hello", "go", "world", "hello"}
		for _, w := range words {
			inputChan <- w
		}
	}()

	uniqueCount := withTimeout(t, outputChan)
	assert.Equal(t, 3, uniqueCount, "Should have 3 unique words")
}

func TestReducerNilInputChannel(t *testing.T) {
	log.Println("============== TestReducerNilInputChannel ================")
	outputChan := make(chan []int)

	// Pass nil input channel - reducer should create its own
	reducer := NewIDReducer(nil, outputChan)
	reducer.FlushPeriod = 50 * time.Millisecond
	defer reducer.Stop()

	// Use the reducer's send channel
	go func() {
		reducer.Send(1)
		reducer.Send(2)
		reducer.Send(3)
	}()

	batch := withTimeout(t, outputChan)
	assert.Equal(t, []int{1, 2, 3}, batch)
}

func TestReducerSendChan(t *testing.T) {
	log.Println("============== TestReducerSendChan ================")
	outputChan := make(chan []int)

	reducer := NewIDReducer(nil, outputChan)
	reducer.FlushPeriod = 50 * time.Millisecond
	defer reducer.Stop()

	sendChan := reducer.SendChan()

	go func() {
		sendChan <- 10
		sendChan <- 20
		sendChan <- 30
	}()

	batch := withTimeout(t, outputChan)
	assert.Equal(t, []int{10, 20, 30}, batch)
}

func TestReducerStop(t *testing.T) {
	log.Println("============== TestReducerStop ================")
	inputChan := make(chan int)
	outputChan := make(chan []int, 10)

	reducer := NewIDReducer(inputChan, outputChan)
	reducer.FlushPeriod = 1 * time.Second // Long period

	// Send some data
	go func() {
		inputChan <- 1
		inputChan <- 2
	}()

	time.Sleep(50 * time.Millisecond)

	// Stop should complete without blocking
	done := make(chan bool)
	go func() {
		reducer.Stop()
		done <- true
	}()

	select {
	case <-done:
		// Success - Stop completed
	case <-time.After(testTimeout):
		t.Fatal("Stop() blocked for too long")
	}
}

func TestReducerEmptyFlush(t *testing.T) {
	log.Println("============== TestReducerEmptyFlush ================")
	inputChan := make(chan int)
	outputChan := make(chan []int, 10)

	reducer := NewIDReducer(inputChan, outputChan)
	reducer.FlushPeriod = 30 * time.Millisecond
	defer reducer.Stop()

	// Don't send any data, just wait for a flush
	batch := withTimeout(t, outputChan)

	// Empty flush should produce nil/empty slice
	assert.Equal(t, 0, len(batch), "Empty flush should produce empty batch")
}

func TestReducerWithStructCollection(t *testing.T) {
	log.Println("============== TestReducerWithStructCollection ================")

	// Test with a struct as the collection type (uses zero value reset)
	type Stats struct {
		Sum   int
		Count int
	}

	inputChan := make(chan int)
	outputChan := make(chan float64)

	reducer := NewReducer[int, Stats, float64](inputChan, outputChan)
	reducer.FlushPeriod = 50 * time.Millisecond
	reducer.CollectFunc = func(input int, stats Stats) (Stats, bool) {
		stats.Sum += input
		stats.Count++
		return stats, true
	}
	reducer.ReduceFunc = func(stats Stats) float64 {
		if stats.Count == 0 {
			return 0
		}
		return float64(stats.Sum) / float64(stats.Count)
	}
	defer reducer.Stop()

	// Send values: average of 2,4,6,8,10 = 6
	go func() {
		for i := 2; i <= 10; i += 2 {
			inputChan <- i
		}
	}()

	avg := withTimeout(t, outputChan)
	assert.Equal(t, 6.0, avg, "Average should be 6.0")
}
