package fanout

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/pion/rtp"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/metrics"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/worker"
)

var _ io.Writer = &Router{}
var _ worker.Worker = &Router{}

// Flags of the state of a [Router]. They are only ever set, never cleared.
const (
	stateClosed uint32 = 1 << iota
)

// Flags of the state of a listener. They are only ever set, never cleared.
const (
	listenerStopped uint32 = 1 << iota
	listenerEvicted
)

// A PacketWriter sends an RTP packet to one listener. Pion's local tracks implement it. It is not [io.Writer], because
// that would make every listener parse the same bytes again.
type PacketWriter interface {
	WriteRTP(p *rtp.Packet) error
}

// A Router copies each written packet to every listener across its shards. It is safe for concurrent use, and it holds
// no lock: its tables are atomic.
type Router struct {
	logger     *slog.Logger
	metrics    *metrics.Metrics
	shards     []*shard
	queueLen   int
	maxDropped int
	onEvict    func(id uint64)
	doneCh     chan struct{}

	// state holds the flags above.
	state atomic.Uint32

	// listeners maps the id of each listener to its *listener.
	listeners sync.Map

	// size is the number of listeners.
	size atomic.Int64
}

// shard owns a share of the listeners. Its list is replaced as a whole, never changed, so the goroutine that copies
// packets reads it without a lock, and adding or removing a listener is a compare-and-swap that retries when another
// change got in first.
type shard struct {
	inCh      chan *rtp.Packet
	listeners atomic.Pointer[[]*listener]
}

// A listener is one connected client. With a queue, a writer goroutine drains it; without one, the shard writes.
type listener struct {
	id      uint64
	shard   *shard
	writer  PacketWriter
	queueCh chan *rtp.Packet
	stopCh  chan struct{}

	// state holds the flags above.
	state atomic.Uint32

	// dropped is the number of packets in a row that the listener lost because its queue was full, and failed is the
	// number in a row that could not be written. Either reaching [RouterOptions.MaxDropped] evicts the listener.
	dropped atomic.Int32
	failed  atomic.Int32
}

