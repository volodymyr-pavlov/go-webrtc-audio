package stream

import (
	"io"
	"log/slog"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/playlist"
)

// A ClipEvent says what happened to a clip. It is a bit, like the other statuses of the program.
type ClipEvent uint8

const (
	// ClipStarted is the event of the first packet of a clip going out.
	ClipStarted ClipEvent = 1 << iota

	// ClipEnded is the event of the last packet of a clip having gone out.
	ClipEnded
)

// PayloadType is the RTP payload type of the Opus packets. Pion rewrites it for each listener to the type that was
// negotiated, so the value only has to be a dynamic one.
const PayloadType = 111

// PlayoutOptions describe a [Playout].
type PlayoutOptions struct {
	// Playlist is the audio to stream. It must have at least one track.
	Playlist *playlist.Playlist

	// Sink receives every packet, marshaled, in one Write. As for every [io.Writer], it must not keep the bytes after
	// Write returns: the playout reuses them for the next packet.
	Sink io.Writer

	// OnPacket is called after each packet with the index of its track. It may be nil.
	OnPacket func(track int)

	// OnTrackStart is called before the first packet of a track with the index of that track, after the clip that
	// introduces it, if there is one, has played. It may be nil. It runs on the goroutine of the playout, so it must not
	// block.
	OnTrackStart func(track int)

	// Interludes holds the clip that plays in front of the next track, if there is one. It may be nil, and then no clip
	// is ever played.
	Interludes *Interludes

	// OnClip is called when a clip starts, with [ClipStarted], and when it has played to its end, with [ClipEnded]. It may
	// be nil. It runs on the goroutine of the playout, so it must not block.
	OnClip func(clip *Clip, event ClipEvent)

	// OnError is called with the error of a packet that could not be made or written. It may be nil.
	OnError func(err error)

	// Logger is where the playout writes. The default is [slog.Default].
	Logger *slog.Logger
}
