package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/api"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/config"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/edge"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/launch"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "server:", err)
		os.Exit(1)
	}
}

// run builds the server from the command-line arguments and runs it until the process is interrupted. It returns the
// first error that stops it.
func run(args []string) error {
	opts, err := parseFlags(args)
	if err != nil {
		return err
	}
	logger, err := newLogger(opts.LogLevel)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	env, err := config.NewEnv(ctx, logger, opts)
	if err != nil {
		return fmt.Errorf("create the environment: %w", err)
	}
	defer env.Close()

	server, err := api.NewServer(opts.HTTPAddr, logger, env, opts)
	if err != nil {
		return fmt.Errorf("create the server: %w", err)
	}
	logger.Info("listening", "http", opts.HTTPAddr, "ice_port", opts.ICEPort, "mode", opts.Mode.String())
	return launch.Serve(ctx, logger, server, launch.DefaultShutdownTimeout)
}

// newLogger returns a JSON logger on standard error at the named level, tagged with the program version.
func newLogger(levelName string) (*slog.Logger, error) {
	var level slog.Level
	if err := level.UnmarshalText([]byte(levelName)); err != nil {
		return nil, fmt.Errorf("log level: %w", err)
	}
	// Whatever is logged, by this program or by a library that it hands the logger to, has no network address in it.
	handler := &edge.AddressHandler{Next: slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})}
	return slog.New(handler).With("version", version), nil
}
