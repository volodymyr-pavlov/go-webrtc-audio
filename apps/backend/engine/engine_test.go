package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/metrics"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/playlist"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/stream"
)

func fakePlaylist() *playlist.Playlist {
	packets := make([][]byte, 50)
	for i := range packets {
		packets[i] = []byte{0xf8, byte(i), 0, 0}
	}
	return &playlist.Playlist{
		Tracks: []playlist.Track{
			{
				Name:    "fake.opus",
				Packets: packets,
			},
		},
	}
}

func startEngine(t *testing.T, mode Mode, port int) (*Engine, *metrics.Metrics) {
	t.Helper()
	return startEngineWith(t, Options{
		Mode:    mode,
		ICEPort: port,
	})
}

func startEngineWith(t *testing.T, opts Options) (*Engine, *metrics.Metrics) {
	t.Helper()
	counters := metrics.New()
	opts.Playlist = fakePlaylist()
	opts.Flags |= FlagLoopback
	opts.Metrics = counters
	eng, err := New(opts)
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
	return eng, counters
}

// connectClient connects a browser stand-in and returns the number of packets it has read so far.
func connectClient(t *testing.T, eng *Engine) (*webrtc.PeerConnection, uint64, func() int) {
	t.Helper()
	client, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	countCh := make(chan struct{}, 1000)
	client.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		for {
			if _, _, err := track.ReadRTP(); err != nil {
				return
			}
			select {
			case countCh <- struct{}{}:
			default:
			}
		}
	})
	if _, err := client.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		t.Fatal(err)
	}
	offer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(client)
	if err := client.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	answer, id, err := eng.Listen(context.Background(), client.LocalDescription().SDP)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answer,
	}); err != nil {
		t.Fatal(err)
	}
	return client, id, func() int { return len(countCh) }
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEveryModeStreamsToAListenerAndCleansUp(t *testing.T) {
	for i, mode := range []Mode{ModeNaive, ModeTrack, ModeFanout} {
		t.Run(string(mode), func(t *testing.T) {
			eng, counters := startEngine(t, mode, 15100+i)
			_, id, packets := connectClient(t, eng)
			waitFor(t, "30 packets", func() bool { return packets() >= 30 })
			if got := counters.Listeners.Load(); got != 1 {
				t.Fatalf("Listeners = %d, want 1", got)
			}
			if err := eng.Leave(id); err != nil {
				t.Fatal(err)
			}
			waitFor(t, "the listener to be gone", func() bool { return counters.Listeners.Load() == 0 })
			if err := eng.Leave(id); !errors.Is(err, ErrNotFound) {
				t.Fatalf("second Leave returned %v, want ErrNotFound", err)
			}
		})
	}
}

func TestListenerThatClosesIsRemoved(t *testing.T) {
	eng, counters := startEngine(t, ModeFanout, 15110)
	client, _, packets := connectClient(t, eng)
	waitFor(t, "packets", func() bool { return packets() >= 5 })
	_ = client.Close()
	waitFor(t, "the server to notice", func() bool { return counters.Listeners.Load() == 0 })
}

func TestNewRejectsAnUnknownMode(t *testing.T) {
	if _, err := New(Options{
		Mode:     128,
		Playlist: fakePlaylist(),
		ICEPort:  15120,
	}); !errors.Is(err, ErrBadMode) {
		t.Fatalf("got %v, want ErrBadMode", err)
	}
}

func TestListenRejectsGarbage(t *testing.T) {
	eng, _ := startEngine(t, ModeFanout, 15121)
	if _, _, err := eng.Listen(context.Background(), "not an sdp"); !errors.Is(err, ErrBadOffer) {
		t.Fatalf("got %v, want ErrBadOffer", err)
	}
}

