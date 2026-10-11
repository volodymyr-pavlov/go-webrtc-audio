package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/ollama"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/metrics"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/playlist"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/stream"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/worker"
)

var _ worker.Worker = &Host{}

const (
	defaultModel     = "phi3.5"
	defaultOllamaURL = "http://127.0.0.1:11434"
	defaultStation   = "the station"
	defaultTTSExt    = ".wav"
	defaultFFmpeg    = "ffmpeg"
	defaultFFprobe   = "ffprobe"

	// clipName is the name of the file that the voice writes in the folder of an introduction.
	clipName = "clip" + defaultTTSExt

	// defaultMargin is the time kept before the end of a track for the playout to pick the clip up, and for the clock of
	// the host, which looks at the track every pollEvery, to be off by that much.
	defaultMargin = 3 * time.Second

	// defaultMinBudget is the least time worth starting an introduction for. A model and a voice cannot finish in less.
	defaultMinBudget = 3 * time.Second

	// defaultMaxTokens is the longest answer asked of the model. An introduction is a sentence or two, which is under
	// fifty tokens; a longer limit only gives a model that rambles more time to do it.
	defaultMaxTokens = 120

	// hostRetries is how many more times a script that fails its checks is asked for. It is one, not the two of an
	// offline run: every try uses time that the track's deadline does not give back.
	hostRetries = 1

	// pollEvery is how often the host looks at which track plays.
	pollEvery = 200 * time.Millisecond
)

// A HostFlag is a bit of [HostOptions.Flags].
type HostFlag uint8

const (
	// HostFlagAllowRemoteModel allows an OllamaURL whose host is not this machine (or a container next to it). Track
	// titles and file names are sent to the model, so the default is to refuse.
	HostFlagAllowRemoteModel HostFlag = 1 << iota
)

// HostOptions describe a [Host]. The zero value of a field that has a default means the default.
type HostOptions struct {
	// Playlist is what the server plays. Its tracks are the ones the host introduces, in the order they play, and it is
	// needed.
	Playlist *playlist.Playlist

	// AudioDir is the directory that Playlist was read from. The host reads the tags and the loudness of the files in the
	// background, so that its introductions can say more than a title. Empty skips that.
	AudioDir string

	// Interludes is the slot of the shared stream in which a finished clip waits for its track. It is needed.
	Interludes *stream.Interludes

	// TrackIndex returns the index in the playlist of the track that plays now. It is needed.
	TrackIndex func() int

	// TrackStarts returns how many tracks have begun so far. The host prepares an introduction each time it grows, so
	// a playlist of one track is introduced every time it comes round. It is needed.
	TrackStarts func() uint64

	// Metrics receives the counters and timings of the host. The default is a new, private [metrics.Metrics].
	Metrics *metrics.Metrics

	// Station is the name of the station, which the presenter may say. The default is "the station".
	Station string

	// Model is the name of the Ollama model. The default is phi3.5, which is under the MIT licence.
	Model string

	// OllamaURL is the address of the Ollama server. The default is http://127.0.0.1:11434.
	OllamaURL string

	// Flags holds the bitwise flags: see [HostFlag].
	Flags HostFlag

	// TTSCommand is the speech program and its arguments, one element each, with {out} in an argument for the file it
	// writes. The text is sent to it on standard input. It is needed: the host speaks, so without a voice it has no use.
	TTSCommand []string

	// TTSExt is the extension of the file the speech program writes, with its dot. The default is ".wav".
	TTSExt string

	// Seed makes the model's choices repeatable where the model supports it. Zero leaves it to the model.
	Seed int

	// Retries is how many more times a script that fails its checks is asked for. The default is 1; a negative value asks
	// once.
	Retries int

	// CallTimeout is how long one call to the model may take, at most; the time left of the track also limits it. The
	// default is two minutes.
	CallTimeout time.Duration

	// MaxTokens is the longest answer asked of the model, in tokens. The default is 120.
	MaxTokens int

	// Margin is the time kept before the end of a track for the playout to pick up the clip. The default is 3 seconds. An
	// introduction is started only if the track has more time left than Margin and a little more.
	Margin time.Duration

	// FFmpeg and FFprobe are the programs to start. The defaults are "ffmpeg" and "ffprobe".
	FFmpeg  string
	FFprobe string

	// Logger is where the host writes. The default is [slog.Default].
	Logger *slog.Logger
}

