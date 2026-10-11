package engine

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
	"github.com/valyala/bytebufferpool"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/edge"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/fanout"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/metrics"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/playlist"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/stream"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/transport"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/worker"
)

var _ worker.Worker = &Engine{}

const (
	defaultConnectTimeout = 15 * time.Second
	defaultEvictAfter     = 5 * time.Second
	disconnectedAfter     = 5 * time.Second
	failedAfter           = 5 * time.Second
	keepAliveEvery        = 2 * time.Second
	defaultMaxListeners   = 5000
	defaultMaxConnecting  = 1000

	// certificateLife is how long the engine uses one DTLS certificate. Pion's certificates are valid for a month, and
	// a browser refuses one that has expired.
	certificateLife = 24 * time.Hour

	// minQueued and minAddrs are the least sizes of the send queue and of the table of destination addresses of the
	// batching socket. Both grow with [Options.MaxListeners]: one tick puts a packet for every listener in the queue.
	minQueued = 8192
	minAddrs  = 16384

	// sendBufferBytes is the size asked for the send buffer of the batching socket. The kernel may grant less.
	sendBufferBytes = 4 << 20
)

var opusCapability = webrtc.RTPCodecCapability{
	MimeType:    webrtc.MimeTypeOpus,
	ClockRate:   48000,
	Channels:    2,
	SDPFmtpLine: "minptime=10;useinbandfec=1",
}

// Flags of the state of an [Engine]. They are only ever set, never cleared.
const (
	engineClosed uint32 = 1 << iota
)

// Flags of the state of a session. They are only ever set, never cleared.
const (
	// sessionJoining is set by the one call of join that goes ahead, before it adds the listener to anything.
	sessionJoining uint32 = 1 << iota

	// sessionJoined is set by join when the listener is added, and counted in the metrics.
	sessionJoined

	// sessionLeft is set by Leave.
	sessionLeft

	// sessionSettled is set by the first of join and Leave, which then take the session out of the count of listeners
	// that are still connecting.
	sessionSettled
)

// An Engine answers listeners and streams the playlist to them. It is safe for concurrent use, and it holds no lock:
// its state is atomic.
type Engine struct {
	opts    Options
	logger  *slog.Logger
	metrics *metrics.Metrics
	api     *webrtc.API
	mux     ice.UDPMux
	router  *fanout.Router
	shared  *webrtc.TrackLocalStaticRTP
	playout *stream.Playout
	tracks  []TrackInfo
	track   atomic.Int32
	starts  atomic.Uint64
	// rtcpBufPool holds the buffers that [Engine.drain] reads the RTCP of the listeners into.
	rtcpBufPool bytebufferpool.Pool

	// interludes is the slot for the clip that plays in front of the next track of the shared stream; it is nil in
	// [ModeNaive], where every listener has a playout of its own. saying is the clip that plays now, or nil.
	interludes *stream.Interludes
	saying     atomic.Pointer[stream.Clip]

	// lastClip is the clip that was played last, and stays after it ends, so that the page can show what the presenter
	// said until it says something else.
	lastClip atomic.Pointer[stream.Clip]

	// state holds the flags above.
	state atomic.Uint32

	// sessions maps the id of each listener to its *session, and count is how many there are.
	sessions sync.Map
	count    atomic.Int64

	// pending is how many sessions have not joined or left yet.
	pending atomic.Int64

	// cert is the DTLS certificate of every new connection, made again once a day.
	cert atomic.Pointer[certificate]
}

// A certificate is a DTLS certificate and the time it was made.
type certificate struct {
	cert webrtc.Certificate
	made time.Time
}

// A session is one listener. Its id is unguessable, because whoever knows it can end the listener.
type session struct {
	id uint64
	pc *webrtc.PeerConnection

	// state holds the flags above. Joining and leaving can happen at once, on different goroutines, and the flags decide
	// which of them does the clearing up: see [Engine.join] and [Engine.Leave].
	state atomic.Uint32

	// stop ends the listener's own playout in [ModeNaive]. It is set before sessionJoined is.
	stop atomic.Pointer[context.CancelFunc]
}

