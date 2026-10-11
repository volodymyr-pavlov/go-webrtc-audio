package metrics

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"sync/atomic"
	"syscall"
	"time"
)

var _ io.WriterTo = &Metrics{}

// Metrics holds the counters and gauges of the server.
type Metrics struct {
	// Listeners is the number of listeners connected now.
	Listeners atomic.Int64

	// Joined counts the listeners that have connected since the start.
	Joined atomic.Uint64

	// Left counts the listeners that have disconnected since the start.
	Left atomic.Uint64

	// Packets counts the RTP packets made for the stream. In naive mode every listener has its own playout, and each
	// counts.
	Packets atomic.Uint64

	// Delivered counts the packets written to a listener.
	Delivered atomic.Uint64

	// Dropped counts the packets that were not sent because a queue was full: a listener's, a shard's or the socket's.
	Dropped atomic.Uint64

	// Evicted counts the listeners removed for losing too many packets in a row.
	Evicted atomic.Uint64

	// WriteErrors counts the packets the transport refused.
	WriteErrors atomic.Uint64

	// UDPSends counts the system calls that sent packets, when the batching socket is in use.
	UDPSends atomic.Uint64

	// UDPPackets counts the packets those system calls sent.
	UDPPackets atomic.Uint64

	// Presenter counts what the AI presenter did and how long it took.
	Presenter PresenterMetrics

	started time.Time

	// monitor is the [RuntimeMonitor] that feeds the runtime health of [Metrics.Snapshot], when there is one.
	monitor atomic.Pointer[RuntimeMonitor]
}

// New returns a Metrics that counts from now.
func New() *Metrics {
	return &Metrics{
		started: time.Now(),
	}
}

// Snapshot is the state of the server at one moment, as [api.MetricsHandler] writes it as JSON.
type Snapshot struct {
	// UptimeSeconds is the time since [New].
	UptimeSeconds float64 `json:"uptime_seconds"`
	// Listeners, Joined, Left, Packets, Delivered, Dropped, Evicted, WriteErrors, UDPSends and UDPPackets are the values
	// of the gauge and the counters of [Metrics] of the same names.
	Listeners   int64  `json:"listeners"`
	Joined      uint64 `json:"joined"`
	Left        uint64 `json:"left"`
	Packets     uint64 `json:"packets"`
	Delivered   uint64 `json:"delivered"`
	Dropped     uint64 `json:"dropped"`
	Evicted     uint64 `json:"evicted"`
	WriteErrors uint64 `json:"write_errors"`
	UDPSends    uint64 `json:"udp_sends"`
	UDPPackets  uint64 `json:"udp_packets"`
	// Goroutines is the number of goroutines now.
	Goroutines int `json:"goroutines"`
	// HeapBytes is the size of the live heap, and RSSBytes the resident memory of the process. Outside Linux RSSBytes is
	// the peak, which is all the system reports.
	HeapBytes uint64 `json:"heap_bytes"`
	RSSBytes  uint64 `json:"rss_bytes"`
	// GCCycles is the number of garbage collections so far.
	GCCycles uint32 `json:"gc_cycles"`
	// Presenter is what the AI presenter has done so far.
	Presenter PresenterSnapshot `json:"presenter"`
	// Runtime is the health of the Go runtime in the latest sample of the [RuntimeMonitor], or in a reading made now if
	// there is no monitor.
	Runtime RuntimeSample `json:"runtime"`
	// CPUSeconds is the user and system CPU time the process has used.
	CPUSeconds float64 `json:"cpu_seconds"`
}

// Snapshot reads every metric once.
func (m *Metrics) Snapshot() Snapshot {
	rt := m.runtimeSample()
	return Snapshot{
		UptimeSeconds: time.Since(m.started).Seconds(),
		Listeners:     m.Listeners.Load(),
		Joined:        m.Joined.Load(),
		Left:          m.Left.Load(),
		Packets:       m.Packets.Load(),
		Delivered:     m.Delivered.Load(),
		Dropped:       m.Dropped.Load(),
		Evicted:       m.Evicted.Load(),
		WriteErrors:   m.WriteErrors.Load(),
		UDPSends:      m.UDPSends.Load(),
		UDPPackets:    m.UDPPackets.Load(),
		Goroutines:    runtime.NumGoroutine(),
		HeapBytes:     rt.HeapBytes,
		RSSBytes:      residentBytes(),
		GCCycles:      uint32(rt.GCCycles),
		Runtime:       rt,
		Presenter:     m.Presenter.snapshot(),
		CPUSeconds:    cpuSeconds(),
	}
}

// runtimeSample returns the latest sample of the monitor, or a reading made now. Neither stops the program.
func (m *Metrics) runtimeSample() RuntimeSample {
	if rm := m.monitor.Load(); rm != nil {
		if s, ok := rm.Latest(); ok {
			return s
		}
	}
	reader, err := newRuntimeReader(m.started)
	if err != nil {
		return RuntimeSample{
			Time:       time.Now().Unix(),
			Goroutines: uint64(runtime.NumGoroutine()),
		}
	}
	return reader.read(time.Now())
}

// WriteTo writes a [Snapshot] to w as JSON. It implements [io.WriterTo].
func (m *Metrics) WriteTo(w io.Writer) (int64, error) {
	body, err := json.Marshal(m.Snapshot())
	if err != nil {
		return 0, fmt.Errorf("metrics: encode the snapshot: %w", err)
	}
	n, err := w.Write(body)
	return int64(n), err
}

// cpuSeconds returns the user and system CPU time the process has used.
func cpuSeconds() float64 {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0
	}
	return seconds(usage.Utime) + seconds(usage.Stime)
}

func seconds(tv syscall.Timeval) float64 {
	return float64(tv.Sec) + float64(tv.Usec)/1e6
}
