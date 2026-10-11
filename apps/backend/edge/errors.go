package edge

import (
	"errors"
	"fmt"

	"github.com/valyala/fasthttp"
)

// An Error is a refusal that the server sends: it is both a Go error and the JSON body of the answer.
//
// Num identifies the error and never changes; two errors are the same under [errors.Is] when their numbers are the
// same. Code is the same identity as a readable constant. Message is the text for people. When Template is set,
// [Error.With] renders the message from it: a placeholder of the form %(name)s is replaced with the param of that name.
// Params carries the values that explain the refusal. The cause of an error, when it has one, stays in the log and is
// never sent.
type Error struct {
	Status   int    `json:"status"`
	Num      uint16 `json:"num"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Template string `json:"-"`
	Params   Params `json:"params,omitempty"`
	cause    error
}

// Error returns the status, number, code, message, params and cause of e as one line for the log.
func (e *Error) Error() string {
	s := fmt.Sprintf("HTTP status [%d] num [%d] code [%s] - %s", e.Status, e.Num, e.Code, e.Message)
	if len(e.Params) != 0 {
		s += fmt.Sprintf(" params [%s]", e.Params)
	}
	if e.cause != nil {
		s += ": " + e.cause.Error()
	}
	return s
}

// Is reports whether target is an [Error] with the same number as e, so a copy that carries params or a cause still
// matches its original.
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && other.Num == e.Num
}

// Unwrap returns the cause of e, or nil.
func (e *Error) Unwrap() error {
	return e.cause
}

// With returns a copy of e that carries params, with the message rendered from the template of e when params fills
// it. The params can be a [Params] value or a struct, whose exported fields become params under their json names. With
// returns e itself when params is empty.
func (e *Error) With(params any) *Error {
	values := paramsFrom(params)
	if len(values) == 0 {
		return e
	}
	clone := *e
	clone.Params = values
	if message, ok := interpolate(e.Template, values); ok {
		clone.Message = message
	}
	return &clone
}

// Wrap returns a copy of e that carries cause, which the [Mux] logs.
func (e *Error) Wrap(cause error) *Error {
	clone := *e
	clone.cause = cause
	return &clone
}

// The numbers of the errors of this package, 100 and up.
const (
	ErrInternalNum uint16 = iota + 100
	ErrTooLargeBodyNum
)

// ErrInternal answers any failure that is not an [Error]. It hides the cause from the client.
var ErrInternal = &Error{
	Status:  fasthttp.StatusInternalServerError,
	Num:     ErrInternalNum,
	Code:    "INTERNAL",
	Message: "An unknown error occurred. Try again later.",
}

// ErrTooLargeBody answers a request whose body is larger than the server accepts.
var ErrTooLargeBody = &Error{
	Status:  fasthttp.StatusRequestEntityTooLarge,
	Num:     ErrTooLargeBodyNum,
	Code:    "TOO_LARGE_BODY",
	Message: "The request body is too large.",
}

// asError returns err as an [Error], and [ErrInternal] carrying err when it is not one.
func asError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return ErrInternal.Wrap(err)
}
