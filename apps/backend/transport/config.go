package transport

import (
	"log/slog"
	"time"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/metrics"
)

const (
	// maxDatagram is the largest packet a [BatchConn] queues; a larger one is written at once. It is one Ethernet frame.
	maxDatagram = 1500

	defaultBatchSize = 64
	defaultMaxQueued = 8192
	defaultMaxAddrs  = 16384
)

// BatchOptions describe a [BatchConn].
type BatchOptions struct {
	// Size is the largest number of packets sent by one system call. The default is 64.
	Size int

	// Delay is how long the sender waits after the first packet of a batch arrives before it sends, so that more
	// packets can join it. Zero sends at once; packets that arrive while a batch is being sent make the next one.
	Delay time.Duration

	// MaxQueued is the number of packets that may wait to be sent. A packet written to a full queue is dropped, which
	// is what a full socket buffer does. It should be at least the number of listeners, because every tick queues a packet
	// for each. The default is 8192.
	MaxQueued int

	// MaxAddrs is the number of destination addresses whose [net.UDPAddr] the sender keeps, so that a packet to a known
	// address allocates nothing. When it is full an address is dropped to make room for a new one, and the next packet to
	// the dropped address makes its [net.UDPAddr] again. It should be at least the number of listeners. The default is
	// 16384.
	MaxAddrs int

	// Metrics receives the counters of the sends. It may be nil.
	Metrics *metrics.Metrics

	// Logger is where a failed send is reported, at most once a second. The default is [slog.Default].
	Logger *slog.Logger
}
