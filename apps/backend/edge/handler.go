package edge

import "github.com/valyala/fasthttp"

var _ Handler = HandlerFunc(nil)

// A Handler handles one request and returns an error instead of writing the error answer itself. The [Mux] turns the
// error into the answer.
type Handler interface {
	ServeHTTP(ctx *fasthttp.RequestCtx) error
}

// A HandlerFunc lets a plain function act as a [Handler].
type HandlerFunc func(ctx *fasthttp.RequestCtx) error

// ServeHTTP calls f.
func (f HandlerFunc) ServeHTTP(ctx *fasthttp.RequestCtx) error {
	return f(ctx)
}
