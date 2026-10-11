package config

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"strings"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/ai"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/engine"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/metrics"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/playlist"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/turn"
)

// Env is the composition root: every concrete dependency of the server, one field each, built once from the
// settings by [NewEnv]. The HTTP server is not one of them: [api.NewServer] builds it from the Env. A pointer,
// interface, function or channel field is mandatory unless it is tagged env:"optional", and [Env.Validate] says which
// is missing.
type Env struct {
	Logger *slog.Logger

	// Metrics holds the counters and gauges of every dependency below.
	Metrics *metrics.Metrics

	// Engine makes the WebRTC connections of the listeners and streams to them. It holds the UDP port.
	Engine *engine.Engine

	// TURN makes the credentials of the TURN server. It is nil when no TURN server is set.
	TURN *turn.Issuer `env:"optional"`

	// Presenter is the AI presenter. It is nil when the presenter is off.
	Presenter *ai.Host `env:"optional"`

	// Runtime reads the health of the Go runtime. It is nil when the interval is 0 or less.
	Runtime *metrics.RuntimeMonitor `env:"optional"`

	// Page holds the built listener page, with index.html at its root. It is nil when the server serves only the API.
	Page fs.FS `env:"optional"`
}

// NewEnv builds the environment that opts describes: it makes the TURN issuer, loads the playlist, and makes the
// runtime monitor, the engine and the AI presenter. The steps that can fail without opening anything come first, so a
// bad setting is reported before the audio is transcoded and before the UDP port is taken.
//
// It returns an error wrapping the result of [Options.Validate] if opts breaks a rule, and [ErrPresenterMode] if the
// presenter is asked for in a mode that has no shared stream.
func NewEnv(ctx context.Context, logger *slog.Logger, opts Options) (*Env, error) {
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("check settings: %w", err)
	}
	// The presenter introduces the tracks of the shared stream, so it needs a mode that has one.
	if opts.presenterOn() && opts.Mode == engine.ModeNaive {
		return nil, ErrPresenterMode
	}
	counters := metrics.New()

	var issuer *turn.Issuer
	if opts.turnOn() {
		secret, err := os.ReadFile(opts.TURNSecretFile)
		if err != nil {
			return nil, fmt.Errorf("read the TURN secret: %w", err)
		}
		var urls []string
		for _, url := range strings.Split(opts.TURNURLs, ",") {
			urls = append(urls, strings.TrimSpace(url))
		}
		issuer, err = turn.New(turn.Options{
			URLs:   urls,
			Secret: bytes.TrimSpace(secret),
			TTL:    opts.TURNTTL,
		})
		if err != nil {
			return nil, err
		}
	}

	audioPlaylist, err := playlist.Load(ctx, opts.AudioDir, playlist.Options{
		FFmpeg:        opts.FFmpeg,
		FrameDuration: opts.Frame,
	})
	if err != nil {
		return nil, err
	}
	logger.Info("playlist loaded",
		"tracks", len(audioPlaylist.Tracks),
		"frame", audioPlaylist.Interval().String(),
		"duration", audioPlaylist.Duration().String(),
		"bytes", audioPlaylist.Bytes(),
	)

	var monitor *metrics.RuntimeMonitor
	if opts.RuntimeInterval > 0 {
		monitor, err = metrics.NewRuntimeMonitor(counters, metrics.RuntimeOptions{
			Interval: opts.RuntimeInterval,
			CSVDir:   opts.RuntimeCSV,
		})
		if err != nil {
			return nil, err
		}
	}

	eng, err := engine.New(engine.Options{
		Mode:           opts.Mode,
		Playlist:       audioPlaylist,
		ICEPort:        opts.ICEPort,
		PublicIPs:      opts.publicIPs(),
		Flags:          opts.engineFlags(),
		Shards:         opts.Shards,
		QueueLen:       opts.QueueLen,
		BatchSize:      opts.Batch,
		BatchDelay:     opts.BatchDelay,
		ConnectTimeout: opts.ConnectTimeout,
		EvictAfter:     opts.EvictAfter,
		MaxListeners:   opts.MaxListeners,
		MaxConnecting:  opts.MaxConnecting,
		Metrics:        counters,
		Logger:         logger,
	})
	if err != nil {
		return nil, err
	}

	var presenter *ai.Host
	if opts.presenterOn() {
		var flags ai.HostFlag
		if opts.Flags&FlagRemoteModel != 0 {
			flags |= ai.HostFlagAllowRemoteModel
		}
		presenter, err = ai.NewHost(ai.HostOptions{
			Playlist:    audioPlaylist,
			AudioDir:    opts.AudioDir,
			Interludes:  eng.Interludes(),
			TrackIndex:  eng.TrackIndex,
			TrackStarts: eng.TrackStarts,
			Metrics:     counters,
			Station:     opts.PresenterName,
			Model:       opts.PresenterModel,
			OllamaURL:   opts.OllamaURL,
			Flags:       flags,
			TTSCommand:  opts.PresenterTTS,
			TTSExt:      opts.PresenterExt,
			Seed:        opts.PresenterSeed,
			Margin:      opts.PresenterMargin,
			FFmpeg:      opts.FFmpeg,
			Logger:      logger,
		})
		if err != nil {
			return nil, errors.Join(fmt.Errorf("make the presenter: %w", err), eng.Close())
		}
		logger.Info("the AI presenter is on", "model", opts.PresenterModel, "ollama", opts.OllamaURL)
	}

	var page fs.FS
	if opts.WebDir != "" {
		page = os.DirFS(opts.WebDir)
	}

	return &Env{
		Logger:    logger,
		Metrics:   counters,
		Engine:    eng,
		TURN:      issuer,
		Presenter: presenter,
		Runtime:   monitor,
		Page:      page,
	}, nil
}

// Validate returns an error naming the first mandatory field that is missing.
func (e *Env) Validate() error {
	return validateFields(e)
}

// Close closes the engine, which holds the UDP port, in a program that stops before the server runs. A server that ran
// has closed it already when it shut down.
func (e *Env) Close() error {
	return e.Engine.Close()
}
