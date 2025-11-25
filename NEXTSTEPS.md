# Next Steps

## Recent Changes

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
- [ ] Add concurrency stress tests
- [ ] Test resource cleanup under error conditions

### Features
- [ ] Consider adding metrics/observability hooks
- [ ] Consider adding context.Context support for cancellation
- [ ] Explore additional concurrency patterns
