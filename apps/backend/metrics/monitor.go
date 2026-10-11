package metrics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/valyala/bytebufferpool"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/worker"
)

var _ worker.Worker = &RuntimeMonitor{}

var _ io.WriterTo = &RuntimeMonitor{}

// A RuntimeMonitor reads the health of the Go runtime at a fixed interval and keeps the last readings. Nothing that
// reads from it takes a lock: the readings are immutable and sit in a ring of atomic pointers.
type RuntimeMonitor struct {
	interval time.Duration
	csvDir   string
	reader   *runtimeReader

	// ring holds the latest samples, and written counts all the samples ever stored, so the newest is at written-1.
	ring    []atomic.Pointer[RuntimeSample]
	written atomic.Uint64

	// state holds the flags below.
	state atomic.Uint32

	// buffers holds the buffers that [RuntimeMonitor.WriteTo] builds the CSV in.
	bufPool bytebufferpool.Pool
}

// The flags of the state of a [RuntimeMonitor]. They are only ever set, never cleared.
const (
	// monitorClosed is set by the first call of [RuntimeMonitor.Close].
	monitorClosed uint32 = 1 << iota
)

// NewRuntimeMonitor returns a monitor that attaches itself to m: from then on every [Snapshot] of m carries the latest
// sample. It returns [ErrNoRuntimeMetric] if this version of Go does not report a metric that the monitor needs.
func NewRuntimeMonitor(m *Metrics, opts RuntimeOptions) (*RuntimeMonitor, error) {
	interval := opts.Interval
	if interval <= 0 {
		interval = defaultRuntimeInterval
	}
	keep := opts.Keep
	if keep <= 0 {
		keep = defaultRuntimeKeep
	}
	reader, err := newRuntimeReader(m.started)
	if err != nil {
		return nil, err
	}
	rm := &RuntimeMonitor{
		interval: interval,
		csvDir:   opts.CSVDir,
		reader:   reader,
		ring:     make([]atomic.Pointer[RuntimeSample], keep),
	}
	m.monitor.Store(rm)
	return rm, nil
}

// Do takes a sample now and then one every interval, until ctx is cancelled. It returns nil.
func (rm *RuntimeMonitor) Do(ctx context.Context) error {
	rm.sample(time.Now())
	ticker := time.NewTicker(rm.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-ticker.C:
			rm.sample(now)
		}
	}
}

// Close writes the samples held to the directory of [RuntimeOptions.CSVDir], if there is one. Only the first call
// writes.
func (rm *RuntimeMonitor) Close() error {
	if rm.state.Or(monitorClosed)&monitorClosed != 0 || rm.csvDir == "" {
		return nil
	}
	if err := os.MkdirAll(rm.csvDir, 0o755); err != nil {
		return fmt.Errorf("metrics: make the directory of the runtime samples: %w", err)
	}
	file, err := os.Create(filepath.Join(rm.csvDir, fmt.Sprintf("runtime_%d.csv", time.Now().Unix())))
	if err != nil {
		return fmt.Errorf("metrics: create the file of the runtime samples: %w", err)
	}
	if _, err := rm.WriteTo(file); err != nil {
		return errors.Join(err, file.Close())
	}
	return file.Close()
}

// sample reads the runtime and stores the sample. Only [RuntimeMonitor.Do] calls it.
func (rm *RuntimeMonitor) sample(now time.Time) {
	s := rm.reader.read(now)
	n := rm.written.Load()
	rm.ring[n%uint64(len(rm.ring))].Store(&s)
	rm.written.Store(n + 1)
}

// Latest returns the newest sample, and false if there is none yet.
func (rm *RuntimeMonitor) Latest() (RuntimeSample, bool) {
	n := rm.written.Load()
	if n == 0 {
		return RuntimeSample{}, false
	}
	if s := rm.ring[(n-1)%uint64(len(rm.ring))].Load(); s != nil {
		return *s, true
	}
	return RuntimeSample{}, false
}

// History returns the samples held, the oldest first. A sample that is replaced while the ring is being read is left
// out.
func (rm *RuntimeMonitor) History() []RuntimeSample {
	out := make([]RuntimeSample, 0, rm.held())
	rm.each(func(s *RuntimeSample) {
		out = append(out, *s)
	})
	return out
}