func TestListenersHearTheStreamThroughBatchedSockets(t *testing.T) {
	eng, counters := startEngineWith(t, Options{
		Mode:      ModeFanout,
		ICEPort:   15130,
		BatchSize: 32,
	})
	_, id, packets := connectClient(t, eng)
	waitFor(t, "30 packets", func() bool { return packets() >= 30 })
	if counters.UDPPackets.Load() == 0 {
		t.Fatal("no packet went through the batching socket")
	}
	if err := eng.Leave(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the listener to be gone", func() bool { return counters.Listeners.Load() == 0 })
}

func TestAListenerThatLeavesBeforeItJoinsIsNeverAdded(t *testing.T) {
	eng, counters := startEngine(t, ModeFanout, 15150)
	pc, err := eng.api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	s := &session{pc: pc}
	if err := eng.register(s); err != nil {
		t.Fatal(err)
	}
	track, err := webrtc.NewTrackLocalStaticRTP(opusCapability, "audio", "radio")
	if err != nil {
		t.Fatal(err)
	}

	if err := eng.Leave(s.id); err != nil {
		t.Fatal(err)
	}
	// The "connected" event of a connection that has just been closed arrives late.
	eng.join(s, track)

	if got := counters.Listeners.Load(); got != 0 {
		t.Errorf("Listeners = %d, want 0", got)
	}
	if got := counters.Joined.Load(); got != 0 {
		t.Errorf("Joined = %d, want 0", got)
	}
	if got := eng.router.Len(); got != 0 {
		t.Errorf("the router holds %d listeners, want 0", got)
	}
}

func TestListenersOverTheLimitAreRefused(t *testing.T) {
	eng, _ := startEngineWith(t, Options{
		Mode:         ModeFanout,
		ICEPort:      15151,
		MaxListeners: 1,
	})
	_, id, _ := connectClient(t, eng)

	if _, _, err := eng.Listen(context.Background(), "not an sdp"); !errors.Is(err, ErrFull) {
		t.Fatalf("got %v, want ErrFull", err)
	}
	if err := eng.Leave(id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := eng.Listen(context.Background(), "not an sdp"); !errors.Is(err, ErrBadOffer) {
		t.Fatalf("after a listener left, got %v, want ErrBadOffer", err)
	}
}

func TestListenerIdsCannotBeGuessed(t *testing.T) {
	eng, _ := startEngine(t, ModeFanout, 15152)
	_, first, _ := connectClient(t, eng)
	_, second, _ := connectClient(t, eng)
	if first == second || second == first+1 || first < 1<<20 || second < 1<<20 {
		t.Fatalf("ids %d and %d look sequential or small", first, second)
	}
}

func TestParseModeKnowsTheNamesOfTheModes(t *testing.T) {
	for _, mode := range []Mode{ModeNaive, ModeTrack, ModeFanout} {
		got, err := ParseMode(mode.String())
		if err != nil || got != mode {
			t.Errorf("ParseMode(%q) = %v, %v", mode.String(), got, err)
		}
	}
	if _, err := ParseMode("loud"); !errors.Is(err, ErrBadMode) {
		t.Errorf("got %v, want ErrBadMode", err)
	}
}

func TestRouterQueuesAreOffOverTheBatchingSocketUnlessAsked(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		want int
	}{
		{"plain sends keep the default queue", Options{}, 0},
		{"batching has no queue", Options{BatchSize: 64}, -1},
		{"batching with a queue asked for keeps it", Options{
			BatchSize: 64,
			QueueLen:  8,
		}, 8},
		{"plain sends with no queue asked for", Options{QueueLen: -1}, -1},
	}
	for _, tt := range tests {
		if got := routerQueueLen(tt.opts); got != tt.want {
			t.Errorf("%s: got %d, want %d", tt.name, got, tt.want)
		}
	}
}

// offerOf returns the offer of a browser stand-in that will never finish connecting.
func offerOf(t *testing.T) string {
	t.Helper()
	client, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.AddTransceiverFromKind(webrtc.RTPCodecTypeAudio, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	}); err != nil {
		t.Fatal(err)
	}
	offer, err := client.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(client)
	if err := client.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	<-gathered
	return client.LocalDescription().SDP
}