// New returns the [Engine] that opts describes and opens its UDP port. It returns [ErrBadMode] for an unknown mode and
// [ErrNoPlaylist] for a playlist that has nothing to play.
func New(opts Options) (*Engine, error) {
	opts, err := withDefaults(opts)
	if err != nil {
		return nil, err
	}
	e := &Engine{
		opts:    opts,
		logger:  opts.Logger,
		metrics: opts.Metrics,
		tracks:  tracksOf(opts.Playlist),
	}
	if e.logger == nil {
		e.logger = slog.Default()
	}
	if e.metrics == nil {
		e.metrics = metrics.New()
	}
	mux, err := e.listen()
	if err != nil {
		return nil, err
	}
	e.mux = mux
	if err := e.build(); err != nil {
		_ = mux.Close() // The error of building is the one to report.
		return nil, err
	}
	return e, nil
}

// withDefaults returns opts with the defaults filled in, or the error for a mode or playlist that cannot be used.
func withDefaults(opts Options) (Options, error) {
	if opts.Mode == 0 {
		opts.Mode = ModeFanout
	}
	if !opts.Mode.Valid() {
		return opts, fmt.Errorf("%w %d", ErrBadMode, opts.Mode)
	}
	if err := checkPlaylist(opts.Playlist); err != nil {
		return opts, err
	}
	if opts.ConnectTimeout <= 0 {
		opts.ConnectTimeout = defaultConnectTimeout
	}
	if opts.EvictAfter <= 0 {
		opts.EvictAfter = defaultEvictAfter
	}
	if opts.MaxListeners == 0 {
		opts.MaxListeners = defaultMaxListeners
	}
	if opts.MaxConnecting == 0 {
		opts.MaxConnecting = defaultMaxConnecting
	}
	return opts, nil
}

// build makes the Pion API on the mux and the parts that the mode needs.
func (e *Engine) build() error {
	settings, err := e.settings()
	if err != nil {
		return err
	}
	media := &webrtc.MediaEngine{}
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: opusCapability,
		PayloadType:        stream.PayloadType,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return fmt.Errorf("engine: register opus: %w", err)
	}
	// NewAPI registers Pion's default interceptors (resends, sender and receiver reports, statistics) unless it is given a
	// registry. An empty one keeps them out: a one-way Opus stream has no use for them, and each ran for every packet of
	// every listener.
	e.api = webrtc.NewAPI(
		webrtc.WithMediaEngine(media),
		webrtc.WithSettingEngine(settings),
		webrtc.WithInterceptorRegistry(&interceptor.Registry{}),
	)
	return e.buildMode()
}

// settings returns the Pion settings for the mux and the options of the engine.
func (e *Engine) settings() (webrtc.SettingEngine, error) {
	settings := webrtc.SettingEngine{}
	settings.LoggerFactory = e.pionLogs()
	settings.SetICEUDPMux(e.mux)
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	settings.SetICETimeouts(disconnectedAfter, failedAfter, keepAliveEvery)
	settings.SetIncludeLoopbackCandidate(e.opts.Flags&FlagLoopback != 0)
	settings.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	if len(e.opts.PublicIPs) > 0 {
		err := settings.SetICEAddressRewriteRules(webrtc.ICEAddressRewriteRule{
			External:        e.opts.PublicIPs,
			AsCandidateType: webrtc.ICECandidateTypeHost,
			Mode:            webrtc.ICEAddressRewriteReplace,
		})
		if err != nil {
			return settings, fmt.Errorf("engine: set the public addresses: %w", err)
		}
	}
	return settings, nil
}

// buildMode makes the shared track, router and playout that the mode needs.
func (e *Engine) buildMode() error {
	var err error
	switch e.opts.Mode {
	case ModeNaive:
	case ModeTrack:
		e.shared, err = webrtc.NewTrackLocalStaticRTP(opusCapability, "audio", "radio")
		if err != nil {
			return err
		}
		e.interludes = &stream.Interludes{}
		e.playout, err = e.newSharedPlayout(e.shared, func(track int) {
			e.track.Store(int32(track))
			e.metrics.Packets.Add(1)
			e.metrics.Delivered.Add(uint64(max(e.metrics.Listeners.Load(), 0)))
		})
	case ModeFanout:
		e.router = fanout.NewRouter(fanout.RouterOptions{
			Shards:     e.opts.Shards,
			QueueLen:   routerQueueLen(e.opts),
			MaxDropped: int(e.opts.EvictAfter / e.opts.Playlist.Interval()),
			OnEvict:    e.abandon,
			Metrics:    e.metrics,
			Logger:     e.logger,
		})
		e.interludes = &stream.Interludes{}
		e.playout, err = e.newSharedPlayout(e.router, func(track int) { e.track.Store(int32(track)) })
	}
	return err
}