// held returns the number of samples in the ring.
func (rm *RuntimeMonitor) held() uint64 {
	return min(rm.written.Load(), uint64(len(rm.ring)))
}

// each calls visit with every sample held, the oldest first, without copying it. The sample is not changed after it is
// stored, so visit may read it, but it must not keep it past the call. A sample that is replaced while the ring is being
// read is left out.
func (rm *RuntimeMonitor) each(visit func(*RuntimeSample)) {
	n := rm.written.Load()
	size := uint64(len(rm.ring))
	first := uint64(0)
	if n > size {
		first = n - size
	}
	var previous int64
	for i := first; i < n; i++ {
		s := rm.ring[i%size].Load()
		if s == nil || s.Time < previous {
			continue
		}
		previous = s.Time
		visit(s)
	}
}

// runtimeColumns are the columns of the CSV that [RuntimeMonitor.WriteTo] writes, in the order of [appendRow].
var runtimeColumns = []string{
	"time",
	"goroutines",
	"procs",
	"heap_bytes",
	"stack_bytes",
	"total_bytes",
	"heap_goal_bytes",
	"gc_cycles",
	"alloc_bytes_per_second",
	"gc_cpu_percent",
	"mutex_wait_seconds",
	"sched_latency_p99_seconds",
	"sched_latency_max_seconds",
	"gc_pause_p99_seconds",
	"gc_pause_max_seconds",
}

// csvRowBytes is the size that a row of the CSV usually has, which [RuntimeMonitor.WriteTo] uses to make room for all
// the rows at once. A longer row still fits: the buffer grows.
const csvRowBytes = 160

// WriteTo writes the samples held to w as CSV, with a header, the oldest first. It builds the CSV in a pooled buffer and
// reads each sample where it is stored, so a call allocates nothing once the buffer is big enough. It implements
// [io.WriterTo].
func (rm *RuntimeMonitor) WriteTo(w io.Writer) (int64, error) {
	buf := rm.bufPool.Get()
	defer rm.bufPool.Put(buf)
	buf.B = slices.Grow(buf.B, 512+int(rm.held())*csvRowBytes)
	for i, column := range runtimeColumns {
		if i > 0 {
			buf.B = append(buf.B, ',')
		}
		buf.B = append(buf.B, column...)
	}
	buf.B = append(buf.B, '\n')
	rm.each(func(s *RuntimeSample) {
		buf.B = appendRow(buf.B, s)
	})
	n, err := w.Write(buf.B)
	if err != nil {
		return int64(n), fmt.Errorf("metrics: write the runtime samples: %w", err)
	}
	return int64(n), nil
}

// appendRow appends the CSV line of s.
func appendRow(buf []byte, s *RuntimeSample) []byte {
	buf = strconv.AppendInt(buf, s.Time, 10)
	buf = appendCount(buf, s.Goroutines)
	buf = appendCount(buf, s.Procs)
	buf = appendCount(buf, s.HeapBytes)
	buf = appendCount(buf, s.StackBytes)
	buf = appendCount(buf, s.TotalBytes)
	buf = appendCount(buf, s.HeapGoalBytes)
	buf = appendCount(buf, s.GCCycles)
	buf = appendFloat(buf, s.AllocBytesPerSecond, 'f', 0)
	buf = appendFloat(buf, s.GCCPUPercent, 'f', 3)
	buf = appendFloat(buf, s.MutexWaitSeconds, 'g', 6)
	buf = appendFloat(buf, s.SchedLatencyP99Seconds, 'g', 6)
	buf = appendFloat(buf, s.SchedLatencyMaxSeconds, 'g', 6)
	buf = appendFloat(buf, s.GCPauseP99Seconds, 'g', 6)
	buf = appendFloat(buf, s.GCPauseMaxSeconds, 'g', 6)
	return append(buf, '\n')
}

// appendCount appends a comma and v.
func appendCount(buf []byte, v uint64) []byte {
	return strconv.AppendUint(append(buf, ','), v, 10)
}

// appendFloat appends a comma and v, written with the given format and precision.
func appendFloat(buf []byte, v float64, format byte, precision int) []byte {
	return strconv.AppendFloat(append(buf, ','), v, format, precision, 64)
}
