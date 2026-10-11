package api

import (
	"bufio"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/tidwall/gjson"
	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/fasthttputil"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/config"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/edge"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/engine"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/metrics"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/playlist"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/stream"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/turn"
)

func newServer(t *testing.T, port int, page *fstest.MapFS, maxListeners int) *Server {
	t.Helper()
	return newServerWith(t, port, page, maxListeners, false)
}

// newServerWithTURN is newServer, with TURN credentials handed out by issuer.
func newServerWithTURN(t *testing.T, port int, issuer *turn.Issuer) *Server {
	t.Helper()
	s := newServer(t, port, nil, 0)
	s.ice.(*ICEHandler).TURN = issuer
	return s
}

// newServerWith is newServer, with a runtime monitor that is already running if withRuntime is set.
func newServerWith(t *testing.T, port int, page *fstest.MapFS, maxListeners int, withRuntime bool) *Server {
	t.Helper()
	counters := metrics.New()
	eng, err := engine.New(engine.Options{
		Playlist: &playlist.Playlist{
			Tracks: []playlist.Track{
				{
					Name:    "one.wav",
					Packets: make([][]byte, 150),
				},
				{
					Name:    "two.wav",
					Packets: make([][]byte, 50),
				},
			},
		},
		ICEPort:      port,
		MaxListeners: maxListeners,
		Metrics:      counters,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		_ = eng.Do(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		_ = eng.Close()
		<-doneCh
	})
	env := &config.Env{
		Logger:  slog.New(slog.NewTextHandler(io.Discard, nil)),
		Metrics: counters,
		Engine:  eng,
	}
	if withRuntime {
		monitor, err := metrics.NewRuntimeMonitor(counters, metrics.RuntimeOptions{Interval: 10 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		monitorCtx, stopMonitor := context.WithCancel(context.Background())
		monitorDoneCh := make(chan struct{})
		go func() {
			defer close(monitorDoneCh)
			_ = monitor.Do(monitorCtx)
		}()
		t.Cleanup(func() {
			stopMonitor()
			<-monitorDoneCh
		})
		eventually(t, "the runtime monitor to take a sample", func() bool {
			_, ok := monitor.Latest()
			return ok
		})
		env.Runtime = monitor
	}
	if page != nil {
		env.Page = page
	}
	s, err := NewServer("127.0.0.1:0", env.Logger, env, config.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// request sends one request to the routes of s and returns the answer.
func request(s *Server, method, path, body string) *fasthttp.Response {
	var req fasthttp.Request
	req.Header.SetMethod(method)
	req.SetRequestURI(path)
	req.SetBodyString(body)
	// Init gives the context the stand-in of a server that it needs to act as a context.Context.
	var ctx fasthttp.RequestCtx
	ctx.Init(&req, nil, nil)
	s.Routes().Handler()(&ctx)
	return &ctx.Response
}

// header returns the header key of res.
func header(res *fasthttp.Response, key string) string {
	return string(res.Header.Peek(key))
}

// envelopeData returns the Data of the answer in the envelope, and fails the test if the answer is not valid JSON or has no
// Data.
func envelopeData(t *testing.T, res *fasthttp.Response) gjson.Result {
	t.Helper()
	if !gjson.ValidBytes(res.Body()) {
		t.Fatalf("not valid JSON: %q", res.Body())
	}
	result := gjson.GetBytes(res.Body(), "data")
	if !result.Exists() {
		t.Fatalf("the answer has no data: %q", res.Body())
	}
	return result
}

func TestPlaylistAnswersWithTheNowPlayingTrackAndAllTracks(t *testing.T) {
	h := newServer(t, 15140, nil, 0)
	for _, path := range []string{"/api/playlist"} {
		rec := request(h, fasthttp.MethodGet, path, "")
		if rec.StatusCode() != fasthttp.StatusOK {
			t.Fatalf("%s: %d", path, rec.StatusCode())
		}
		got := envelopeData(t, rec)
		if got.Get("track").String() != "one.wav" || got.Get("tracks.#").Int() != 2 ||
			got.Get("tracks.0.name").String() != "one.wav" || got.Get("tracks.0.seconds").Int() != 3 ||
			got.Get("tracks.1.name").String() != "two.wav" || got.Get("tracks.1.seconds").Int() != 1 {
			t.Errorf("%s: got %s", path, got.Raw)
		}
	}
}

func TestMetricsAnswerUnderThePrefix(t *testing.T) {
	h := newServer(t, 15141, nil, 0)
	rec := request(h, fasthttp.MethodGet, "/api/metrics", "")
	if rec.StatusCode() != fasthttp.StatusOK || !strings.Contains(string(rec.Body()), `"listeners"`) {
		t.Fatalf("%d %q", rec.StatusCode(), string(rec.Body()))
	}
}

func TestThePageIsServedOnlyWhenOneIsGiven(t *testing.T) {
	without := newServer(t, 15142, nil, 0)
	if rec := request(without, fasthttp.MethodGet, "/", ""); rec.StatusCode() != fasthttp.StatusNotFound {
		t.Errorf("without a page, / is %d, want 404", rec.StatusCode())
	}

	with := newServer(t, 15143, &fstest.MapFS{"index.html": {Data: []byte("<h1>page</h1>")}}, 0)
	rec := request(with, fasthttp.MethodGet, "/", "")
	if rec.StatusCode() != fasthttp.StatusOK || !strings.Contains(string(rec.Body()), "<h1>page</h1>") {
		t.Errorf("with a page, / is %d %q", rec.StatusCode(), string(rec.Body()))
	}
}

func TestListenRefusesAnOfferThatIsNotSDP(t *testing.T) {
	h := newServer(t, 15144, nil, 0)
	rec := request(h, fasthttp.MethodPost, "/api/listen", "not an offer")
	if rec.StatusCode() != fasthttp.StatusBadRequest {
		t.Fatalf("%d %q, want 400", rec.StatusCode(), string(rec.Body()))
	}
}

func TestLeaveRefusesBadAndUnknownIds(t *testing.T) {
	h := newServer(t, 15145, nil, 0)
	if rec := request(h, fasthttp.MethodDelete, "/api/listen/abc", ""); rec.StatusCode() != fasthttp.StatusBadRequest {
		t.Errorf("a bad id is %d, want 400", rec.StatusCode())
	}
	if rec := request(h, fasthttp.MethodDelete, "/api/listen/999", ""); rec.StatusCode() != fasthttp.StatusNotFound {
		t.Errorf("an unknown id is %d, want 404", rec.StatusCode())
	}
}

// offer makes the SDP offer of a browser stand-in that only receives audio.
func offer(t *testing.T) string {
	t.Helper()
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		t.Fatal(err)
	}
	created, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(created); err != nil {
		t.Fatal(err)
	}
	<-gathered
	return pc.LocalDescription().SDP
}

func TestListenAnswersWithARelativeLocationThatLeaveAcceptsUnderThePrefix(t *testing.T) {
	h := newServer(t, 15146, nil, 0)
	rec := request(h, fasthttp.MethodPost, "/api/listen", offer(t))
	if rec.StatusCode() != fasthttp.StatusCreated {
		t.Fatalf("%d %q, want 201", rec.StatusCode(), string(rec.Body()))
	}
	if got := header(rec, "Content-Type"); got != "application/sdp" {
		t.Errorf("Content-Type = %q", got)
	}
	location := header(rec, "Location")
	if !regexp.MustCompile(`^listen/[0-9]+$`).MatchString(location) {
		t.Fatalf("Location = %q, want listen/<id>", location)
	}
	// A browser resolves a relative Location against the address it posted to, which here ends in /api/listen.
	if rec := request(h, fasthttp.MethodDelete, "/api/"+location, ""); rec.StatusCode() != fasthttp.StatusNoContent {
		t.Fatalf("DELETE answered %d, want 204", rec.StatusCode())
	}
	if rec := request(h, fasthttp.MethodDelete, "/api/"+location, ""); rec.StatusCode() != fasthttp.StatusNotFound {
		t.Fatalf("a second DELETE answered %d, want 404", rec.StatusCode())
	}
}

func TestListenRefusesListenersOverTheLimitWith503(t *testing.T) {
	h := newServer(t, 15147, nil, 1)
	if rec := request(h, fasthttp.MethodPost, "/api/listen", offer(t)); rec.StatusCode() != fasthttp.StatusCreated {
		t.Fatalf("the first listener got %d", rec.StatusCode())
	}
	if rec := request(h, fasthttp.MethodPost, "/api/listen", offer(t)); rec.StatusCode() != fasthttp.StatusServiceUnavailable {
		t.Fatalf("the second listener got %d, want 503", rec.StatusCode())
	}
}

func TestTracksAreMadeOnceAndEveryCallReturnsTheSameList(t *testing.T) {
	counters := metrics.New()
	eng, err := engine.New(engine.Options{
		Playlist: &playlist.Playlist{
			Tracks: []playlist.Track{{
				Name:    "one.wav",
				Packets: make([][]byte, 50),
			}},
		},
		ICEPort: 15148,
		Metrics: counters,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	first, second := eng.Tracks(), eng.Tracks()
	if len(first) != 1 || &first[0] != &second[0] {
		t.Fatal("Tracks made a new list for a second call")
	}
	if allocs := testing.AllocsPerRun(100, func() { _ = eng.Tracks() }); allocs != 0 {
		t.Fatalf("Tracks allocates %v times for each call, want none", allocs)
	}
}

func TestRuntimeAnswersWithTheSamplesAsCSVWhenThereIsAMonitor(t *testing.T) {
	h := newServerWith(t, 15160, nil, 0, true)
	for _, path := range []string{"/api/runtime"} {
		rec := request(h, fasthttp.MethodGet, path, "")
		if rec.StatusCode() != fasthttp.StatusOK || !strings.HasPrefix(header(rec, "Content-Type"), "text/csv") {
			t.Fatalf("%s: %d %q", path, rec.StatusCode(), header(rec, "Content-Type"))
		}
		lines := strings.Split(strings.TrimSpace(string(rec.Body())), "\n")
		if len(lines) < 2 || !strings.HasPrefix(lines[0], "time,goroutines,") {
			t.Fatalf("%s: %q", path, string(rec.Body()))
		}
	}
}

func TestRuntimeIsNotThereWithoutAMonitor(t *testing.T) {
	h := newServer(t, 15161, nil, 0)
	if rec := request(h, fasthttp.MethodGet, "/api/runtime", ""); rec.StatusCode() != fasthttp.StatusNotFound {
		t.Fatalf("got %d, want 404", rec.StatusCode())
	}
}

func TestMetricsCarryTheRuntimeHealth(t *testing.T) {
	h := newServerWith(t, 15162, nil, 0, true)
	rec := request(h, fasthttp.MethodGet, "/api/metrics", "")
	runtime := gjson.GetBytes(rec.Body(), "runtime")
	if runtime.Get("goroutines").Uint() == 0 || runtime.Get("procs").Uint() == 0 || runtime.Get("heap_bytes").Uint() == 0 {
		t.Fatalf("empty runtime health: %s", runtime.Raw)
	}
}

func TestICEHandsOutTurnCredentialsThatChangeEveryTime(t *testing.T) {
	secret := []byte("0123456789abcdef-secret")
	issuer, err := turn.New(turn.Options{
		URLs:   []string{"turn:turn.example.com:3478"},
		Secret: secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	h := newServerWithTURN(t, 15163, issuer)

	paths := []string{"/api/ice", "/api/ice", "/api/ice"}
	usernames := make([]string, 0, len(paths))
	for _, path := range paths {
		rec := request(h, fasthttp.MethodGet, path, "")
		if rec.StatusCode() != fasthttp.StatusOK || header(rec, "Cache-Control") != "no-store" {
			t.Fatalf("%s: %d, cache control %q", path, rec.StatusCode(), header(rec, "Cache-Control"))
		}
		if strings.Contains(string(rec.Body()), string(secret)) {
			t.Fatalf("%s: the secret is in the answer", path)
		}
		servers := envelopeData(t, rec).Get("iceServers")
		first := servers.Get("0")
		if servers.Get("#").Int() != 1 || first.Get("urls.0").String() != "turn:turn.example.com:3478" ||
			first.Get("username").String() == "" || first.Get("credential").String() == "" {
			t.Fatalf("%s: %s", path, servers.Raw)
		}
		usernames = append(usernames, first.Get("username").String())
	}
	if usernames[0] == usernames[1] || usernames[1] == usernames[2] {
		t.Fatalf("the same username was handed out twice: %v", usernames)
	}
}

func TestICEIsAnEmptyListWithoutATurnServer(t *testing.T) {
	h := newServer(t, 15164, nil, 0)
	rec := request(h, fasthttp.MethodGet, "/api/ice", "")
	if rec.StatusCode() != fasthttp.StatusOK {
		t.Fatalf("got %d", rec.StatusCode())
	}
	if got := strings.TrimSpace(string(rec.Body())); got != `{"data":{"iceServers":[]}}` {
		t.Fatalf("got %s, want an empty list and not null", got)
	}
}

func TestPlaylistTellsWhetherThereIsAPresenterWhetherItSpeaksAndWhatItSaidLast(t *testing.T) {
	type answer struct {
		Flags uint8
		Said  string
	}
	read := func(h *Server) answer {
		t.Helper()
		got := envelopeData(t, request(h, fasthttp.MethodGet, "/api/playlist", ""))
		return answer{
			Flags: uint8(got.Get("flags").Uint()),
			Said:  got.Get("said").String(),
		}
	}

	// A server without a presenter says so, and the page then shows no place for one.
	if got := read(newServer(t, 15165, nil, 0)); got.Flags != 0 || got.Said != "" {
		t.Fatalf("a server without a presenter: %+v", got)
	}

	h := newServer(t, 15166, nil, 0)
	h.playlist.(*PlaylistHandler).Flags |= FlagPresenter
	if got := read(h); got.Flags != PlaylistPresenter || got.Said != "" {
		t.Fatalf("a presenter that has not spoken: %+v", got)
	}
	packets := make([][]byte, 25)
	for i := range packets {
		packets[i] = []byte{0xf8, byte(i), 0, 0}
	}
	h.playlist.(*PlaylistHandler).Engine.Interludes().Offer(&stream.Clip{
		Before:  0,
		Packets: packets,
		Text:    "Here is one.wav.",
	})
	eventually(t, "the presenter to speak", func() bool { return read(h).Flags&PlaylistSpeaking != 0 })
	if got := read(h); got.Said != "Here is one.wav." {
		t.Fatalf("while speaking: %+v", got)
	}
	eventually(t, "the presenter to stop", func() bool { return read(h).Flags&PlaylistSpeaking == 0 })
	// The words stay after the voice stops, until the presenter says something else.
	if got := read(h); got.Flags != PlaylistPresenter || got.Said != "Here is one.wav." {
		t.Fatalf("after speaking: %+v", got)
	}
}

func TestListenRefusesABodyOverTheLimitBeforeAHandlerRuns(t *testing.T) {
	s := newServer(t, 15167, nil, 0)
	server := &fasthttp.Server{
		Handler:            s.Routes().Handler(),
		ErrorHandler:       edge.ErrorHandler,
		MaxRequestBodySize: MaxOfferSize,
		Logger:             quietFasthttpLogger{},
	}
	listener := fasthttputil.NewInmemoryListener()
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Shutdown() })
	conn, err := listener.Dial()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	body := strings.Repeat("v=0\n", MaxOfferSize)
	if _, err := io.WriteString(conn, "POST /api/listen HTTP/1.1\r\nHost: a\r\nContent-Length: "+itoa(len(body))+"\r\n\r\n"+body); err != nil {
		t.Fatal(err)
	}

	var res fasthttp.Response
	if err := res.Read(bufio.NewReader(conn)); err != nil {
		t.Fatal(err)
	}

	if res.StatusCode() != fasthttp.StatusRequestEntityTooLarge || !strings.Contains(string(res.Body()), "TOO_LARGE_BODY") {
		t.Fatalf("got %d %q, want 413 and the code", res.StatusCode(), res.Body())
	}
}

// quietFasthttpLogger is a fasthttp logger that writes nothing.
type quietFasthttpLogger struct{}

func (quietFasthttpLogger) Printf(string, ...any) {}

func itoa(n int) string {
	return strconv.Itoa(n)
}

// eventually polls ok until it returns true and fails the test if that takes more than five seconds.
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

func TestNewServerRefusesAnEnvironmentThatLacksAPart(t *testing.T) {
	_, err := NewServer("127.0.0.1:0", slog.New(slog.NewTextHandler(io.Discard, nil)), &config.Env{}, config.Options{})
	if !errors.Is(err, config.ErrMissingFromEnv) {
		t.Fatalf("got %v, want ErrMissingFromEnv", err)
	}
}

func TestStartupFailsWhenTheHTTPPortIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = taken.Close() })
	s := newServer(t, 15168, nil, 0)
	s.addr = taken.Addr().String()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Startup(ctx); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want the listen error", err)
	}
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
