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
	fanin := NewFanIn[int](nil)
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
		val := <-fanin.RecvChan()
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
	fanin := NewFanIn(outch)

	var vals []int
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		n := 0
		for fanin.IsRunning() {
			i := <-fanin.RecvChan()
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
	inch := []chan int{
		make(chan int),
		make(chan int),
		make(chan int),
		make(chan int),
		make(chan int),
	}
	outch := make(chan int)
	fanin := NewFanIn(outch)

	for ch := 0; ch < 5; ch++ {
		fanin.Add(inch[ch])
	}

	var results []int
	var m sync.Mutex
	resch := make(chan int)
	writer := NewWriter(func(val int) error {
		m.Lock()
		results = append(results, val)
		m.Unlock()
		resch <- val
		return nil
	})
	fanout := NewFanOut[int](nil)
	fanout.Add(writer.SendChan(), nil, false)

	go func() {
		for {
			select {
			case val := <-fanin.RecvChan():
				fanout.Send(val)
				break
			}
		}
	}()

	// log.Println("Sending 5 values")
	for i := 0; i < 5; i++ {
		inch[i] <- i
	}
	// log.Println("Waiting 5 values")
	for i := 0; i < 5; i++ {
		<-resch
	}

	// log.Println("Results: ", results)
	assert.Equal(t, len(results), 5)
	fanin.Stop()
	fanout.Stop()
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

	fanin := NewFanIn[Message[int]](nil)
	for _, r := range readers {
		fanin.Add(r.RecvChan())
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

	go func() {
		for {
			select {
			case val, _ := <-fanin.RecvChan():
				writer.Send(val.Value)
				break
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
	fanin.Stop()
	writer.Stop()
}
