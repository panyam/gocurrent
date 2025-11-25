# Component Standardization Analysis

## Executive Summary

All components in gocurrent are "building blocks" that take input channel(s) and/or produce output channel(s). However, there are several inconsistencies in naming conventions, lifecycle management, and API patterns that make composition difficult.

## Current Components

| Component | Input Channel(s) | Output Channel(s) | Notes |
|-----------|------------------|-------------------|-------|
| Reader | None | `Message[R]` | Uses ReaderFunc to generate |
| Writer | `W` | None | Uses WriterFunc for side effects |
| Mapper | `I` | `O` | Transform/filter between channels |
| Pipe | `T` | `T` | Identity mapper |
| Reducer | `T` | `U` | Batch and reduce with time windows |
| FanIn | Multiple `T` | Single `T` | Merge pattern |
| FanOut | Single `T` | Multiple `T` | Broadcast pattern |
| Map | N/A | N/A | Thread-safe map (not a channel component) |

## Inconsistencies Found

### 1. Channel Access Method Naming ⚠️ HIGH PRIORITY

**Current State:**
```go
// Reader
RecvChan() <-chan Message[R]   // ✓ Read-only return (correct)

// Writer
SendChan() chan W               // ✗ Bidirectional return (should be write-only)
Send(W)                         // ✓ Convenience method exists

// Reducer
RecvChan() <-chan U             // ✓ Read-only return (correct)
SendChan() chan<- T             // ✓ Write-only return (correct)
Send(T)                         // ✓ Convenience method exists

// FanIn
RecvChan() chan T               // ✗ Bidirectional return (should be read-only)

// FanOut
SendChan() <-chan T             // ✗✗ CRITICAL BUG: Returns READ-only, should be WRITE-only!
Send(T)                         // ✓ Convenience method exists

// Mapper
// No channel access methods (channels passed in constructor)
```

**Issues:**
1. **FanOut.SendChan()** returns `<-chan T` (read-only) when it should return `chan<- T` (write-only) - this is a BUG
2. **Writer.SendChan()** returns bidirectional `chan W` instead of write-only `chan<- W`
3. **FanIn.RecvChan()** returns bidirectional `chan T` instead of read-only `<-chan T`
4. Method names are inconsistent: `RecvChan` vs `SendChan` - not symmetric
5. **Mapper** has no channel accessors at all

**Proposed Standardization:**

**Option A: Keep Current Names, Fix Directions**
```go
// For OUTPUT channels (data flows OUT of component):
RecvChan() <-chan T    // Always read-only

// For INPUT channels (data flows INTO component):
SendChan() chan<- T    // Always write-only
Send(T)                // Convenience method
```

**Option B: More Explicit Names (RECOMMENDED)**
```go
// For OUTPUT channels:
OutputChan() <-chan T  // or Out() <-chan T

// For INPUT channels:
InputChan() chan<- T   // or In() chan<- T
Send(T)                // Convenience method
```

**Recommendation:** Use Option B (OutputChan/InputChan) because:
- More explicit about data flow direction
- Easier for newcomers to understand
- Matches common naming in other libraries
- "Send/Recv" terminology is overloaded in Go

### 2. Lifecycle Management Patterns

**Current State:**

```go
// Reader, Writer, Mapper, FanIn, FanOut
- Use RunnerBase[C] for lifecycle
- Reader/Writer/Mapper: RunnerBase[string] with "stop" command
- FanIn/FanOut: RunnerBase[Cmd] with typed command structs
- Implement Stop() via RunnerBase.Stop()

// Reducer
- Does NOT use RunnerBase
- Has its own sync.WaitGroup
- Custom Stop() with command channel
```

**Issues:**
1. Reducer doesn't use RunnerBase (inconsistent)
2. Mix of string commands vs typed command structs
3. No standard pattern for custom commands beyond "stop"

**Proposed Standardization:**
1. **All** components should use RunnerBase for consistency
2. Components needing custom commands should use typed command structs (like FanIn/FanOut)
3. Migrate Reducer to use RunnerBase

### 3. Completion Signaling

**Current State:**

```go
// Reader, Writer
ClosedChan() <-chan error   // Signals when done, with optional error

// Mapper, FanIn, FanOut
OnDone func(...)            // Callback only (Mapper has it)
// No ClosedChan() for any of these

// Reducer
// No completion signaling at all
```

**Issues:**
1. Only Reader and Writer have ClosedChan()
2. Mapper has OnDone callback but no ClosedChan
3. FanIn has OnChannelRemoved but no general completion signal
4. Inconsistent error reporting mechanisms

**Proposed Standardization:**
All components should have:
```go
ClosedChan() <-chan error  // For monitoring completion
OnDone func(...)           // Optional callback where appropriate
```

### 4. Constructor Patterns

**Current State:**

