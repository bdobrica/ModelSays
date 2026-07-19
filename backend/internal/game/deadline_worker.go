package game

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrDeadlineTransitionsUnavailable = errors.New("deadline transition processing is unavailable")

type DueRoundRepository interface {
	RevealDueRounds(ctx context.Context, dueAt, occurredAt time.Time, limit int) (int, error)
}

type DeadlineWorkerConfig struct {
	Enabled      bool
	GracePeriod  time.Duration
	PollInterval time.Duration
	BatchSize    int
}

type DeadlineWorker struct {
	repository DueRoundRepository
	clock      Clock
	config     DeadlineWorkerConfig
	mu         sync.RWMutex
	started    bool
	lastOK     time.Time
	lastErr    error
}

func NewDeadlineWorker(repository DueRoundRepository, clock Clock, config DeadlineWorkerConfig) *DeadlineWorker {
	if clock == nil {
		clock = systemClock{}
	}
	if config.PollInterval <= 0 {
		config.PollInterval = 250 * time.Millisecond
	}
	if config.BatchSize < 1 || config.BatchSize > 100 {
		config.BatchSize = 25
	}
	if config.GracePeriod < 0 {
		config.GracePeriod = 0
	}
	return &DeadlineWorker{repository: repository, clock: clock, config: config}
}

func (worker *DeadlineWorker) Run(ctx context.Context) {
	if !worker.config.Enabled {
		return
	}
	worker.mu.Lock()
	worker.started = true
	worker.mu.Unlock()
	worker.process(ctx)
	ticker := time.NewTicker(worker.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.process(ctx)
		}
	}
}

func (worker *DeadlineWorker) process(ctx context.Context) {
	now := worker.clock.Now()
	_, err := worker.repository.RevealDueRounds(ctx, now.Add(-worker.config.GracePeriod), now, worker.config.BatchSize)
	worker.mu.Lock()
	defer worker.mu.Unlock()
	worker.lastErr = err
	if err == nil {
		worker.lastOK = now
	}
}

func (worker *DeadlineWorker) Ready() error {
	if !worker.config.Enabled {
		return nil
	}
	worker.mu.RLock()
	defer worker.mu.RUnlock()
	if worker.repository == nil || !worker.started {
		return ErrDeadlineTransitionsUnavailable
	}
	if worker.lastErr != nil {
		return worker.lastErr
	}
	if worker.lastOK.IsZero() {
		return ErrDeadlineTransitionsUnavailable
	}
	if worker.clock.Now().Sub(worker.lastOK) > 3*worker.config.PollInterval {
		return ErrDeadlineTransitionsUnavailable
	}
	return nil
}
