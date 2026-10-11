package metrics

import (
	"os"
	"strconv"
	"strings"
)

// residentBytes returns the resident set size of the process, read from /proc/self/statm.
func residentBytes() uint64 {
	statm, err := os.ReadFile("/proc/self/statm")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(statm))
	if len(fields) < 2 {
		return 0
	}
	pages, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return pages * uint64(os.Getpagesize())
}
