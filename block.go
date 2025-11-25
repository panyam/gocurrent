package gocurrent

import (
	"fmt"
	"sync"
)

// Component represents any building block that can be part of a Block.
// All gocurrent primitives can implement this interface.
type Component interface {
	// Stop stops the component and cleans up resources
	Stop() error

	// IsRunning returns true if the component is currently running
	IsRunning() bool
}

// InputComponent represents a component with an input channel
type InputComponent[T any] interface {
	Component

	// InputChan returns the channel for sending input to this component
	InputChan() chan<- T

	// Send is a convenience method for sending to the input channel
	Send(T)
}

// OutputComponent represents a component with an output channel
type OutputComponent[T any] interface {
	Component

	// OutputChan returns the channel for receiving output from this component
	OutputChan() <-chan T
}

// Block represents a composite component made up of multiple connected primitives.
// A Block itself acts as a component and can be nested within other Blocks.
type Block struct {
	name       string
	components []Component
	mu         sync.RWMutex
	started    bool
	wg         sync.WaitGroup
}

// NewBlock creates a new block with the given name
func NewBlock(name string) *Block {
	return &Block{
		name:       name,
		components: make([]Component, 0),
	}
}

// Add adds a component to this block
func (b *Block) Add(component Component) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.components = append(b.components, component)
}

// Connect connects the output of one component to the input of another
// using a Pipe. Returns the pipe so it can be managed if needed.
func Connect[T any](from OutputComponent[T], to InputComponent[T]) *Mapper[T, T] {
	return NewPipe(from.OutputChan(), to.InputChan())
}

// ConnectWith connects two components using a custom mapper function
func ConnectWith[I, O any](from OutputComponent[I], to InputComponent[O],
	mapper func(I) (O, bool, bool)) *Mapper[I, O] {
	return NewMapper(from.OutputChan(), to.InputChan(), mapper)
}

// Stop stops all components in this block in reverse order
func (b *Block) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.started {
		return nil
	}

	// Stop in reverse order to allow downstream components to drain
	for i := len(b.components) - 1; i >= 0; i-- {
		if err := b.components[i].Stop(); err != nil {
			return fmt.Errorf("failed to stop component %d: %w", i, err)
		}
	}

	b.started = false
	b.wg.Wait()
	return nil
}

// IsRunning returns true if any component in the block is running
func (b *Block) IsRunning() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, comp := range b.components {
		if comp.IsRunning() {
			return true
		}
	}
	return false
}

// Name returns the block's name
func (b *Block) Name() string {
	return b.name
}

// Count returns the number of components in this block
func (b *Block) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.components)
}

// Example composite components that implement common patterns:

// Pipeline creates a linear sequence of components connected by pipes
type Pipeline[T any] struct {
	*Block
	input  chan T
	output chan T
}

// NewPipeline creates a new pipeline block
func NewPipeline[T any](name string) *Pipeline[T] {
	return &Pipeline[T]{
		Block:  NewBlock(name),
		input:  make(chan T),
		output: make(chan T),
	}
}

// InputChan implements InputComponent
func (p *Pipeline[T]) InputChan() chan<- T {
	return p.input
}

// OutputChan implements OutputComponent
func (p *Pipeline[T]) OutputChan() <-chan T {
	return p.output
}

// Send implements InputComponent
func (p *Pipeline[T]) Send(value T) {
	p.input <- value
}

// Example: Broadcast pattern - one input, multiple outputs
type Broadcast[T any] struct {
	*Block
	fanout *FanOut[T]
}

// NewBroadcast creates a broadcast block using FanOut
func NewBroadcast[T any](name string) *Broadcast[T] {
	fanout := NewFanOut[T](nil)
	block := NewBlock(name)
	block.Add(fanout)

	return &Broadcast[T]{
		Block:  block,
		fanout: fanout,
	}
}

// InputChan implements InputComponent
func (b *Broadcast[T]) InputChan() chan<- T {
	return b.fanout.inputChan
}

// Send implements InputComponent
func (b *Broadcast[T]) Send(value T) {
	b.fanout.Send(value)
}

// AddOutput adds a new output channel to the broadcast
func (b *Broadcast[T]) AddOutput(filter FilterFunc[T]) chan T {
	return b.fanout.New(filter)
}

// Example: Merge pattern - multiple inputs, one output
type Merge[T any] struct {
	*Block
	fanin *FanIn[T]
}

// NewMerge creates a merge block using FanIn
func NewMerge[T any](name string) *Merge[T] {
	fanin := NewFanIn[T](nil)
	block := NewBlock(name)
	block.Add(fanin)

	return &Merge[T]{
		Block: block,
		fanin: fanin,
	}
}

// OutputChan implements OutputComponent
func (m *Merge[T]) OutputChan() <-chan T {
	return m.fanin.RecvChan()
}

// AddInput adds a new input channel to the merge
func (m *Merge[T]) AddInput(input <-chan T) {
	m.fanin.Add(input)
}