// listen opens the UDP port of every connection. Without batching, Pion's multiplexer opens one socket for each address
// of the machine and shares them among the listeners. With batching it is one [transport.BatchConn] on all addresses: a
// socket for each address would offer one candidate for each, and when the server sits behind a NAT and gives them all
// the same public address Pion keeps one candidate and drops the rest, so the checks that arrive on a dropped
// candidate's socket would never be read.
func (e *Engine) listen() (ice.UDPMux, error) {
	if e.opts.BatchSize <= 0 {
		muxOptions := []ice.UDPMuxFromPortOption{ice.UDPMuxFromPortWithNetworks(ice.NetworkTypeUDP4)}
		if e.opts.Flags&FlagLoopback != 0 {
			muxOptions = append(muxOptions, ice.UDPMuxFromPortWithLoopback())
		}
		mux, err := ice.NewMultiUDPMuxFromPort(e.opts.ICEPort, muxOptions...)
		if err != nil {
			return nil, fmt.Errorf("engine: listen on udp port %d: %w", e.opts.ICEPort, err)
		}
		return mux, nil
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: e.opts.ICEPort})
	if err != nil {
		return nil, fmt.Errorf("engine: listen on udp port %d: %w", e.opts.ICEPort, err)
	}
	_ = conn.SetWriteBuffer(sendBufferBytes) // The kernel may cap it, and a smaller buffer still works.
	batch := transport.NewBatchConn(conn, transport.BatchOptions{
		Size:      e.opts.BatchSize,
		Delay:     e.opts.BatchDelay,
		MaxQueued: scaled(e.opts.MaxListeners, minQueued),
		MaxAddrs:  scaled(e.opts.MaxListeners, minAddrs),
		Metrics:   e.metrics,
		Logger:    e.logger,
	})
	return ice.NewUDPMuxDefault(ice.UDPMuxParams{
		Logger:  e.pionLogs().NewLogger("ice"),
		UDPConn: batch,
	}), nil
}

// newPlayout returns a playout that writes to w and calls onPacket after each packet. An error of a write is counted.
func (e *Engine) newPlayout(w io.Writer, onPacket func(track int)) (*stream.Playout, error) {
	return stream.NewPlayout(e.playoutOptions(w, onPacket))
}

// newSharedPlayout returns a playout like [Engine.newPlayout] for the shared stream. Only this one plays the clips of
// the presenter.
func (e *Engine) newSharedPlayout(w io.Writer, onPacket func(track int)) (*stream.Playout, error) {
	opts := e.playoutOptions(w, onPacket)
	opts.Interludes = e.interludes
	opts.OnTrackStart = func(int) { e.starts.Add(1) }
	opts.OnClip = func(clip *stream.Clip, event stream.ClipEvent) {
		if event&stream.ClipStarted != 0 {
			e.saying.Store(clip)
			e.lastClip.Store(clip)
			e.metrics.Presenter.Played.Add(1)
			return
		}
		e.saying.Store(nil)
	}
	return stream.NewPlayout(opts)
}

// playoutOptions returns the options that every playout of the engine has.
func (e *Engine) playoutOptions(w io.Writer, onPacket func(track int)) stream.PlayoutOptions {
	return stream.PlayoutOptions{
		Playlist: e.opts.Playlist,
		Sink:     w,
		OnPacket: onPacket,
		OnError:  func(error) { e.metrics.WriteErrors.Add(1) },
		Logger:   e.logger,
	}
}

// Interludes returns the slot in which a clip waits for the next track of the shared stream to start, so that a
// presenter can introduce it. It returns nil in [ModeNaive], where there is no shared stream.
func (e *Engine) Interludes() *stream.Interludes {
	return e.interludes
}

// TrackStarts returns how many tracks of the shared stream have begun since the engine started, counting a track again
// each time the playlist comes round to it. A presenter watches it to see a track begin, which the index alone cannot
// show when the playlist has one track.
func (e *Engine) TrackStarts() uint64 {
	return e.starts.Load()
}

// TrackIndex returns the index in the playlist of the track that plays now in the shared stream. While a clip plays it
// is the index of the track that the clip introduces.
func (e *Engine) TrackIndex() int {
	return int(e.track.Load())
}

// Speaking reports whether a clip of the presenter is playing now.
func (e *Engine) Speaking() bool {
	return e.saying.Load() != nil
}

// Said returns the words of the clip that the presenter played last, which stay until the next clip starts, and an
// empty string if it has not spoken yet.
func (e *Engine) Said() string {
	if clip := e.lastClip.Load(); clip != nil {
		return clip.Text
	}
	return ""
}

