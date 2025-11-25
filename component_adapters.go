package gocurrent

// This file contains adapter methods to make existing components
// conform to the Component, InputComponent, and OutputComponent interfaces.

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

// IsRunning returns true if the reducer is still running
func (r *Reducer[T, C, U]) IsRunning() bool {
	// Reducer doesn't have an isRunning flag, check if channels are not nil
	return r.inputChan != nil
}

// Pipe adapters (Pipe is just a Mapper, so inherits its adapters)
