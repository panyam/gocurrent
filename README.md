# gocurrent

[![Go Reference](https://pkg.go.dev/badge/github.com/panyam/gocurrent.svg)](https://pkg.go.dev/github.com/panyam/gocurrent)
[![Go Report Card](https://goreportcard.com/badge/github.com/panyam/gocurrent)](https://goreportcard.com/report/github.com/panyam/gocurrent)

A Go library providing utilities for common concurrency patterns with customizable behavior. This package implements several concurrency primitives inspired by Rob Pike's concurrency patterns from his talk ["Go Concurrency Patterns"](https://go.dev/talks/2012/concurrency.slide).

## Installation

```bash
go get github.com/panyam/gocurrent
```

## Quick Start

```go
import "github.com/panyam/gocurrent"

// Create a reader that generates numbers
reader := gocurrent.NewReader(func() (int, error) {
    return rand.Intn(100), nil
})
defer reader.Stop()

// Read from the channel
for msg := range reader.OutputChan() {
    if msg.Error != nil {
        log.Printf("Error: %v", msg.Error)
        continue
    }
    fmt.Printf("Received: %d\n", msg.Value)
}
```

## Components

### Reader

A goroutine wrapper that continuously calls a reader function and sends results to a channel.

```go
// Create a reader that reads from a data source
reader := gocurrent.NewReader(func() (string, error) {
    // Your data reading logic here
    return "data", nil
})
defer reader.Stop()

// Monitor for reader completion or errors
go func() {
    select {
    case err := <-reader.ClosedChan():
        if err != nil {
            log.Printf("Reader terminated with error: %v", err)
        } else {
            log.Println("Reader completed successfully")
        }
    }
}()

// Process messages
for msg := range reader.OutputChan() {
    if msg.Error != nil {
        log.Printf("Read error: %v", msg.Error)
        break
    }
    fmt.Printf("Read: %s\n", msg.Value)
}
```

### Writer

A goroutine for serializing writes using a writer callback method.

```go
// Create a writer that processes data
writer := gocurrent.NewWriter(func(data string) error {
    // Your data writing logic here
    fmt.Printf("Writing: %s\n", data)
    return nil
})
defer writer.Stop()

// Monitor for writer completion or errors
go func() {
    select {
    case err := <-writer.ClosedChan():
        if err != nil {
            log.Printf("Writer terminated with error: %v", err)
        } else {
            log.Println("Writer completed successfully")
        }
    }
}()

// Send data to writer
writer.Send("Hello")
writer.Send("World")
```

### Mapper

Transform and/or filter data between channels.

```go
inputChan := make(chan int, 10)
outputChan := make(chan string, 10)

// Create a mapper that converts integers to strings
mapper := gocurrent.NewMapper(inputChan, outputChan, func(i int) (string, bool, bool) {
    // Return: (output, skip, stop)
    return fmt.Sprintf("Number: %d", i), false, false
})
defer mapper.Stop()

// Send data
inputChan <- 42
inputChan <- 100

// Read transformed data
result := <-outputChan // "Number: 42"
```

### Reducer

Collect and reduce values from an input channel with configurable time windows. The Reducer has three type parameters:
- `T` - the input event type
- `C` - the intermediate collection type (where events are batched)
- `U` - the output type after reduction

Reducers use functional options for configuration:

```go
// Simple case: Use NewIDReducer to collect events into a slice
// With all defaults (creates its own channels, 100ms flush period)
reducer := gocurrent.NewIDReducer[int]()
defer reducer.Stop()

// With custom configuration
inputChan := make(chan int, 10)
outputChan := make(chan []int, 10)
reducer := gocurrent.NewIDReducer[int](
    gocurrent.WithInputChan[int, []int, []int](inputChan),
    gocurrent.WithOutputChan[int, []int, []int](outputChan),
    gocurrent.WithFlushPeriod[int, []int, []int](100 * time.Millisecond))
defer reducer.Stop()

// Send data
for i := 0; i < 5; i++ {
    reducer.Send(i)
}

// After FlushPeriod, receive the collected batch
batch := <-outputChan // []int{0, 1, 2, 3, 4}
```

For custom collection and reduction logic, use `NewReducer` directly:

```go
// Custom reducer: collect strings into a map, reduce to a summary
type WordCount map[string]int

inputChan := make(chan string, 10)
outputChan := make(chan string, 10)
reducer := gocurrent.NewReducer[string, WordCount, string](
    gocurrent.WithInputChan[string, WordCount, string](inputChan),
    gocurrent.WithOutputChan[string, WordCount, string](outputChan),
    gocurrent.WithFlushPeriod[string, WordCount, string](100 * time.Millisecond))
reducer.CollectFunc = func(word string, counts WordCount) (WordCount, bool) {
    if counts == nil {
        counts = make(WordCount)
    }
    counts[word]++
    return counts, false // Return true to trigger immediate flush
}
reducer.ReduceFunc = func(counts WordCount) string {
    return fmt.Sprintf("Counted %d unique words", len(counts))
}
```

#### Custom Flush Triggers

The `CollectFunc` can signal when to flush by returning `true` as the second return value. This enables custom flush criteria beyond time-based flushing:

```go
// Length-based flush: flush when collection reaches 100 items
reducer.CollectFunc = func(input int, collection []int) ([]int, bool) {
    newCollection := append(collection, input)
    shouldFlush := len(newCollection) >= 100
    return newCollection, shouldFlush
}

// Custom criteria: flush when sum exceeds threshold
reducer.CollectFunc = func(input int, sum int) (int, bool) {
    newSum := sum + input
    shouldFlush := newSum > 1000
    return newSum, shouldFlush
}

// Manual flush is also available
reducer.Flush()
```

### Pipe

Connect a reader and writer channel with identity transform.

```go
inputChan := make(chan string, 10)
outputChan := make(chan string, 10)

// Create a pipe (identity mapper)
pipe := gocurrent.NewPipe(inputChan, outputChan)
defer pipe.Stop()

inputChan <- "hello"
result := <-outputChan // "hello"
```

### FanIn

Merge multiple input channels into a single output channel.

```go
// Create input channels
chan1 := make(chan int, 10)
chan2 := make(chan int, 10)
chan3 := make(chan int, 10)

// Create fan-in
fanIn := gocurrent.NewFanIn[int](nil)
defer fanIn.Stop()

// Add input channels
fanIn.Add(chan1, chan2, chan3)

// Send data to different channels
chan1 <- 1
chan2 <- 2
chan3 <- 3

// Read merged output
for i := 0; i < 3; i++ {
    result := <-fanIn.OutputChan()
    fmt.Printf("Received: %d\n", result)
}
```

### FanOut

Distribute messages from one channel to multiple output channels. Three dispatch strategies are available, each implementing the `FanOuter[T]` interface:

| Type | Sender blocks? | FIFO ordering? | Goroutines |
|---|---|---|---|
| `QueuedFanOut` (recommended) | No (until queue full) | Strict | 2 total (bounded) |
| `SyncFanOut` | Yes (all outputs) | Strict | 0 extra |
| `AsyncFanOut` | No | None | N per event |

```go
// QueuedFanOut (recommended) — non-blocking sender, strict FIFO
fo := gocurrent.NewQueuedFanOut[string]()
defer fo.Stop()

out1 := fo.New(nil) // No filter
out2 := fo.New(func(s *string) *string {
    if len(*s) > 5 { return s }
    return nil
})

fo.Send("hello")       // Goes to out1 only (filtered from out2)
fo.Send("hello world") // Goes to both out1 and out2

// SyncFanOut — sender blocks until all outputs receive each event
syncFo := gocurrent.NewSyncFanOut[int]()

// AsyncFanOut — fire-and-forget, no ordering guarantee
asyncFo := gocurrent.NewAsyncFanOut[int]()
```

#### Migration from `FanOut[T]`

The old `FanOut[T]` type and `NewFanOut()` constructor have been removed. Replace with the explicit type that matches your use case:

| Old code | New code |
|---|---|
| `NewFanOut[T]()` (default async) | `NewQueuedFanOut[T]()` |
| `NewFanOut[T](WithFanOutSendSync[T](true))` | `NewSyncFanOut[T]()` |
| `*FanOut[T]` type | `FanOuter[T]` interface or concrete type |

### SyncMap

A type-safe generic wrapper around `sync.Map`, providing the same concurrent map
semantics optimized for read-heavy workloads but with compile-time type safety.

```go
// Usable without initialization (zero value is ready to use)
var m gocurrent.SyncMap[string, int]

// Basic operations — same API as sync.Map, just typed
m.Store("key1", 42)
value, ok := m.Load("key1")
if ok {
    fmt.Printf("Value: %d\n", value)
}

// Atomic load-and-delete
prev, loaded := m.LoadAndDelete("key1")

// Iterate all entries
m.Range(func(k string, v int) bool {
    fmt.Printf("%s = %d\n", k, v)
    return true // return false to stop
})
```

## Features

- **Type Safety**: All components are fully generic and type-safe
- **Resource Management**: Proper cleanup and lifecycle management
- **Composability**: Components can be easily combined
- **Customizable**: Configurable behavior and filtering
- **Thread Safety**: Built-in synchronization where needed
- **Error Handling**: Comprehensive error propagation with completion signaling
- **Monitoring**: Built-in channels for monitoring goroutine completion and errors

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the GPL License.

## References

- [Go Concurrency Patterns](https://go.dev/talks/2012/concurrency.slide) by Rob Pike
- [Go Blog: Go Concurrency Patterns](https://blog.golang.org/pipelines)

