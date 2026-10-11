package api

import "time"

// Flag is a bit of the flags of the [Server], which [NewServer] sets from the options and the environment.
type Flag uint8

const (
	// FlagProfiling serves /debug/pprof. It must not be reachable by the public.
	FlagProfiling Flag = 1 << iota

	// FlagPresenter says that the server has an AI presenter, which the page shows a place for even when the presenter is
	// silent.
	FlagPresenter
)

// apiPrefix is the first part of every path of the API.
const apiPrefix = "/api"

// MaxOfferSize is the largest request body that the server reads, which is the size limit of the offer; a real offer is
// about 1 KB. [NewServer] sets it as the MaxRequestBodySize of the fasthttp server.
const MaxOfferSize = 64 << 10

// Timeouts of the HTTP server.
const (
	httpReadTimeout = 15 * time.Second
	httpIdleTimeout = 60 * time.Second
)
