package transport

import "errors"

// ErrClosed is returned by [BatchConn.WriteTo] and [BatchConn.WriteToAddrPort] after the connection has been closed.
var ErrClosed = errors.New("transport: connection is closed")

// ErrBadAddr is returned by [BatchConn.WriteTo] when the address is not a UDP address.
var ErrBadAddr = errors.New("transport: address is not a UDP address")
