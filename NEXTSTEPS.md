# Next Steps

## Recent Changes

### Race Condition Fixes (Completed — Issue #2)
- ✅ Replaced `sync.Mutex` + `bool` with `sync/atomic.Bool` for `RunnerBase.isRunning`
- ✅ Added `done chan struct{}` to RunnerBase for safe Stop/cleanup coordination
- ✅ Eliminated concurrent send+close race on `controlChan` (controlChan is never closed now)
- ✅ Fixed Writer `Send()` TOCTOU race using `select` on `Done()` channel
- ✅ Fixed Reader cleanup race — msgChannel no longer closed from cleanup (avoids race with inner goroutine)
- ✅ Fixed FanIn `pipeClosed` callback race — now routes through controlChan instead of direct slice access
- ✅ Fixed FanIn `OnDone` assignment race — set at Mapper construction time via `WithMapperOnDone` option
- ✅ Fixed Reducer `Flush()` race — now routes through command channel to goroutine
- ✅ Fixed Reducer `ReduceFunc`/`CollectFunc` assignment race — set via options before `start()`
- ✅ Added `WithReduceFunc`, `WithCollectFunc` functional options for Reducer
- ✅ Added `Done() <-chan struct{}` method to RunnerBase for cross-goroutine coordination
- ✅ Removed all channel nilling in cleanup paths (was source of races)
- ✅ Added race-specific test suite (5 new tests)
- ✅ Added Makefile with `test-race` target
- ✅ Added GitHub Actions CI with race detection
- ✅ Added pre-push git hook running race-detected tests
- ✅ All tests pass with `go test -race`

### Reducer API Simplification (Completed)
- ✅ Added `Reducer2[T, C]` type alias for simplified 2-parameter API (where U == C)
- ✅ Added `NewReducer2()` constructor and helper functions
- ✅ Added `ReducerOption2[T, C]` type alias for cleaner option signatures
- ✅ Added `WithFlushPeriod2()`, `WithInputChan2()`, `WithOutputChan2()` option helpers
- ✅ Added `NewIDReducer2()` helper function
- ✅ Added tests for Reducer2 API
- ✅ Maintained full backwards compatibility with 3-parameter `Reducer[T, C, U]`

### Component Standardization - Phase 3: Lifecycle Consistency (Completed)
- ✅ Added `ClosedChan() <-chan error` to Mapper for completion signaling
- ✅ Added `ClosedChan() <-chan error` to FanIn for completion signaling
- ✅ Added `ClosedChan() <-chan error` to FanOut for completion signaling
- ✅ Added `ClosedChan() <-chan error` to Reducer for completion signaling
- ✅ Fixed Mapper.OnDone callback (was never being called before)
- ✅ Fixed FanIn waitgroup management (removed incorrect tracking)
- ✅ All components now have consistent lifecycle monitoring

### Component Standardization - Phase 2: Method Naming (Completed)
- ✅ Renamed `RecvChan()` -> `OutputChan()` across all components
- ✅ Renamed `SendChan()` -> `InputChan()` across all components
- ✅ Updated all tests and internal references
- ✅ Consistent, intuitive naming for data flow direction

### Component Standardization - Phase 1: Channel Direction Fixes (Completed)
- ✅ Fixed `FanOut.SendChan()` to return `chan<- T` (write-only) instead of `<-chan T`
- ✅ Fixed `Writer.SendChan()` to return `chan<- W` (write-only) instead of `chan W`
- ✅ Fixed `FanIn.RecvChan()` to return `<-chan T` (read-only) instead of `chan T`
- ✅ All tests continue to pass after fixes
- ✅ Created STANDARDIZATION.md with comprehensive analysis
- ✅ Created block.go and component_adapters.go for composition framework

### Custom Flush Triggers for Reducer (Completed)
- ✅ Repurposed `CollectFunc` bool return value to signal when to flush
- ✅ Updated `start()` method to trigger immediate flush when `CollectFunc` returns true
- ✅ Added comprehensive tests for length-based, custom criteria, and immediate flush scenarios
- ✅ All existing tests continue to pass

### Functional Options Pattern for Reducer (Completed)
- ✅ Migrated `NewReducer` and `NewIDReducer` to use functional options pattern
- ✅ Added `ReducerOption[T, C, U]` type for configuration
- ✅ Implemented `WithFlushPeriod`, `WithInputChan`, and `WithOutputChan` options
- ✅ Updated all tests to use new API
- ✅ Updated README.md documentation

## TODO

### Component Standardization - Phase 4: Functional Options (Future)
- [ ] Apply functional options pattern to Reader constructor
- [ ] Apply functional options pattern to Writer constructor
- [ ] Apply functional options pattern to Mapper constructor
- [ ] Apply functional options pattern to FanIn constructor
- [ ] Apply functional options pattern to FanOut constructor

### Component Improvements (Future)
- [ ] Consider adding overflow handling for Reducer (partial collection when limit is reached)
- [ ] Migrate Reducer to use RunnerBase for consistency
- [ ] Add context.Context support for cancellation
- [ ] Consider adding metrics/observability hooks

### Documentation
- [ ] Add more comprehensive examples
- [ ] Add architecture documentation if needed
- [ ] Document performance characteristics and best practices

### Testing
- [ ] Add benchmarks for all components
- [x] Add concurrency stress tests (race condition tests added)
- [x] Test resource cleanup under error conditions (covered by race tests)

### Features
- [ ] Consider adding metrics/observability hooks
- [ ] Consider adding context.Context support for cancellation
- [ ] Explore additional concurrency patterns
