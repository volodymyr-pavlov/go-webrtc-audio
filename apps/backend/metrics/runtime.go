package metrics

import (
	"fmt"
	"math"
	"runtime/metrics"
	"slices"
	"time"
)

// Names of the runtime metrics that a [RuntimeSample] is made of.
const (
	metricGoroutines   = "/sched/goroutines:goroutines"
	metricProcs        = "/sched/gomaxprocs:threads"
	metricHeapObjects  = "/memory/classes/heap/objects:bytes"
	metricHeapStacks   = "/memory/classes/heap/stacks:bytes"
	metricTotalMemory  = "/memory/classes/total:bytes"
	metricHeapGoal     = "/gc/heap/goal:bytes"
	metricGCCycles     = "/gc/cycles/total:gc-cycles"
	metricAllocBytes   = "/gc/heap/allocs:bytes"
	metricGCCPU        = "/cpu/classes/gc/total:cpu-seconds"
	metricTotalCPU     = "/cpu/classes/total:cpu-seconds"
	metricMutexWait    = "/sync/mutex/wait/total:seconds"
	metricSchedLatency = "/sched/latencies:seconds"
	metricGCPause      = "/sched/pauses/total/gc:seconds"
)

// runtimeMetrics are the metrics read, in the order of the indexes below.
var runtimeMetrics = []string{
	metricGoroutines,
	metricProcs,
	metricHeapObjects,
	metricHeapStacks,
	metricTotalMemory,
	metricHeapGoal,
	metricGCCycles,
	metricAllocBytes,
	metricGCCPU,
	metricTotalCPU,
	metricMutexWait,
	metricSchedLatency,
	metricGCPause,
}

const (
	iGoroutines = iota
	iProcs
	iHeapObjects
	iHeapStacks
	iTotalMemory
	iHeapGoal
	iGCCycles
	iAllocBytes
	iGCCPU
	iTotalCPU
	iMutexWait
	iSchedLatency
	iGCPause
)

// A RuntimeSample is one reading of the health of the Go runtime. The fields that describe an interval cover the time
// since the sample before it; in the first sample they cover the time since the program started.
type RuntimeSample struct {
	// Time is when the sample was taken, in seconds since the Unix epoch.
	Time int64 `json:"time"`

	// Goroutines is the number of goroutines, and Procs the number of threads that may run Go code at once.
	Goroutines uint64 `json:"goroutines"`
	Procs      uint64 `json:"procs"`

	// HeapBytes is the memory of the objects on the heap, live or not yet freed. StackBytes is the memory of goroutine
	// stacks, and TotalBytes all the memory the Go runtime has mapped.
	HeapBytes  uint64 `json:"heap_bytes"`
	StackBytes uint64 `json:"stack_bytes"`
	TotalBytes uint64 `json:"total_bytes"`

	// HeapGoalBytes is the heap size at which the next garbage collection starts, and GCCycles the number of
	// collections so far.
	HeapGoalBytes uint64 `json:"heap_goal_bytes"`
	GCCycles      uint64 `json:"gc_cycles"`

	// AllocBytesPerSecond is how fast the program allocated memory in the interval.
	AllocBytesPerSecond float64 `json:"alloc_bytes_per_second"`

	// GCCPUPercent is the share of the CPU time that the garbage collector used in the interval.
	GCCPUPercent float64 `json:"gc_cpu_percent"`

	// MutexWaitSeconds is the time goroutines spent waiting for a sync.Mutex or sync.RWMutex in the interval, added up
	// over all goroutines.
	MutexWaitSeconds float64 `json:"mutex_wait_seconds"`

	// SchedLatencyP99Seconds and SchedLatencyMaxSeconds are how long a runnable goroutine waited for a thread in the
	// interval: the 99th percentile and the longest. They are bounds of the buckets of the runtime's histogram, not
	// exact times.
	SchedLatencyP99Seconds float64 `json:"sched_latency_p99_seconds"`
	SchedLatencyMaxSeconds float64 `json:"sched_latency_max_seconds"`

	// GCPauseP99Seconds and GCPauseMaxSeconds are how long garbage collection stopped the program in the interval, the
	// 99th percentile and the longest, with the same bounds on their precision.
	GCPauseP99Seconds float64 `json:"gc_pause_p99_seconds"`
	GCPauseMaxSeconds float64 `json:"gc_pause_max_seconds"`
}

