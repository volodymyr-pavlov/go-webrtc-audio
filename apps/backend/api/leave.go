package api

import (
	"github.com/valyala/fasthttp"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/edge"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/engine"
)

var _ edge.Handler = &LeaveHandler{}

// LeaveHandlerArgs are the arguments of DELETE /api/listen/{id}.
type LeaveHandlerArgs struct {
	// ID names the listener, from the path.
	ID uint64
}

// Parse reads the id from the path of the request.
//
// It returns [ErrBadListenerID] if the id is not a positive number.
func (a *LeaveHandlerArgs) Parse(ctx *fasthttp.RequestCtx) error {
	a.ID = edge.ParamUint[uint64](ctx, "id")
	if a.ID == 0 {
		return ErrBadListenerID
	}
	return nil
}

// LeaveHandler answers DELETE /api/listen/{id}: it ends that listener, and answers [ErrNotFoundListener] if there is
// none.
type LeaveHandler struct {
	// Engine holds the listeners.
	Engine *engine.Engine
}

// ServeHTTP answers the request.
func (h *LeaveHandler) ServeHTTP(ctx *fasthttp.RequestCtx) error {
	var args LeaveHandlerArgs
	if err := args.Parse(ctx); err != nil {
		return err
	}
	if err := h.Engine.Leave(args.ID); err != nil {
		return ErrNotFoundListener.With(edge.Params{"id": args.ID})
	}
	ctx.SetStatusCode(fasthttp.StatusNoContent)
	return nil
}