// Track returns the name of the track that plays now in the shared stream. In [ModeNaive] every listener has its own
// position, and Track returns the name of the first track.
func (e *Engine) Track() string {
	return e.opts.Playlist.Tracks[e.track.Load()].Name
}

// A TrackInfo describes one track of the playlist.
type TrackInfo struct {
	// Name is the file name of the track.
	Name string

	// Duration is the length of the track.
	Duration time.Duration
}

// Tracks returns the tracks of the playlist, in the order they play. The playlist never changes, so the list is made
// once, by [New], and every call returns that same list, which the caller must not modify.
func (e *Engine) Tracks() []TrackInfo {
	return e.tracks
}

// checkPlaylist returns [ErrNoPlaylist] if there is nothing to play in list.
func checkPlaylist(list *playlist.Playlist) error {
	if err := list.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrNoPlaylist, err)
	}
	return nil
}

// tracksOf describes the tracks of a playlist.
func tracksOf(list *playlist.Playlist) []TrackInfo {
	if list == nil {
		return nil
	}
	tracks := make([]TrackInfo, len(list.Tracks))
	for i := range list.Tracks {
		tracks[i] = TrackInfo{
			Name:     list.Tracks[i].Name,
			Duration: list.TrackDuration(i),
		}
	}
	return tracks
}

// scaled returns twice limit, or floor if that is less, or zero, which means the default of the receiver, when there is
// no limit.
func scaled(limit, floor int) int {
	if limit <= 0 {
		return 0
	}
	return max(floor, 2*limit)
}

// routerQueueLen returns the queue length to give the router: the one in opts, except that the default is no queue over
// the batching socket.
func routerQueueLen(opts Options) int {
	if opts.QueueLen == 0 && opts.BatchSize > 0 {
		return -1
	}
	return opts.QueueLen
}

// Do runs the shared playout and router, if the mode has them, until ctx is cancelled, and then returns nil.
func (e *Engine) Do(ctx context.Context) error {
	var pool worker.Pool
	if e.playout != nil {
		pool.Add(e.playout)
	}
	if e.router != nil {
		pool.Add(e.router)
	}
	if len(pool.Workers) == 0 {
		<-ctx.Done()
		return nil
	}
	return pool.Do(ctx)
}

// Listen answers the offer of a listener and returns the answer and the id of the new listener. It waits for ctx only
// while it gathers the connection candidates.
//
// The listener starts to hear the stream when its connection is up, and is removed when the connection fails or
// closes, or after [Options.ConnectTimeout] if it never comes up. It returns [ErrBadOffer] if the offer is not valid,
// [ErrFull] if the engine already holds [Options.MaxListeners] listeners, [ErrBusy] if [Options.MaxConnecting] of them
// have not connected yet, and [ErrClosed] after [Engine.Close].
func (e *Engine) Listen(ctx context.Context, offer string) (string, uint64, error) {
	if err := e.admit(); err != nil {
		return "", 0, err
	}
	cert, err := e.certificate()
	if err != nil {
		return "", 0, err
	}
	pc, err := e.api.NewPeerConnection(webrtc.Configuration{
		Certificates: []webrtc.Certificate{cert},
	})
	if err != nil {
		return "", 0, fmt.Errorf("engine: new peer connection: %w", err)
	}
	s := &session{
		pc: pc,
	}
	if err := e.register(s); err != nil {
		_ = pc.Close() // The registration error is the one to report.
		return "", 0, err
	}
	fail := func(err error) (string, uint64, error) {
		e.abandon(s.id)
		return "", 0, err
	}

	local := e.shared
	if local == nil {
		var err error
		local, err = webrtc.NewTrackLocalStaticRTP(opusCapability, "audio", "radio")
		if err != nil {
			return fail(err)
		}
	}
	timeout, err := e.attach(s, local)
	if err != nil {
		return fail(err)
	}

	answer, err := negotiate(ctx, pc, offer)
	if err != nil {
		timeout.Stop()
		return fail(err)
	}
	return answer, s.id, nil
}

