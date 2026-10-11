package main

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/volodymyr-pavlov/go-webrtc-audio/apps/backend/metrics"
)

// A sample is the server's metrics at one moment.
type sample struct {
	at      time.Time
	metrics metrics.Snapshot
}

// A sampler reads the metrics of the server once a second.
type sampler struct {
	url    string
	client *http.Client

	// samples is the list of samples taken so far. It is replaced as a whole, never changed, and only [sampler.run] adds
	// to it, so readers need no lock.
	samples atomic.Pointer[[]sample]
}

func (s *sampler) take(ctx context.Context) (sample, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url+"/api/metrics", nil)
	if err != nil {
		return sample{}, err
	}
	response, err := s.client.Do(request)
	if err != nil {
		return sample{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return sample{}, err
	}
	snapshot, err := metrics.ParseSnapshot(body)
	if err != nil {
		return sample{}, err
	}
	return sample{
		at:      time.Now(),
		metrics: snapshot,
	}, nil
}

// run samples until ctx is cancelled.
func (s *sampler) run(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if one, err := s.take(ctx); err == nil {
			var next []sample
			if old := s.samples.Load(); old != nil {
				next = append(next, *old...)
			}
			next = append(next, one)
			s.samples.Store(&next)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// between returns the samples taken in [from, to].
func (s *sampler) between(from, to time.Time) []sample {
	var out []sample
	all := s.samples.Load()
	if all == nil {
		return nil
	}
	for _, one := range *all {
		if !one.at.Before(from) && !one.at.After(to) {
			out = append(out, one)
		}
	}
	return out
}
