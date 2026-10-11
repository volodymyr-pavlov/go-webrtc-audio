package fanout

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/rtp"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/inmem"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/metrics"
)

// recorder is a PacketWriter that keeps the sequence numbers it was given.
type recorder struct {
	seqs inmem.List[uint16]
}

func (r *recorder) WriteRTP(p *rtp.Packet) error {
	r.seqs.Add(p.SequenceNumber)
	return nil
}

func (r *recorder) count() int {
	return r.seqs.Len()
}

// blocker is a PacketWriter that does not return until it is released.
type blocker struct {
	releaseCh chan struct{}
	writes    atomic.Int32
}

func (b *blocker) WriteRTP(*rtp.Packet) error {
	b.writes.Add(1)
	<-b.releaseCh
	return nil
}

func startRouter(t *testing.T, opts RouterOptions) *Router {
	t.Helper()
	router := NewRouter(opts)
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		_ = router.Do(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		_ = router.Close()
		<-doneCh
	})
	return router
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// packet returns a marshaled RTP packet with the given sequence number.
func packet(seq uint16) []byte {
	b, err := (&rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			SequenceNumber: seq,
		},
		Payload: []byte{1, 2, 3},
	}).Marshal()
	if err != nil {
		panic(err)
	}
	return b
}

func TestEveryListenerGetsEveryPacketInOrder(t *testing.T) {
	router := startRouter(t, RouterOptions{Shards: 3})
	writers := make([]*recorder, 10)
	for i := range writers {
		writers[i] = &recorder{}
		if err := router.Add(uint64(i), writers[i]); err != nil {
			t.Fatal(err)
		}
	}
	for seq := range 20 {
		router.Write(packet(uint16(seq)))
		time.Sleep(time.Millisecond)
	}
	for i, w := range writers {
		waitFor(t, "all packets", func() bool { return w.count() == 20 })
		for want, got := range w.seqs.All() {
			if uint16(want) != got {
				t.Fatalf("listener %d got %v", i, w.seqs.All())
			}
		}
	}
}

func TestSlowListenerDoesNotHoldBackTheOthers(t *testing.T) {
	counters := metrics.New()
	router := startRouter(t, RouterOptions{
		Shards:   1,
		QueueLen: 4,
		Metrics:  counters,
	})
	slow := &blocker{releaseCh: make(chan struct{})}
	fast := &recorder{}
	if err := router.Add(1, slow); err != nil {
		t.Fatal(err)
	}
	if err := router.Add(2, fast); err != nil {
		t.Fatal(err)
	}
	for seq := range 50 {
		router.Write(packet(uint16(seq)))
		time.Sleep(time.Millisecond)
	}
	waitFor(t, "the fast listener to get everything", func() bool { return fast.count() == 50 })
	waitFor(t, "the slow listener to lose packets", func() bool { return counters.Dropped.Load() > 0 })
	close(slow.releaseCh)
}