// attach adds local to the connection of s, starts to drain its RTCP, and sets the timer and the callback that join or
// abandon the session. It returns the timer, which the caller stops if the negotiation fails.
func (e *Engine) attach(s *session, local *webrtc.TrackLocalStaticRTP) (*time.Timer, error) {
	sender, err := s.pc.AddTrack(local)
	if err != nil {
		return nil, fmt.Errorf("engine: add track: %w", err)
	}
	go e.drain(sender)

	timeout := time.AfterFunc(e.opts.ConnectTimeout, func() {
		if s.state.Load()&(sessionJoining|sessionJoined) == 0 {
			e.abandon(s.id)
		}
	})
	s.pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateConnected:
			timeout.Stop()
			e.join(s, local)
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			timeout.Stop()
			e.abandon(s.id)
		}
	})
	return timeout, nil
}

// certificate returns the DTLS certificate for a new connection. Pion makes a new key and certificate for every
// connection that is not given one, which costs about as much as the rest of the setup together and would make offers
// that never connect cheap to send. The key only proves who the server is, the keys of the media are made again for
// every connection, so one certificate can serve them all, and it is replaced once a day.
func (e *Engine) certificate() (webrtc.Certificate, error) {
	if c := e.cert.Load(); c != nil && time.Since(c.made) < certificateLife {
		return c.cert, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return webrtc.Certificate{}, fmt.Errorf("engine: make a key: %w", err)
	}
	cert, err := webrtc.GenerateCertificate(key)
	if err != nil {
		return webrtc.Certificate{}, fmt.Errorf("engine: make a certificate: %w", err)
	}
	e.cert.Store(&certificate{
		cert: *cert,
		made: time.Now(),
	})
	return *cert, nil
}

// admit refuses a listener before any work is done for it, when the engine is closed or full.
func (e *Engine) admit() error {
	switch {
	case e.state.Load()&engineClosed != 0:
		return ErrClosed
	case e.opts.MaxListeners > 0 && e.count.Load() >= int64(e.opts.MaxListeners):
		return ErrFull
	case e.opts.MaxConnecting > 0 && e.pending.Load() >= int64(e.opts.MaxConnecting):
		return ErrBusy
	}
	return nil
}

// register reserves a place for the session, which is checked again here because the engine may have filled up while
// the peer connection was being made, and gives it a new unguessable id.
func (e *Engine) register(s *session) error {
	if err := reserve(&e.count, e.opts.MaxListeners, ErrFull); err != nil {
		return err
	}
	if err := reserve(&e.pending, e.opts.MaxConnecting, ErrBusy); err != nil {
		e.count.Add(-1)
		return err
	}
	for {
		id, err := newID()
		if err != nil {
			e.count.Add(-1)
			e.pending.Add(-1)
			return err
		}
		if _, taken := e.sessions.LoadOrStore(id, s); !taken {
			s.id = id
			break
		}
	}
	// A Close that ran while the session was being added may have missed it.
	if e.state.Load()&engineClosed != 0 {
		e.sessions.Delete(s.id)
		e.count.Add(-1)
		e.pending.Add(-1)
		return ErrClosed
	}
	return nil
}

// reserve adds one to n, unless n is already at limit, which it returns err for. A limit of zero or less is none.
func reserve(n *atomic.Int64, limit int, err error) error {
	for {
		have := n.Load()
		if limit > 0 && have >= int64(limit) {
			return err
		}
		if n.CompareAndSwap(have, have+1) {
			return nil
		}
	}
}

// settle takes a session out of the count of listeners that are still connecting. It does so once, whichever of join
// and Leave comes first.
func (e *Engine) settle(s *session) {
	if s.state.Or(sessionSettled)&sessionSettled == 0 {
		e.pending.Add(-1)
	}
}

// newID returns a random non-zero number.
func newID() (uint64, error) {
	var b [8]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			return 0, fmt.Errorf("engine: make an id: %w", err)
		}
		if id := binary.BigEndian.Uint64(b[:]); id != 0 {
			return id, nil
		}
	}
}

// join starts the stream for a listener whose connection has come up. It does nothing for a listener that has already
// left or joined.
//
// Joining and [Engine.Leave] may run at the same time, and exactly one of them clears up. If Leave finds sessionJoined
// set, the listener is fully added and Leave removes it. If join finds sessionLeft set after it sets sessionJoined,
// Leave ran before the listener was fully added and left it to join, which removes it.
func (e *Engine) join(s *session, local *webrtc.TrackLocalStaticRTP) {
	if !s.state.CompareAndSwap(0, sessionJoining) {
		return
	}
	e.settle(s)
	e.metrics.Joined.Add(1)
	e.metrics.Listeners.Add(1)
	switch e.opts.Mode {
	case ModeFanout:
		if err := e.router.Add(s.id, local); err != nil {
			e.logger.Warn("listener not added to the router", "id", s.id, "err", err)
			go e.abandon(s.id)
		}
	case ModeNaive:
		playout, err := e.newPlayout(local, func(int) {
			e.metrics.Packets.Add(1)
			e.metrics.Delivered.Add(1)
		})
		if err != nil {
			e.logger.Warn("playout not started", "id", s.id, "err", err)
			go e.abandon(s.id)
			break
		}
		ctx, cancel := context.WithCancel(context.Background())
		s.stop.Store(&cancel)
		go func() {
			if err := playout.Do(ctx); err != nil {
				e.logger.Warn("playout stopped", "id", s.id, "err", err)
			}
		}()
	case ModeTrack:
	}
	if s.state.Or(sessionJoined)&sessionLeft != 0 {
		e.unjoin(s)
	}
}

