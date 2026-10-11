package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "loadgen:", err)
		os.Exit(1)
	}
}

// newAPI returns the Pion API that the listeners use: Opus only, over UDP4.
func newAPI(flags configFlag) (*webrtc.API, error) {
	settings := webrtc.SettingEngine{}
	settings.SetNetworkTypes([]webrtc.NetworkType{webrtc.NetworkTypeUDP4})
	settings.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled)
	settings.SetIncludeLoopbackCandidate(true)
	if flags&flagAllInterfaces == 0 {
		settings.SetInterfaceFilter(func(name string) bool { return name == "lo" || name == "lo0" })
	}
	media := &webrtc.MediaEngine{}
	if err := media.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{
			MimeType:    webrtc.MimeTypeOpus,
			ClockRate:   48000,
			Channels:    2,
			SDPFmtpLine: "minptime=10;useinbandfec=1",
		},
		PayloadType: 111,
	}, webrtc.RTPCodecTypeAudio); err != nil {
		return nil, err
	}
	return webrtc.NewAPI(webrtc.WithMediaEngine(media), webrtc.WithSettingEngine(settings)), nil
}

// phases are the time spans of the idle and hold phases of a run, and the last reading of the server.
type phases struct {
	idleFrom, idleTo time.Time
	holdFrom, holdTo time.Time
	after            sample
}

// outcome is how many listeners connected and how many failed.
type outcome struct {
	connected, failed int
}

// run runs the load test described by args and writes the report. It returns the first error that stops it.
func run(args []string) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	api, err := newAPI(cfg.flags)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	watch := &sampler{
		url:    cfg.server,
		client: client,
	}
	if _, err := watch.take(ctx); err != nil {
		return fmt.Errorf("server at %s: %w", cfg.server, err)
	}
	samplerCtx, stopSampler := context.WithCancel(ctx)
	var samplerDone sync.WaitGroup
	samplerDone.Add(1)
	go func() {
		defer samplerDone.Done()
		watch.run(samplerCtx)
	}()

	var ph phases
	ph.idleFrom = time.Now()
	sleep(ctx, cfg.idle)
	ph.idleTo = time.Now()

	listeners, connecting := connectAll(ctx, cfg, api, client)

	ph.holdFrom = time.Now()
	sleep(ctx, cfg.hold)
	ph.holdTo = time.Now()

	ph.after = leave(ctx, cfg, listeners, watch)
	stopSampler()
	samplerDone.Wait()
	for _, l := range listeners[leaving(cfg):] {
		l.disconnect(context.Background())
	}

	summary := summarize(cfg, listeners, connecting, watch, ph)
	summary.writeTable(os.Stdout, tableHeader)
	if cfg.jsonPath != "" {
		encoded, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(cfg.jsonPath, append(encoded, '\n'), 0o644)
	}
	return nil
}

// connectAll starts cfg.listeners listeners, at most cfg.concurrency at a time, and waits until each has connected
// or failed.
func connectAll(ctx context.Context, cfg config, api *webrtc.API, client *http.Client) ([]*listener, outcome) {
	listeners := make([]*listener, cfg.listeners)
	var failed, connected atomic.Int32
	var wg sync.WaitGroup
	slotsCh := make(chan struct{}, cfg.concurrency)
	for i := range listeners {
		listeners[i] = &listener{
			api:    api,
			base:   cfg.server,
			client: client,
		}
		wg.Add(1)
		slotsCh <- struct{}{}
		go func() {
			defer wg.Done()
			connectCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
			err := listeners[i].connect(connectCtx)
			cancel()
			<-slotsCh
			if err != nil {
				failed.Add(1)
				listeners[i].failure.Store(&err)
				fmt.Fprintf(os.Stderr, "listener %d: %v\n", i, err)
				return
			}
			connected.Add(1)
		}()
	}
	wg.Wait()
	return listeners, outcome{
		connected: int(connected.Load()),
		failed:    int(failed.Load()),
	}
}

// leaving returns how many listeners leave before the end of the run.
func leaving(cfg config) int {
	return int(float64(cfg.listeners) * cfg.leave)
}

