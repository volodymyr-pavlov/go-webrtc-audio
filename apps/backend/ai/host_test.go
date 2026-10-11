package ai

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/inmem"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/metrics"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/playlist"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/stream"
)

// funcWriter writes the script of a host with a function, and records the requests that it gets. Its Intro method is the
// script writer that a test gives a host.
type funcWriter struct {
	requests inmem.List[introRequest]
	run      func(ctx context.Context, req introRequest) (Script, error)
}

func (f *funcWriter) Intro(ctx context.Context, req introRequest) (Script, error) {
	f.requests.Add(req)
	return f.run(ctx, req)
}

func (f *funcWriter) count() int {
	return f.requests.Len()
}

func (f *funcWriter) request(i int) introRequest {
	return f.requests.All()[i]
}

// instant says a fixed text at once.
func instant(text string) func(context.Context, introRequest) (Script, error) {
	return func(context.Context, introRequest) (Script, error) {
		return Script{
			Text:   text,
			Source: sourceModel,
		}, nil
	}
}

// blocking waits until its context ends, as a model that takes too long does.
func blocking(startedCh chan<- struct{}, endedCh chan<- error) func(context.Context, introRequest) (Script, error) {
	return func(ctx context.Context, _ introRequest) (Script, error) {
		if startedCh != nil {
			startedCh <- struct{}{}
		}
		<-ctx.Done()
		if endedCh != nil {
			endedCh <- ctx.Err()
		}
		return Script{}, ctx.Err()
	}
}

func silentVoice(context.Context, string, string) error { return nil }

func twoPacketClip(context.Context, string) (playlist.Track, error) {
	return playlist.Track{Packets: [][]byte{
		{1},
		{2},
	}}, nil
}

// testHost returns a host whose tracks each last trackPackets packets of 20 ms, and the things a test needs to watch it.
// A fakePlayout is what the host watches: the track that plays and how many tracks have begun.
type fakePlayout struct {
	index  atomic.Int32
	starts atomic.Uint64
}

// Begin makes track i begin.
func (p *fakePlayout) Begin(i int32) {
	p.index.Store(i)
	p.starts.Add(1)
}

func testHost(t *testing.T, tracks, trackPackets int, writer func(context.Context, introRequest) (Script, error), voice func(context.Context, string, string) error) (*Host, *fakePlayout, *stream.Interludes, *metrics.Metrics) {
	t.Helper()
	list := &playlist.Playlist{}
	for i := 0; i < tracks; i++ {
		list.Tracks = append(list.Tracks, playlist.Track{
			Name:    "track-" + string(rune('a'+i)) + ".wav",
			Packets: make([][]byte, trackPackets),
		})
	}
	playout := &fakePlayout{}
	interludes := &stream.Interludes{}
	m := metrics.New()
	h := &Host{
		station:     "Night Owls",
		list:        list,
		interludes:  interludes,
		trackIndex:  func() int { return int(playout.index.Load()) },
		trackStarts: playout.starts.Load,
		stats:       &m.Presenter,
		writer:      writer,
		voice:       voice,
		load:        twoPacketClip,
		logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		margin:      100 * time.Millisecond,
		minBudget:   100 * time.Millisecond,
		poll:        5 * time.Millisecond,
		items:       make([]atomic.Pointer[Item], tracks),
	}
	return h, playout, interludes, m
}

// run starts the host and returns a function that stops it and waits for it.
func runHost(t *testing.T, h *Host) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		if err := h.Do(ctx); err != nil {
			t.Errorf("Do returned %v", err)
		}
	}()
	return func() {
		cancel()
		select {
		case <-doneCh:
		case <-time.After(5 * time.Second):
			t.Error("Do did not return after its context was cancelled")
		}
	}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestTheHostPreparesTheIntroductionOfTheNextTrackWhenATrackBegins(t *testing.T) {
	defer goleak.VerifyNone(t)
	writer := &funcWriter{run: instant("Next, track b.")}
	h, index, interludes, m := testHost(t, 3, 100, writer.Intro, silentVoice)
	stop := runHost(t, h)
	defer stop()

	index.Begin(0)
	eventually(t, "a clip", func() bool { return interludes.Waiting() != nil })

	clip := interludes.Waiting()
	if clip.Before != 1 || clip.Text != "Next, track b." || len(clip.Packets) != 2 {
		t.Fatalf("clip %+v", clip)
	}
	// The first introduction opens the station, so it has no previous track to follow.
	req := writer.request(0)
	if req.Station != "Night Owls" || req.Next.Track.Name != "track-b.wav" || req.Previous != nil {
		t.Fatalf("request %+v", req)
	}
	if m.Presenter.Prepared.Load() != 1 || m.Presenter.Late.Load() != 0 || m.Presenter.Failed.Load() != 0 {
		t.Fatalf("%+v", m.Snapshot().Presenter)
	}
	stop()
}