// unjoin takes a listener out of everything join added it to, and counts it out.
func (e *Engine) unjoin(s *session) {
	if e.router != nil {
		e.router.Remove(s.id)
	}
	if stop := s.stop.Load(); stop != nil {
		(*stop)()
	}
	e.metrics.Left.Add(1)
	e.metrics.Listeners.Add(-1)
}

// Leave removes a listener and closes its connection. It returns [ErrNotFound] if there is no such listener.
func (e *Engine) Leave(id uint64) error {
	value, ok := e.sessions.LoadAndDelete(id)
	if !ok {
		return ErrNotFound
	}
	e.count.Add(-1)
	s := value.(*session)
	wasJoined := s.state.Or(sessionLeft)&sessionJoined != 0
	e.settle(s)
	if wasJoined {
		e.unjoin(s)
	}
	_ = s.pc.Close() // The listener is gone from the engine whether or not the close succeeds.
	return nil
}

// abandon removes a listener that the engine itself gives up on. [Engine.Leave] fails only with [ErrNotFound], which
// here means that someone else removed it first, so there is nothing to report.
func (e *Engine) abandon(id uint64) {
	_ = e.Leave(id)
}

// Close removes every listener and closes the UDP port. It may be called more than once.
func (e *Engine) Close() error {
	if e.state.Or(engineClosed)&engineClosed != 0 {
		return nil
	}
	e.sessions.Range(func(id, _ any) bool {
		e.abandon(id.(uint64))
		return true
	})
	var routerErr error
	if e.router != nil {
		routerErr = e.router.Close()
	}
	return errors.Join(routerErr, e.mux.Close())
}

// negotiate sets the offer and returns the answer with all its candidates, because the clients do not trickle.
func negotiate(ctx context.Context, pc *webrtc.PeerConnection, offer string) (string, error) {
	if err := pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offer,
	}); err != nil {
		return "", fmt.Errorf("%w: %w", ErrBadOffer, err)
	}
	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("engine: create answer: %w", err)
	}
	gathered := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(answer); err != nil {
		return "", fmt.Errorf("engine: set local description: %w", err)
	}
	select {
	case <-gathered:
	case <-ctx.Done():
		return "", fmt.Errorf("engine: gathering candidates: %w", ctx.Err())
	}
	return pc.LocalDescription().SDP, nil
}

// rtcpBufSize is the size of the buffer that [Engine.drain] reads the RTCP of a listener into: one packet of the
// largest size that the network carries.
const rtcpBufSize = 1500

// drain reads and drops the RTCP the listener sends, which a sender has to do for the connection to stay healthy. It
// returns when the sender is closed. The buffer comes from the pool and goes back to it, so that a listener that joins
// does not allocate one.
func (e *Engine) drain(sender *webrtc.RTPSender) {
	buf := e.rtcpBufPool.Get()
	defer e.rtcpBufPool.Put(buf)
	buf.B = slices.Grow(buf.B, rtcpBufSize)[:rtcpBufSize]
	for {
		if _, _, err := sender.Read(buf.B); err != nil {
			return
		}
	}
}

// pionLogs returns the logger factory of Pion. Pion warns about a socket on all addresses, which is what is wanted here,
// so only its errors are shown. They go to the logger of the engine with the network addresses removed, because the error
// of a failed write names the address of the listener.
func (e *Engine) pionLogs() *logging.DefaultLoggerFactory {
	cleaned := slog.New(&edge.AddressHandler{Next: e.logger.Handler()})
	return &logging.DefaultLoggerFactory{
		Writer:          slog.NewLogLogger(cleaned.Handler(), slog.LevelError).Writer(),
		DefaultLogLevel: logging.LogLevelError,
	}
}