// runtimeReader reads [RuntimeSample]s. It keeps the readings of the last sample, to take the difference, so one
// goroutine at a time may use it.
type runtimeReader struct {
	samples []metrics.Sample
	last    time.Time

	allocBytes uint64
	gcCPU      float64
	totalCPU   float64
	mutexWait  float64
	sched      []uint64
	pauses     []uint64
}

// newRuntimeReader returns a reader whose first sample covers the time since start, or [ErrNoRuntimeMetric] if the
// runtime lacks one of the metrics.
func newRuntimeReader(start time.Time) (*runtimeReader, error) {
	r := &runtimeReader{
		samples: make([]metrics.Sample, len(runtimeMetrics)),
		last:    start,
	}
	for i, name := range runtimeMetrics {
		r.samples[i].Name = name
	}
	metrics.Read(r.samples)
	for _, s := range r.samples {
		if s.Value.Kind() == metrics.KindBad {
			return nil, fmt.Errorf("%w: %s", ErrNoRuntimeMetric, s.Name)
		}
	}
	// Nothing is taken off the first sample: it covers the time since now, which the caller gives as the start.
	return r, nil
}

// read returns the sample of now.
func (r *runtimeReader) read(now time.Time) RuntimeSample {
	metrics.Read(r.samples)
	seconds := now.Sub(r.last).Seconds()
	r.last = now

	s := RuntimeSample{
		Time:          now.Unix(),
		Goroutines:    r.samples[iGoroutines].Value.Uint64(),
		Procs:         r.samples[iProcs].Value.Uint64(),
		HeapBytes:     r.samples[iHeapObjects].Value.Uint64(),
		StackBytes:    r.samples[iHeapStacks].Value.Uint64(),
		TotalBytes:    r.samples[iTotalMemory].Value.Uint64(),
		HeapGoalBytes: r.samples[iHeapGoal].Value.Uint64(),
		GCCycles:      r.samples[iGCCycles].Value.Uint64(),
	}

	alloc := r.samples[iAllocBytes].Value.Uint64()
	if seconds > 0 {
		s.AllocBytesPerSecond = float64(alloc-r.allocBytes) / seconds
	}
	r.allocBytes = alloc

	gc, total := r.samples[iGCCPU].Value.Float64(), r.samples[iTotalCPU].Value.Float64()
	if total > r.totalCPU {
		s.GCCPUPercent = 100 * (gc - r.gcCPU) / (total - r.totalCPU)
	}
	r.gcCPU, r.totalCPU = gc, total

	wait := r.samples[iMutexWait].Value.Float64()
	s.MutexWaitSeconds = wait - r.mutexWait
	r.mutexWait = wait

	sched := r.samples[iSchedLatency].Value.Float64Histogram()
	s.SchedLatencyP99Seconds, s.SchedLatencyMaxSeconds, r.sched = interval(r.sched, sched)
	pauses := r.samples[iGCPause].Value.Float64Histogram()
	s.GCPauseP99Seconds, s.GCPauseMaxSeconds, r.pauses = interval(r.pauses, pauses)
	return s
}

// interval returns the 99th percentile and the maximum of the events that a runtime histogram counted since the
// reading that had the counts last, and the counts of h to pass as last to the next call. The runtime's histograms
// count from the start of the program, which would hide a problem of the last second under hours of good seconds.
func interval(last []uint64, h *metrics.Float64Histogram) (float64, float64, []uint64) {
	counts := slices.Clone(h.Counts)
	if len(last) != len(counts) {
		last = make([]uint64, len(counts))
	}
	delta := make([]uint64, len(counts))
	var total uint64
	for i, count := range counts {
		delta[i] = count - last[i]
		total += delta[i]
	}
	if total == 0 {
		return 0, 0, counts
	}
	target := uint64(math.Ceil(0.99 * float64(total)))
	var p99, longest float64
	var seen uint64
	for i, count := range delta {
		if count == 0 {
			continue
		}
		bound := upperBound(h.Buckets, i)
		longest = bound
		if p99 == 0 && seen+count >= target {
			p99 = bound
		}
		seen += count
	}
	return p99, longest, counts
}

// upperBound returns the upper edge of bucket i, or its lower edge if the bucket reaches to infinity.
func upperBound(buckets []float64, i int) float64 {
	if upper := buckets[i+1]; !math.IsInf(upper, 1) {
		return upper
	}
	return buckets[i]
}