func TestListenerThatKeepsLosingPacketsIsEvicted(t *testing.T) {
	evictedCh := make(chan uint64, 1)
	router := startRouter(t, RouterOptions{
		Shards:     1,
		QueueLen:   1,
		MaxDropped: 5,
		OnEvict:    func(id uint64) { evictedCh <- id },
	})
	stuck := &blocker{releaseCh: make(chan struct{})}
	defer close(stuck.releaseCh)
	if err := router.Add(7, stuck); err != nil {
		t.Fatal(err)
	}
	go func() {
		for seq := range 200 {
			router.Write(packet(uint16(seq)))
			time.Sleep(time.Millisecond)
		}
	}()
	select {
	case id := <-evictedCh:
		if id != 7 {
			t.Fatalf("evicted %d", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the stuck listener was never evicted")
	}
}

func TestRemoveStopsDelivery(t *testing.T) {
	router := startRouter(t, RouterOptions{Shards: 2})
	w := &recorder{}
	if err := router.Add(1, w); err != nil {
		t.Fatal(err)
	}
	router.Write(packet(0))
	waitFor(t, "the first packet", func() bool { return w.count() == 1 })
	router.Remove(1)
	if router.Len() != 0 {
		t.Fatalf("Len = %d", router.Len())
	}
	router.Write(packet(1))
	time.Sleep(20 * time.Millisecond)
	if w.count() != 1 {
		t.Fatalf("a removed listener got %d packets", w.count())
	}
}

func TestAddReportsDuplicatesAndClosedRouters(t *testing.T) {
	router := startRouter(t, RouterOptions{Shards: 1})
	if err := router.Add(1, &recorder{}); err != nil {
		t.Fatal(err)
	}
	if err := router.Add(1, &recorder{}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("got %v, want ErrDuplicate", err)
	}
	_ = router.Close()
	if err := router.Add(2, &recorder{}); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v, want ErrClosed", err)
	}
}

// failing is a PacketWriter whose writes all fail.
type failing struct{}

func (failing) WriteRTP(*rtp.Packet) error { return errors.New("closed") }

func TestWithoutQueuesTheShardWritesToEveryListener(t *testing.T) {
	router := startRouter(t, RouterOptions{
		Shards:   2,
		QueueLen: -1,
	})
	writers := []*recorder{
		{},
		{},
		{},
	}
	for i, w := range writers {
		if err := router.Add(uint64(i), w); err != nil {
			t.Fatal(err)
		}
	}
	for seq := range 10 {
		router.Write(packet(uint16(seq)))
		time.Sleep(time.Millisecond)
	}
	for _, w := range writers {
		waitFor(t, "all packets", func() bool { return w.count() == 10 })
	}
}

func TestWithoutQueuesAListenerThatFailsToWriteIsEvicted(t *testing.T) {
	evictedCh := make(chan uint64, 1)
	router := startRouter(t, RouterOptions{
		Shards:     1,
		QueueLen:   -1,
		MaxDropped: 3,
		OnEvict:    func(id uint64) { evictedCh <- id },
	})
	if err := router.Add(4, failing{}); err != nil {
		t.Fatal(err)
	}
	for seq := range 20 {
		router.Write(packet(uint16(seq)))
		time.Sleep(time.Millisecond)
	}
	select {
	case id := <-evictedCh:
		if id != 4 {
			t.Fatalf("evicted %d", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the failing listener was never evicted")
	}
}

func TestAQueuedListenerWhoseWritesFailIsEvicted(t *testing.T) {
	evictedCh := make(chan uint64, 1)
	router := startRouter(t, RouterOptions{
		Shards:     1,
		QueueLen:   4,
		MaxDropped: 3,
		OnEvict:    func(id uint64) { evictedCh <- id },
	})
	if err := router.Add(6, failing{}); err != nil {
		t.Fatal(err)
	}
	for seq := range 20 {
		router.Write(packet(uint16(seq)))
		time.Sleep(time.Millisecond)
	}
	select {
	case id := <-evictedCh:
		if id != 6 {
			t.Fatalf("evicted %d", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the listener whose writes fail was never evicted")
	}
}

func TestWriteRefusesBytesThatAreNotAnRTPPacket(t *testing.T) {
	router := startRouter(t, RouterOptions{Shards: 1})
	if n, err := router.Write([]byte{1, 2, 3}); err == nil || n != 0 {
		t.Fatalf("got %d, %v for bytes that are not RTP", n, err)
	}
}

func TestWriteDoesNotKeepTheCallersBytes(t *testing.T) {
	router := startRouter(t, RouterOptions{Shards: 1})
	w := &recorder{}
	if err := router.Add(1, w); err != nil {
		t.Fatal(err)
	}
	b := packet(7)
	if _, err := router.Write(b); err != nil {
		t.Fatal(err)
	}
	for i := range b {
		b[i] = 0xff // The playout reuses its buffer as soon as Write returns.
	}
	waitFor(t, "the packet", func() bool { return w.count() == 1 })
	if w.seqs.All()[0] != 7 {
		t.Fatalf("the listener got sequence %d, want 7: the router kept the caller's bytes", w.seqs.All()[0])
	}
}

func TestWriteAllocatesOncePerPacketAndNotOncePerListener(t *testing.T) {
	allocs := func(listeners int) float64 {
		router := startRouter(t, RouterOptions{
			Shards:   2,
			QueueLen: -1,
		})
		for i := range listeners {
			if err := router.Add(uint64(i), &recorder{}); err != nil {
				t.Fatal(err)
			}
		}
		b := packet(1)
		return testing.AllocsPerRun(200, func() { _, _ = router.Write(b) })
	}
	one, many := allocs(1), allocs(50)
	if one != many {
		t.Fatalf("%v allocations for one listener and %v for fifty, want the same: the cost is per packet", one, many)
	}
}
