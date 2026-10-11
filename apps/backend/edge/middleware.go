package edge

import (
	"fmt"
	"runtime/debug"
	"slices"

	"github.com/valyala/fasthttp"
)

// A Middleware wraps a [HandlerFunc] with behaviour that runs before or after it.
type Middleware func(next HandlerFunc) HandlerFunc

// Chain joins first and mw into one middleware: first runs outermost, then mw in the order given.
func Chain(first Middleware, mw ...Middleware) Middleware {
	return func(next HandlerFunc) HandlerFunc {
		for _, m := range slices.Backward(mw) {
			next = m(next)
		}
		return first(next)
	}
}

// Nop is a middleware that changes nothing.
func Nop() Middleware {
	return func(next HandlerFunc) HandlerFunc { return next }
}

// Recover catches a panic further down the chain and returns it as an [ErrInternal] that carries the panic and its
// stack.
func Recover() Middleware {
	return func(next HandlerFunc) HandlerFunc {
		return func(ctx *fasthttp.RequestCtx) (err error) {
			defer func() {
				if x := recover(); x != nil {
					err = ErrInternal.Wrap(fmt.Errorf("panic [%v] trace [%s]", x, debug.Stack()))
				}
			}()
			return next(ctx)
		}
	}
}
