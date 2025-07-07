// Package gocurrent provides utilities for common Go concurrency patterns.
//
// This package implements several concurrency primitives inspired by Rob Pike's
// concurrency patterns from his talk "Go Concurrency Patterns" (https://go.dev/talks/2012/concurrency.slide).
//
// The main components include:
//
//   - Reader: A goroutine wrapper that continuously calls a reader function and sends results to a channel, with error signaling via ClosedChan()
//   - Writer: A goroutine for serializing writes using a writer callback, with error signaling via ClosedChan()
//   - Mapper: Transform and/or filter data between channels
//   - Reducer: Collect and reduce N values from an input channel with configurable time windows
//   - Pipe: Connect a reader and writer channel with identity transform
//   - FanIn: Merge multiple input channels into a single output channel
//   - FanOut: Distribute messages from one channel to multiple output channels
//   - Map: A thread-safe map with read/write locking capabilities
//
// All concurrency primitives are designed to be composable and provide
// fine-grained control over goroutine lifecycles, resource management, and
// error monitoring through completion signaling channels.
package gocurrent
