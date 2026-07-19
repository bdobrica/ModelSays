package game

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type recordingDueRepository struct {
	mu      sync.Mutex
	dueAt   []time.Time
	result  int
	err     error
	block   chan struct{}
	entered chan struct{}
}

func (repository *recordingDueRepository) RevealDueRounds(_ context.Context, dueAt, _ time.Time, _ int) (int, error) {
	if repository.entered != nil {
		close(repository.entered)
	}
	if repository.block != nil {
		<-repository.block
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.dueAt = append(repository.dueAt, dueAt)
	return repository.result, repository.err
}

func TestDeadlineWorkerAppliesGraceBoundaryAndReadiness(t *testing.T) {
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: now}
	repository := &recordingDueRepository{}
	worker := NewDeadlineWorker(repository, clock, DeadlineWorkerConfig{
		Enabled: true, GracePeriod: 2 * time.Second, PollInterval: time.Second, BatchSize: 10,
	})
	if err := worker.Ready(); !errors.Is(err, ErrDeadlineTransitionsUnavailable) {
		t.Fatalf("Ready before processing = %v, want unavailable", err)
	}
	worker.mu.Lock()
	worker.started = true
	worker.mu.Unlock()
	worker.process(context.Background())
	if got := repository.dueAt[0]; !got.Equal(now.Add(-2 * time.Second)) {
		t.Fatalf("due boundary = %s, want %s", got, now.Add(-2*time.Second))
	}
	if err := worker.Ready(); err != nil {
		t.Fatalf("Ready after successful processing = %v", err)
	}
	clock.Set(now.Add(4 * time.Second))
	if err := worker.Ready(); !errors.Is(err, ErrDeadlineTransitionsUnavailable) {
		t.Fatalf("stale Ready = %v, want unavailable", err)
	}
}

func TestDeadlineWorkerDisableSwitchNeedsNoRepository(t *testing.T) {
	worker := NewDeadlineWorker(nil, nil, DeadlineWorkerConfig{Enabled: false})
	if err := worker.Ready(); err != nil {
		t.Fatalf("disabled worker Ready = %v", err)
	}
	worker.Run(context.Background())
}

func TestDeadlineWorkerReportsRepositoryFailure(t *testing.T) {
	expected := errors.New("database unavailable")
	worker := NewDeadlineWorker(&recordingDueRepository{err: expected}, &fakeClock{now: time.Now().UTC()},
		DeadlineWorkerConfig{Enabled: true})
	worker.mu.Lock()
	worker.started = true
	worker.mu.Unlock()
	worker.process(context.Background())
	if !errors.Is(worker.Ready(), expected) {
		t.Fatalf("Ready = %v, want %v", worker.Ready(), expected)
	}
}