const (
	// hostOpened says that the presenter has opened the station: the first introduction welcomes the listeners, and the
	// ones after it do not.
	hostOpened uint32 = 1 << iota
)

// A clipLoader turns the audio file at path into the Opus packets of the stream.
type clipLoader func(ctx context.Context, path string) (playlist.Track, error)

// A Host is the AI presenter of a running station. While a track plays it writes, speaks and converts the introduction
// of the next track, and puts the clip in the slot of the shared stream, from which the playout plays it when that
// track begins. The track's remaining time is the deadline: an introduction that cannot be ready by then is dropped, counted,
// and the stream goes on without it. Nothing the host does is on the path of a packet.
//
// It is a [worker.Worker]; it needs a language model, a speech program and ffmpeg, and it does nothing else.
type Host struct {
	station     string
	list        *playlist.Playlist
	audioDir    string
	interludes  *stream.Interludes
	trackIndex  func() int
	trackStarts func() uint64
	stats       *metrics.PresenterMetrics

	// state holds the flags below.
	state atomic.Uint32
	model llms.Model

	// writer writes what the presenter says before a track, voice turns that text into an audio clip at a path, load turns
	// the clip into the packets of the stream, analyzer reads what is known about an audio file, and tagger describes a
	// track. Each is one function, so that a test can give a host a function of its own for any of them.
	writer    func(ctx context.Context, req introRequest) (Script, error)
	voice     func(ctx context.Context, text, path string) error
	load      clipLoader
	analyzer  func(ctx context.Context, path string) (Track, error)
	tagger    func(ctx context.Context, track Track) (Tags, error)
	logger    *slog.Logger
	margin    time.Duration
	minBudget time.Duration
	poll      time.Duration

	// items holds what is known about each track, in the order of the playlist. An entry is nil until the track has been
	// read, which happens in the background, so an introduction written before that says what the file name says.
	items []atomic.Pointer[Item]
}

// NewHost returns the [Host] that opts describes, with its model client, its voice and its analyzer. It returns an
// error that matches [ErrBadOptions] for options that cannot work, and [ErrRemoteModel] for a model server that is not
// local.
func NewHost(opts HostOptions) (*Host, error) {
	if err := validateOptions(opts); err != nil {
		return nil, err
	}
	opts = withDefaults(opts)
	if !strings.HasPrefix(opts.TTSExt, ".") {
		return nil, fmt.Errorf("%w: TTSExt must start with a dot, such as .wav, not %q", ErrBadOptions, opts.TTSExt)
	}
	if err := checkModelServer(opts.OllamaURL, opts.Flags); err != nil {
		return nil, err
	}

	// "json" makes Ollama answer in JSON, which every prompt of the host asks for.
	model, err := ollama.New(
		ollama.WithModel(opts.Model),
		ollama.WithServerURL(opts.OllamaURL),
		ollama.WithFormat("json"),
	)
	if err != nil {
		return nil, fmt.Errorf("ai: the model client: %w", err)
	}
	asker := newAsker(model, askOptions{
		Retries:   opts.Retries,
		Seed:      opts.Seed,
		Timeout:   opts.CallTimeout,
		MaxTokens: opts.MaxTokens,
		Logger:    opts.Logger,
	})
	runner := Exec
	speaker, err := newSpeaker(runner, voiceOptions{
		Command:   opts.TTSCommand,
		Extension: opts.TTSExt,
		FFmpeg:    opts.FFmpeg,
		Logger:    opts.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadOptions, err)
	}
	frame := opts.Playlist.Interval()
	h := &Host{
		station:    opts.Station,
		list:       opts.Playlist,
		audioDir:   opts.AudioDir,
		interludes: opts.Interludes,
		trackIndex: opts.TrackIndex,
		stats:      &opts.Metrics.Presenter,
		model:      model,
		writer:     newPresenter(asker).Intro,
		voice:      speaker.Say,
		load: func(ctx context.Context, path string) (playlist.Track, error) {
			return playlist.LoadTrack(ctx, path, playlist.Options{
				FFmpeg:        opts.FFmpeg,
				FrameDuration: frame,
			})
		},
		analyzer: newAnalyzer(runner, analyzerOptions{
			FFmpeg:  opts.FFmpeg,
			FFprobe: opts.FFprobe,
		}).Track,
		tagger:    newTagger(asker).Tag,
		logger:    opts.Logger,
		margin:    opts.Margin,
		minBudget: defaultMinBudget,
		poll:      pollEvery,
		items:     make([]atomic.Pointer[Item], len(opts.Playlist.Tracks)),
	}
	return h, nil
}

