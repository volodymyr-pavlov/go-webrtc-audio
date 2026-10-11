package edge

import (
	"github.com/valyala/bytebufferpool"
	"github.com/valyala/fasthttp"
)

// A Response is the JSON envelope of an answer: Data on success, Error on failure. The empty one is left out.
type Response struct {
	Error error `json:"error,omitempty"`
	Data  any   `json:"data,omitempty"`
}

// Write writes data as the Data of a successful [Response] with status.
func Write(ctx *fasthttp.RequestCtx, status int, data any) error {
	return WriteRaw(ctx, status, Response{Data: data})
}

// WriteError writes err as the Error of a failed [Response] with status.
func WriteError(ctx *fasthttp.RequestCtx, status int, err error) error {
	return WriteRaw(ctx, status, Response{Error: err})
}

// WriteRaw writes v as a JSON answer with status, without the [Response] envelope. Nothing is written if v does not
// encode, so the error that is returned becomes the answer. It encodes with [EncodeJSON], and the answer copies the
// pooled bytes.
func WriteRaw(ctx *fasthttp.RequestCtx, status int, v any) error {
	bp, err := EncodeJSON(v)
	if err != nil {
		return err
	}
	defer bytebufferpool.Put(bp)
	ctx.SetContentType("application/json")
	ctx.SetStatusCode(status)
	ctx.SetBody(bp.B)
	return nil
}
