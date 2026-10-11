package main

import (
	"flag"
	"strings"
	"time"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/config"
	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/engine"
)

// parseFlags reads the command-line arguments into the settings of the server.
func parseFlags(args []string) (config.Options, error) {
	var c config.Options
	set := flag.NewFlagSet("server", flag.ContinueOnError)
	set.StringVar(&c.HTTPAddr, "http", ":8080", "address of the HTTP server")
	set.StringVar(&c.AudioDir, "audio", "audio", "directory with the audio files")
	set.StringVar(&c.WebDir, "web", "",
		"directory with the built listener page (apps/frontend/.dist); empty serves only the API")
	set.StringVar(&c.ICEHost, "ice-host", "", "public IP addresses for listeners behind a NAT, comma separated")
	set.IntVar(&c.ICEPort, "ice-port", 5005, "UDP port of every WebRTC connection")
	modeName := set.String("mode", engine.ModeFanout.String(), "how the stream reaches listeners: naive, track or fanout")
	set.IntVar(&c.Shards, "shards", 0, "shards of the fan-out router; 0 is one per CPU core")
	set.IntVar(&c.QueueLen, "queue", 0,
		"packets a listener may have waiting; 0 is 32, or none with -batch; negative has no queues")
	loopback := set.Bool("loopback", false, "offer the loopback address, to connect from this machine")
	pprof := set.Bool("pprof", false, "serve /debug/pprof, which must not be reachable by the public")
	set.DurationVar(&c.ConnectTimeout, "connect-timeout", 0, "time a listener has to connect; 0 is 15s")
	set.DurationVar(&c.EvictAfter, "evict-after", 0,
		"time a connected listener may lose every packet before it is dropped; 0 is 5s")
	set.IntVar(&c.MaxListeners, "max-listeners", 0,
		"most listeners held at once, connected or connecting; 0 is 5000, negative has no limit")
	set.IntVar(&c.MaxConnecting, "max-connecting", 0,
		"most listeners held at once that have not connected yet; 0 is 1000, negative has no limit")
	set.DurationVar(&c.RuntimeInterval, "runtime-interval", time.Second,
		"time between readings of the health of the Go runtime; 0 or less reads none")
	set.StringVar(&c.RuntimeCSV, "runtime-csv", "",
		"directory to write the last readings of the runtime health to, as CSV, when the server stops")
	presenter := set.Bool("presenter", false,
		"an AI presenter introduces the next track in a voice while a track plays; needs -presenter-tts and a local Ollama")
	set.StringVar(&c.PresenterName, "presenter-name", "Go radio", "the name of the station that the presenter says")
	set.StringVar(&c.PresenterModel, "presenter-model", "phi3.5", "the Ollama model that writes what the presenter says")
	set.StringVar(&c.OllamaURL, "presenter-ollama", "http://127.0.0.1:11434", "address of the Ollama server")
	remoteModel := set.Bool("presenter-remote-model", false,
		"allow an Ollama server that is not on this machine; track titles and file names are sent to it")
	presenterTTS := set.String("presenter-tts", "",
		"speech program and its arguments, separated by spaces, with {out} for the file it writes; the text is sent on stdin")
	set.StringVar(&c.PresenterExt, "presenter-tts-ext", ".wav", "extension of the file the speech program writes")
	set.DurationVar(&c.PresenterMargin, "presenter-margin", 0,
		"time kept before the end of a track for the clip to be picked up; 0 is 3s")
	set.IntVar(&c.PresenterSeed, "presenter-seed", 0, "seed for the model's choices, where the model supports one")
	set.StringVar(&c.TURNURLs, "turn-url", "",
		"turn: or turns: addresses of a TURN server, comma separated, handed to browsers by GET /api/ice; empty hands out none")
	set.StringVar(&c.TURNSecretFile, "turn-secret-file", "",
		"file with the secret shared with the TURN server (static-auth-secret of coturn), at least 16 bytes")
	set.DurationVar(&c.TURNTTL, "turn-ttl", time.Hour, "how long the TURN credentials that are handed out last")
	set.DurationVar(&c.Frame, "frame", 20*time.Millisecond,
		"audio in each packet: 20ms, 40ms or 60ms; longer means fewer packets to send")
	set.IntVar(&c.Batch, "batch", 0,
		"send up to this many packets with one system call (sendmmsg on Linux); 0 sends one at a time")
	set.DurationVar(&c.BatchDelay, "batch-delay", 0, "time to wait for more packets to join a batch; 0 sends at once")
	set.StringVar(&c.FFmpeg, "ffmpeg", "ffmpeg", "the ffmpeg program")
	set.StringVar(&c.LogLevel, "log", "info", "log level: debug, info, warn or error")
	if err := set.Parse(args); err != nil {
		return config.Options{}, err
	}
	mode, err := engine.ParseMode(*modeName)
	if err != nil {
		return config.Options{}, err
	}
	c.Mode = mode
	c.PresenterTTS = strings.Fields(*presenterTTS)
	if *presenter {
		c.Flags |= config.FlagPresenter
	}
	if *remoteModel {
		c.Flags |= config.FlagRemoteModel
	}
	if *loopback {
		c.Flags |= config.FlagLoopback
	}
	if *pprof {
		c.Flags |= config.FlagProfiling
	}
	return c, nil
}
