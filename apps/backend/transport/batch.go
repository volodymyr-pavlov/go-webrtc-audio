package transport

import (
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/ipv4"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/metrics"
)

var _ net.PacketConn = &BatchConn{}

// datagram is one queued packet. Datagrams are reused, so a write does not allocate.
type datagram struct {
	payload [maxDatagram]byte
	n       int
	addr    netip.AddrPort
	bufs    [1][]byte
}

var datagrams = sync.Pool{
	New: func() any { return &datagram{} },
}

// Flags of the state of a [BatchConn]. They are only ever set, never cleared.
const (
	stateClosed uint32 = 1 << iota

	// stateSleeping is set by the sender goroutine when it has found the queue empty and is about to wait to be woken.
	// A writer that pushes while it is set clears it and wakes the sender; whichever of them clears it does the waking.
	stateSleeping
)

// A BatchConn is a UDP socket whose writes are queued and sent in batches. It implements [net.PacketConn], and the
// methods ReadFromAddrPort and WriteToAddrPort that let Pion's ICE multiplexer skip the address allocations. It is
// safe for concurrent use.
type BatchConn struct {
	conn    *net.UDPConn
	packets *ipv4.PacketConn
	size    int
	delay   time.Duration
	metrics *metrics.Metrics
	logger  *slog.Logger

	// queue holds the packets that wait to be sent. A full queue drops the packet, as a full socket buffer would.
	queue *ring

	// wakeCh wakes the sender goroutine when it sleeps and a packet arrives.
	wakeCh chan struct{}

	// state holds the flags above.
	state atomic.Uint32

	// lastLog is when the last failed send was reported, in nanoseconds since the Unix epoch.
	lastLog atomic.Int64

	doneCh chan struct{}
	wg     sync.WaitGroup

	// The sender goroutine alone uses these, so they need no synchronization.
	batch    []*datagram
	messages []ipv4.Message
	addrs    map[netip.AddrPort]*net.UDPAddr
	maxAddrs int
}

// NewBatchConn returns a [BatchConn] that sends through conn, and starts its sender goroutine. [BatchConn.Close]
// closes conn.
func NewBatchConn(conn *net.UDPConn, opts BatchOptions) *BatchConn {
	size := opts.Size
	if size <= 0 {
		size = defaultBatchSize
	}
	maxQueued := opts.MaxQueued
	if maxQueued <= 0 {
		maxQueued = defaultMaxQueued
	}
	maxAddrs := opts.MaxAddrs
	if maxAddrs <= 0 {
		maxAddrs = defaultMaxAddrs
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	c := &BatchConn{
		conn:     conn,
		packets:  ipv4.NewPacketConn(conn),
		size:     size,
		delay:    opts.Delay,
		metrics:  opts.Metrics,
		logger:   logger,
		queue:    newRing(maxQueued),
		wakeCh:   make(chan struct{}, 1),
		doneCh:   make(chan struct{}),
		batch:    make([]*datagram, 0, size),
		messages: make([]ipv4.Message, 0, size),
		addrs:    map[netip.AddrPort]*net.UDPAddr{},
		maxAddrs: maxAddrs,
	}
	c.wg.Add(1)
	go c.run()
	return c
}

// WriteTo queues p for addr and returns at once. The bytes of p are copied, so the caller may reuse them. It returns
// [ErrBadAddr] if addr is not a UDP address and [ErrClosed] after [BatchConn.Close]. A send that later fails is not
// reported, as with any UDP write that is accepted by the kernel and lost on the way.
func (c *BatchConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	udp, ok := addr.(*net.UDPAddr)
	if !ok {
		return 0, ErrBadAddr
	}
	return c.WriteToAddrPort(p, udp.AddrPort())
}

// WriteToAddrPort queues p for addr and returns at once, like [BatchConn.WriteTo], without allocating.
func (c *BatchConn) WriteToAddrPort(p []byte, addr netip.AddrPort) (int, error) {
	if c.state.Load()&stateClosed != 0 {
		return 0, ErrClosed
	}
	if len(p) > maxDatagram {
		return c.conn.WriteToUDPAddrPort(p, addr)
	}
	d := datagrams.Get().(*datagram)
	d.n = copy(d.payload[:], p)
	d.addr = addr
	if !c.queue.push(d) {
		datagrams.Put(d)
		if c.metrics != nil {
			c.metrics.Dropped.Add(1)
		}
		return len(p), nil
	}
	if c.state.Load()&stateSleeping != 0 && c.state.And(^stateSleeping)&stateSleeping != 0 {
		select {
		case c.wakeCh <- struct{}{}:
		default:
		}
	}
	return len(p), nil
}

// run sends what is queued, a batch at a time, and sleeps while the queue is empty.
func (c *BatchConn) run() {
	defer c.wg.Done()
	for {
		d, ok := c.queue.pop()
		if !ok {
			// Say that the sender is about to sleep, and look once more: a packet pushed before the flag was set did not
			// wake anybody.
			c.state.Or(stateSleeping)
			if d, ok = c.queue.pop(); !ok {
				select {
				case <-c.doneCh:
					c.drain()
					return
				case <-c.wakeCh:
				}
				continue
			}
			c.state.And(^stateSleeping)
		}
		c.batch = append(c.batch[:0], d)
		if c.delay > 0 {
			c.wait()
		}
		c.fill()
		c.send(c.batch)
		c.drain()
	}
}

// wait holds the batch for the delay, so that more packets can join it, unless the socket is closed first.
func (c *BatchConn) wait() {
	timer := time.NewTimer(c.delay)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-c.doneCh:
	}
}