// validateOptions returns an error that matches [ErrBadOptions] for options that are missing or out of range.
func validateOptions(opts HostOptions) error {
	switch {
	case opts.Playlist == nil || len(opts.Playlist.Tracks) == 0:
		return fmt.Errorf("%w: Playlist is needed and must have tracks", ErrBadOptions)
	case opts.Interludes == nil:
		return fmt.Errorf("%w: Interludes is needed", ErrBadOptions)
	case opts.TrackIndex == nil:
		return fmt.Errorf("%w: TrackIndex is needed", ErrBadOptions)
	case opts.TrackStarts == nil:
		return fmt.Errorf("%w: TrackStarts is needed", ErrBadOptions)
	case len(opts.TTSCommand) == 0:
		return fmt.Errorf("%w: TTSCommand is needed, because the host speaks", ErrBadOptions)
	case opts.CallTimeout < 0 || opts.Margin < 0 || opts.MaxTokens < 0:
		return fmt.Errorf("%w: CallTimeout, Margin and MaxTokens must not be negative", ErrBadOptions)
	}
	return nil
}

// withDefaults returns opts with the default in every field that is zero and has one.
func withDefaults(opts HostOptions) HostOptions {
	if opts.Model == "" {
		opts.Model = defaultModel
	}
	if opts.OllamaURL == "" {
		opts.OllamaURL = defaultOllamaURL
	}
	if opts.TTSExt == "" {
		opts.TTSExt = defaultTTSExt
	}
	if opts.Station == "" {
		opts.Station = defaultStation
	}
	if opts.Retries == 0 {
		opts.Retries = hostRetries
	}
	if opts.MaxTokens == 0 {
		opts.MaxTokens = defaultMaxTokens
	}
	if opts.Margin == 0 {
		opts.Margin = defaultMargin
	}
	if opts.Metrics == nil {
		opts.Metrics = metrics.New()
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return opts
}

// checkModelServer refuses a model server that is not on this machine, unless [HostFlagAllowRemoteModel] is set: the
// track titles and the file names go to the model, and they are not sent off the machine by accident.
func checkModelServer(address string, flags HostFlag) error {
	u, err := url.Parse(address)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return fmt.Errorf("%w: OllamaURL must be an http or https address, not %q", ErrBadOptions, address)
	}
	if flags&HostFlagAllowRemoteModel != 0 {
		return nil
	}
	switch host := u.Hostname(); host {
	case "127.0.0.1", "localhost", "::1", "host.docker.internal", "ollama":
		return nil
	default:
		return fmt.Errorf("%w %q: set HostFlagAllowRemoteModel to send track titles to it", ErrRemoteModel, host)
	}
}

