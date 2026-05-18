# gocurrent

## Version
0.0.14

## Provides
- concurrency-patterns: Rob Pike concurrency pattern implementations
- reader-writer: Reader/Writer goroutines for continuous data acquisition and serialized writes
- mapper-reducer: Mapper for data transformation, Reducer for batching with time windows
- fan-in-fan-out: FanIn for channel merging; FanOut with 3 dispatch strategies (SyncFanOut, AsyncFanOut, QueuedFanOut) via FanOuter interface
- event-log: Generic append-only Queue[T] with offset-based reads, per-subscriber wake-ups, late-subscriber replay; multi-producer N-consumer
- barrier-resolution: Per-entry Resolve/AwaitResolution + AppendBarrier producer-blocking convenience; first-writer-wins under multi-resolver
- thread-safe-map: Atomic map operations

## Module
github.com/panyam/gocurrent

## Location
newstack/gocurrent/main

## Stack Dependencies
None

## Integration

### Go Module
```go
// go.mod
require github.com/panyam/gocurrent 0.0.10

// Local development
replace github.com/panyam/gocurrent => ~/newstack/gocurrent/main
```

### Key Imports
```go
import "github.com/panyam/gocurrent/conc"
```

## Status
Stable

## Conventions
- Generous use of generics
- Channel-based patterns
- Error propagation via channels
