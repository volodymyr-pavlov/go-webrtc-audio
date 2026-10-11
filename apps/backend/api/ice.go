package api

import (
	"github.com/valyala/fasthttp"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/edge"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/turn"
)

var _ edge.Handler = &ICEHandler{}

// ICEResponse is the data of the answer of GET /api/ice: the servers that the browser puts in the iceServers of its
// connection.
type ICEResponse struct {
	ICEServers []turn.Server `json:"iceServers"`
}

// ICEHandler answers GET /api/ice with the credentials of the TURN server, new for every request and valid for a
// limited time. Without a TURN server the list is empty, so the page needs no setting to tell the two cases apart.
type ICEHandler struct {
	// TURN makes the credentials. A nil TURN hands out none, and a browser then connects without a relay.
	TURN *turn.Issuer
}

// ServeHTTP answers the request.
//
// It returns [ErrNoCredentials] if the credentials cannot be made.
func (h *ICEHandler) ServeHTTP(ctx *fasthttp.RequestCtx) error {
	servers := []turn.Server{}
	if h.TURN != nil {
		server, err := h.TURN.Issue()
		if err != nil {
			return ErrNoCredentials.Wrap(err)
		}
		servers = append(servers, server)
	}
	ctx.Response.Header.Set("Cache-Control", "no-store")
	return edge.Write(ctx, fasthttp.StatusOK, ICEResponse{ICEServers: servers})
}