func TestTheLastTrackIsFollowedByTheFirstOneAgain(t *testing.T) {
	writer := &funcWriter{run: instant("Back to the start.")}
	h, index, interludes, _ := testHost(t, 3, 100, writer.Intro, silentVoice)
	stop := runHost(t, h)
	defer stop()

	index.Begin(2)
	eventually(t, "a clip", func() bool { return interludes.Waiting() != nil })
	if clip := interludes.Waiting(); clip.Before != 0 {
		t.Fatalf("the clip is for track %d, want 0", clip.Before)
	}
}

func TestAnIntroductionThatCannotBeReadyBeforeTheTrackEndsIsDroppedAndCounted(t *testing.T) {
	startedCh := make(chan struct{}, 1)
	writer := &funcWriter{run: blocking(startedCh, nil)}
	// Tracks of 30 packets last 600 ms: the budget is 500 ms, and the model never answers.
	h, index, interludes, m := testHost(t, 2, 30, writer.Intro, silentVoice)
	stop := runHost(t, h)
	defer stop()

	index.Begin(0)
	<-startedCh
	eventually(t, "the introduction to be counted late", func() bool { return m.Presenter.Late.Load() == 1 })
	if interludes.Waiting() != nil {
		t.Fatal("a clip was offered although the model never answered")
	}
	if m.Presenter.Failed.Load() != 0 || m.Presenter.Prepared.Load() != 0 {
		t.Fatalf("%+v", m.Snapshot().Presenter)
	}
}

func TestATrackTooShortForAnIntroductionGetsNoneAndTheModelIsNotAsked(t *testing.T) {
	writer := &funcWriter{run: instant("never")}
	// Tracks of 10 packets last 200 ms; with the margin of 100 ms, a budget of 100 ms, and a minimum of 100 ms that is
	// not met once the margin is 150 ms.
	h, index, _, m := testHost(t, 2, 10, writer.Intro, silentVoice)
	h.margin = 150 * time.Millisecond
	stop := runHost(t, h)
	defer stop()

	index.Begin(0)
	eventually(t, "the track to be counted late", func() bool { return m.Presenter.Late.Load() == 1 })
	if writer.count() != 0 {
		t.Fatal("the model was asked for an introduction that had no time")
	}
}

func TestANewTrackCancelsTheIntroductionThatIsStillBeingMade(t *testing.T) {
	startedCh := make(chan struct{}, 2)
	endedCh := make(chan error, 2)
	writer := &funcWriter{run: blocking(startedCh, endedCh)}
	h, index, _, m := testHost(t, 3, 500, writer.Intro, silentVoice)
	stop := runHost(t, h)
	defer stop()

	index.Begin(0)
	<-startedCh
	index.Begin(1)
	if err := <-endedCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("the first introduction ended with %v, want a cancellation", err)
	}
	<-startedCh
	if writer.request(1).Next.Track.Name != "track-c.wav" {
		t.Fatalf("the second request is for %s, want track-c.wav", writer.request(1).Next.Track.Name)
	}
	// Being replaced is not being late, and it is not a failure.
	if m.Presenter.Late.Load() != 0 || m.Presenter.Failed.Load() != 0 {
		t.Fatalf("%+v", m.Snapshot().Presenter)
	}
}

