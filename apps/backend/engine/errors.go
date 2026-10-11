package engine

import "errors"

// ErrBadMode is returned by [New] when the mode is not one of the known ones.
var ErrBadMode = errors.New("engine: unknown mode")

// ErrClosed is returned by [Engine.Listen] after the engine has been closed.
var ErrClosed = errors.New("engine: closed")

// ErrNotFound is returned by [Engine.Leave] when no listener has the id.
var ErrNotFound = errors.New("engine: no such listener")

// ErrBadOffer is returned by [Engine.Listen] when the offer is not a valid SDP offer.
var ErrBadOffer = errors.New("engine: invalid offer")

// ErrNoPlaylist is returned by [New] when the playlist is missing, has no track, or has a track without packets.
var ErrNoPlaylist = errors.New("engine: playlist has no packets")

// ErrBusy is returned by [Engine.Listen] when the engine already holds [Options.MaxConnecting] listeners that have not
// connected yet.
var ErrBusy = errors.New("engine: too many listeners connecting")

// ErrFull is returned by [Engine.Listen] when the engine already holds [Options.MaxListeners] listeners.
var ErrFull = errors.New("engine: too many listeners")
