package turn

import "errors"

// ErrNoURL is returned by [New] when there is no URL.
var ErrNoURL = errors.New("turn: no url")

// ErrBadURL is returned by [New] when a URL is not a turn: or turns: URI.
var ErrBadURL = errors.New("turn: url must be a turn: or turns: uri")

// ErrShortSecret is returned by [New] when the secret is shorter than 16 bytes.
var ErrShortSecret = errors.New("turn: secret is shorter than 16 bytes")