func TestAFailureOfTheModelOrTheVoiceIsCountedAndNothingIsOffered(t *testing.T) {
	boom := errors.New("model down")
	for name, tc := range map[string]struct {
		writer func(context.Context, introRequest) (Script, error)
		voice  func(context.Context, string, string) error
	}{
		"the model": {
			writer: func(context.Context, introRequest) (Script, error) { return Script{}, boom },
			voice:  silentVoice,
		},
		"the voice": {
			writer: instant("hello"),
			voice:  func(context.Context, string, string) error { return boom },
		},
	} {
		h, index, interludes, m := testHost(t, 2, 100, (&funcWriter{run: tc.writer}).Intro, tc.voice)
		stop := runHost(t, h)
		index.Begin(0)
		eventually(t, name+" to be counted as failed", func() bool { return m.Presenter.Failed.Load() == 1 })
		if interludes.Waiting() != nil {
			t.Errorf("%s failed, and a clip was offered", name)
		}
		stop()
	}
}

func TestAFailureOfTheConversionIsCountedToo(t *testing.T) {
	h, index, interludes, m := testHost(t, 2, 100, (&funcWriter{run: instant("hello")}).Intro, silentVoice)
	h.load = func(context.Context, string) (playlist.Track, error) {
		return playlist.Track{}, errors.New("ffmpeg failed")
	}
	stop := runHost(t, h)
	defer stop()
	index.Begin(0)
	eventually(t, "the failure", func() bool { return m.Presenter.Failed.Load() == 1 })
	if interludes.Waiting() != nil {
		t.Fatal("a clip was offered")
	}
}

func TestAPlainLineIsOfferedAndCountedAsAFallback(t *testing.T) {
	writer := &funcWriter{run: func(context.Context, introRequest) (Script, error) {
		return Script{
			Text:   "Next on Night Owls: track b.",
			Source: sourceFallback,
		}, nil
	}}
	h, index, interludes, m := testHost(t, 2, 100, writer.Intro, silentVoice)
	stop := runHost(t, h)
	defer stop()
	index.Begin(0)
	eventually(t, "a clip", func() bool { return interludes.Waiting() != nil })
	if m.Presenter.Fallback.Load() != 1 || m.Presenter.Prepared.Load() != 1 {
		t.Fatalf("%+v", m.Snapshot().Presenter)
	}
}

func TestANewerClipReplacesTheOneThatNeverPlayedAndIsCounted(t *testing.T) {
	writer := &funcWriter{run: instant("hello")}
	h, index, interludes, m := testHost(t, 3, 100, writer.Intro, silentVoice)
	stop := runHost(t, h)
	defer stop()
	index.Begin(0)
	eventually(t, "the first clip", func() bool { return interludes.Waiting() != nil })
	index.Begin(2)
	eventually(t, "the second clip", func() bool { return interludes.Waiting() != nil && interludes.Waiting().Before == 0 })
	if m.Presenter.Replaced.Load() != 1 || m.Presenter.Prepared.Load() != 2 {
		t.Fatalf("%+v", m.Snapshot().Presenter)
	}
}

func TestTheTimingsOfTheLastIntroductionAreKept(t *testing.T) {
	writer := &funcWriter{run: func(context.Context, introRequest) (Script, error) {
		time.Sleep(20 * time.Millisecond)
		return Script{
			Text:   "hello",
			Source: sourceModel,
		}, nil
	}}
	voice := func(context.Context, string, string) error {
		time.Sleep(20 * time.Millisecond)
		return nil
	}
	h, index, interludes, m := testHost(t, 2, 100, writer.Intro, voice)
	stop := runHost(t, h)
	defer stop()
	index.Begin(0)
	eventually(t, "a clip", func() bool { return interludes.Waiting() != nil })
	s := m.Snapshot().Presenter
	if s.ScriptMillis < 20 || s.VoiceMillis < 20 || s.ClipMillis < s.ScriptMillis+s.VoiceMillis {
		t.Fatalf("%+v", s)
	}
}

// fixedAnalyzer returns an analyzer that describes every file as the same real track, or fails with err if there is one.
func fixedAnalyzer(err error) func(context.Context, string) (Track, error) {
	return func(_ context.Context, path string) (Track, error) {
		if err != nil {
			return Track{}, err
		}
		return Track{
			File:   path,
			Name:   filepath.Base(path),
			Title:  "Real Title",
			Artist: "Real Artist",
		}, nil
	}
}

