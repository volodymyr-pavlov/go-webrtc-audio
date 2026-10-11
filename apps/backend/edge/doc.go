// Package edge is the edge of the server: the HTTP plumbing that an API shares, apart from what that API says.
//
// A [Handler] handles a request and returns an error instead of writing the error answer. A [Mux] maps methods and
// paths to handlers, wraps them in [Middleware] such as [Recover], and turns a returned [Error] into a JSON answer
// with its status. The cause of an error is logged and never sent, so a person sees only the message. [Write] and
// [WriteError] write an answer in a [Response] envelope, with the answer as Data or the error as Error. [WriteRaw]
// writes one without it. An [AddressHandler] removes network addresses from log lines, because the libraries that the
// server uses write the address of a listener into their own.
//
// An [Error] carries an HTTP status, a number and a code that never change, and a message for people, which a template
// and [Params] may fill in. The errors that every API shares, such as [ErrInternal], live here. The errors of one API
// live in that API, with their numbers in one block so that no two are the same.
package edge
