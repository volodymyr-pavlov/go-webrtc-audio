package api

import (
	"encoding/json"
	"time"

	"github.com/valyala/fasthttp"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/edge"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/engine"
)

var _ edge.Handler = &PlaylistHandler{}

// PlaylistTrack is a track in the answer of GET /api/playlist.
type PlaylistTrack struct {
	Name    string `json:"name"`
	Seconds int    `json:"seconds"`
}

// PlaylistResponse is the data of the answer of GET /api/playlist: the track that plays now, and all the tracks in the
// order they play. The tracks are the same for ever, so they are encoded once, and an answer only adds the track that
// plays now.
type PlaylistResponse struct {
	Track string `json:"track"`

	// Flags holds the bitwise flags: [PlaylistPresenter] says that the server has an AI presenter, and
	// [PlaylistSpeaking] that its voice is on now. Said is the last thing the presenter said, which stays until it says
	// the next.
	Flags  uint8           `json:"flags"`
	Said   string          `json:"said"`
	Tracks json.RawMessage `json:"tracks"`
}

// The bits of the Flags of the answer of GET /api/playlist.
const (
	// PlaylistPresenter is set when the server has an AI presenter.
	PlaylistPresenter uint8 = 1 << iota

	// PlaylistSpeaking is set while the presenter's voice is on.
	PlaylistSpeaking
)

// PlaylistHandler answers GET /api/playlist with the track that plays now, the flags and the tracks in the order they
// play.
type PlaylistHandler struct {
	// Engine knows the track that plays now and what the presenter does.
	Engine *engine.Engine

	// Tracks is the encoded list of the tracks, as [EncodeTracks] makes it from the tracks of Engine.
	Tracks json.RawMessage

	// Flags holds the bitwise flags: [FlagPresenter] is the one that the answer tells.
	Flags Flag
}

// ServeHTTP answers the request.
func (h *PlaylistHandler) ServeHTTP(ctx *fasthttp.RequestCtx) error {
	return edge.Write(ctx, fasthttp.StatusOK, PlaylistResponse{
		Track:  h.Engine.Track(),
		Flags:  h.flags(),
		Said:   h.Engine.Said(),
		Tracks: h.Tracks,
	})
}

// flags returns the bits of the answer: whether there is a presenter and whether it speaks.
func (h *PlaylistHandler) flags() uint8 {
	var flags uint8
	if h.Flags&FlagPresenter != 0 {
		flags |= PlaylistPresenter
	}
	if h.Engine.Speaking() {
		flags |= PlaylistSpeaking
	}
	return flags
}

// EncodeTracks encodes the tracks of the playlist, for [PlaylistHandler.Tracks].
func EncodeTracks(tracks []engine.TrackInfo) json.RawMessage {
	list := make([]PlaylistTrack, len(tracks))
	for i, track := range tracks {
		list[i] = PlaylistTrack{
			Name:    track.Name,
			Seconds: int(track.Duration.Round(time.Second) / time.Second),
		}
	}
	encoded, err := json.Marshal(list)
	if err != nil {
		// Strings and integers always encode.
		panic(err)
	}
	return encoded
}