// fixedTagger returns a tagger that gives every track the same tags, or fails with err if there is one.
func fixedTagger(err error) func(context.Context, Track) (Tags, error) {
	return func(context.Context, Track) (Tags, error) {
		if err != nil {
			return Tags{}, err
		}
		return Tags{
			Mood:   moodDreamy,
			Energy: 2,
			Genre:  "synthwave",
			Source: sourceModel,
		}, nil
	}
}

func TestTheHostReadsTheTracksInTheBackgroundAndUsesWhatItFound(t *testing.T) {
	writer := &funcWriter{run: instant("hello")}
	h, index, interludes, _ := testHost(t, 2, 100, writer.Intro, silentVoice)
	h.audioDir = "audio"
	h.analyzer, h.tagger = fixedAnalyzer(nil), fixedTagger(nil)
	stop := runHost(t, h)
	defer stop()

	eventually(t, "both tracks to be read", func() bool { return h.items[0].Load() != nil && h.items[1].Load() != nil })
	index.Begin(0)
	eventually(t, "a clip", func() bool { return interludes.Waiting() != nil })
	next := writer.request(0).Next
	if next.Track.Title != "Real Title" || next.Tags.Mood != moodDreamy {
		t.Fatalf("the request did not use what was found: %+v", next)
	}
}

func TestATrackThatIsNotReadYetIsIntroducedByItsFileName(t *testing.T) {
	h, _, _, _ := testHost(t, 2, 100, (&funcWriter{run: instant("hello")}).Intro, silentVoice)
	item := h.item(1)
	if item.Track.Name != "track-b.wav" || item.Tags.Mood != 0 || item.Track.DisplayTitle() != "track b" {
		t.Fatalf("%+v", item)
	}
}

func TestReadingStopsAtTheFirstErrorOfTheModelAndSkipsAFileThatCannotBeRead(t *testing.T) {
	h, _, _, _ := testHost(t, 3, 100, (&funcWriter{run: instant("hello")}).Intro, silentVoice)
	h.audioDir = "audio"
	h.analyzer, h.tagger = fixedAnalyzer(errors.New("not audio")), fixedTagger(nil)
	h.describe(context.Background())
	for i := range h.items {
		if h.items[i].Load() != nil {
			t.Fatalf("track %d was described although it could not be read", i)
		}
	}

	h.analyzer, h.tagger = fixedAnalyzer(nil), fixedTagger(errors.New("connection refused"))
	h.describe(context.Background())
	for i := range h.items {
		if h.items[i].Load() != nil {
			t.Fatalf("track %d was described although the model failed", i)
		}
	}
}

func TestTheModelIsWarmedUpOnceAtTheStart(t *testing.T) {
	h, _, _, _ := testHost(t, 2, 100, (&funcWriter{run: instant("hello")}).Intro, silentVoice)
	model := &fakeModel{Answers: []string{`{"ok": true}`}}
	h.model = model
	stop := runHost(t, h)
	eventually(t, "the warm-up", func() bool { return len(model.prompts()) == 1 })
	stop()
	if got := len(model.prompts()); got != 1 {
		t.Fatalf("%d warm-up requests, want 1", got)
	}
}

