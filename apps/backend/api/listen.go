package api

import (
	"errors"
	"fmt"

	"github.com/valyala/fasthttp"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/edge"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/engine"
)

var _ edge.Handler = &ListenHandler{}

// ListenHandlerArgs are the arguments of POST /api/listen.
type ListenHandlerArgs struct {
	// Offer is the SDP offer of the browser, the body of the request.
	Offer string
}

// Parse reads the offer from the body of the request. The server refuses a body larger than [MaxOfferSize] before a
// handler runs, with [edge.ErrTooLargeBody].
func (a *ListenHandlerArgs) Parse(ctx *fasthttp.RequestCtx) error {
	a.Offer = string(ctx.PostBody())
	return nil
}

// ListenHandler answers POST /api/listen: it reads the SDP offer of the browser and answers with the SDP answer and the
// Location of the new listener. A refusal is [ErrBadOffer] for a bad offer, and [ErrClosed], [ErrFull] or [ErrBusy]
// when the engine cannot take one.
type ListenHandler struct {
	// Engine answers the offer.
	Engine *engine.Engine
}

// ServeHTTP answers the request.
func (h *ListenHandler) ServeHTTP(ctx *fasthttp.RequestCtx) error {
	var args ListenHandlerArgs
	if err := args.Parse(ctx); err != nil {
		return err
	}
	answer, id, err := h.Engine.Listen(ctx, args.Offer)
	switch {
	case errors.Is(err, engine.ErrBadOffer):
		return ErrBadOffer
	case errors.Is(err, engine.ErrClosed):
		return ErrClosed
	case errors.Is(err, engine.ErrFull):
		return ErrFull
	case errors.Is(err, engine.ErrBusy):
		return ErrBusy
	case err != nil:
		return ErrCannotConnect.Wrap(err)
	}
	ctx.SetStatusCode(fasthttp.StatusCreated)
	ctx.SetContentType("application/sdp")
	// Relative, so that it is right behind a proxy that adds or removes the /api prefix.
	ctx.Response.Header.Set("Location", fmt.Sprintf("listen/%d", id))
	ctx.SetBodyString(answer)
	return nil
}
