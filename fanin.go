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
	stopping   chan struct{} // closed at start of cleanup to unblock pipeClosed
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
//
//	// Simple usage with owned channel (backwards compatible)
//	fanin := NewFanIn[int]()
//
//	// With existing channel (backwards compatible)
//	outChan := make(chan int, 10)
//	fanin := NewFanIn(WithFanInOutputChan(outChan))
//
//	// With buffered output
//	fanin := NewFanIn[int](WithFanInOutputBuffer[int](100))
func NewFanIn[T any](opts ...FanInOption[T]) *FanIn[T] {
	out := &FanIn[T]{
		RunnerBase: NewRunnerBase(fanInCmd[T]{Name: "stop"}),
		selfOwnOut: true,
		closedChan: make(chan error, 1),
		stopping:   make(chan struct{}),
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
	// Signal stopping FIRST so pipeClosed callbacks can return immediately
	// instead of blocking on controlChan. This breaks the deadlock cycle:
	// cleanup → pipe.Stop() → pipe.cleanup → pipeClosed → controlChan send.
	close(fi.stopping)
	for _, input := range fi.inputs {
		input.Stop()
	}
	if fi.selfOwnOut {
		close(fi.outChan)
	}
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
				// Set OnDone at construction time via option to avoid racing
				// with the Mapper goroutine (which starts immediately).
				input := NewMapper(cmd.AddedChannel, fi.outChan, idMapperFunc[T],
					WithMapperOnDone[T, T](func(m *Mapper[T, T]) { fi.pipeClosed(m) }))
				fi.inputs = append(fi.inputs, input)
			} else if cmd.Name == "remove" {
				// Remove an existing reader from our list
				log.Println("Removing channel: ", cmd.RemovedChannel)
				fi.remove(cmd.RemovedChannel)
			} else if cmd.Name == "pipe_closed" {
				// A pipe self-terminated (its input channel was closed).
				// Remove it from our inputs list.
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

// pipeClosed is the OnDone callback for internal Mapper (pipe) goroutines.
// It routes the notification through the FanIn's control channel to ensure
// fi.inputs is only modified by the FanIn goroutine, avoiding data races.
// Uses the stopping channel to avoid deadlock when FanIn is shutting down.
func (fi *FanIn[T]) pipeClosed(p *Mapper[T, T]) {
	select {
	case fi.controlChan <- fanInCmd[T]{Name: "pipe_closed", RemovedChannel: p.input}:
	case <-fi.stopping:
		// FanIn is shutting down, cleanup will handle everything
	case <-fi.Done():
		// FanIn already stopped
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
