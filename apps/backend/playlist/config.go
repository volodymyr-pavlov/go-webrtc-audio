package playlist

import "time"

// DefaultFrameDuration is the length of the audio in a packet when [Options.FrameDuration] is zero.
const DefaultFrameDuration = 20 * time.Millisecond

// SampleRate is the clock rate of Opus in RTP, in hertz.
const SampleRate = 48000

// Bitrate is the Opus bitrate of every track, in bits per second.
const Bitrate = 64000

// Options describe how [Load] reads the files.
type Options struct {
	// FFmpeg is the ffmpeg program to run. The default is "ffmpeg".
	FFmpeg string

	// FrameDuration is the length of the audio in each packet: 20, 40 or 60 milliseconds. The default is 20 ms. A longer
	// frame means fewer packets, and so fewer network sends for the server, at the price of more delay and of a longer
	// gap when a packet is lost.
	FrameDuration time.Duration
}