```go
// Reducer (NEW PATTERN - functional options)
NewReducer[T, C, U](opts ...ReducerOption[T, C, U]) *Reducer[T, C, U]
NewIDReducer[T](opts ...ReducerOption[T, []T, []T]) *Reducer[T, []T, []T]

// All others (OLD PATTERN - positional parameters)
NewReader[R](read ReaderFunc[R]) *Reader[R]
NewWriter[W](write WriterFunc[W]) *Writer[W]
NewMapper[T, U](input <-chan T, output chan<- U, mapper func(...)) *Mapper[T, U]
NewPipe[T](input <-chan T, output chan<- T) *Mapper[T, T]
NewFanIn[T](outChan chan T) *FanIn[T]
NewFanOut[T](inputChan chan T) *FanOut[T]
```

**Issues:**
1. Only Reducer uses functional options pattern
2. Positional parameters make it hard to add new optional fields
3. Inconsistent API experience across components

**Proposed Standardization:**
Migrate ALL constructors to functional options pattern:

```go
// Example for Reader
type ReaderOption[R any] func(*Reader[R])

func WithReaderFunc[R any](fn ReaderFunc[R]) ReaderOption[R]
func WithOutputBuffer[R any](size int) ReaderOption[R]
func WithOnDone[R any](fn func(*Reader[R])) ReaderOption[R]

NewReader[R](opts ...ReaderOption[R]) *Reader[R]

// Example for Writer
type WriterOption[W any] func(*Writer[W])

func WithWriterFunc[W any](fn WriterFunc[W]) WriterOption[W]
func WithInputBuffer[W any](size int) WriterOption[W]

NewWriter[W](opts ...WriterOption[W]) *Writer[W]
```

### 5. Channel Ownership Convention

**Current State:**

```go
// Reducer - uses nil = "create and own"
if out.inputChan == nil {
    out.inputChan = make(chan T)
}

// FanIn, FanOut - uses nil = "create and own"
if outChan == nil {
    outChan = make(chan T)
    selfOwnOut = true
}

// Reader, Writer - always creates and owns channels
msgChannel: make(chan Message[R])

// Mapper, Pipe - NEVER owns channels (passed in constructor)
```

**Issues:**
1. Reader/Writer always create their own channels (can't provide your own)
2. Mapper/Pipe never own channels (you must provide)
3. Reducer/FanIn/FanOut use nil = "create and own" pattern

**Proposed Standardization:**
Adopt the "nil = create and own" pattern universally via functional options:

```go
// Default: component creates and owns
NewReader[int]()

// Provide your own output channel
outputChan := make(chan Message[int])
NewReader[int](WithOutputChan(outputChan))

// For Writer - provide input channel
inputChan := make(chan int)
NewWriter[int](WithInputChan(inputChan))
```

## Proposed Changes Summary

### Phase 1: Fix Critical Bugs (Breaking Changes)
1. ✅ Fix `FanOut.SendChan()` to return `chan<- T` instead of `<-chan T`
2. ✅ Fix `Writer.SendChan()` to return `chan<- W` instead of `chan W`
3. ✅ Fix `FanIn.RecvChan()` to return `<-chan T` instead of `chan T`

### Phase 2: Add Aliases for Smooth Migration (Non-Breaking)
4. Add `InputChan()` as alias for `SendChan()` where applicable
5. Add `OutputChan()` as alias for `RecvChan()` where applicable
6. Mark old methods as deprecated in comments

### Phase 3: Lifecycle Consistency
7. Migrate Reducer to use RunnerBase
8. Add `ClosedChan() <-chan error` to all components
9. Standardize command patterns

### Phase 4: Constructor Modernization
10. Migrate all constructors to functional options
11. Make channel ownership configurable via options
12. Add buffer size options

### Phase 5: Documentation & Examples
13. Update README with new patterns
14. Add migration guide
15. Update all examples

## Backward Compatibility Strategy

To minimize breakage:

1. **Additive approach**: Add new methods alongside old ones
2. **Deprecation comments**: Mark old methods as deprecated
3. **Transition period**: Keep both old and new APIs for 1-2 versions
4. **Migration guide**: Provide clear upgrade path

Example:
```go
// Deprecated: Use OutputChan instead. This method will be removed in v2.0.0.
func (r *Reader[R]) RecvChan() <-chan Message[R] {
    return r.OutputChan()
}

// OutputChan returns the channel for receiving output from this reader.
func (r *Reader[R]) OutputChan() <-chan Message[R] {
    return r.msgChannel
}
```

## Benefits After Standardization

1. **Composability**: Uniform interfaces make components easily composable
2. **Discoverability**: Consistent naming makes API intuitive
3. **Extensibility**: Functional options allow adding features without breaking changes
4. **Type Safety**: Proper channel directions prevent misuse
5. **Maintainability**: Shared patterns reduce code duplication
6. **Testability**: Standard interfaces easier to mock

## Next Steps

1. Get feedback on proposed naming (OutputChan/InputChan vs RecvChan/SendChan)
2. Decide on backward compatibility approach
3. Create migration guide
4. Implement changes in phases
5. Update documentation and examples
