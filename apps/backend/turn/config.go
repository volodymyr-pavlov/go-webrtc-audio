package turn

import "time"

const (
	// minSecretLength is the shortest secret that is accepted, in bytes.
	minSecretLength = 16

	defaultTTL = time.Hour
)

// Options describe an [Issuer].
type Options struct {
	// URLs are the addresses of the TURN server as the browser is to use them, each a turn: or turns: URI, such as
	// turn:turn.example.com:3478?transport=udp or turns:turn.example.com:443?transport=tcp. At least one is needed.
	URLs []string

	// Secret is the secret that the TURN server shares with this server (static-auth-secret in coturn). It must be at
	// least 16 bytes long.
	Secret []byte

	// TTL is how long credentials last. The default is one hour.
	TTL time.Duration

	// Now returns the current time. The default is [time.Now].
	Now func() time.Time
}
