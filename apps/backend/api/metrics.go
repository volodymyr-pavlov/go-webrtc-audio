package api

import (
	"io"

	"github.com/valyala/fasthttp"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/edge"
)

var _ edge.Handler = &MetricsHandler{}

// MetricsHandler answers GET /api/metrics with what its source writes, which is a snapshot as JSON, without the
// envelope of the other answers, because a program reads it.
type MetricsHandler struct {
	// Metrics writes the snapshot.
	Metrics io.WriterTo
}

// ServeHTTP answers the request.
func (h *MetricsHandler) ServeHTTP(ctx *fasthttp.RequestCtx) error {
	ctx.SetContentType("application/json")
	_, err := h.Metrics.WriteTo(ctx)
	return err
}

var _ edge.Handler = &RuntimeHandler{}

// RuntimeHandler answers GET /api/runtime with what its source writes, which is the samples of the health of the Go
// runtime as CSV.
type RuntimeHandler struct {
	// Monitor writes the samples.
	Monitor io.WriterTo
}

// ServeHTTP answers the request.
func (h *RuntimeHandler) ServeHTTP(ctx *fasthttp.RequestCtx) error {
	ctx.SetContentType("text/csv")
	_, err := h.Monitor.WriteTo(ctx)
	return err
}
