package gocurrent

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func ExampleFanIn() {
	// Create 5 input channels and send 5 numbers into them
	// the collector channel
	fanin := NewFanIn[int]()
	defer fanin.Stop()

	NUM_CHANS := 2
	NUM_MSGS := 3

	var inchans []chan int
	for i := 0; i < NUM_CHANS; i++ {
		inchan := make(chan int)
		inchans = append(inchans, inchan)
		fanin.Add(inchan)
	}

	for i := 0; i < NUM_CHANS; i++ {
		go func(inchan chan int) {
			// send some  numbers into this fanin
			for j := 0; j < NUM_MSGS; j++ {
				inchan <- j
			}
		}(inchans[i])
	}

	// collect the fanned values
	var vals []int
	for i := 0; i < NUM_CHANS*NUM_MSGS; i++ {
		val := <-fanin.OutputChan()
		vals = append(vals, val)
	}

	// sort and print them for testing
	sort.Ints(vals)

	for _, v := range vals {
		fmt.Println(v)
	}

	// Output:
	// 0
	// 0
	// 1
	// 1
	// 2
	// 2
}

func TestFanIn(t *testing.T) {
	log.Println("===================== TestFanIn =====================")
	inch := []chan int{
		make(chan int),
		make(chan int),
		make(chan int),
		make(chan int),
		make(chan int),
	}
	outch := make(chan int)
	fanin := NewFanIn(WithFanInOutputChan(outch))

	var vals []int
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		n := 0
		for fanin.IsRunning() {
			i := <-fanin.OutputChan()
			vals = append(vals, i)
			n += 1
			if n >= 15 {
				fanin.Stop()
			}
		}
		wg.Done()
	}()

	for ch := 0; ch < 5; ch++ {
		fanin.Add(inch[ch])
		for msg := 0; msg < 3; msg++ {
			v := ch*3 + msg
			// log.Println("Writing values: ", v)
			inch[ch] <- v
		}
	}
	wg.Wait()

	// Sort since fanin can combine in any order
	sort.Ints(vals)
	for i := 0; i < 15; i++ {
		assert.Equal(t, vals[i], i, "Out vals dont match")
	}
}

func TestMultiReadFanInToFanOut(t *testing.T) {
	log.Println("===================== TestMultiReadFanInToFanOut =====================")

	// FanIn: merge 5 channels
	fanin := NewFanIn[int](WithFanInOutputBuffer[int](5))
	inch := make([]chan int, 5)
	for i := range inch {
		inch[i] = make(chan int, 1)
		fanin.Add(inch[i])
	}

	// FanOut: broadcast to a writer
	var results []int
	var m sync.Mutex
	resch := make(chan int, 5)
	writer := NewWriter(func(val int) error {
		m.Lock()
		results = append(results, val)
		m.Unlock()
		resch <- val
		return nil
	})
	fanout := NewSyncFanOut[int](WithFanOutInputBuffer[int](5))
	<-fanout.Add(writer.InputChan(), nil, true)

	// Pump goroutine: FanIn → FanOut
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case val := <-fanin.OutputChan():
				fanout.Send(val)
			}
		}
	}()

	// Send 5 values (buffered channels so these don't block)
	for i := 0; i < 5; i++ {
		inch[i] <- i
	}
	// Collect all 5 results
	for i := 0; i < 5; i++ {
		<-resch
	}

	m.Lock()
	assert.Equal(t, 5, len(results))
	m.Unlock()
	close(done)
	fanin.Stop()
	fanout.Stop()
	writer.Stop()
}

func TestMultiReadFanInFromReaders(t *testing.T) {
	log.Println("===================== TestMultiReadFanInFromReaders =====================")
	makereader := func(ch chan int) *Reader[int] {
		return NewReader(func() (int, error) {
			val := <-ch
			return val, nil
		})
	}
	NUM_CHANS := 1
	var inch []chan int
	var readers []*Reader[int]
	for i := 0; i < NUM_CHANS; i++ {
		inch = append(inch, make(chan int))
		readers = append(readers, makereader(inch[i]))
	}

	fanin := NewFanIn[Message[int]]()
	for _, r := range readers {
		fanin.Add(r.OutputChan())
	}

	var results []bool
	for i := 0; i < NUM_CHANS; i++ {
		results = append(results, false)
	}
	var m sync.Mutex
	resch := make(chan int, NUM_CHANS)
	writer := NewWriter(func(val int) error {
		m.Lock()
		results[val] = true
		m.Unlock()
		resch <- val
		return nil
	})

	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case val, ok := <-fanin.OutputChan():
				if !ok {
					return
				}
				writer.Send(val.Value)
			}
		}
	}()

	for i := 0; i < NUM_CHANS; i++ {
		inch[i] <- i
	}
	for i := 0; i < NUM_CHANS; i++ {
		<-resch
	}

	// log.Println("Results: ", results)
	assert.Equal(t, len(results), NUM_CHANS)
	for i := 0; i < NUM_CHANS; i++ {
		assert.Equal(t, true, results[i])
	}
	close(done)
	fanin.Stop()
	writer.Stop()
}
