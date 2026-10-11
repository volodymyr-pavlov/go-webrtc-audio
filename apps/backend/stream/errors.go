package stream

import "errors"

// ErrNoPlaylist is returned by [NewPlayout] when the options hold no playlist, or a playlist without tracks or with an
// empty track.
var ErrNoPlaylist = errors.New("stream: playlist is empty")

// ErrNoSink is returned by [NewPlayout] when the options hold no sink.
var ErrNoSink = errors.New("stream: no sink")
