package engine

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/metrics"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/playlist"
)

// Mode says how the stream reaches the listeners. Exactly one bit is set.
type Mode uint8

const (
	// ModeNaive runs one playout for each listener.
	ModeNaive Mode = 1 << iota

	// ModeTrack runs one playout and writes to one track that all listeners share.
	ModeTrack

	// ModeFanout runs one playout and a router with a queue and a writer for each listener.
	ModeFanout
)

// Valid reports whether m is one of the modes above.
func (m Mode) Valid() bool {
	return m == ModeNaive || m == ModeTrack || m == ModeFanout
}

// String returns the name of the mode, as the command line spells it.
func (m Mode) String() string {
	switch m {
	case ModeNaive:
		return "naive"
	case ModeTrack:
		return "track"
	case ModeFanout:
		return "fanout"
	}
	return "unknown"
}

// ParseMode returns the mode with the given name. It returns [ErrBadMode] for any other name.
func ParseMode(name string) (Mode, error) {
	for _, m := range []Mode{
		ModeNaive,
		ModeTrack,
		ModeFanout,
	} {
		if m.String() == name {
			return m, nil
		}
	}
	return 0, fmt.Errorf("%w %q", ErrBadMode, name)
}

// Flag is a bit of [Options.Flags].
type Flag uint8

const (
	// FlagLoopback offers the loopback address, so that a client on the same machine can connect.
	FlagLoopback Flag = 1 << iota
)

// Options describe an [Engine].
type Options struct {
	// Mode says how the stream reaches the listeners. The default, zero, is [ModeFanout].
	Mode Mode

	// Playlist is the audio to stream.
	Playlist *playlist.Playlist

	// ICEPort is the UDP port that every connection uses.
	ICEPort int

	// PublicIPs are the addresses the listeners should connect to, when the server is behind a NAT. Empty means the
	// addresses of the server's own interfaces.
	PublicIPs []string

	// Flags holds the bitwise flags: see [Flag].
	Flags Flag

	// Shards is the number of shards of the router in [ModeFanout]. The default is one per CPU core.
	Shards int

	// QueueLen is the length of a listener's queue in [ModeFanout]. The default, zero, is 32 packets, or no queue when
	// [Options.BatchSize] is above zero, because a send through the batching socket only puts the packet in its shared
	// queue and never waits. A negative value has no queues and no writer goroutines, and the shards write to the
	// listeners.
	QueueLen int

	// BatchSize turns on sending in batches when it is above zero: the packets of all listeners go through one queue per
	// socket, and a goroutine sends up to BatchSize of them with one system call (sendmmsg on Linux). Zero sends each
	// packet with its own system call.
	BatchSize int

	// BatchDelay is how long the sender of a batch waits for more packets after the first one. Zero sends at once.
	BatchDelay time.Duration

	// MaxListeners is the most listeners, connected or still connecting, that the engine holds at once. A listener over
	// the limit is refused with [ErrFull]. The default is 5000; a negative value has no limit.
	MaxListeners int

	// MaxConnecting is the most listeners that the engine holds at once that have not connected yet. A listener over the
	// limit is refused with [ErrBusy], so that offers that never connect cannot take every place of [Options.MaxListeners]
	// from the listeners that do. The default is 1000; a negative value has no limit.
	MaxConnecting int

	// ConnectTimeout is how long a listener may take to connect before it is dropped. The default is 15 seconds.
	ConnectTimeout time.Duration

	// EvictAfter is how long a connected listener may lose every packet, or have every write fail, before it is
	// dropped. The router counts it in packets of the playlist's frame. The default is 5 seconds.
	EvictAfter time.Duration

	// Metrics receives the counters. The default is a registry nothing reads.
	Metrics *metrics.Metrics

	// Logger is where the engine writes. The default is [slog.Default].
	Logger *slog.Logger
}
