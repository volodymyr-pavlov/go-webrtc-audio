// Package engine accepts WebRTC listeners and gives each of them the stream.
//
// An [Engine] answers the SDP offer of a browser or of the load generator, with one Opus track that the listener only
// receives. How the stream reaches the track is the [Mode]:
//
//   - [ModeNaive] runs a playout for every listener, so every listener has its own goroutine, timer and playlist
//     position. It is the baseline the other modes are measured against.
//   - [ModeTrack] runs one playout and writes each packet to one local track that every listener is bound to. Pion
//     encrypts and sends the packet to the listeners one after another on the playout's goroutine.
//   - [ModeFanout] runs one playout and a [fanout.Router], which gives every listener a queue and a writer goroutine.
//
// All connections share one UDP port, so the server needs one port opened and not one per listener.
package engine
