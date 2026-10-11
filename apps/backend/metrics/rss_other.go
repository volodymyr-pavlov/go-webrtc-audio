//go:build !linux

package metrics

import (
	"runtime"
	"syscall"
)

// residentBytes returns the peak resident set size of the process, which is all that getrusage reports outside Linux.
func residentBytes() uint64 {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0
	}
	if runtime.GOOS == "darwin" {
		return uint64(usage.Maxrss)
	}
	return uint64(usage.Maxrss) * 1024
}