func TestNewHostRefusesOptionsNoRunCouldUse(t *testing.T) {
	list := &playlist.Playlist{Tracks: []playlist.Track{{
		Name:    "a.wav",
		Packets: [][]byte{{1}},
	}}}
	good := HostOptions{
		Playlist:    list,
		Interludes:  &stream.Interludes{},
		TrackIndex:  func() int { return 0 },
		TrackStarts: func() uint64 { return 0 },
		TTSCommand:  []string{"piper", "--output_file", "{out}"},
	}
	tests := map[string]func(*HostOptions){
		"no playlist":               func(o *HostOptions) { o.Playlist = nil },
		"an empty playlist":         func(o *HostOptions) { o.Playlist = &playlist.Playlist{} },
		"no interludes":             func(o *HostOptions) { o.Interludes = nil },
		"no track index":            func(o *HostOptions) { o.TrackIndex = nil },
		"no track starts":           func(o *HostOptions) { o.TrackStarts = nil },
		"no voice":                  func(o *HostOptions) { o.TTSCommand = nil },
		"a voice without {out}":     func(o *HostOptions) { o.TTSCommand = []string{"say", "-f", "-"} },
		"an extension without dot":  func(o *HostOptions) { o.TTSExt = "wav" },
		"a negative timeout":        func(o *HostOptions) { o.CallTimeout = -time.Second },
		"a negative margin":         func(o *HostOptions) { o.Margin = -time.Second },
		"not a url":                 func(o *HostOptions) { o.OllamaURL = "not a url" },
		"a scheme that is not http": func(o *HostOptions) { o.OllamaURL = "ftp://127.0.0.1" },
		"no host":                   func(o *HostOptions) { o.OllamaURL = "http://" },
	}
	for name, change := range tests {
		o := good
		change(&o)
		if _, err := NewHost(o); !errors.Is(err, ErrBadOptions) {
			t.Errorf("%s: got %v, want ErrBadOptions", name, err)
		}
	}
	if _, err := NewHost(good); err != nil {
		t.Fatalf("the defaults were refused: %v", err)
	}
}

func TestTheModelServerMustBeOnThisMachineUnlessAllowed(t *testing.T) {
	list := &playlist.Playlist{Tracks: []playlist.Track{{
		Name:    "a.wav",
		Packets: [][]byte{{1}},
	}}}
	with := func(address string, flags HostFlag) error {
		_, err := NewHost(HostOptions{
			Playlist:    list,
			Interludes:  &stream.Interludes{},
			TrackIndex:  func() int { return 0 },
			TrackStarts: func() uint64 { return 0 },
			TTSCommand:  []string{"piper", "--output_file", "{out}"},
			OllamaURL:   address,
			Flags:       flags,
		})
		return err
	}
	for _, local := range []string{"http://127.0.0.1:11434", "http://localhost:11434", "http://[::1]:11434", "http://host.docker.internal:11434", "http://ollama:11434"} {
		if err := with(local, 0); err != nil {
			t.Errorf("%s: %v", local, err)
		}
	}
	for _, remote := range []string{"http://ai.example.com:11434", "https://10.0.0.5", "http://127.0.0.1.evil.example"} {
		if err := with(remote, 0); !errors.Is(err, ErrRemoteModel) || !errors.Is(err, ErrBadOptions) {
			t.Errorf("%s: got %v, want ErrRemoteModel", remote, err)
		}
		if err := with(remote, HostFlagAllowRemoteModel); err != nil {
			t.Errorf("%s with HostFlagAllowRemoteModel: %v", remote, err)
		}
	}
}

func TestTheSecondIntroductionFollowsTheTrackThatPlays(t *testing.T) {
	writer := &funcWriter{run: instant("Up next, track c.")}
	h, index, interludes, _ := testHost(t, 3, 100, writer.Intro, silentVoice)
	stop := runHost(t, h)
	defer stop()

	index.Begin(0)
	eventually(t, "the first clip", func() bool { return interludes.Waiting() != nil })
	index.Begin(1)
	eventually(t, "the second clip", func() bool { return interludes.Waiting() != nil && interludes.Waiting().Before == 2 })

	req := writer.request(1)
	if req.Previous == nil || req.Previous.Track.Name != "track-b.wav" || req.Next.Track.Name != "track-c.wav" {
		t.Fatalf("request %+v", req)
	}
}

func TestAPlaylistOfOneTrackIsIntroducedEveryTimeItComesRound(t *testing.T) {
	writer := &funcWriter{run: instant("Here it is again.")}
	h, index, interludes, m := testHost(t, 1, 100, writer.Intro, silentVoice)
	stop := runHost(t, h)
	defer stop()

	index.Begin(0)
	eventually(t, "the first clip", func() bool { return m.Presenter.Prepared.Load() == 1 })
	if clip := interludes.Waiting(); clip == nil || clip.Before != 0 {
		t.Fatalf("clip %+v, want one for track 0", clip)
	}
	index.Begin(0)
	eventually(t, "the second clip", func() bool { return m.Presenter.Prepared.Load() == 2 })
}
