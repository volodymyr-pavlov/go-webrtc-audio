package transport

import "sync/atomic"

// A ring is a bounded queue that many goroutines may push to and many may pop from, without a lock. It is the design of
// Dmitry Vyukov's bounded MPMC queue: every slot carries a sequence number that says whose turn it is, and a push or a
// pop claims its position with a compare-and-swap.
type ring struct {
	slots []slot
	mask  uint64

	// head is the position of the next push and tail the position of the next pop. Producers write head and the
	// consumer writes tail, so each has a cache line of its own, and neither shares one with the fields above.
	_    [cacheLine]byte
	head atomic.Uint64
	_    [cacheLine - 8]byte
	tail atomic.Uint64
}

// cacheLine is the size in bytes of a cache line on the machines this runs on.
const cacheLine = 64

type slot struct {
	// seq is the position that may push to the slot, and, once it holds a value, that position plus one, which is the
	// position that may pop it.
	seq   atomic.Uint64
	value *datagram
}

// newRing returns a ring that holds at least size values. Its size is rounded up to a power of two.
func newRing(size int) *ring {
	n := 1
	for n < size {
		n <<= 1
	}
	r := &ring{
		slots: make([]slot, n),
		mask:  uint64(n - 1),
	}
	for i := range r.slots {
		r.slots[i].seq.Store(uint64(i))
	}
	return r
}

// push adds d, and reports false if the ring is full.
func (r *ring) push(d *datagram) bool {
	for {
		pos := r.head.Load()
		s := &r.slots[pos&r.mask]
		switch diff := int64(s.seq.Load()) - int64(pos); {
		case diff == 0:
			if r.head.CompareAndSwap(pos, pos+1) {
				s.value = d
				s.seq.Store(pos + 1)
				return true
			}
		case diff < 0:
			return false
		}
	}
}

// pop removes the oldest value, and reports false if the ring is empty.
func (r *ring) pop() (*datagram, bool) {
	for {
		pos := r.tail.Load()
		s := &r.slots[pos&r.mask]
		switch diff := int64(s.seq.Load()) - int64(pos+1); {
		case diff == 0:
			if r.tail.CompareAndSwap(pos, pos+1) {
				d := s.value
				s.value = nil
				s.seq.Store(pos + r.mask + 1)
				return d, true
			}
		case diff < 0:
			return nil, false
		}
	}
}
