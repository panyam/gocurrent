# Next Steps

## Recent Changes

### Component Standardization - Phase 1: Channel Direction Fixes (Completed)
- ✅ Fixed `FanOut.SendChan()` to return `chan<- T` (write-only) instead of `<-chan T`
- ✅ Fixed `Writer.SendChan()` to return `chan<- W` (write-only) instead of `chan W`
- ✅ Fixed `FanIn.RecvChan()` to return `<-chan T` (read-only) instead of `chan T`
- ✅ All tests continue to pass after fixes
- ✅ Created STANDARDIZATION.md with comprehensive analysis

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

### Component Standardization - Phase 2: Method Naming (In Progress)
- [ ] Replace `RecvChan()` with `OutputChan()` across all components
- [ ] Replace `SendChan()` with `InputChan()` across all components
- [ ] Update all tests and examples to use new method names
- [ ] Update README.md with standardized naming

### Component Standardization - Phase 3: Lifecycle Management
- [ ] Add `ClosedChan() <-chan error` to Mapper, FanIn, FanOut, Reducer
- [ ] Migrate Reducer to use RunnerBase for consistency
- [ ] Standardize command patterns across all components

### Component Standardization - Phase 4: Functional Options
- [ ] Apply functional options pattern to Reader constructor
- [ ] Apply functional options pattern to Writer constructor
- [ ] Apply functional options pattern to Mapper constructor
- [ ] Apply functional options pattern to FanIn constructor
- [ ] Apply functional options pattern to FanOut constructor

### API Improvements
- [ ] Consider adding overflow handling for Reducer (partial collection when limit is reached)

### Documentation
- [ ] Add more comprehensive examples
- [ ] Add architecture documentation if needed
- [ ] Document performance characteristics and best practices

### Testing
- [ ] Add benchmarks for all components
- [ ] Add concurrency stress tests
- [ ] Test resource cleanup under error conditions

### Features
- [ ] Consider adding metrics/observability hooks
- [ ] Consider adding context.Context support for cancellation
- [ ] Explore additional concurrency patterns
