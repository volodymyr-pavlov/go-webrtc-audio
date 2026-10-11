package edge

import (
	"encoding/json"

	"github.com/valyala/bytebufferpool"
)

// EncodeJSON encodes v as JSON, with no trailing newline, into a buffer from the pool. The caller gives the buffer back
// with [bytebufferpool.Put] once it has used the bytes; on an error the buffer has already been given back.
func EncodeJSON(v any) (*bytebufferpool.ByteBuffer, error) {
	bp := bytebufferpool.Get()
	if err := json.NewEncoder(bp).Encode(v); err != nil {
		bytebufferpool.Put(bp)
		return nil, err
	}
	bp.B = bp.B[:len(bp.B)-1] // the encoder ends the value with a newline
	return bp, nil
}
