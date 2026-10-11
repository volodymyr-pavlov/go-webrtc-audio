package edge

import (
	"testing"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	// fasthttp starts two goroutines for the whole process, one that updates the Date header and one in the worker pool
	// of every server. They are not a leak of a test.
	goleak.VerifyTestMain(m,
		goleak.IgnoreAnyFunction("github.com/valyala/fasthttp.updateServerDate.func1"),
		goleak.IgnoreAnyFunction("github.com/valyala/fasthttp.(*workerPool).Start.func2"),
	)
}
