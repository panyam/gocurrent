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
	reducer := NewIDReducer(
		WithInputChan[int, []int, []int](inputChan),
		WithOutputChan[int, []int](outputChan),
		WithFlushPeriod[int, []int, []int](50*time.Millisecond))
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

	reducer := NewIDReducer(
		WithInputChan[int, []int, []int](inputChan),
		WithOutputChan[int, []int](outputChan),
		WithFlushPeriod[int, []int, []int](50*time.Millisecond))
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

	// Set a very long flush period so auto-flush doesn't trigger
	reducer := NewIDReducer(
		WithInputChan[int, []int, []int](inputChan),
		WithOutputChan[int, []int](outputChan),
		WithFlushPeriod[int, []int, []int](10*time.Second))
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

	reducer := NewIDReducer(
		WithInputChan[int, []int, []int](inputChan),
		WithOutputChan[int, []int](outputChan),
		WithFlushPeriod[int, []int, []int](30*time.Millisecond))
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
	reducer := NewReducer(
		WithInputChan[int, int, int](inputChan),
		WithOutputChan[int, int](outputChan),
		WithFlushPeriod[int, int, int](50*time.Millisecond))
	reducer.CollectFunc = func(input int, sum int) (int, bool) {
		return sum + input, false
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

	reducer := NewReducer(
		WithInputChan[string, WordCount, int](inputChan),
		WithOutputChan[string, WordCount](outputChan),
		WithFlushPeriod[string, WordCount, int](50*time.Millisecond))
	reducer.CollectFunc = func(word string, counts WordCount) (WordCount, bool) {
		if counts == nil {
			counts = make(WordCount)
		}
		counts[word]++
		return counts, false
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

	// Don't provide input channel - reducer should create its own
	reducer := NewIDReducer(
		WithOutputChan[int, []int](outputChan),
		WithFlushPeriod[int, []int, []int](50*time.Millisecond))
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

	reducer := NewIDReducer(
		WithOutputChan[int, []int](outputChan),
		WithFlushPeriod[int, []int, []int](50*time.Millisecond))
	defer reducer.Stop()

	sendChan := reducer.InputChan()

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

	reducer := NewIDReducer(
		WithInputChan[int, []int, []int](inputChan),
		WithOutputChan[int, []int](outputChan),
		WithFlushPeriod[int, []int, []int](1*time.Second)) // Long period

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

	reducer := NewIDReducer(
		WithInputChan[int, []int, []int](inputChan),
		WithOutputChan[int, []int](outputChan),
		WithFlushPeriod[int, []int, []int](30*time.Millisecond))
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

	reducer := NewReducer(
		WithInputChan[int, Stats, float64](inputChan),
		WithOutputChan[int, Stats](outputChan),
		WithFlushPeriod[int, Stats, float64](50*time.Millisecond))
	reducer.CollectFunc = func(input int, stats Stats) (Stats, bool) {
		stats.Sum += input
		stats.Count++
		return stats, false
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

func TestReducerLengthBasedFlush(t *testing.T) {
	log.Println("============== TestReducerLengthBasedFlush ================")
	inputChan := make(chan int)
	outputChan := make(chan []int, 10)

	// Create a reducer that flushes when collection reaches 5 items
	reducer := NewReducer(
		WithInputChan[int, []int, []int](inputChan),
		WithOutputChan[int, []int](outputChan),
		WithFlushPeriod[int, []int, []int](10*time.Second)) // Long period to avoid time-based flush
	reducer.ReduceFunc = IDFunc[[]int]
	reducer.CollectFunc = func(input int, collection []int) ([]int, bool) {
		newCollection := append(collection, input)
		shouldFlush := len(newCollection) >= 5
		return newCollection, shouldFlush
	}
	defer reducer.Stop()

	// Send 12 items - should get 2 batches of 5 and then wait for timer
	go func() {
		for i := range 12 {
			inputChan <- i
		}
	}()

	// Should get first batch of 5
	batch1 := withTimeout(t, outputChan)
	assert.Equal(t, 5, len(batch1), "First batch should have 5 items")
	assert.Equal(t, []int{0, 1, 2, 3, 4}, batch1)

	// Should get second batch of 5
	batch2 := withTimeout(t, outputChan)
	assert.Equal(t, 5, len(batch2), "Second batch should have 5 items")
	assert.Equal(t, []int{5, 6, 7, 8, 9}, batch2)

	// Remaining 2 items should not trigger immediate flush
	// (would need manual flush or timer)
}

func TestReducerCustomFlushCriteria(t *testing.T) {
	log.Println("============== TestReducerCustomFlushCriteria ================")
	inputChan := make(chan int)
	outputChan := make(chan int, 10)

	// Create a reducer that flushes when sum exceeds 100
	reducer := NewReducer(
		WithInputChan[int, int, int](inputChan),
		WithOutputChan[int, int](outputChan),
		WithFlushPeriod[int, int, int](10*time.Second))
	reducer.CollectFunc = func(input int, sum int) (int, bool) {
		newSum := sum + input
		shouldFlush := newSum > 100
		return newSum, shouldFlush
	}
	reducer.ReduceFunc = func(sum int) int {
		return sum
	}
	defer reducer.Stop()

	// Send values that will accumulate past 100
	go func() {
		inputChan <- 30
		inputChan <- 40
		inputChan <- 35 // Total: 105, should trigger flush
		inputChan <- 20
		inputChan <- 50
		inputChan <- 40 // Total: 110, should trigger second flush
	}()

	// First flush when sum > 100
	result1 := withTimeout(t, outputChan)
	assert.Equal(t, 105, result1, "First flush should be 105")

	// Second flush when sum > 100 again
	result2 := withTimeout(t, outputChan)
	assert.Equal(t, 110, result2, "Second flush should be 110")
}

func TestReducerImmediateFlushOnEveryItem(t *testing.T) {
	log.Println("============== TestReducerImmediateFlushOnEveryItem ================")
	inputChan := make(chan int)
	outputChan := make(chan []int, 10)

	// Create a reducer that flushes on every single item
	reducer := NewReducer(
		WithInputChan[int, []int, []int](inputChan),
		WithOutputChan[int, []int](outputChan),
		WithFlushPeriod[int, []int, []int](10*time.Second))
	reducer.ReduceFunc = IDFunc[[]int]
	reducer.CollectFunc = func(input int, collection []int) ([]int, bool) {
		return append(collection, input), true // Always flush
	}
	defer reducer.Stop()

	// Send 3 items
	go func() {
		for i := range 3 {
			inputChan <- i
			time.Sleep(5 * time.Millisecond) // Small delay to allow flush processing
		}
	}()

	// Should get 3 separate batches, each with 1 item
	batch1 := withTimeout(t, outputChan)
	assert.Equal(t, []int{0}, batch1)

	batch2 := withTimeout(t, outputChan)
	assert.Equal(t, []int{1}, batch2)

	batch3 := withTimeout(t, outputChan)
	assert.Equal(t, []int{2}, batch3)
}

func TestReducer2SimpleUsage(t *testing.T) {
	log.Println("============== TestReducer2SimpleUsage ================")
	inputChan := make(chan int)
	outputChan := make(chan []int)

	// Use the simpler 2-parameter API
	reducer := NewIDReducer(
		WithInputChan2[int, []int](inputChan),
		WithOutputChan2[int](outputChan),
		WithFlushPeriod2[int, []int](50*time.Millisecond))
	defer reducer.Stop()

	// Send values
	go func() {
		for i := range 5 {
			inputChan <- i
		}
	}()

	// Wait for flush
	batch := withTimeout(t, outputChan)

	assert.Equal(t, 5, len(batch), "Should have collected 5 items")
	assert.Equal(t, []int{0, 1, 2, 3, 4}, batch)
}

func TestReducer2CustomCollectFunc(t *testing.T) {
	log.Println("============== TestReducer2CustomCollectFunc ================")
	inputChan := make(chan int)
	outputChan := make(chan int)

	// Custom reducer2 that sums integers (C == U == int)
	reducer := NewReducer2(
		WithInputChan2[int, int](inputChan),
		WithOutputChan2[int](outputChan),
		WithFlushPeriod2[int, int](50*time.Millisecond))
	reducer.CollectFunc = func(input int, sum int) (int, bool) {
		return sum + input, false
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