func TestNewRejectsAPlaylistWithNothingToPlay(t *testing.T) {
	for name, list := range map[string]*playlist.Playlist{
		"nil":       nil,
		"no tracks": {},
		"a track of nothing": {Tracks: []playlist.Track{
			{
				Name:    "a.wav",
				Packets: [][]byte{{1}},
			},
			{Name: "b.wav"},
		}},
	} {
		_, err := New(Options{
			Mode:     ModeFanout,
			ICEPort:  15152,
			Playlist: list,
		})
		if !errors.Is(err, ErrNoPlaylist) {
			t.Errorf("%s: got %v, want ErrNoPlaylist", name, err)
		}
	}
}

func TestListenersThatNeverConnectCannotTakeEveryPlace(t *testing.T) {
	eng, counters := startEngineWith(t, Options{
		Mode:          ModeFanout,
		ICEPort:       15153,
		MaxConnecting: 2,
	})
	first, _, err := eng.Listen(context.Background(), offerOf(t))
	if err != nil || first == "" {
		t.Fatalf("first offer: %v", err)
	}
	_, secondID, err := eng.Listen(context.Background(), offerOf(t))
	if err != nil {
		t.Fatalf("second offer: %v", err)
	}
	if _, _, err := eng.Listen(context.Background(), offerOf(t)); !errors.Is(err, ErrBusy) {
		t.Fatalf("third offer while two are connecting: got %v, want ErrBusy", err)
	}
	if err := eng.Leave(secondID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := eng.Listen(context.Background(), offerOf(t)); err != nil {
		t.Fatalf("after one gave up: %v", err)
	}
	if counters.Listeners.Load() != 0 {
		t.Fatalf("%d listeners joined, none connected", counters.Listeners.Load())
	}
}

func TestListenersThatConnectLeaveTheConnectingCount(t *testing.T) {
	eng, counters := startEngineWith(t, Options{
		Mode:          ModeFanout,
		ICEPort:       15154,
		MaxConnecting: 1,
	})
	for i := 0; i < 3; i++ {
		_, _, _ = connectClient(t, eng)
		want := int64(i + 1)
		waitFor(t, "the listener to join", func() bool { return counters.Listeners.Load() == want })
	}
	if got := eng.pending.Load(); got != 0 {
		t.Fatalf("%d listeners count as connecting, want 0", got)
	}
}

func TestFailedOffersDoNotLeakTheConnectingCount(t *testing.T) {
	eng, _ := startEngineWith(t, Options{
		Mode:          ModeFanout,
		ICEPort:       15155,
		MaxConnecting: 1,
	})
	for i := 0; i < 5; i++ {
		if _, _, err := eng.Listen(context.Background(), "not an sdp"); !errors.Is(err, ErrBadOffer) {
			t.Fatalf("offer %d: got %v, want ErrBadOffer", i, err)
		}
	}
	if got := eng.pending.Load(); got != 0 {
		t.Fatalf("%d listeners count as connecting after failed offers, want 0", got)
	}
}

// fingerprint returns the SHA-256 fingerprint of a certificate.
func fingerprint(t *testing.T, c webrtc.Certificate) string {
	t.Helper()
	prints, err := c.GetFingerprints()
	if err != nil || len(prints) == 0 {
		t.Fatalf("fingerprints: %v", err)
	}
	return prints[0].Value
}

func TestConnectionsShareACertificateThatIsRenewedDaily(t *testing.T) {
	eng, _ := startEngineWith(t, Options{
		Mode:    ModeFanout,
		ICEPort: 15156,
	})
	first, err := eng.certificate()
	if err != nil {
		t.Fatal(err)
	}
	again, err := eng.certificate()
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint(t, first) != fingerprint(t, again) {
		t.Fatal("a second connection got another certificate")
	}
	eng.cert.Store(&certificate{
		cert: first,
		made: time.Now().Add(-2 * certificateLife),
	})
	renewed, err := eng.certificate()
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint(t, first) == fingerprint(t, renewed) {
		t.Fatal("the certificate was not renewed after a day")
	}
}

func TestSendQueueAndAddressTableGrowWithTheListenerLimit(t *testing.T) {
	tests := []struct {
		limit, floor, want int
	}{
		{5000, minQueued, 10000},
		{100, minQueued, minQueued},
		{20000, minAddrs, 40000},
		{-1, minQueued, 0},
		{0, minQueued, 0},
	}
	for _, tt := range tests {
		if got := scaled(tt.limit, tt.floor); got != tt.want {
			t.Errorf("scaled(%d, %d) = %d, want %d", tt.limit, tt.floor, got, tt.want)
		}
	}
}

func TestAClipOfThePresenterIsPlayedInFrontOfItsTrackAndShownWhileItPlays(t *testing.T) {
	for _, mode := range []Mode{ModeFanout, ModeTrack} {
		eng, counters := startEngine(t, mode, 15170+int(mode))
		if eng.Interludes() == nil {
			t.Fatalf("%v: no slot for clips", mode)
		}
		if eng.Speaking() || eng.Said() != "" {
			t.Fatalf("%v: the presenter speaks before it was asked to", mode)
		}
		packets := make([][]byte, 25)
		for i := range packets {
			packets[i] = []byte{0xf8, byte(i), 0, 0}
		}
		eng.Interludes().Offer(&stream.Clip{
			Before:  0,
			Packets: packets,
			Text:    "Welcome back.",
		})

		// The only track has 50 packets, a second: its start comes round every second, and the clip plays at the next.
		waitFor(t, "the clip to start", func() bool { return eng.Speaking() && eng.Said() == "Welcome back." })
		if eng.TrackIndex() != 0 {
			t.Fatalf("%v: track index %d, want the index of the track the clip introduces", mode, eng.TrackIndex())
		}
		waitFor(t, "the clip to end", func() bool { return !eng.Speaking() })
		// What was said stays until the presenter says something else.
		if eng.Said() != "Welcome back." {
			t.Fatalf("%v: said %q after the clip, want the words to stay", mode, eng.Said())
		}
		if got := counters.Presenter.Played.Load(); got != 1 {
			t.Fatalf("%v: %d clips played, want 1", mode, got)
		}
		if eng.Interludes().Waiting() != nil {
			t.Fatalf("%v: the clip is still waiting after it played", mode)
		}
	}
}

func TestThereIsNoSlotForClipsWhereEveryListenerHasItsOwnPlayout(t *testing.T) {
	eng, _ := startEngine(t, ModeNaive, 15179)
	if eng.Interludes() != nil || eng.Speaking() || eng.Said() != "" {
		t.Fatal("naive mode has a shared clip slot")
	}
}

func TestAListenerThatNeverConnectsIsDroppedAfterTheConnectTimeout(t *testing.T) {
	eng, _ := startEngineWith(t, Options{
		Mode:           ModeFanout,
		ICEPort:        15157,
		ConnectTimeout: 200 * time.Millisecond,
	})
	_, id, err := eng.Listen(context.Background(), offerOf(t))
	if err != nil {
		t.Fatal(err)
	}
	if eng.pending.Load() != 1 {
		t.Fatalf("%d listeners count as connecting, want 1", eng.pending.Load())
	}

	// No answer is ever set, so the connection never comes up.
	waitFor(t, "the listener to be dropped", func() bool { return errors.Is(eng.Leave(id), ErrNotFound) })
	if got := eng.pending.Load(); got != 0 {
		t.Fatalf("%d listeners count as connecting after the timeout, want 0", got)
	}
}

func TestTheEngineCountsTheTracksThatBegin(t *testing.T) {
	eng, _ := startEngineWith(t, Options{
		Mode:    ModeFanout,
		ICEPort: 15158,
	})
	waitFor(t, "the first track to begin", func() bool { return eng.TrackStarts() >= 1 })
}
