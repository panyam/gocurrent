# Next Steps

## Recent Changes

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

### API Improvements
- [ ] Consider applying functional options pattern to other constructors (Reader, Writer, Mapper, etc.)
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
