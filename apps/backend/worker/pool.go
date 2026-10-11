package worker

import (
	"context"
	"errors"

	"golang.org/x/sync/errgroup"
)

var _ Worker = &Pool{}

// A Pool is a [Worker] made of other workers. It runs them together, and it stops them all when one fails.
type Pool struct {
	// Workers are the workers the pool runs. A nil worker is skipped.
	Workers []Worker
}

// Add adds workers to the pool, before [Pool.Do] runs. It skips a nil worker.
func (p *Pool) Add(workers ...Worker) {
	for _, worker := range workers {
		if worker != nil {
			p.Workers = append(p.Workers, worker)
		}
	}
}

// Do runs every worker at once and returns when all of them have returned. When one returns an error, Do cancels the
// context of the others, and it returns the first error.
func (p *Pool) Do(ctx context.Context) error {
	group, ctx := errgroup.WithContext(ctx)
	for _, worker := range p.Workers {
		if worker == nil {
			continue
		}
		group.Go(func() error { return worker.Do(ctx) })
	}
	return group.Wait()
}

// Close closes every worker, whether or not an earlier one failed, and returns the errors joined.
func (p *Pool) Close() error {
	var errs []error
	for _, worker := range p.Workers {
		if worker == nil {
			continue
		}
		if err := worker.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
