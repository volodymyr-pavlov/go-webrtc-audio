package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/engine"
)

// A Flag is a bit of [Options.Flags].
type Flag uint8

const (
	// FlagLoopback also offers the loopback address, so that a browser on the same machine can connect.
	FlagLoopback Flag = 1 << iota

	// FlagProfiling serves /debug/pprof. It must not be reachable by the public.
	FlagProfiling

	// FlagPresenter turns the AI presenter on.
	FlagPresenter

	// FlagRemoteModel allows an Ollama server that is not on this machine.
	FlagRemoteModel
)

// Options are the settings of the server. A zero value of a setting that has a default means the default: the packages
// that read it say which.
type Options struct {
	// HTTPAddr is the address of the HTTP server.
	HTTPAddr string

	// AudioDir is the directory with the audio files.
	AudioDir string

	// WebDir is the directory with the built listener page. Empty serves only the API.
	WebDir string

	// ICEHost holds the public IP addresses for listeners behind a NAT, separated by commas.
	ICEHost string

	// ICEPort is the one UDP port of every WebRTC connection.
	ICEPort int

	// Mode is how the stream reaches the listeners.
	Mode engine.Mode

	// Shards is the number of shards of the router. 0 is one for each CPU core.
	Shards int

	// QueueLen is the number of packets a listener may have waiting. See [engine.Options.QueueLen].
	QueueLen int

	// Flags holds the bitwise flags: see [Flag].
	Flags Flag

	// ConnectTimeout is the time a listener has to connect. 0 is the default of the engine.
	ConnectTimeout time.Duration

	// EvictAfter is how long a connected listener may lose every packet before it is dropped. 0 is the default of the
	// engine.
	EvictAfter time.Duration

	// MaxListeners is the most listeners held at once, and MaxConnecting the most that have not connected yet.
	MaxListeners  int
	MaxConnecting int

	// RuntimeInterval is the time between readings of the health of the Go runtime. 0 or less reads none.
	RuntimeInterval time.Duration

	// RuntimeCSV is the directory that the last readings are written to, as CSV, when the server stops.
	RuntimeCSV string

	// PresenterName is the name of the station that the presenter says.
	PresenterName string

	// PresenterModel is the Ollama model that writes what the presenter says, and OllamaURL is the server it runs on.
	PresenterModel string
	OllamaURL      string

	// PresenterTTS is the speech program and its arguments. PresenterExt is the extension of the file it writes.
	PresenterTTS []string
	PresenterExt string

	// PresenterMargin is the time kept before the end of a track for the clip to be picked up.
	PresenterMargin time.Duration

	// PresenterSeed is the seed for the choices of the model.
	PresenterSeed int

	// TURNURLs holds the turn: or turns: addresses of a TURN server, separated by commas. TURNSecretFile is the file with
	// the secret that the server shares with it, and TURNTTL is how long the credentials last.
	TURNURLs       string
	TURNSecretFile string
	TURNTTL        time.Duration

	// FFmpeg is the ffmpeg program.
	FFmpeg string

	// Frame is the audio in each packet.
	Frame time.Duration

	// Batch is the number of packets that one system call sends, and BatchDelay the time to wait for more to join a batch.
	Batch      int
	BatchDelay time.Duration

	// LogLevel is the name of the log level.
	LogLevel string
}

// Validate returns an error that joins every rule that the settings break, or nil.
func (o Options) Validate() error {
	var errs []error
	if o.HTTPAddr == "" {
		errs = append(errs, ErrNoHTTPAddr)
	}
	if o.AudioDir == "" {
		errs = append(errs, ErrNoAudioDir)
	}
	if o.ICEPort < 1 || o.ICEPort > 65535 {
		errs = append(errs, fmt.Errorf("%w: %d", ErrICEPort, o.ICEPort))
	}
	if !o.Mode.Valid() {
		errs = append(errs, fmt.Errorf("%w: %d", ErrMode, o.Mode))
	}
	if o.Shards < 0 {
		errs = append(errs, fmt.Errorf("shards %w", ErrNegative))
	}
	if o.Batch < 0 {
		errs = append(errs, fmt.Errorf("batch %w", ErrNegative))
	}
	if o.BatchDelay < 0 {
		errs = append(errs, fmt.Errorf("batch delay %w", ErrNegative))
	}
	if o.Batch > 0 && o.BatchDelay > 0 && o.Frame > 0 && o.BatchDelay >= o.Frame {
		errs = append(errs, fmt.Errorf("%w: %v is not shorter than the frame of %v", ErrBatchDelay, o.BatchDelay, o.Frame))
	}
	if o.ConnectTimeout < 0 {
		errs = append(errs, fmt.Errorf("connect timeout %w", ErrNegative))
	}
	if o.EvictAfter < 0 {
		errs = append(errs, fmt.Errorf("evict after %w", ErrNegative))
	}
	if o.turnOn() && o.TURNTTL <= 0 {
		errs = append(errs, fmt.Errorf("%w: %v", ErrTURNTTL, o.TURNTTL))
	}
	if o.TURNURLs != "" && o.TURNSecretFile == "" {
		errs = append(errs, ErrTURNNeedsSecret)
	}
	if o.TURNURLs == "" && o.TURNSecretFile != "" {
		errs = append(errs, ErrTURNNeedsURL)
	}
	return errors.Join(errs...)
}

// presenterOn reports whether the settings ask for the AI presenter.
func (o Options) presenterOn() bool {
	return o.Flags&FlagPresenter != 0
}

// turnOn reports whether the settings set a TURN server.
func (o Options) turnOn() bool {
	return o.TURNURLs != ""
}

// publicIPs returns the addresses of ICEHost, or none.
func (o Options) publicIPs() []string {
	if o.ICEHost == "" {
		return nil
	}
	return strings.Split(o.ICEHost, ",")
}

// engineFlags returns the settings that the engine takes as flags.
func (o Options) engineFlags() engine.Flag {
	if o.Flags&FlagLoopback != 0 {
		return engine.FlagLoopback
	}
	return 0
}
