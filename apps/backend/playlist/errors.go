package playlist

import "errors"

// ErrEmpty is returned when there are no audio files, or when a file holds no audio.
var ErrEmpty = errors.New("playlist: no audio")

// ErrBadFrame is returned by [Load] when the frame duration is not 20, 40 or 60 milliseconds.
var ErrBadFrame = errors.New("playlist: frame duration must be 20, 40 or 60 ms")

// ErrBadPacket is returned when an Ogg page holds no data, or more than the largest Opus packet.
var ErrBadPacket = errors.New("playlist: ogg page is not one opus packet")
