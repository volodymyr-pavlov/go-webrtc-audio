package metrics

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime/metrics"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEveryRuntimeMetricIsKnownToThisGo(t *testing.T) {
	if _, err := newRuntimeReader(time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestASampleDescribesARunningProgram(t *testing.T) {
	reader, err := newRuntimeReader(time.Now().Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	s := reader.read(time.Now())
	if s.Goroutines == 0 || s.Procs == 0 || s.HeapBytes == 0 || s.StackBytes == 0 || s.HeapGoalBytes == 0 {
		t.Fatalf("a field is empty: %+v", s)
	}
	if s.TotalBytes < s.HeapBytes {
		t.Fatalf("total memory %d is less than the heap %d", s.TotalBytes, s.HeapBytes)
	}
	if s.AllocBytesPerSecond <= 0 {
		t.Fatalf("the program has allocated, but the rate is %v", s.AllocBytesPerSecond)
	}
}

func histogram(counts ...uint64) *metrics.Float64Histogram {
	buckets := make([]float64, len(counts)+1)
	for i := range buckets {
		buckets[i] = float64(i)
	}
	return &metrics.Float64Histogram{
		Counts:  counts,
		Buckets: buckets,
	}
}

func TestIntervalTakesTheNinetyNinthPercentileAndTheLongestOfNewEventsOnly(t *testing.T) {
	// 98 events in [0,1), one in [1,2), one in [3,4): the 99th of 100 is in [1,2), the longest is in [3,4).
	p99, longest, last := interval(nil, histogram(98, 1, 0, 1))
	if p99 != 2 || longest != 4 {
		t.Fatalf("p99 %v and longest %v, want 2 and 4", p99, longest)
	}

	// Nothing new: the old events are not counted again.
	p99, longest, last = interval(last, histogram(98, 1, 0, 1))
	if p99 != 0 || longest != 0 {
		t.Fatalf("with no new events: p99 %v and longest %v, want 0 and 0", p99, longest)
	}

	// Ten new events in [2,3) only: the history of good seconds does not hide them.
	p99, longest, _ = interval(last, histogram(98, 1, 10, 1))
	if p99 != 3 || longest != 3 {
		t.Fatalf("p99 %v and longest %v, want 3 and 3", p99, longest)
	}
}

func TestIntervalUsesTheLowerEdgeOfABucketThatReachesToInfinity(t *testing.T) {
	h := histogram(0, 5)
	h.Buckets[2] = infinity()
	p99, longest, _ := interval(nil, h)
	if p99 != 1 || longest != 1 {
		t.Fatalf("p99 %v and longest %v, want 1 and 1", p99, longest)
	}
}

func infinity() float64 {
	var zero float64
	return 1 / zero
}

func newMonitor(t testing.TB, opts RuntimeOptions) (*RuntimeMonitor, *Metrics) {
	t.Helper()
	m := New()
	rm, err := NewRuntimeMonitor(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	return rm, m
}

func TestMonitorKeepsTheLatestSamplesOldestFirst(t *testing.T) {
	rm, _ := newMonitor(t, RuntimeOptions{Keep: 3})
	if _, ok := rm.Latest(); ok {
		t.Fatal("a sample before the first one")
	}
	base := time.Now()
	for i := 0; i < 5; i++ {
		rm.sample(base.Add(time.Duration(i) * time.Second))
	}
	history := rm.History()
	if len(history) != 3 {
		t.Fatalf("%d samples held, want the latest 3", len(history))
	}
	for i := 1; i < len(history); i++ {
		if history[i].Time <= history[i-1].Time {
			t.Fatalf("samples are not oldest first: %d then %d", history[i-1].Time, history[i].Time)
		}
	}
	latest, ok := rm.Latest()
	if !ok || latest.Time != history[2].Time {
		t.Fatalf("latest %d, want %d", latest.Time, history[2].Time)
	}
}

func TestMonitorSamplesAtItsIntervalUntilItIsCancelled(t *testing.T) {
	rm, _ := newMonitor(t, RuntimeOptions{Interval: 10 * time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan error, 1)
	go func() { doneCh <- rm.Do(ctx) }()
	deadline := time.Now().Add(5 * time.Second)
	for rm.written.Load() < 4 {
		if time.Now().After(deadline) {
			t.Fatalf("only %d samples in 5 s at 10 ms", rm.written.Load())
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	if err := <-doneCh; err != nil {
		t.Fatal(err)
	}
	stopped := rm.written.Load()
	time.Sleep(60 * time.Millisecond)
	if rm.written.Load() != stopped {
		t.Fatal("the monitor went on sampling after it was cancelled")
	}
}

func TestSnapshotCarriesTheLatestSampleOfTheMonitor(t *testing.T) {
	rm, m := newMonitor(t, RuntimeOptions{})
	fake := RuntimeSample{
		Time:       42,
		Goroutines: 7,
		HeapBytes:  123456,
		GCCycles:   9,
	}
	rm.ring[0].Store(&fake)
	rm.written.Store(1)

	got := m.Snapshot()
	if got.Runtime != fake || got.HeapBytes != 123456 || got.GCCycles != 9 {
		t.Fatalf("the snapshot did not use the monitor's sample: %+v", got)
	}
}

func TestSnapshotWithoutAMonitorReadsTheRuntimeItself(t *testing.T) {
	got := New().Snapshot()
	if got.Runtime.Goroutines == 0 || got.HeapBytes == 0 {
		t.Fatalf("empty runtime health: %+v", got.Runtime)
	}
}

func TestMonitorWritesCSVWithAHeaderAndOneRowForEachSample(t *testing.T) {
	rm, _ := newMonitor(t, RuntimeOptions{})
	base := time.Now()
	rm.sample(base)
	rm.sample(base.Add(time.Second))
	var out bytes.Buffer
	if _, err := rm.WriteTo(&out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("%d lines, want a header and two rows:\n%s", len(lines), out.String())
	}
	columns := len(runtimeColumns)
	for i, line := range lines {
		if n := strings.Count(line, ",") + 1; n != columns {
			t.Errorf("line %d has %d columns, want %d: %s", i, n, columns, line)
		}
	}
	if !strings.HasPrefix(lines[0], "time,goroutines,") {
		t.Errorf("header: %s", lines[0])
	}
}

func TestCloseWritesTheSamplesOnceToTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "runtime")
	rm, _ := newMonitor(t, RuntimeOptions{CSVDir: dir})
	rm.sample(time.Now())
	if err := rm.Close(); err != nil {
		t.Fatal(err)
	}
	if err := rm.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "runtime_*.csv"))
	if err != nil || len(files) != 1 {
		t.Fatalf("%d files written, want one: %v", len(files), err)
	}
	if data, _ := os.ReadFile(files[0]); !strings.Contains(string(data), "goroutines") {
		t.Fatalf("the file has no header: %q", data)
	}
}

func TestCloseWithoutADirectoryWritesNothing(t *testing.T) {
	rm, _ := newMonitor(t, RuntimeOptions{})
	if err := rm.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReadersAndTheMonitorMayRunTogether(t *testing.T) {
	rm, m := newMonitor(t, RuntimeOptions{Keep: 4})
	var wg sync.WaitGroup
	stopCh := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		now := time.Now()
		for i := 0; ; i++ {
			select {
			case <-stopCh:
				return
			default:
				rm.sample(now.Add(time.Duration(i) * time.Millisecond))
			}
		}
	}()
	for i := 0; i < 200; i++ {
		_ = rm.History()
		_, _ = rm.Latest()
		_ = m.Snapshot()
	}
	close(stopCh)
	wg.Wait()
}

func BenchmarkWriteTo(b *testing.B) {
	rm, _ := newMonitor(b, RuntimeOptions{Keep: 300})
	base := time.Now()
	for i := range 300 {
		rm.sample(base.Add(time.Duration(i) * time.Second))
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := rm.WriteTo(io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
