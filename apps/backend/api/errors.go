package api

import (
	"github.com/valyala/fasthttp"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/edge"
)

// The numbers of the errors of the API, 1000 and up. A number never changes once it is out.
const (
	ErrBadListenerIDNum uint16 = iota + 1000
	ErrBadOfferNum
	ErrNotFoundListenerNum
	ErrCannotConnectNum
	ErrNoCredentialsNum
	ErrClosedNum
	ErrFullNum
	ErrBusyNum
)

// ErrBadListenerID answers a listener id that is not a number.
var ErrBadListenerID = &edge.Error{
	Status:  fasthttp.StatusBadRequest,
	Num:     ErrBadListenerIDNum,
	Code:    "BAD_LISTENER_ID",
	Message: "The listener id is not a number.",
}

// ErrBadOffer answers an offer that is not a valid SDP offer.
var ErrBadOffer = &edge.Error{
	Status:  fasthttp.StatusBadRequest,
	Num:     ErrBadOfferNum,
	Code:    "BAD_OFFER",
	Message: "The offer is not a valid SDP offer.",
}

// ErrNotFoundListener answers a listener id that no listener has.
var ErrNotFoundListener = &edge.Error{
	Status:   fasthttp.StatusNotFound,
	Num:      ErrNotFoundListenerNum,
	Code:     "NOT_FOUND_LISTENER",
	Message:  "The listener is not found.",
	Template: "The listener %(id)s is not found.",
}

// ErrCannotConnect answers an offer that failed for a reason the listener cannot fix.
var ErrCannotConnect = &edge.Error{
	Status:  fasthttp.StatusInternalServerError,
	Num:     ErrCannotConnectNum,
	Code:    "CANNOT_CONNECT",
	Message: "The server cannot connect the listener.",
}

// ErrNoCredentials answers a request for TURN credentials that could not be made.
var ErrNoCredentials = &edge.Error{
	Status:  fasthttp.StatusInternalServerError,
	Num:     ErrNoCredentialsNum,
	Code:    "NO_CREDENTIALS",
	Message: "The server cannot make the TURN credentials.",
}

// ErrClosed answers an offer that came after the engine was closed.
var ErrClosed = &edge.Error{
	Status:  fasthttp.StatusServiceUnavailable,
	Num:     ErrClosedNum,
	Code:    "CLOSED",
	Message: "The server is shutting down.",
}

// ErrFull answers an offer when the engine already holds as many listeners as it may.
var ErrFull = &edge.Error{
	Status:  fasthttp.StatusServiceUnavailable,
	Num:     ErrFullNum,
	Code:    "FULL",
	Message: "The server has too many listeners.",
}

// ErrBusy answers an offer when the engine already holds as many connecting listeners as it may.
var ErrBusy = &edge.Error{
	Status:  fasthttp.StatusServiceUnavailable,
	Num:     ErrBusyNum,
	Code:    "BUSY",
	Message: "The server has too many listeners connecting. Try again in a moment.",
}
