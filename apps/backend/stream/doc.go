// Package stream makes a continuous RTP stream from a [playlist.Playlist].
//
// A [Playout] turns the Opus packets of a playlist into RTP packets and writes one, marshaled, to its [io.Writer] every
// [playlist.Playlist.Interval], measured on the monotonic clock so that a late wake-up is made up for and the stream
// does not drift. The playout has no idea who listens: the writer may be one listener's track or a fan-out to
// thousands.
package stream
