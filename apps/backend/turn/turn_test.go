package turn

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

var secret = []byte("0123456789abcdef-secret")

func TestNewRefusesOptionsThatCannotWork(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want error
	}{
		{"no url", Options{Secret: secret}, ErrNoURL},
		{"a url of another scheme", Options{
			URLs:   []string{"stun:turn.example.com"},
			Secret: secret,
		}, ErrBadURL},
		{"a url of http", Options{
			URLs:   []string{"https://turn.example.com"},
			Secret: secret,
		}, ErrBadURL},
		{"a url with nothing after the scheme", Options{
			URLs:   []string{"turn:"},
			Secret: secret,
		}, ErrBadURL},
		{"a url with a space", Options{
			URLs:   []string{"turn:a b"},
			Secret: secret,
		}, ErrBadURL},
		{"an empty url", Options{
			URLs:   []string{""},
			Secret: secret,
		}, ErrBadURL},
		{"a short secret", Options{
			URLs:   []string{"turn:turn.example.com"},
			Secret: []byte("short"),
		}, ErrShortSecret},
		{"no secret", Options{URLs: []string{"turn:turn.example.com"}}, ErrShortSecret},
	}
	for _, tt := range tests {
		if _, err := New(tt.opts); !errors.Is(err, tt.want) {
			t.Errorf("%s: got %v, want %v", tt.name, err, tt.want)
		}
	}
}

func TestNewAcceptsTurnAndTurnsUrls(t *testing.T) {
	_, err := New(Options{
		URLs:   []string{"turn:turn.example.com:3478?transport=udp", "TURNS:turn.example.com:443?transport=tcp"},
		Secret: secret,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestIssueMakesTheCredentialsThatCoturnChecks(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	issuer, err := New(Options{
		URLs:   []string{"turn:turn.example.com:3478"},
		Secret: secret,
		TTL:    30 * time.Minute,
		Now:    func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := issuer.Issue()
	if err != nil {
		t.Fatal(err)
	}

	expiry, id, ok := strings.Cut(got.Username, ":")
	if !ok || id == "" {
		t.Fatalf("username %q is not <expiry>:<id>", got.Username)
	}
	if want := strconv.FormatInt(now.Add(30*time.Minute).Unix(), 10); expiry != want {
		t.Fatalf("expiry %s, want %s", expiry, want)
	}
	mac := hmac.New(sha1.New, secret)
	mac.Write([]byte(got.Username))
	if want := base64.StdEncoding.EncodeToString(mac.Sum(nil)); got.Credential != want {
		t.Fatalf("credential %s, want %s", got.Credential, want)
	}
	if len(got.URLs) != 1 || got.URLs[0] != "turn:turn.example.com:3478" {
		t.Fatalf("urls: %v", got.URLs)
	}
}

func TestIssueGivesEveryListenerItsOwnUsername(t *testing.T) {
	issuer, err := New(Options{
		URLs:   []string{"turn:turn.example.com"},
		Secret: secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		got, err := issuer.Issue()
		if err != nil {
			t.Fatal(err)
		}
		if seen[got.Username] {
			t.Fatalf("username %s was issued twice", got.Username)
		}
		seen[got.Username] = true
	}
}

func TestIssuedCredentialsNeverHoldTheSecret(t *testing.T) {
	issuer, err := New(Options{
		URLs:   []string{"turn:turn.example.com"},
		Secret: secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := issuer.Issue()
	if err != nil {
		t.Fatal(err)
	}
	all := got.Username + got.Credential + strings.Join(got.URLs, "")
	if strings.Contains(all, string(secret)) {
		t.Fatal("the secret is in the credentials")
	}
}

func TestTheDefaultLifeIsOneHourAndTheOptionsAreCopied(t *testing.T) {
	urls := []string{"turn:turn.example.com"}
	key := append([]byte(nil), secret...)
	issuer, err := New(Options{
		URLs:   urls,
		Secret: key,
	})
	if err != nil {
		t.Fatal(err)
	}
	if issuer.TTL() != time.Hour {
		t.Fatalf("ttl %v, want 1h", issuer.TTL())
	}
	urls[0] = "turn:changed"
	key[0] = 'X'
	got, err := issuer.Issue()
	if err != nil {
		t.Fatal(err)
	}
	if got.URLs[0] != "turn:turn.example.com" {
		t.Fatalf("the issuer followed a change of the caller's slice: %v", got.URLs)
	}
	mac := hmac.New(sha1.New, secret)
	mac.Write([]byte(got.Username))
	if got.Credential != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
		t.Fatal("the issuer followed a change of the caller's secret")
	}
}
