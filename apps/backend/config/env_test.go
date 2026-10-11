package config

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/ai"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/engine"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// toneOptions returns settings for an environment that plays a one-second tone in the given mode, or skips the test
// if ffmpeg is not installed.
func toneOptions(t *testing.T, mode engine.Mode, port int) Options {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is not installed")
	}
	dir := t.TempDir()
	out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-t", "1",
		"-i", "sine=frequency=440:sample_rate=48000", "-ac", "2", filepath.Join(dir, "a.wav")).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, out)
	}
	return Options{
		HTTPAddr: "127.0.0.1:0",
		AudioDir: dir,
		FFmpeg:   "ffmpeg",
		Frame:    20 * time.Millisecond,
		ICEPort:  port,
		Mode:     mode,
	}
}

// newTestEnv builds an environment from opts and closes it when the test ends.
func newTestEnv(t *testing.T, opts Options) (*Env, error) {
	t.Helper()
	env, err := NewEnv(context.Background(), quiet, opts)
	if err == nil {
		t.Cleanup(func() { _ = env.Close() })
	}
	return env, err
}

func secretFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnvValidateNamesTheFirstMandatoryFieldThatIsMissing(t *testing.T) {
	err := (&Env{Logger: quiet}).Validate()

	if !errors.Is(err, ErrMissingFromEnv) || !strings.Contains(err.Error(), "Metrics") {
		t.Fatalf("got %v, want Metrics named", err)
	}
}

func TestEnvValidateLetsAnOptionalFieldBeNil(t *testing.T) {
	err := (&Env{}).Validate()
	if err == nil {
		t.Fatal("an empty environment was accepted")
	}
	for _, optional := range []string{"TURN", "Presenter", "Runtime"} {
		if strings.Contains(err.Error(), optional+" ") {
			t.Fatalf("the optional field %s was reported: %v", optional, err)
		}
	}
}

func TestNewEnvRefusesSettingsThatBreakARuleAndClosesWhatItOpened(t *testing.T) {
	if _, err := NewEnv(context.Background(), quiet, Options{}); !errors.Is(err, ErrNoHTTPAddr) {
		t.Fatalf("got %v, want ErrNoHTTPAddr", err)
	}
	opts := validOptions()
	opts.AudioDir = filepath.Join(t.TempDir(), "none")
	opts.FFmpeg = "ffmpeg"
	opts.Frame = 20 * time.Millisecond

	env, err := NewEnv(context.Background(), quiet, opts)

	if err == nil || env != nil {
		t.Fatalf("got %v, %v, want an error for a directory that is not there", env, err)
	}
}

func TestNewEnvClosesTheEngineWhenThePresenterCannotBeMade(t *testing.T) {
	opts := toneOptions(t, engine.ModeFanout, 15196)
	opts.Flags = FlagPresenter
	opts.PresenterTTS = []string{"say", "-o", "{out}"}
	opts.OllamaURL = "http://model.example.com:11434"
	if _, err := NewEnv(context.Background(), quiet, opts); !errors.Is(err, ai.ErrRemoteModel) {
		t.Fatalf("got %v, want ErrRemoteModel", err)
	}

	// The engine had opened the UDP port before the presenter was refused, so a build on the same port shows that it
	// was closed again.
	opts.Flags = 0
	opts.PresenterTTS = nil
	opts.OllamaURL = ""
	if _, err := newTestEnv(t, opts); err != nil {
		t.Fatalf("the UDP port of the failed build is still taken: %v", err)
	}
}

func TestThePresenterNeedsAModeWithASharedStream(t *testing.T) {
	opts := toneOptions(t, engine.ModeNaive, 15190)
	opts.Flags = FlagPresenter
	opts.PresenterTTS = []string{"say", "-o", "{out}"}

	if _, err := newTestEnv(t, opts); !errors.Is(err, ErrPresenterMode) {
		t.Fatalf("got %v, want ErrPresenterMode", err)
	}
}

func TestThePresenterIsBuiltFromTheSettingsInAModeWithASharedStream(t *testing.T) {
	opts := toneOptions(t, engine.ModeFanout, 15191)
	opts.Flags = FlagPresenter
	opts.PresenterTTS = []string{"say", "-o", "{out}"}

	env, err := newTestEnv(t, opts)
	if err != nil || env.Presenter == nil {
		t.Fatalf("got %v, %v", env, err)
	}
	// Without a voice the presenter has no use, and the options say so.
	opts.PresenterTTS = nil
	opts.ICEPort = 15192
	if _, err := newTestEnv(t, opts); err == nil {
		t.Fatal("a presenter without a voice was accepted")
	}
}

func TestTheTurnIssuerReadsTheSecretFileAndTrimsIt(t *testing.T) {
	opts := toneOptions(t, engine.ModeFanout, 15193)
	opts.TURNURLs = "turn:turn.example.com:3478?transport=udp, turns:turn.example.com:443?transport=tcp"
	opts.TURNSecretFile = secretFile(t, "0123456789abcdef-secret\n")
	opts.TURNTTL = 10 * time.Minute

	env, err := newTestEnv(t, opts)
	if err != nil {
		t.Fatal(err)
	}
	if env.TURN.TTL() != 10*time.Minute {
		t.Fatalf("ttl %v", env.TURN.TTL())
	}
	server, err := env.TURN.Issue()
	if err != nil {
		t.Fatal(err)
	}
	if len(server.URLs) != 2 || strings.ContainsAny(server.URLs[1], " ") {
		t.Fatalf("urls were not split and trimmed: %q", server.URLs)
	}
}

func TestTheTurnIssuerRefusesAShortSecretAndAMissingFile(t *testing.T) {
	short := toneOptions(t, engine.ModeFanout, 15194)
	short.TURNURLs = "turn:turn.example.com"
	short.TURNSecretFile = secretFile(t, "short\n")
	if _, err := newTestEnv(t, short); err == nil {
		t.Error("a short secret was accepted")
	}
	missing := toneOptions(t, engine.ModeFanout, 15195)
	missing.TURNURLs = "turn:turn.example.com"
	missing.TURNSecretFile = filepath.Join(t.TempDir(), "none")
	if _, err := newTestEnv(t, missing); err == nil {
		t.Error("a missing secret file was accepted")
	}
}
