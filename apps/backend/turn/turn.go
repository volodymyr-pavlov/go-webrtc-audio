package turn

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" // The TURN REST API fixes HMAC-SHA1, which does not rely on the collision resistance of SHA-1.
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// An Issuer makes credentials for one TURN server. It is safe for concurrent use.
type Issuer struct {
	urls   []string
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// A Server is one entry of the iceServers of a browser's RTCPeerConnection, in the form that the browser reads it as
// JSON.
type Server struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username"`
	Credential string   `json:"credential"`
}

// New returns the [Issuer] that opts describes. It returns [ErrNoURL], [ErrBadURL] or [ErrShortSecret] for options that
// cannot work.
func New(opts Options) (*Issuer, error) {
	if len(opts.URLs) == 0 {
		return nil, ErrNoURL
	}
	for _, url := range opts.URLs {
		if !validURL(url) {
			return nil, fmt.Errorf("%w: %q", ErrBadURL, url)
		}
	}
	if len(opts.Secret) < minSecretLength {
		return nil, ErrShortSecret
	}
	i := &Issuer{
		urls:   append([]string(nil), opts.URLs...),
		secret: append([]byte(nil), opts.Secret...),
		ttl:    opts.TTL,
		now:    opts.Now,
	}
	if i.ttl <= 0 {
		i.ttl = defaultTTL
	}
	if i.now == nil {
		i.now = time.Now
	}
	return i, nil
}

// validURL reports whether url is a turn: or turns: URI with something after the scheme and no white space.
func validURL(url string) bool {
	scheme, rest, found := strings.Cut(url, ":")
	scheme = strings.ToLower(scheme)
	return found && (scheme == "turn" || scheme == "turns") && rest != "" && !strings.ContainsAny(url, " \t\r\n")
}

// TTL returns how long the credentials of the issuer last.
func (i *Issuer) TTL() time.Duration {
	return i.ttl
}

// Issue returns new credentials. The username is the time at which they expire, in seconds since the Unix epoch, and a
// random identifier that tells one listener from another in the log of the TURN server, and the credential is the
// base64 of the HMAC-SHA1 of the username under the secret. It returns an error only if the system cannot give random
// bytes.
func (i *Issuer) Issue() (Server, error) {
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Server{}, fmt.Errorf("turn: make an identifier: %w", err)
	}
	username := strconv.FormatInt(i.now().Add(i.ttl).Unix(), 10) + ":" + hex.EncodeToString(id[:])
	mac := hmac.New(sha1.New, i.secret)
	mac.Write([]byte(username))
	return Server{
		URLs:       i.urls,
		Username:   username,
		Credential: base64.StdEncoding.EncodeToString(mac.Sum(nil)),
	}, nil
}
