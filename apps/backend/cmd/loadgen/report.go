package main

import (
	"fmt"
	"io"
	"slices"
	"time"
)

// A report is the result of one run, as printed and as written with -json.
type report struct {
	Label     string `json:"label"`
	Listeners int    `json:"listeners"`
	Connected int    `json:"connected"`
	Failed    int    `json:"failed"`

	StartP50ms float64 `json:"start_p50_ms"`
	StartP95ms float64 `json:"start_p95_ms"`
	StartMaxMs float64 `json:"start_max_ms"`

	LossPercent    float64 `json:"loss_percent"`
	GapP50ms       float64 `json:"longest_gap_p50_ms"`
	GapMaxMs       float64 `json:"longest_gap_max_ms"`
	PacketsPerSec  float64 `json:"packets_per_listener_second"`
	PacketsPerSend float64 `json:"packets_per_send"`
	ServerDropped  uint64  `json:"server_dropped"`
	ServerWriteErr uint64  `json:"server_write_errors"`

	IdleCPUPercent   float64 `json:"idle_cpu_percent"`
	CPUPercent       float64 `json:"cpu_percent"`
	CPUMicrosPerPkt  float64 `json:"cpu_microseconds_per_packet"`
	IdleRSSMB        float64 `json:"idle_rss_mb"`
	PeakRSSMB        float64 `json:"peak_rss_mb"`
	HeapMB           float64 `json:"heap_mb"`
	IdleGoroutines   int     `json:"idle_goroutines"`
	PeakGoroutines   int     `json:"peak_goroutines"`
	AfterGoroutines  int     `json:"after_leave_goroutines"`
	AfterListeners   int64   `json:"after_leave_server_listeners"`
	ServerGCCycles   uint32  `json:"server_gc_cycles"`
	HoldSeconds      float64 `json:"hold_seconds"`
	ServerCPUSeconds float64 `json:"server_cpu_seconds"`
}

func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	index := int(float64(len(sorted)-1) * p)
	return sorted[index]
}

func ms(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}

// cpuPercent returns the CPU use, in percent of one core, between two samples.
func cpuPercent(from, to sample) float64 {
	wall := to.at.Sub(from.at).Seconds()
	if wall <= 0 {
		return 0
	}
	return (to.metrics.CPUSeconds - from.metrics.CPUSeconds) / wall * 100
}

func mb(bytes uint64) float64 {
	return float64(bytes) / (1 << 20)
}

// writeTable prints the report as a Markdown table row, with the header when header is true.
// A tableFlag is a bit of the flags of [report.writeTable].
type tableFlag uint8

const (
	// tableHeader writes the header of the table before the row.
	tableHeader tableFlag = 1 << iota
)

func (r report) writeTable(w io.Writer, flags tableFlag) {
	if flags&tableHeader != 0 {
		fmt.Fprintln(w, "| mode | listeners | ok | start p50 ms | start p95 ms | loss % | longest gap ms | CPU % "+
			"| µs CPU per packet | packets/s | packets per send | RSS MB | goroutines | after leave |")
		fmt.Fprintln(w, "|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|")
	}
	fmt.Fprintf(w, "| %s | %d | %d | %.0f | %.0f | %.3f | %.0f | %.1f | %.1f | %.0f | %.1f | %.0f | %d | %d |\n",
		r.Label, r.Listeners, r.Connected, r.StartP50ms, r.StartP95ms, r.LossPercent, r.GapMaxMs,
		r.CPUPercent, r.CPUMicrosPerPkt, r.PacketsPerSec, r.PacketsPerSend, r.PeakRSSMB, r.PeakGoroutines,
		r.AfterGoroutines)
}
