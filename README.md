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
for msg := range reader.RecvChan() {
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

// Process messages
for msg := range reader.RecvChan() {
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

Collect and reduce N values from an input channel with configurable time windows.

```go
inputChan := make(chan int, 10)
outputChan := make(chan []int, 10)

// Create a reducer that collects integers into slices
reducer := gocurrent.NewIDReducer(inputChan, outputChan)
reducer.FlushPeriod = 100 * time.Millisecond
defer reducer.Stop()

// Send data
for i := 0; i < 5; i++ {
    reducer.Send(i)
}

// After FlushPeriod, receive the collected batch
batch := <-outputChan // []int{0, 1, 2, 3, 4}
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
    result := <-fanIn.RecvChan()
    fmt.Printf("Received: %d\n", result)
}
```

### FanOut

Distribute messages from one channel to multiple output channels.

```go
// Create fan-out
fanOut := gocurrent.NewFanOut[string](nil)
defer fanOut.Stop()

// Add output channels
out1 := fanOut.New(nil) // No filter
out2 := fanOut.New(func(s *string) *string {
    // Filter: only pass strings longer than 5 chars
    if len(*s) > 5 {
        return s
    }
    return nil
})

// Send data
fanOut.Send("hello")    // Goes to out1 only
fanOut.Send("hello world") // Goes to both out1 and out2

// Read from outputs
select {
case msg := <-out1:
    fmt.Printf("Out1: %s\n", msg)
case msg := <-out2:
    fmt.Printf("Out2: %s\n", msg)
}
```

### Map

A thread-safe map with read/write locking capabilities.

```go
// Create a thread-safe map
safeMap := gocurrent.NewMap[string, int]()

// Basic operations
safeMap.Set("key1", 42)
value, exists := safeMap.Get("key1")
if exists {
    fmt.Printf("Value: %d\n", value)
}

// Transaction-style operations
safeMap.Update(func(m map[string]int) {
    m["key2"] = 100
    m["key3"] = 200
})

safeMap.View(func() {
    val1, _ := safeMap.LGet("key1", false) // No lock needed in View
    val2, _ := safeMap.LGet("key2", false)
    fmt.Printf("Sum: %d\n", val1 + val2)
})
```

## Features

- **Type Safety**: All components are fully generic and type-safe
- **Resource Management**: Proper cleanup and lifecycle management
- **Composability**: Components can be easily combined
- **Customizable**: Configurable behavior and filtering
- **Thread Safety**: Built-in synchronization where needed
- **Error Handling**: Comprehensive error propagation

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License.

## References

- [Go Concurrency Patterns](https://go.dev/talks/2012/concurrency.slide) by Rob Pike
- [Go Blog: Go Concurrency Patterns](https://blog.golang.org/pipelines)

