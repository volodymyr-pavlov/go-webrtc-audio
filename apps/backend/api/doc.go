// Package api serves the HTTP side of the server: the offer and answer exchange, the playlist and the metrics, the
// profiler, and, when one is given, the built listener page.
//
// A listener posts its SDP offer to /listen and receives the SDP answer. The Location header of the answer, a path
// relative to the request address, names the listener, and a DELETE to that address ends it. Every connection candidate
// is already in the answer, so the client does not have to trickle candidates.
//
// Each route is its own handler, a type whose exported fields are what it needs, such as [ListenHandler]. A route with
// arguments parses them into a type such as [LeaveHandlerArgs], and one with a JSON answer has a response type such as
// [PlaylistResponse]. [NewServer] builds the handlers from the [config.Env], and [Server.Routes] maps the paths to
// them. [Server.Startup] serves them and runs the engine, the presenter and the runtime monitor alongside;
// [Server.Shutdown] stops all of it. The errors that the API answers with, and their numbers, are in errors.go. The
// HTTP plumbing, the handlers that return errors, the middleware and the envelope of the answers, is in package edge.
package api