// Do runs the host until ctx is cancelled, and then returns nil. It warms up the model, reads the tracks in the
// background, and starts an introduction for the next track whenever a track begins, which it sees in the count of
// [HostOptions.TrackStarts]. A track that begins before the introduction of the one before it is done cancels that
// introduction and starts its own.
func (h *Host) Do(ctx context.Context) error {
	var background sync.WaitGroup
	defer background.Wait()

	background.Add(2)
	go func() {
		defer background.Done()
		h.warmUp(ctx)
	}()
	go func() {
		defer background.Done()
		h.describe(ctx)
	}()

	ticker := time.NewTicker(h.poll)
	defer ticker.Stop()
	var lastStarts uint64
	cancelJob := func() {}
	defer func() { cancelJob() }()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		starts := h.trackStarts()
		if starts == lastStarts {
			continue
		}
		lastStarts = starts
		index := h.trackIndex()
		cancelJob()
		jobCtx, cancel := context.WithCancel(ctx)
		cancelJob = cancel
		background.Add(1)
		go func() {
			defer background.Done()
			h.prepare(jobCtx, index)
		}()
	}
}

// Close does nothing; the host holds no resource besides its goroutines, which end with [Host.Do].
func (h *Host) Close() error {
	return nil
}

// warmUp makes the model load into memory with a one-word request, so that the first introduction does not pay for it:
// loading a model of a few gigabytes takes longer than a short track.
func (h *Host) warmUp(ctx context.Context) {
	if h.model == nil {
		return
	}
	started := time.Now()
	_, err := llms.GenerateFromSinglePrompt(ctx, h.model, `Answer with the JSON {"ok": true}.`, llms.WithMaxTokens(8))
	if err != nil {
		if ctx.Err() == nil {
			h.logger.Warn("the model did not warm up", "err", err)
		}
		return
	}
	h.logger.Info("the model is ready", "took", time.Since(started).Round(time.Millisecond))
}

// describe reads the tags and the loudness of every track, and asks the model to describe it, one track after another
// in the background. A track that cannot be read keeps the entry it has, which is none. It stops at the first error of
// the model itself, which is not worth repeating for every track.
func (h *Host) describe(ctx context.Context) {
	if h.audioDir == "" || h.analyzer == nil || h.tagger == nil {
		return
	}
	for i, t := range h.list.Tracks {
		track, err := h.analyzer(ctx, filepath.Join(h.audioDir, t.Name))
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			h.logger.Warn("track not read", "track", t.Name, "err", err)
			continue
		}
		tags, err := h.tagger(ctx, track)
		if err != nil {
			if ctx.Err() == nil {
				h.logger.Warn("the model cannot describe tracks, so the introductions use titles only", "err", err)
			}
			return
		}
		h.items[i].Store(&Item{
			Track: track,
			Tags:  tags,
		})
		h.logger.Info("track described", "track", t.Name, "mood", tags.Mood, "energy", tags.Energy, "source", tags.Source)
	}
}

// item returns what is known about track i: what describe found, or the name of the file.
func (h *Host) item(i int) Item {
	if known := h.items[i].Load(); known != nil {
		return *known
	}
	return Item{Track: Track{Name: h.list.Tracks[i].Name}}
}

// prepare writes, speaks and converts the introduction of the track after track index, and offers the clip to the
// playout. The time left of track index, less the margin, is its deadline.
func (h *Host) prepare(ctx context.Context, index int) {
	started := time.Now()
	next := (index + 1) % len(h.list.Tracks)
	budget, ok := h.budget(index, next)
	if !ok {
		return
	}
	ctx, cancel := context.WithDeadline(ctx, started.Add(budget))
	defer cancel()

	script, ok := h.script(ctx, index, next)
	if !ok {
		return
	}
	clip, ok := h.synthesize(ctx, script, started)
	if !ok {
		return
	}
	h.offer(index, next, clip, script, started)
}

