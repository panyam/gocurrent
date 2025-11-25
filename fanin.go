package gocurrent

import "log"

type fanInCmd[T any] struct {
	Name           string
	AddedChannel   <-chan T
	RemovedChannel <-chan T
}

// FanIn merges multiple input channels into a single output channel.
// It implements the fan-in concurrency pattern where messages from multiple
// sources are combined into one stream.
type FanIn[T any] struct {
	RunnerBase[fanInCmd[T]]
	// OnChannelRemoved is called when a channel is removed so the caller can
	// perform other cleanups etc based on this
	OnChannelRemoved func(fi *FanIn[T], inchan <-chan T)

	inputs     []*Mapper[T, T]
	selfOwnOut bool
	outChan    chan T
	closedChan chan error
}

// FanInOption is a functional option for configuring a FanIn
type FanInOption[T any] func(*FanIn[T])

// WithFanInOutputChan sets the output channel for the FanIn
func WithFanInOutputChan[T any](ch chan T) FanInOption[T] {
	return func(fi *FanIn[T]) {
		fi.outChan = ch
		fi.selfOwnOut = false
	}
}

// WithFanInOutputBuffer creates a buffered output channel for the FanIn
func WithFanInOutputBuffer[T any](size int) FanInOption[T] {
	return func(fi *FanIn[T]) {
		fi.outChan = make(chan T, size)
		fi.selfOwnOut = true
	}
}

// WithFanInOnChannelRemoved sets the callback for when a channel is removed
func WithFanInOnChannelRemoved[T any](fn func(*FanIn[T], <-chan T)) FanInOption[T] {
	return func(fi *FanIn[T]) {
		fi.OnChannelRemoved = fn
	}
}

// NewFanIn creates a new FanIn that merges multiple input channels with functional options.
// By default, creates and owns an unbuffered output channel. Use options to customize.
// The FanIn starts running immediately upon creation.
//
// Examples:
//   // Simple usage with owned channel (backwards compatible)
//   fanin := NewFanIn[int]()
//
//   // With existing channel (backwards compatible)
//   outChan := make(chan int, 10)
//   fanin := NewFanIn(WithFanInOutputChan(outChan))
//
//   // With buffered output
//   fanin := NewFanIn[int](WithFanInOutputBuffer[int](100))
func NewFanIn[T any](opts ...FanInOption[T]) *FanIn[T] {
	out := &FanIn[T]{
		RunnerBase: NewRunnerBase(fanInCmd[T]{Name: "stop"}),
		selfOwnOut: true,
		closedChan: make(chan error, 1),
	}

	// Apply options
	for _, opt := range opts {
		opt(out)
	}

	// Create default channel if not provided
	if out.outChan == nil {
		out.outChan = make(chan T)
	}

	out.start()
	return out
}

// ClosedChan returns the channel used to signal when the fan-in is done
func (fi *FanIn[T]) ClosedChan() <-chan error {
	return fi.closedChan
}

// OutputChan returns the channel on which merged output can be received.
func (fi *FanIn[T]) OutputChan() <-chan T {
	return fi.outChan
}

// Add adds one or more input channels to the FanIn.
// Messages from these channels will be merged into the output channel.
// Panics if any input channel is nil.
func (fi *FanIn[T]) Add(inputs ...<-chan T) {
	for _, input := range inputs {
		if input == nil {
			panic("Cannot add nil channels")
		}
		fi.controlChan <- fanInCmd[T]{Name: "add", AddedChannel: input}
	}
}

// Remove removes an input channel from the FanIn's monitor list.
// The channel will no longer contribute to the merged output.
func (fi *FanIn[T]) Remove(target <-chan T) {
	fi.controlChan <- fanInCmd[T]{Name: "remove", RemovedChannel: target}
}

// Count returns the number of input channels currently being monitored.
func (fi *FanIn[T]) Count() int {
	return len(fi.inputs)
}

func (fi *FanIn[T]) cleanup() {
	for _, input := range fi.inputs {
		input.Stop()
	}
	fi.inputs = nil
	if fi.selfOwnOut {
		close(fi.outChan)
	}
	fi.outChan = nil
	close(fi.closedChan)
	fi.RunnerBase.cleanup()
}

func (fi *FanIn[T]) start() {
	fi.RunnerBase.start()
	go func() {
		defer fi.cleanup()
		for {
			cmd := <-fi.controlChan
			if cmd.Name == "stop" {
				return
			} else if cmd.Name == "add" {
				// Add a new reader to our list
				input := NewPipe(cmd.AddedChannel, fi.outChan)
				fi.inputs = append(fi.inputs, input)
				input.OnDone = fi.pipeClosed
			} else if cmd.Name == "remove" {
				// Remove an existing reader from our list
				log.Println("Removing channel: ", cmd.RemovedChannel)
				fi.remove(cmd.RemovedChannel)
			}
		}
	}()
}

func (fi *FanIn[T]) removeAt(index int) {
	inchan := fi.inputs[index].input
	fi.inputs[index].Stop()
	fi.inputs[index] = fi.inputs[len(fi.inputs)-1]
	fi.inputs = fi.inputs[:len(fi.inputs)-1]
	if fi.OnChannelRemoved != nil {
		fi.OnChannelRemoved(fi, inchan)
	}
}

func (fi *FanIn[T]) pipeClosed(p *Mapper[T, T]) {
	for index, input := range fi.inputs {
		if input == p {
			fi.removeAt(index)
			break
		}
	}
}

func (fi *FanIn[T]) remove(inchan <-chan T) {
	for index, input := range fi.inputs {
		if input.input == inchan {
			fi.removeAt(index)
			break
		}
	}
}
