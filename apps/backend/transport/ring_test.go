package transport

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestRingKeepsOrderAndRefusesWhenFull(t *testing.T) {
	r := newRing(4)
	var in [4]*datagram
	for i := range in {
		in[i] = &datagram{n: i}
		if !r.push(in[i]) {
			t.Fatalf("push %d refused on a ring with room", i)
		}
	}
	if r.push(&datagram{}) {
		t.Fatal("a full ring took another value")
	}
	for i := range in {
		got, ok := r.pop()
		if !ok || got != in[i] {
			t.Fatalf("pop %d returned %v, %v", i, got, ok)
		}
	}
	if _, ok := r.pop(); ok {
		t.Fatal("an empty ring returned a value")
	}
	// It works again after a full turn.
	if !r.push(&datagram{n: 9}) {
		t.Fatal("push refused after the ring was emptied")
	}
	if got, ok := r.pop(); !ok || got.n != 9 {
		t.Fatalf("pop returned %v, %v", got, ok)
	}
}

func TestRingRoundsItsSizeUpToAPowerOfTwo(t *testing.T) {
	r := newRing(5)
	pushed := 0
	for r.push(&datagram{}) {
		pushed++
	}
	if pushed != 8 {
		t.Fatalf("a ring of 5 holds %d, want 8", pushed)
	}
}

func TestRingLosesNothingWithManyPushersAndPoppers(t *testing.T) {
	r := newRing(64)
	const pushers, each = 8, 5000
	var popped atomic.Int64
	var sum atomic.Int64
	var pushing, popping sync.WaitGroup
	doneCh := make(chan struct{})

	for range 4 {
		popping.Add(1)
		go func() {
			defer popping.Done()
			for {
				if d, ok := r.pop(); ok {
					popped.Add(1)
					sum.Add(int64(d.n))
					continue
				}
				select {
				case <-doneCh:
					return
				default:
				}
			}
		}()
	}
	for p := range pushers {
		pushing.Add(1)
		go func() {
			defer pushing.Done()
			for i := range each {
				d := &datagram{n: p*each + i}
				for !r.push(d) {
				}
			}
		}()
	}
	pushing.Wait()
	for popped.Load() < pushers*each {
	}
	close(doneCh)
	popping.Wait()

	want := int64(pushers*each) * int64(pushers*each-1) / 2
	if popped.Load() != pushers*each || sum.Load() != want {
		t.Fatalf("popped %d values with sum %d, want %d with sum %d", popped.Load(), sum.Load(), pushers*each, want)
	}
}
