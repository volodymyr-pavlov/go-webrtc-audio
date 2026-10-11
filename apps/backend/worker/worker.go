package worker

import (
	"context"
	"io"
)

// A Worker is a part of the server that runs until the server stops: the engine, with the playout and the shards inside
// it, the AI presenter, the runtime monitor.
type Worker interface {
	// Close lets go of what the worker holds, such as sockets. It may be called after Do has returned, and more than
	// once; a later call closes nothing again.
	io.Closer

	// Do runs the worker until ctx is cancelled, and then returns nil. It returns an error at once if the worker cannot go
	// on. A worker that holds something that blocks it, such as a router, also returns when it is closed.
	Do(ctx context.Context) error
}
