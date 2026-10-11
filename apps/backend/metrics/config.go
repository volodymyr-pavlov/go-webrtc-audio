package metrics

import "time"

const (
	defaultRuntimeInterval = time.Second
	defaultRuntimeKeep     = 300
)

// RuntimeOptions describe a [RuntimeMonitor].
type RuntimeOptions struct {
	// Interval is the time between two samples. The default is one second.
	Interval time.Duration

	// Keep is the number of samples held in memory, the latest ones. The default is 300, which is five minutes at the
	// default interval.
	Keep int

	// CSVDir is a directory that [RuntimeMonitor.Close] writes the samples held to, as a CSV file named
	// runtime_<unix time>.csv. It is created if it does not exist. Empty writes nothing.
	CSVDir string
}