// budget returns the time that an introduction for track next may take: the time left of track index, less the margin.
// It returns false, after counting the introduction as late, if that is less than the least worth starting.
func (h *Host) budget(index, next int) (time.Duration, bool) {
	budget := h.list.TrackDuration(index) - h.margin
	if budget >= h.minBudget {
		return budget, true
	}
	h.stats.Late.Add(1)
	h.logger.Warn("no time for an introduction", "next", h.list.Tracks[next].Name,
		"budget", budget.Round(time.Millisecond))
	return 0, false
}

// script asks the presenter for the words before track next. The first words after the start open the station, and
// the ones after that follow the track that plays. It returns false if there are none.
func (h *Host) script(ctx context.Context, index, next int) (Script, bool) {
	request := introRequest{
		Station: h.station,
		Next:    h.item(next),
	}
	if h.state.Load()&hostOpened != 0 {
		previous := h.item(index)
		request.Previous = &previous
	}
	scripted := time.Now()
	script, err := h.writer(ctx, request)
	h.stats.ScriptMillis.Store(time.Since(scripted).Milliseconds())
	if err != nil {
		h.failed(ctx, "write the introduction", err)
		return Script{}, false
	}
	h.state.Or(hostOpened)
	if script.Source&sourceFallback != 0 {
		h.stats.Fallback.Add(1)
	}
	return script, true
}

// synthesize speaks script into a temporary folder and converts the file to the packets of the stream. started is when
// the introduction began. It returns false if a step fails or the time is up.
func (h *Host) synthesize(ctx context.Context, script Script, started time.Time) (playlist.Track, bool) {
	dir, err := os.MkdirTemp("", "host-")
	if err != nil {
		h.failed(ctx, "make a place for the clip", err)
		return playlist.Track{}, false
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			h.logger.Debug("the clip's folder was not removed", "err", err)
		}
	}()
	spoken := time.Now()
	path := filepath.Join(dir, clipName)
	if err := h.voice(ctx, script.Text, path); err != nil {
		h.failed(ctx, "speak the introduction", err)
		return playlist.Track{}, false
	}
	h.stats.VoiceMillis.Store(time.Since(spoken).Milliseconds())
	clip, err := h.load(ctx, path)
	if err != nil {
		h.failed(ctx, "convert the clip", err)
		return playlist.Track{}, false
	}
	h.stats.ClipMillis.Store(time.Since(started).Milliseconds())
	if ctx.Err() != nil {
		h.failed(ctx, "finish the introduction", ctx.Err())
		return playlist.Track{}, false
	}
	return clip, true
}

// offer puts the clip in the slot of the playout, to play before track next, and counts it. index is the track that
// plays now.
func (h *Host) offer(index, next int, clip playlist.Track, script Script, started time.Time) {
	offer := &stream.Clip{
		Before:  next,
		Packets: clip.Packets,
		Text:    script.Text,
	}
	if replaced := h.interludes.Offer(offer); replaced != nil {
		h.stats.Replaced.Add(1)
	}
	h.stats.Prepared.Add(1)
	if h.trackIndex() != index {
		// The track ended while the clip was being finished. The clip waits for its track to come round again.
		h.stats.Late.Add(1)
	}
	h.logger.Info("introduction ready", "next", h.list.Tracks[next].Name,
		"took", time.Since(started).Round(time.Millisecond),
		"source", script.Source, "says", script.Text)
}

// failed counts an introduction that did not get made at step: late if its time ran out, failed if a program or the
// model returned an error. One that the start of the next track cancelled is counted as neither; the host moves on to
// the introduction of the track after it.
func (h *Host) failed(ctx context.Context, step string, err error) {
	if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
			h.stats.Late.Add(1)
			h.logger.Warn("the introduction was not ready in time", "step", step)
		}
		return
	}
	h.stats.Failed.Add(1)
	h.logger.Warn("introduction failed", "step", step, "err", err)
}
