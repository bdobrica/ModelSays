package security

import (
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (clock *fakeClock) Now() time.Time { return clock.now }

func TestLimiterBoundaryResetIsolationAndBound(t *testing.T) {
	clock := &fakeClock{now: time.Unix(100, 0)}
	limiter := NewLimiter(clock, 2)
	policy := Policy{Limit: 2, Window: time.Minute}
	for index := 0; index < 2; index++ {
		if allowed, _ := limiter.Allow("a", policy); !allowed {
			t.Fatalf("request %d unexpectedly denied", index+1)
		}
	}
	if allowed, retry := limiter.Allow("a", policy); allowed || retry != time.Minute {
		t.Fatalf("boundary allowed=%v retry=%s", allowed, retry)
	}
	if allowed, _ := limiter.Allow("b", policy); !allowed {
		t.Fatal("isolated key was denied")
	}
	_ = mustAllow(limiter, "c", policy)
	if len(limiter.entries) > 2 {
		t.Fatalf("entries grew beyond bound: %d", len(limiter.entries))
	}
	clock.now = clock.now.Add(time.Minute)
	if allowed, retry := limiter.Allow("a", policy); !allowed || retry != 0 {
		t.Fatalf("window did not reset: allowed=%v retry=%s", allowed, retry)
	}
}

func TestLimiterConcurrentAttemptsHonorExactCapacity(t *testing.T) {
	limiter := NewLimiter(nil, 100)
	policy := Policy{Limit: 12, Window: time.Minute}
	var allowed atomic.Int64
	var group sync.WaitGroup
	for index := 0; index < 100; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if ok, _ := limiter.Allow("shared-room", policy); ok {
				allowed.Add(1)
			}
		}()
	}
	group.Wait()
	if allowed.Load() != 12 {
		t.Fatalf("allowed %d concurrent attempts, want 12", allowed.Load())
	}
}

func mustAllow(limiter *Limiter, key string, policy Policy) bool {
	allowed, _ := limiter.Allow(key, policy)
	return allowed
}

func TestClientKeyTrustsOnlyConfiguredProxyChain(t *testing.T) {
	config := DefaultConfig()
	config.TrustedProxyCIDRs = []string{"10.0.0.0/8"}
	controller := NewControllerWithClock(config, nil, []byte("stable-test-salt"))

	direct := httptest.NewRequest("GET", "/", nil)
	direct.RemoteAddr = "203.0.113.7:1234"
	direct.Header.Set("X-Forwarded-For", "198.51.100.2")
	spoofed := controller.ClientKey(direct)
	direct.Header.Del("X-Forwarded-For")
	if spoofed != controller.ClientKey(direct) {
		t.Fatal("untrusted peer changed identity with X-Forwarded-For")
	}

	proxied := httptest.NewRequest("GET", "/", nil)
	proxied.RemoteAddr = "10.0.0.5:443"
	proxied.Header.Set("X-Forwarded-For", "198.51.100.2, 10.0.0.4")
	clientKey := controller.ClientKey(proxied)
	proxied.Header.Set("X-Forwarded-For", "198.51.100.3, 10.0.0.4")
	if clientKey == controller.ClientKey(proxied) {
		t.Fatal("trusted proxy did not use first untrusted client address")
	}

	proxied.Header.Set("X-Forwarded-For", "not-an-ip")
	malformed := controller.ClientKey(proxied)
	proxied.Header.Del("X-Forwarded-For")
	if malformed != controller.ClientKey(proxied) {
		t.Fatal("malformed proxy header was not ignored")
	}
}

func TestProviderCircuitAndModeration(t *testing.T) {
	config := DefaultConfig()
	config.ProviderGlobal = Policy{Limit: 2, Window: time.Hour}
	config.ProviderRoom = Policy{Limit: 1, Window: time.Hour}
	controller := NewControllerWithClock(config, nil, []byte("salt"))
	if allowed, _ := controller.AllowProvider("ROOMAA"); !allowed {
		t.Fatal("first provider call denied")
	}
	if allowed, _ := controller.AllowProvider("ROOMAA"); allowed {
		t.Fatal("room circuit did not open")
	}
	if !controller.Moderated("that slur is nigger") || controller.Moderated("nightingale") {
		t.Fatal("moderation did not use exact normalized words")
	}
}

func TestDefaultPoliciesAdmitRepresentativeTwelvePlayerParty(t *testing.T) {
	controller := NewControllerWithClock(DefaultConfig(), nil, []byte("salt"))
	cfg := controller.Config()
	client := "shared-party-network"
	room := "ROOMAA"
	for player := 0; player < 12; player++ {
		if allowed, _ := controller.Allow("join-ip", client, cfg.JoinIP); !allowed {
			t.Fatalf("join %d denied by client policy", player+1)
		}
		if allowed, _ := controller.Allow("join-room", room, cfg.JoinRoom); !allowed {
			t.Fatalf("join %d denied by room policy", player+1)
		}
		for poll := 0; poll < 20; poll++ {
			if allowed, _ := controller.Allow("lookup", client, cfg.Lookup); !allowed {
				t.Fatalf("player %d poll %d denied", player+1, poll+1)
			}
		}
		if allowed, _ := controller.Allow("guess-room", room, cfg.GuessRoom); !allowed {
			t.Fatalf("guess %d denied by room policy", player+1)
		}
	}
	if len(controller.limiter.entries) > cfg.MaxKeys {
		t.Fatalf("limiter entries = %d, max = %d", len(controller.limiter.entries), cfg.MaxKeys)
	}
}
