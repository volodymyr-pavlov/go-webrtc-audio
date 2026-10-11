package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/metrics"
)

// run adds samples while between reads them, which the race detector checks.
func TestSamplerCanBeReadWhileItIsAddedTo(t *testing.T) {
	// The server answers with the snapshot that the metrics handler of the API writes, as the real one does.
	body, err := json.Marshal(metrics.New().Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer server.Close()
	watch := &sampler{
		url:    server.URL,
		client: http.DefaultClient,
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		watch.run(ctx)
	}()

	from := time.Now()
	deadline := time.Now().Add(2500 * time.Millisecond)
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for time.Now().Before(deadline) {
				watch.between(from, time.Now())
			}
		}()
	}
	readers.Wait()
	cancel()
	wg.Wait()

	if got := len(watch.between(from, time.Now())); got < 2 {
		t.Fatalf("only %d samples in 2.5 s, want at least 2", got)
	}
}
