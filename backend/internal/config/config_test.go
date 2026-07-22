package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDeadlineTransitionConfiguration(t *testing.T) {
	t.Setenv("AUTO_REVEAL_ENABLED", "false")
	t.Setenv("AUTO_REVEAL_GRACE_SECONDS", "3")
	t.Setenv("TRANSITION_POLL_INTERVAL_MS", "400")
	t.Setenv("TRANSITION_BATCH_SIZE", "12")
	t.Setenv("LIVINGROOM_REVEAL_PAUSE_SECONDS", "9")
	cfg := Load()
	if cfg.AutoRevealEnabled {
		t.Fatal("AUTO_REVEAL_ENABLED=false was not applied")
	}
	if cfg.AutoRevealGrace != 3*time.Second || cfg.TransitionPoll != 400*time.Millisecond || cfg.TransitionBatchSize != 12 {
		t.Fatalf("transition config = grace %s poll %s batch %d", cfg.AutoRevealGrace, cfg.TransitionPoll, cfg.TransitionBatchSize)
	}
	if cfg.LivingRoomRevealPause != 9*time.Second {
		t.Fatalf("living-room reveal pause = %s", cfg.LivingRoomRevealPause)
	}
	cfg.LivingRoomRevealPause = 2 * time.Second
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "LIVINGROOM_REVEAL_PAUSE_SECONDS") {
		t.Fatalf("invalid living-room reveal pause validation = %v", err)
	}
}

func TestProductionConfigurationValidation(t *testing.T) {
	cfg := Load()
	cfg.AppEnv = "production"
	cfg.DatabaseURL = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Fatalf("Validate() = %v, want DATABASE_URL failure", err)
	}
	cfg.DatabaseURL = "postgres://example"
	cfg.MetricsToken = "secret"
	cfg.ModelPolicy.CaptureRawResponses = true
	cfg.OpenAIAPIKey = "secret"
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "MODEL_CAPTURE") {
		t.Fatalf("Validate() = %v, want raw response failure", err)
	}
	cfg.ModelPolicy.CaptureRawResponses = false
	if err := cfg.Validate(); err != nil {
		t.Fatalf("valid production config: %v", err)
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

func TestLoadAbuseConfiguration(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "10.0.0.0/8,192.0.2.10/32")
	t.Setenv("RATE_LIMIT_CREATE_REQUESTS", "7")
	t.Setenv("RATE_LIMIT_CREATE_WINDOW_SECONDS", "90")
	t.Setenv("PROVIDER_LIMIT_GLOBAL_REQUESTS", "50")
	t.Setenv("MODERATION_DENY_WORDS", "blocked,denied")
	cfg := Load()
	if len(cfg.Abuse.TrustedProxyCIDRs) != 2 || cfg.Abuse.Create.Limit != 7 || cfg.Abuse.Create.Window != 90*time.Second {
		t.Fatalf("abuse config = %#v", cfg.Abuse)
	}
	if cfg.Abuse.ProviderGlobal.Limit != 50 || len(cfg.Abuse.ModerationDenyWords) != 2 {
		t.Fatalf("provider/moderation config = %#v", cfg.Abuse)
	}
}
