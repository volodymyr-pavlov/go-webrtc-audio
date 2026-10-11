package stream

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/pion/rtp"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/playlist"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/worker"
)

var _ worker.Worker = &Playout{}

// A playoutState is a bit of the state of a [Playout].
type playoutState uint8

const (
	// playoutIntroduced is set when a clip has just ended and the cursor is still at the first packet of the track it
	// introduced, so that a second clip, offered while the first played, does not play straight after it.
	playoutIntroduced playoutState = 1 << iota
)

// maxCatchUp is how many late packets the playout sends at once after a stall. A longer stall skips the rest, because a
// listener cannot use audio that is older than its jitter buffer.
const maxCatchUp = 5

// bufferSize holds one RTP packet: the 12-byte header and the largest Opus packet, with room to spare.
const bufferSize = 1500

// A Playout reads a playlist and writes its packets, as marshaled RTP, to an [io.Writer] in real time.
type Playout struct {
	cursor   *playlist.Cursor
	interval time.Duration
	samples  uint64
	sink     io.Writer
	onPacket func(track int)
	onStart  func(track int)
	onError  func(err error)

	// interludes holds the clip that waits for a track boundary; clip and clipAt are the clip that plays now and the
	// packet of it to play next. Only the goroutine of the playout uses clip and clipAt.
	interludes *Interludes
	onClip     func(clip *Clip, event ClipEvent)
	clip       *Clip
	clipAt     int

	// state holds the flags below. Only the goroutine of the playout uses it.
	state  playoutState
	logger *slog.Logger
	buf    []byte

	// sent is the number of packets made. The sequence number, the RTP timestamp and the marker of the first packet are
	// all worked out from it.
	sent atomic.Uint64

	// skipped is the number of packets of audio dropped after a stall. The RTP clock moves past them, and the sequence
	// number does not, so the listeners see a gap and not a slowdown.
	skipped atomic.Uint64
}

// NewPlayout returns the [Playout] that opts describes.
//
// It returns [ErrNoPlaylist] if the playlist has no tracks or a track has no packets, and [ErrNoSink] if there is no
// sink.
func NewPlayout(opts PlayoutOptions) (*Playout, error) {
	if opts.Playlist.Validate() != nil {
		return nil, ErrNoPlaylist
	}
	if opts.Sink == nil {
		return nil, ErrNoSink
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Playout{
		cursor:   playlist.NewCursor(opts.Playlist),
		interval: opts.Playlist.Interval(),
		samples:  uint64(opts.Playlist.SamplesPerPacket()),
		sink:     opts.Sink,
		onPacket: opts.OnPacket,
		onStart:  opts.OnTrackStart,
		onError:  opts.OnError,

		interludes: opts.Interludes,
		onClip:     opts.OnClip,
		logger:     logger,
		buf:        make([]byte, bufferSize),
	}, nil
}

// Do writes a packet every [playlist.Playlist.Interval] until ctx is cancelled, and then returns nil.
func (p *Playout) Do(ctx context.Context) error {
	start := time.Now()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		ticks := p.sent.Load() + p.skipped.Load()
		due := start.Add(time.Duration(ticks) * p.interval)
		if wait := time.Until(due); wait > 0 {
			timer.Reset(wait)
			select {
			case <-ctx.Done():
				return nil
			case <-timer.C:
			}
		}
		late := int64(time.Since(due) / p.interval)
		if late > maxCatchUp {
			p.logger.Warn("playout fell behind and skips audio", "late_packets", late)
			p.skip(uint64(late - maxCatchUp))
			continue
		}
		p.send()
		if ctx.Err() != nil {
			return nil
		}
	}
}

// skip drops n packets of audio. A clip that is playing loses its packets too, because time passes for it as well.
func (p *Playout) skip(n uint64) {
	for range n {
		p.nextPayload()
	}
	p.skipped.Add(n)
}

// nextPayload returns the next Opus packet and the index of the track it belongs to: the next packet of the clip that
// is playing, or, at the start of a track that a waiting clip comes before, the first packet of that clip, or the next
// packet of the playlist. A clip counts as part of the track it introduces, so the page names that track while the
// presenter talks about it.
func (p *Playout) nextPayload() ([]byte, int) {
	if p.clip == nil {
		next, atStart := p.cursor.AtTrackStart()
		if atStart && p.interludes != nil && p.state&playoutIntroduced == 0 {
			if clip := p.interludes.take(next); clip != nil {
				p.clip, p.clipAt = clip, 0
				if p.onClip != nil {
					p.onClip(clip, ClipStarted)
				}
			}
		}
		if p.clip == nil {
			if atStart && p.onStart != nil {
				p.onStart(next)
			}
			p.state &^= playoutIntroduced
			return p.cursor.Next()
		}
	}
	payload, track := p.clip.Packets[p.clipAt], p.clip.Before
	p.clipAt++
	if p.clipAt >= len(p.clip.Packets) {
		finished := p.clip
		p.clip, p.clipAt = nil, 0
		p.state |= playoutIntroduced
		if p.onClip != nil {
			p.onClip(finished, ClipEnded)
		}
	}
	return payload, track
}

// send writes the next packet of the stream to the sink and reports it to the observers. A failed write is reported
// with onError and does not stop the stream.
func (p *Playout) send() {
	n := p.sent.Add(1) - 1
	payload, track := p.nextPayload()
	packet := rtp.Packet{
		Header: rtp.Header{
			Version:        2,
			Marker:         n == 0,
			PayloadType:    PayloadType,
			SequenceNumber: uint16(n),
			Timestamp:      uint32((n + p.skipped.Load()) * p.samples),
		},
		Payload: payload,
	}
	size, err := packet.MarshalTo(p.buf)
	if err == nil {
		_, err = p.sink.Write(p.buf[:size])
	}
	if err != nil && p.onError != nil {
		p.onError(err)
	}
	if p.onPacket != nil {
		p.onPacket(track)
	}
}

// Close does nothing; the playout holds no resource besides its goroutine.
func (p *Playout) Close() error {
	return nil
}
