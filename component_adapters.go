package gocurrent

// This file contains adapter methods to make existing components
// conform to the Component, InputComponent, and OutputComponent interfaces.

// Reader adapters

// OutputChan is an alias for RecvChan to conform to OutputComponent interface
func (r *Reader[R]) OutputChan() <-chan Message[R] {
	return r.RecvChan()
}

// Writer adapters

// InputChan is an alias for SendChan to conform to InputComponent interface
func (w *Writer[W]) InputChan() chan<- W {
	return w.SendChan()
}

// Mapper adapters

// InputChan returns the input channel (exposes the private field for Component interface)
func (m *Mapper[I, O]) InputChan() chan<- I {
	// Mapper takes a read-only channel, which makes it incompatible with InputComponent
	// This is a limitation of the current design where Mapper doesn't own its input
	// For now, return nil to indicate Mapper doesn't support this interface fully
	return nil
}

// OutputChan returns the output channel (exposes the private field for Component interface)
func (m *Mapper[I, O]) OutputChan() <-chan O {
	// Mapper takes a write-only channel, which makes it incompatible with OutputComponent
	// This is a limitation of the current design where Mapper doesn't own its output
	// For now, return nil to indicate Mapper doesn't support this interface fully
	return nil
}

// Reducer adapters

// InputChan is an alias for SendChan to conform to InputComponent interface
func (r *Reducer[T, C, U]) InputChan() chan<- T {
	return r.SendChan()
}

// OutputChan is an alias for RecvChan to conform to OutputComponent interface
func (r *Reducer[T, C, U]) OutputChan() <-chan U {
	return r.RecvChan()
}

// IsRunning returns true if the reducer is still running
func (r *Reducer[T, C, U]) IsRunning() bool {
	// Reducer doesn't have an isRunning flag, check if channels are not nil
	return r.inputChan != nil
}

// FanIn adapters

// OutputChan is an alias for RecvChan to conform to OutputComponent interface
func (fi *FanIn[T]) OutputChan() <-chan T {
	return fi.RecvChan()
}

// FanOut adapters

// InputChan returns the input channel for FanOut to conform to InputComponent interface
func (fo *FanOut[T]) InputChan() chan<- T {
	return fo.inputChan
}

// NOTE: FanOut.SendChan() currently returns <-chan T which is incorrect.
// It should return chan<- T (write-only). This is a bug that should be fixed.
// For now, we provide the correct InputChan() method above.

// Pipe adapters (Pipe is just a Mapper, so inherits its adapters)
