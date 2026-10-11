package fanout

import (
	"log/slog"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/metrics"
)

const (
	defaultQueueLen   = 32
	defaultMaxDropped = 250
	shardQueueLen     = 16
)

// RouterOptions describe a [Router].
type RouterOptions struct {
	// Shards is the number of goroutines that copy packets to listeners. The default is the number of CPU cores.
	Shards int

	// QueueLen is how many packets a listener may have waiting. Every listener has a writer goroutine of its own that
	// sends them, so a listener that cannot keep up loses packets from its own queue and never delays the others. The
	// default is 32. A negative value has no queues and no writer goroutines: the shard writes to each listener itself,
	// which saves a goroutine for each listener, and a write that blocks holds back the other listeners of its shard.
	QueueLen int

	// MaxDropped is how many packets in a row a listener may lose, or have fail to be written, before it is evicted. The
	// default is 250, five seconds of audio at 20 ms frames.
	MaxDropped int

	// OnEvict is called, on its own goroutine, with the id of a listener that was evicted. It may be nil.
	OnEvict func(id uint64)

	// Metrics receives the counters of the router. The default is a registry nothing reads.
	Metrics *metrics.Metrics

	// Logger is where the router writes. The default is [slog.Default].
	Logger *slog.Logger
}
