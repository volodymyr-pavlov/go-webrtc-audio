// Package turn makes the credentials with which a browser may use a TURN server.
//
// A TURN server relays the media of a listener whose network does not let it reach the server directly, for example
// one that blocks UDP. The server that this package serves is coturn, or any other that accepts the shared-secret
// scheme of the TURN REST API: a username that carries its own expiry time, and a password that is the HMAC of the
// username under a secret that only the TURN server and this server know. The server hands out such credentials, which
// stop working after a short time, and the secret never leaves it.
package turn
