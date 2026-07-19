package config

import (
	"testing"
	"time"
)

func TestLoadDeadlineTransitionConfiguration(t *testing.T) {
	t.Setenv("AUTO_REVEAL_ENABLED", "false")
	t.Setenv("AUTO_REVEAL_GRACE_SECONDS", "3")
	t.Setenv("TRANSITION_POLL_INTERVAL_MS", "400")
	t.Setenv("TRANSITION_BATCH_SIZE", "12")
	cfg := Load()
	if cfg.AutoRevealEnabled {
		t.Fatal("AUTO_REVEAL_ENABLED=false was not applied")
	}
	if cfg.AutoRevealGrace != 3*time.Second || cfg.TransitionPoll != 400*time.Millisecond || cfg.TransitionBatchSize != 12 {
		t.Fatalf("transition config = grace %s poll %s batch %d", cfg.AutoRevealGrace, cfg.TransitionPoll, cfg.TransitionBatchSize)
	}
}

func TestLoadDeadlineTransitionDefaultsRejectInvalidValues(t *testing.T) {
	t.Setenv("AUTO_REVEAL_GRACE_SECONDS", "-1")
	t.Setenv("TRANSITION_POLL_INTERVAL_MS", "0")
	t.Setenv("TRANSITION_BATCH_SIZE", "0")
	cfg := Load()
	if !cfg.AutoRevealEnabled || cfg.AutoRevealGrace != 0 || cfg.TransitionPoll != 250*time.Millisecond || cfg.TransitionBatchSize != 25 {
		t.Fatalf("default transition config = enabled %t grace %s poll %s batch %d",
			cfg.AutoRevealEnabled, cfg.AutoRevealGrace, cfg.TransitionPoll, cfg.TransitionBatchSize)
	}
}
