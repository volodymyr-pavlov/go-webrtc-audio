package fanout

import "errors"

// ErrClosed is returned by [Router.Add] after the router has been closed.
var ErrClosed = errors.New("fanout: router is closed")

// ErrDuplicate is returned by [Router.Add] when the id is already in use.
var ErrDuplicate = errors.New("fanout: listener id is already in use")