// fill adds to the batch what is already queued, up to the size of a batch, without waiting.
func (c *BatchConn) fill() {
	for len(c.batch) < c.size {
		d, ok := c.queue.pop()
		if !ok {
			return
		}
		c.batch = append(c.batch, d)
	}
}

// drain sends everything that is queued, in batches, and then whatever arrived while it was sending.
func (c *BatchConn) drain() {
	for {
		c.batch = c.batch[:0]
		c.fill()
		if len(c.batch) == 0 {
			return
		}
		c.send(c.batch)
	}
}

// udpAddr returns the [net.UDPAddr] of a, made once. The sender goroutine alone calls it. When the table is full one
// address, whichever the map yields first, is dropped for the new one: emptying the table would make every listener
// allocate again at the next tick.
func (c *BatchConn) udpAddr(a netip.AddrPort) *net.UDPAddr {
	if addr, ok := c.addrs[a]; ok {
		return addr
	}
	if len(c.addrs) >= c.maxAddrs {
		for old := range c.addrs {
			delete(c.addrs, old)
			break
		}
	}
	addr := net.UDPAddrFromAddrPort(a)
	c.addrs[a] = addr
	return addr
}

// send writes one batch with as few system calls as the platform allows, and gives the datagrams back.
func (c *BatchConn) send(batch []*datagram) {
	c.messages = c.messages[:0]
	for _, d := range batch {
		addr := c.udpAddr(d.addr)
		d.bufs[0] = d.payload[:d.n]
		c.messages = append(c.messages, ipv4.Message{
			Buffers: d.bufs[:],
			Addr:    addr,
		})
	}
	messages := c.messages
	for len(messages) > 0 {
		n, err := c.packets.WriteBatch(messages, 0)
		if c.metrics != nil && n > 0 {
			c.metrics.UDPSends.Add(1)
			c.metrics.UDPPackets.Add(uint64(n))
		}
		if err != nil || n <= 0 {
			// The packet at the head cannot be sent, for instance because the socket has no route to its address. UDP
			// does not retry, so drop it and go on with the rest. WriteBatch reports -1 when it sent nothing, so the
			// count must be fixed up before it is used to move on, or the same packets would be sent for ever.
			if c.metrics != nil {
				c.metrics.WriteErrors.Add(1)
			}
			c.logSendError(err, len(messages), n)
			n = max(n, 0) + 1
		}
		messages = messages[min(n, len(messages)):]
	}
	for _, d := range batch {
		datagrams.Put(d)
	}
}

// ReadFrom reads a packet from the socket.
func (c *BatchConn) ReadFrom(p []byte) (int, net.Addr, error) {
	return c.conn.ReadFrom(p)
}

// ReadFromAddrPort reads a packet from the socket without allocating its address.
func (c *BatchConn) ReadFromAddrPort(p []byte) (int, netip.AddrPort, error) {
	return c.conn.ReadFromUDPAddrPort(p)
}

// Close sends what is queued, stops the sender and closes the socket. It may be called more than once.
func (c *BatchConn) Close() error {
	if c.state.Or(stateClosed)&stateClosed != 0 {
		return nil
	}
	close(c.doneCh)
	c.wg.Wait()
	return c.conn.Close()
}

// LocalAddr returns the address of the socket.
func (c *BatchConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// SetDeadline sets the read and write deadlines of the socket. The write deadline applies to the sender goroutine's
// sends and not to [BatchConn.WriteTo], which only queues.
func (c *BatchConn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}

// SetReadDeadline sets the read deadline of the socket.
func (c *BatchConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}

// SetWriteDeadline sets the write deadline of the socket. The sender goroutine's sends honour it, and a send that
// misses it is dropped like any other failed send. [BatchConn.WriteTo] only queues, and never waits for it.
func (c *BatchConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

// SetWriteBuffer sets the size of the socket's send buffer, which absorbs the burst of a batch.
func (c *BatchConn) SetWriteBuffer(bytes int) error {
	return c.conn.SetWriteBuffer(bytes)
}

// logSendError reports a failed send, at most once a second, because a send that fails often fails for every packet.
func (c *BatchConn) logSendError(err error, queued, sent int) {
	now := time.Now().UnixNano()
	last := c.lastLog.Load()
	if now-last < int64(time.Second) || !c.lastLog.CompareAndSwap(last, now) {
		return
	}
	c.logger.Warn("udp send failed", "err", err, "messages", queued, "sent", sent)
}
