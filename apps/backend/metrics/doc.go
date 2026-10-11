// Package metrics holds the counters and gauges of the server and writes them out as JSON, together with what the
// process costs: goroutines, memory and CPU time.
//
// A counter is an [sync/atomic.Uint64] and a gauge, which also goes down, an [sync/atomic.Int64]. Both are atomic, so nothing
// that updates one needs a lock.
//
// A [RuntimeMonitor] reads the health of the Go runtime once a second, from runtime/metrics, which does not stop the
// program as runtime.ReadMemStats does. The latest reading is in every [Snapshot], and the last few minutes of readings
// can be written as CSV.
package metrics
