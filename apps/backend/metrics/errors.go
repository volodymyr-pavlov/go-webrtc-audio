package metrics

import "errors"

// ErrNoRuntimeMetric is returned by [NewRuntimeMonitor] when this version of Go does not report a metric that the
// monitor reads.
var ErrNoRuntimeMetric = errors.New("metrics: runtime metric not supported")

// ErrBadSnapshot is returned by [ParseSnapshot] for JSON that is not an object.
var ErrBadSnapshot = errors.New("metrics: the snapshot is not a JSON object")
