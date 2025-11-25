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
}

// NewFanIn creates a new FanIn that merges multiple input channels into outChan.
// If outChan is nil, a new channel is created and owned by the FanIn.
// The FanIn starts running immediately upon creation.
func NewFanIn[T any](outChan chan T) *FanIn[T] {
	selfOwnOut := false
	if outChan == nil {
		outChan = make(chan T)
		selfOwnOut = true
	}
	out := &FanIn[T]{
		RunnerBase: NewRunnerBase(fanInCmd[T]{Name: "stop"}),
		outChan:    outChan,
		selfOwnOut: selfOwnOut,
	}
	out.start()
	return out
}

// RecvChan returns the channel on which merged output can be received.
func (fi *FanIn[T]) RecvChan() <-chan T {
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
		fi.wg.Done()
	}
	fi.inputs = nil
	if fi.selfOwnOut {
		close(fi.outChan)
	}
	fi.outChan = nil
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
				fi.wg.Add(1)
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
	fi.wg.Done()
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
