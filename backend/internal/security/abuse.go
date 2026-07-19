package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Clock interface{ Now() time.Time }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

type Policy struct {
	Limit  int
	Window time.Duration
}

type Config struct {
	TrustedProxyCIDRs   []string
	MaxKeys             int
	IP                  Policy
	Create              Policy
	Lookup              Policy
	JoinIP              Policy
	JoinRoom            Policy
	PlayerAction        Policy
	GuessPlayer         Policy
	GuessRoom           Policy
	EventIP             Policy
	EventRoom           Policy
	ProviderGlobal      Policy
	ProviderRoom        Policy
	ModerationDenyWords []string
}

func DefaultConfig() Config {
	return Config{
		MaxKeys:             10000,
		IP:                  Policy{Limit: 600, Window: time.Minute},
		Create:              Policy{Limit: 10, Window: time.Minute},
		Lookup:              Policy{Limit: 360, Window: time.Minute},
		JoinIP:              Policy{Limit: 30, Window: time.Minute},
		JoinRoom:            Policy{Limit: 30, Window: time.Minute},
		PlayerAction:        Policy{Limit: 60, Window: time.Minute},
		GuessPlayer:         Policy{Limit: 12, Window: 10 * time.Second},
		GuessRoom:           Policy{Limit: 180, Window: time.Minute},
		EventIP:             Policy{Limit: 20, Window: time.Minute},
		EventRoom:           Policy{Limit: 20, Window: time.Minute},
		ProviderGlobal:      Policy{Limit: 120, Window: time.Hour},
		ProviderRoom:        Policy{Limit: 20, Window: time.Hour},
		ModerationDenyWords: []string{"nigger", "faggot", "kike"},
	}
}

type entry struct {
	start time.Time
	count int
}

type Limiter struct {
	mu      sync.Mutex
	clock   Clock
	maxKeys int
	entries map[string]entry
}

func NewLimiter(clock Clock, maxKeys int) *Limiter {
	if clock == nil {
		clock = realClock{}
	}
	if maxKeys < 1 {
		maxKeys = 10000
	}
	return &Limiter{clock: clock, maxKeys: maxKeys, entries: make(map[string]entry)}
}

func (limiter *Limiter) Allow(key string, policy Policy) (bool, time.Duration) {
	if policy.Limit < 1 || policy.Window <= 0 {
		return true, 0
	}
	now := limiter.clock.Now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	current, exists := limiter.entries[key]
	if !exists || !now.Before(current.start.Add(policy.Window)) {
		if len(limiter.entries) >= limiter.maxKeys {
			limiter.evictExpiredOrOldest(now)
		}
		limiter.entries[key] = entry{start: now, count: 1}
		return true, 0
	}
	if current.count >= policy.Limit {
		return false, current.start.Add(policy.Window).Sub(now)
	}
	current.count++
	limiter.entries[key] = current
	return true, 0
}

func (limiter *Limiter) evictExpiredOrOldest(now time.Time) {
	var oldestKey string
	var oldest time.Time
	for key, current := range limiter.entries {
		if !now.Before(current.start.Add(24 * time.Hour)) {
			delete(limiter.entries, key)
			return
		}
		if oldestKey == "" || current.start.Before(oldest) {
			oldestKey, oldest = key, current.start
		}
	}
	delete(limiter.entries, oldestKey)
}

type Controller struct {
	config  Config
	limiter *Limiter
	trusted []*net.IPNet
	salt    []byte
}

func NewController(config Config) *Controller {
	return NewControllerWithClock(config, nil, nil)
}

func NewControllerWithClock(config Config, clock Clock, salt []byte) *Controller {
	defaults := DefaultConfig()
	config = normalize(config, defaults)
	trusted := make([]*net.IPNet, 0, len(config.TrustedProxyCIDRs))
	for _, value := range config.TrustedProxyCIDRs {
		_, network, err := net.ParseCIDR(strings.TrimSpace(value))
		if err == nil {
			trusted = append(trusted, network)
		}
	}
	if len(salt) == 0 {
		salt = make([]byte, 32)
		_, _ = rand.Read(salt)
	}
	return &Controller{config: config, limiter: NewLimiter(clock, config.MaxKeys), trusted: trusted, salt: salt}
}

func normalize(config Config, defaults Config) Config {
	if config.MaxKeys < 1 {
		config.MaxKeys = defaults.MaxKeys
	}
	policies := []*Policy{
		&config.IP, &config.Create, &config.Lookup, &config.JoinIP, &config.JoinRoom,
		&config.PlayerAction, &config.GuessPlayer, &config.GuessRoom, &config.EventIP,
		&config.EventRoom, &config.ProviderGlobal, &config.ProviderRoom,
	}
	defaultPolicies := []Policy{
		defaults.IP, defaults.Create, defaults.Lookup, defaults.JoinIP, defaults.JoinRoom,
		defaults.PlayerAction, defaults.GuessPlayer, defaults.GuessRoom, defaults.EventIP,
		defaults.EventRoom, defaults.ProviderGlobal, defaults.ProviderRoom,
	}
	for index, policy := range policies {
		if policy.Limit < 1 || policy.Window <= 0 {
			*policy = defaultPolicies[index]
		}
	}
	if len(config.ModerationDenyWords) == 0 {
		config.ModerationDenyWords = defaults.ModerationDenyWords
	}
	return config
}

func (controller *Controller) ClientKey(request *http.Request) string {
	peer := remoteIP(request.RemoteAddr)
	client := peer
	if controller.isTrusted(peer) {
		if forwarded, ok := parseForwardedFor(request.Header.Get("X-Forwarded-For")); ok {
			chain := append(forwarded, peer)
			for index := len(chain) - 1; index >= 0; index-- {
				if !controller.isTrusted(chain[index]) {
					client = chain[index]
					break
				}
			}
		}
	}
	sum := sha256.Sum256(append(append([]byte(nil), controller.salt...), []byte(client.String())...))
	return hex.EncodeToString(sum[:16])
}

func remoteIP(remote string) net.IP {
	host, _, err := net.SplitHostPort(remote)
	if err == nil {
		return net.ParseIP(host)
	}
	return net.ParseIP(remote)
}

func parseForwardedFor(value string) ([]net.IP, bool) {
	if strings.TrimSpace(value) == "" {
		return nil, false
	}
	parts := strings.Split(value, ",")
	result := make([]net.IP, 0, len(parts))
	for _, part := range parts {
		ip := net.ParseIP(strings.TrimSpace(part))
		if ip == nil {
			return nil, false
		}
		result = append(result, ip)
	}
	return result, true
}

func (controller *Controller) isTrusted(ip net.IP) bool {
	if ip == nil {
		return false
	}
	for _, network := range controller.trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func (controller *Controller) Allow(scope, key string, policy Policy) (bool, time.Duration) {
	return controller.limiter.Allow(scope+":"+key, policy)
}

func (controller *Controller) Config() Config { return controller.config }

func (controller *Controller) AllowProvider(roomCode string) (bool, time.Duration) {
	if allowed, retry := controller.Allow("provider-room", strings.ToUpper(strings.TrimSpace(roomCode)), controller.config.ProviderRoom); !allowed {
		return false, retry
	}
	return controller.Allow("provider-global", "all", controller.config.ProviderGlobal)
}

func (controller *Controller) Moderated(value string) bool {
	words := strings.FieldsFunc(strings.ToLower(value), func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	})
	for _, word := range words {
		for _, denied := range controller.config.ModerationDenyWords {
			if word == strings.ToLower(strings.TrimSpace(denied)) {
				return true
			}
		}
	}
	return false
}
