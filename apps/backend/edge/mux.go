package edge

import (
	"errors"
	"log/slog"

	"github.com/fasthttp/router"
	"github.com/valyala/fasthttp"
)

// A Mux maps methods and paths to handlers and answers any error they return. A path may name a part of itself, as in
// "/listen/{id}", which [ParamString] and [ParamUint] read.
type Mux struct {
	router *router.Router
	logger *slog.Logger
	mw     Middleware
}

// NewMux returns a [Mux] in which mw runs outermost for every handler. A nil logger means [slog.Default].
func NewMux(logger *slog.Logger, mw Middleware) *Mux {
	if logger == nil {
		logger = slog.Default()
	}
	if mw == nil {
		mw = Nop()
	}
	return &Mux{
		router: router.New(),
		logger: logger,
		mw:     mw,
	}
}

// GET serves GET requests to path with handler, behind the [Mux] middleware and then mw. POST and DELETE do the same
// for their methods.
func (m *Mux) GET(path string, handler Handler, mw ...Middleware) {
	m.Handle(fasthttp.MethodGet, path, handler, mw...)
}

// POST serves POST requests to path with handler. See [Mux.GET].
func (m *Mux) POST(path string, handler Handler, mw ...Middleware) {
	m.Handle(fasthttp.MethodPost, path, handler, mw...)
}

// DELETE serves DELETE requests to path with handler. See [Mux.GET].
func (m *Mux) DELETE(path string, handler Handler, mw ...Middleware) {
	m.Handle(fasthttp.MethodDelete, path, handler, mw...)
}

// Handle serves the requests with the given method and path with handler. The middleware of the [Mux] runs first, then
// mw in order.
func (m *Mux) Handle(method, path string, handler Handler, mw ...Middleware) {
	serve := Chain(m.mw, mw...)(handler.ServeHTTP)
	m.router.Handle(method, path, func(ctx *fasthttp.RequestCtx) {
		if err := serve(ctx); err != nil {
			m.fail(ctx, err)
		}
	})
}

// Plain serves the GET requests to path with h, which writes its own answers and is outside the middleware.
func (m *Mux) Plain(path string, h fasthttp.RequestHandler) {
	m.router.GET(path, h)
}

// NotFound serves the requests that match no route with h, which writes its own answers and is outside the
// middleware.
func (m *Mux) NotFound(h fasthttp.RequestHandler) {
	m.router.NotFound = h
}

// Handler returns the fasthttp handler of the [Mux].
func (m *Mux) Handler() fasthttp.RequestHandler {
	return m.router.Handler
}

// fail logs err when it is the server's fault, and answers it.
func (m *Mux) fail(ctx *fasthttp.RequestCtx, err error) {
	e := asError(err)
	if e.Status >= fasthttp.StatusInternalServerError {
		m.logger.Error("request failed", "method", string(ctx.Method()), "path", string(ctx.Path()), "err", err)
	}
	if err := WriteError(ctx, e.Status, e); err != nil {
		m.logger.Error("could not write the error answer",
			"method", string(ctx.Method()), "path", string(ctx.Path()), "err", err)
		ctx.SetStatusCode(fasthttp.StatusInternalServerError)
	}
}

// ErrorHandler is the fasthttp error handler for a request that the server could not read. It answers a body that is
// too large with [ErrTooLargeBody], and any other failure with a plain text status.
func ErrorHandler(ctx *fasthttp.RequestCtx, err error) {
	var smallBuffer *fasthttp.ErrSmallBuffer
	switch {
	case errors.Is(err, fasthttp.ErrBodyTooLarge):
		if err := WriteError(ctx, ErrTooLargeBody.Status, ErrTooLargeBody); err != nil {
			ctx.Logger().Printf("could not write the error answer: %v", err)
		}
	case errors.As(err, &smallBuffer):
		ctx.Error("The request header is too large.", fasthttp.StatusRequestHeaderFieldsTooLarge)
	default:
		ctx.Error("The request could not be read.", fasthttp.StatusBadRequest)
	}
}