// NewRouter returns the [Router] that opts describes. It starts no goroutines until [Router.Do] runs.
func NewRouter(opts RouterOptions) *Router {
	count := opts.Shards
	if count <= 0 {
		count = runtime.GOMAXPROCS(0)
	}
	queueLen := opts.QueueLen
	if queueLen == 0 {
		queueLen = defaultQueueLen
	}
	maxDropped := opts.MaxDropped
	if maxDropped <= 0 {
		maxDropped = defaultMaxDropped
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	m := opts.Metrics
	if m == nil {
		m = metrics.New()
	}
	router := &Router{
		logger:     logger,
		metrics:    m,
		queueLen:   queueLen,
		maxDropped: maxDropped,
		onEvict:    opts.OnEvict,
		shards:     make([]*shard, count),
		doneCh:     make(chan struct{}),
	}
	for i := range router.shards {
		s := &shard{
			inCh: make(chan *rtp.Packet, shardQueueLen),
		}
		empty := []*listener{}
		s.listeners.Store(&empty)
		router.shards[i] = s
	}
	return router
}

// Write takes one marshaled RTP packet and hands it to every shard, parsing it once for all of them. It never waits: a
// shard that is too far behind loses the packet, and its listeners hear a gap. As for every [io.Writer], b may be
// reused as soon as Write returns, so the packet is parsed from a copy. It returns an error only if b is not an RTP
// packet.
func (r *Router) Write(b []byte) (int, error) {
	packet := &rtp.Packet{}
	if err := packet.Unmarshal(bytes.Clone(b)); err != nil {
		return 0, err
	}
	r.metrics.Packets.Add(1)
	for _, s := range r.shards {
		select {
		case s.inCh <- packet:
		default:
			r.metrics.Dropped.Add(uint64(len(*s.listeners.Load())))
		}
	}
	return len(b), nil
}

// Add puts a listener in the least loaded shard and starts its writer. It returns [ErrClosed] after [Router.Close] and
// [ErrDuplicate] if id is taken.
func (r *Router) Add(id uint64, w PacketWriter) error {
	if r.state.Load()&stateClosed != 0 {
		return ErrClosed
	}
	target := r.shards[0]
	for _, s := range r.shards[1:] {
		if len(*s.listeners.Load()) < len(*target.listeners.Load()) {
			target = s
		}
	}
	l := &listener{
		id:     id,
		shard:  target,
		writer: w,
		stopCh: make(chan struct{}),
	}
	if r.queueLen > 0 {
		l.queueCh = make(chan *rtp.Packet, r.queueLen)
	}
	if _, taken := r.listeners.LoadOrStore(id, l); taken {
		return ErrDuplicate
	}
	for {
		old := target.listeners.Load()
		next := make([]*listener, len(*old), len(*old)+1)
		copy(next, *old)
		next = append(next, l)
		if target.listeners.CompareAndSwap(old, &next) {
			break
		}
	}
	r.size.Add(1)
	// A Close that ran while the listener was being added may have missed it.
	if r.state.Load()&stateClosed != 0 {
		r.Remove(id)
		return ErrClosed
	}
	if l.queueCh != nil {
		go r.write(l)
	}
	return nil
}

// Remove takes a listener out and stops its writer. It does nothing if the id is unknown.
func (r *Router) Remove(id uint64) {
	value, ok := r.listeners.LoadAndDelete(id)
	if !ok {
		return
	}
	l := value.(*listener)
	for {
		old := l.shard.listeners.Load()
		next := make([]*listener, 0, len(*old))
		for _, other := range *old {
			if other != l {
				next = append(next, other)
			}
		}
		if l.shard.listeners.CompareAndSwap(old, &next) {
			break
		}
	}
	r.size.Add(-1)
	if l.state.Or(listenerStopped)&listenerStopped == 0 {
		close(l.stopCh)
	}
}

// Len returns the number of listeners.
func (r *Router) Len() int {
	return int(r.size.Load())
}

// write is the goroutine of a listener: it sends what is in the queue until the listener is removed.
func (r *Router) write(l *listener) {
	for {
		select {
		case <-l.stopCh:
			return
		case p := <-l.queueCh:
			r.send(l, p)
		}
	}
}

// send writes p to a listener, on the goroutine of the listener's writer or, without queues, of its shard.
func (r *Router) send(l *listener, p *rtp.Packet) {
	if err := l.writer.WriteRTP(p); err != nil {
		r.metrics.WriteErrors.Add(1)
		r.evictAfter(l, l.failed.Add(1))
		return
	}
	l.failed.Store(0)
	r.metrics.Delivered.Add(1)
}

// evictAfter evicts a listener, once, when the number of packets in a row that went wrong reaches the limit.
func (r *Router) evictAfter(l *listener, inARow int32) {
	if int(inARow) < r.maxDropped || r.onEvict == nil {
		return
	}
	if l.state.Or(listenerEvicted)&listenerEvicted == 0 {
		r.metrics.Evicted.Add(1)
		go r.onEvict(l.id)
	}
}

// Do runs the shards until ctx is cancelled or the router is closed, and then returns nil.
func (r *Router) Do(ctx context.Context) error {
	var wg sync.WaitGroup
	for _, s := range r.shards {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.run(ctx, s)
		}()
	}
	wg.Wait()
	return nil
}

// run forwards every packet that arrives at the shard s to all the listeners of the shard, until ctx is cancelled or
// the router is closed.
func (r *Router) run(ctx context.Context, s *shard) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.doneCh:
			return
		case p := <-s.inCh:
			for _, l := range *s.listeners.Load() {
				r.enqueue(l, p)
			}
		}
	}
}

// enqueue hands p to a listener. Without a queue it writes p at once. With one it drops p if the queue is full, and
// evicts the listener after too many drops in a row.
func (r *Router) enqueue(l *listener, p *rtp.Packet) {
	if l.queueCh == nil {
		r.send(l, p)
		return
	}
	select {
	case l.queueCh <- p:
		l.dropped.Store(0)
	default:
		r.metrics.Dropped.Add(1)
		r.evictAfter(l, l.dropped.Add(1))
	}
}

// Close stops every listener's writer and makes [Router.Do] return. It may be called more than once.
func (r *Router) Close() error {
	if r.state.Or(stateClosed)&stateClosed != 0 {
		return nil
	}
	close(r.doneCh)
	r.listeners.Range(func(id, _ any) bool {
		r.Remove(id.(uint64))
		return true
	})
	return nil
}
