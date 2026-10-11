package api

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/valyala/fasthttp"
	"github.com/valyala/fasthttp/pprofhandler"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/config"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/edge"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/worker"
)

// A Server is the HTTP side of the program: the handlers of the API, the page, the profiler, and the fasthttp server
// behind them. It also runs the engine, the presenter and the runtime monitor, so that one [Server.Startup] starts
// everything and one [Server.Shutdown] stops it. [NewServer] builds it.
type Server struct {
	addr   string
	logger *slog.Logger
	flags  Flag
	page   fs.FS
	http   *fasthttp.Server
	pool   *worker.Pool

	listen   edge.Handler
	leave    edge.Handler
	playlist edge.Handler
	ice      edge.Handler
	metrics  edge.Handler
	runtime  edge.Handler
}

// NewServer returns the [Server] that serves env on addr. It serves the profiler when opts has [config.FlagProfiling],
// and it tells the page about the presenter when env has one.
//
// It returns an error wrapping [config.ErrMissingFromEnv] if env lacks a mandatory dependency.
func NewServer(addr string, logger *slog.Logger, env *config.Env, opts config.Options) (*Server, error) {
	if err := env.Validate(); err != nil {
		return nil, fmt.Errorf("check the environment: %w", err)
	}
	var flags Flag
	if opts.Flags&config.FlagProfiling != 0 {
		flags |= FlagProfiling
	}
	if env.Presenter != nil {
		flags |= FlagPresenter
	}
	pool := &worker.Pool{}
	pool.Add(env.Engine)
	if env.Presenter != nil {
		pool.Add(env.Presenter)
	}
	if env.Runtime != nil {
		pool.Add(env.Runtime)
	}

	server := &Server{
		addr:   addr,
		logger: logger,
		flags:  flags,
		page:   env.Page,
		pool:   pool,
		listen: &ListenHandler{Engine: env.Engine},
		leave:  &LeaveHandler{Engine: env.Engine},
		playlist: &PlaylistHandler{
			Engine: env.Engine,
			Tracks: EncodeTracks(env.Engine.Tracks()),
			Flags:  flags,
		},
		ice:     &ICEHandler{TURN: env.TURN},
		metrics: &MetricsHandler{Metrics: env.Metrics},
	}
	if env.Runtime != nil {
		server.runtime = &RuntimeHandler{Monitor: env.Runtime}
	}
	server.http = &fasthttp.Server{
		Handler:            server.Routes().Handler(),
		ErrorHandler:       edge.ErrorHandler,
		MaxRequestBodySize: MaxOfferSize,
		ReadTimeout:        httpReadTimeout,
		IdleTimeout:        httpIdleTimeout,
		Logger:             slog.NewLogLogger(&edge.AddressHandler{Next: logger.Handler()}, slog.LevelWarn),
	}
	return server, nil
}

// Startup runs the engine, the presenter, the runtime monitor and the HTTP server until one of them stops or ctx ends.
// It returns the error of the one that failed, or ctx.Err().
func (s *Server) Startup(ctx context.Context) error {
	poolCh := make(chan error, 1)
	httpCh := make(chan error, 1)
	go func() { poolCh <- s.pool.Do(ctx) }()
	go func() { httpCh <- s.http.ListenAndServe(s.addr) }()

	select {
	case err := <-poolCh:
		if err != nil {
			return fmt.Errorf("the worker pool: %w", err)
		}
	case err := <-httpCh:
		if err != nil {
			return fmt.Errorf("the HTTP server: %w", err)
		}
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// Shutdown closes the engine, the presenter and the runtime monitor, and shuts the HTTP server down. Open requests may
// finish until ctx ends. It returns the errors joined.
func (s *Server) Shutdown(ctx context.Context) error {
	return errors.Join(s.pool.Close(), s.http.ShutdownWithContext(ctx))
}

// Routes returns the [edge.Mux] that serves the API under the prefix /api. The listener page, the profiler when
// [FlagProfiling] is set, and the files of the page are outside the prefix.
func (s *Server) Routes() *edge.Mux {
	mux := edge.NewMux(s.logger, edge.Recover())
	mux.POST(apiPrefix+"/listen", s.listen)
	mux.DELETE(apiPrefix+"/listen/{id}", s.leave)
	mux.GET(apiPrefix+"/playlist", s.playlist)
	mux.GET(apiPrefix+"/ice", s.ice)
	mux.GET(apiPrefix+"/metrics", s.metrics)
	if s.runtime != nil {
		mux.GET(apiPrefix+"/runtime", s.runtime)
	}
	if s.flags&FlagProfiling != 0 {
		mux.Plain("/debug/pprof/", pprofhandler.PprofHandler)
		mux.Plain("/debug/pprof/{name:*}", pprofhandler.PprofHandler)
	}
	if s.page != nil {
		files := &fasthttp.FS{
			FS:         s.page,
			IndexNames: []string{"index.html"},
		}
		mux.NotFound(files.NewRequestHandler())
	}
	return mux
}