// leave disconnects the listeners that leave early, waits for the server to settle and returns the last reading of
// it. A failed reading is reported and gives a zero sample.
func leave(ctx context.Context, cfg config, listeners []*listener, watch *sampler) sample {
	var wg sync.WaitGroup
	for _, l := range listeners[:leaving(cfg)] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.disconnect(ctx)
		}()
	}
	wg.Wait()
	sleep(ctx, cfg.settle)
	after, err := watch.take(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "loadgen: the last reading of the server failed, so the report has no final figures: %v\n",
			err)
	}
	return after
}

// sleep waits for d or until ctx is done.
func sleep(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

// summarize builds the report of a run from the listeners, the outcome of connecting and the readings of the server.
func summarize(cfg config, listeners []*listener, connecting outcome, watch *sampler, ph phases) report {
	connected := connecting.connected
	idleFrom, idleTo, holdFrom, holdTo, after := ph.idleFrom, ph.idleTo, ph.holdFrom, ph.holdTo, ph.after
	r := report{
		Label:     cfg.label,
		Listeners: cfg.listeners,
		Connected: connecting.connected,
		Failed:    connecting.failed,
	}
	var starts, gaps []float64
	var received, lost uint64
	for _, l := range listeners {
		if l.failure.Load() != nil {
			continue
		}
		starts = append(starts, ms(time.Duration(l.started.Load())))
		gaps = append(gaps, ms(time.Duration(l.maxGap.Load())))
		received += l.received.Load()
		lost += l.lost.Load()
	}
	r.StartP50ms = percentile(starts, 0.5)
	r.StartP95ms = percentile(starts, 0.95)
	r.StartMaxMs = percentile(starts, 1)
	r.GapP50ms = percentile(gaps, 0.5)
	r.GapMaxMs = percentile(gaps, 1)
	if received+lost > 0 {
		r.LossPercent = float64(lost) / float64(received+lost) * 100
	}

	idle := watch.between(idleFrom, idleTo)
	hold := watch.between(holdFrom, holdTo)
	r.HoldSeconds = holdTo.Sub(holdFrom).Seconds()
	if len(idle) > 1 {
		r.IdleCPUPercent = cpuPercent(idle[0], idle[len(idle)-1])
		r.IdleRSSMB = mb(idle[len(idle)-1].metrics.RSSBytes)
		r.IdleGoroutines = idle[len(idle)-1].metrics.Goroutines
	}
	if len(hold) > 1 {
		first, last := hold[0], hold[len(hold)-1]
		r.CPUPercent = cpuPercent(first, last)
		r.ServerCPUSeconds = last.metrics.CPUSeconds - first.metrics.CPUSeconds
		r.HeapMB = mb(last.metrics.HeapBytes)
		r.ServerGCCycles = last.metrics.GCCycles - first.metrics.GCCycles
		r.ServerDropped = last.metrics.Dropped - first.metrics.Dropped
		r.ServerWriteErr = last.metrics.WriteErrors - first.metrics.WriteErrors
		r.PeakRSSMB = mb(slices.MaxFunc(hold, func(a, b sample) int {
			return cmp.Compare(a.metrics.RSSBytes, b.metrics.RSSBytes)
		}).metrics.RSSBytes)
		r.PeakGoroutines = slices.MaxFunc(hold, func(a, b sample) int {
			return cmp.Compare(a.metrics.Goroutines, b.metrics.Goroutines)
		}).metrics.Goroutines
		if sends := last.metrics.UDPSends - first.metrics.UDPSends; sends > 0 {
			r.PacketsPerSend = float64(last.metrics.UDPPackets-first.metrics.UDPPackets) / float64(sends)
		}
		if delivered := last.metrics.Delivered - first.metrics.Delivered; delivered > 0 {
			r.CPUMicrosPerPkt = r.ServerCPUSeconds / float64(delivered) * 1e6
			r.PacketsPerSec = float64(delivered) / last.at.Sub(first.at).Seconds() / float64(max(connected, 1))
		}
	}
	r.AfterGoroutines = after.metrics.Goroutines
	r.AfterListeners = after.metrics.Listeners
	return r
}
