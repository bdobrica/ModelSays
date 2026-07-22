package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/llm"
	"github.com/bogdandobrica/modelsays/backend/internal/security"
)

type Config struct {
	AppEnv                string
	DatabaseURL           string
	OpenAIAPIKey          string
	HTTPAddr              string
	CORSAllowedOrigins    []string
	DefaultModels         DefaultModels
	ModelPolicy           llm.Policy
	EventPollInterval     time.Duration
	EventHeartbeat        time.Duration
	EventMaxConnections   int
	EventWriteTimeout     time.Duration
	AutoRevealEnabled     bool
	AutoRevealGrace       time.Duration
	TransitionPoll        time.Duration
	TransitionBatchSize   int
	LivingRoomRevealPause time.Duration
	Abuse                 security.Config
	MetricsToken          string
	ShutdownTimeout       time.Duration
}

type DefaultModels struct {
	Prediction string
	Question   string
	Judge      string
}

func Load() Config {
	defaultPolicy := llm.DefaultPolicy()
	defaultAbuse := security.DefaultConfig()
	return Config{
		AppEnv:                getEnv("APP_ENV", "development"),
		DatabaseURL:           getEnv("DATABASE_URL", ""),
		OpenAIAPIKey:          getEnv("OPENAI_API_KEY", ""),
		HTTPAddr:              getEnv("HTTP_ADDR", ":8080"),
		CORSAllowedOrigins:    splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:5173")),
		EventPollInterval:     time.Duration(getEnvInt("EVENT_POLL_INTERVAL_MS", 250)) * time.Millisecond,
		EventHeartbeat:        time.Duration(getEnvInt("EVENT_HEARTBEAT_SECONDS", 15)) * time.Second,
		EventMaxConnections:   getEnvInt("EVENT_MAX_CONNECTIONS", 100),
		EventWriteTimeout:     time.Duration(getEnvInt("EVENT_WRITE_TIMEOUT_SECONDS", 5)) * time.Second,
		AutoRevealEnabled:     !strings.EqualFold(getEnv("AUTO_REVEAL_ENABLED", "true"), "false"),
		AutoRevealGrace:       time.Duration(getEnvNonNegativeInt("AUTO_REVEAL_GRACE_SECONDS", 0)) * time.Second,
		TransitionPoll:        time.Duration(getEnvInt("TRANSITION_POLL_INTERVAL_MS", 250)) * time.Millisecond,
		TransitionBatchSize:   getEnvInt("TRANSITION_BATCH_SIZE", 25),
		LivingRoomRevealPause: time.Duration(getEnvInt("LIVINGROOM_REVEAL_PAUSE_SECONDS", 8)) * time.Second,
		MetricsToken:          getEnv("METRICS_TOKEN", ""),
		ShutdownTimeout:       time.Duration(getEnvInt("SHUTDOWN_TIMEOUT_SECONDS", 15)) * time.Second,
		Abuse: security.Config{
			TrustedProxyCIDRs:   splitCSV(getEnv("TRUSTED_PROXY_CIDRS", "")),
			MaxKeys:             getEnvInt("RATE_LIMIT_MAX_KEYS", defaultAbuse.MaxKeys),
			IP:                  ratePolicy("RATE_LIMIT_IP", defaultAbuse.IP),
			Create:              ratePolicy("RATE_LIMIT_CREATE", defaultAbuse.Create),
			Lookup:              ratePolicy("RATE_LIMIT_LOOKUP", defaultAbuse.Lookup),
			JoinIP:              ratePolicy("RATE_LIMIT_JOIN_IP", defaultAbuse.JoinIP),
			JoinRoom:            ratePolicy("RATE_LIMIT_JOIN_ROOM", defaultAbuse.JoinRoom),
			PlayerAction:        ratePolicy("RATE_LIMIT_PLAYER_ACTION", defaultAbuse.PlayerAction),
			GuessPlayer:         ratePolicy("RATE_LIMIT_GUESS_PLAYER", defaultAbuse.GuessPlayer),
			GuessRoom:           ratePolicy("RATE_LIMIT_GUESS_ROOM", defaultAbuse.GuessRoom),
			EventIP:             ratePolicy("RATE_LIMIT_EVENT_IP", defaultAbuse.EventIP),
			EventRoom:           ratePolicy("RATE_LIMIT_EVENT_ROOM", defaultAbuse.EventRoom),
			ProviderGlobal:      ratePolicy("PROVIDER_LIMIT_GLOBAL", defaultAbuse.ProviderGlobal),
			ProviderRoom:        ratePolicy("PROVIDER_LIMIT_ROOM", defaultAbuse.ProviderRoom),
			ModerationDenyWords: splitCSV(getEnv("MODERATION_DENY_WORDS", strings.Join(defaultAbuse.ModerationDenyWords, ","))),
		},
		DefaultModels: DefaultModels{
			Prediction: getEnv("DEFAULT_PREDICTION_MODEL", "gpt-5.6-luna"),
			Question:   getEnv("DEFAULT_QUESTION_MODEL", "gpt-5.6-luna"),
			Judge:      getEnv("DEFAULT_JUDGE_MODEL", "gpt-5.6-luna"),
		},
		ModelPolicy: llm.Policy{
			AllowedQuestionModels:   splitCSV(getEnv("ALLOWED_QUESTION_MODELS", "gpt-5.6-luna")),
			AllowedPredictionModels: splitCSV(getEnv("ALLOWED_PREDICTION_MODELS", "gpt-5.6-luna")),
			AllowedJudgeModels:      splitCSV(getEnv("ALLOWED_JUDGE_MODELS", "gpt-5.6-luna")),
			Timeout:                 time.Duration(getEnvInt("MODEL_TIMEOUT_SECONDS", int(defaultPolicy.Timeout/time.Second))) * time.Second,
			MaxAttempts:             getEnvInt("MODEL_MAX_ATTEMPTS", defaultPolicy.MaxAttempts),
			MaxCallsPerGame:         getEnvInt("MODEL_MAX_CALLS_PER_GAME", defaultPolicy.MaxCallsPerGame),
			MaxEstimatedCostUSD:     getEnvFloat("MODEL_MAX_COST_USD_PER_GAME", defaultPolicy.MaxEstimatedCostUSD),
			CaptureRawResponses:     strings.EqualFold(getEnv("MODEL_CAPTURE_RAW_RESPONSES", "false"), "true"),
			MaxRawResponseBytes:     getEnvInt("MODEL_RAW_RESPONSE_MAX_BYTES", defaultPolicy.MaxRawResponseBytes),
		}.Normalize(),
	}
}

func (config Config) Validate() error {
	if config.AppEnv != "development" && config.AppEnv != "test" && config.AppEnv != "production" {
		return fmt.Errorf("APP_ENV must be development, test, or production")
	}
	if config.LivingRoomRevealPause < 3*time.Second || config.LivingRoomRevealPause > 30*time.Second {
		return fmt.Errorf("LIVINGROOM_REVEAL_PAUSE_SECONDS must be between 3 and 30")
	}
	if len(config.CORSAllowedOrigins) == 0 {
		return fmt.Errorf("CORS_ALLOWED_ORIGINS must contain at least one origin")
	}
	for _, origin := range config.CORSAllowedOrigins {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Path != "" ||
			parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return fmt.Errorf("CORS_ALLOWED_ORIGINS contains invalid origin %q", origin)
		}
	}
	if config.AppEnv == "production" {
		if config.DatabaseURL == "" {
			return fmt.Errorf("DATABASE_URL is required in production")
		}
		if config.MetricsToken == "" {
			return fmt.Errorf("METRICS_TOKEN is required in production")
		}
		if config.OpenAIAPIKey != "" && config.ModelPolicy.CaptureRawResponses {
			return fmt.Errorf("MODEL_CAPTURE_RAW_RESPONSES must be false in production")
		}
	}
	return nil
}

func ratePolicy(prefix string, fallback security.Policy) security.Policy {
	return security.Policy{
		Limit:  getEnvInt(prefix+"_REQUESTS", fallback.Limit),
		Window: time.Duration(getEnvInt(prefix+"_WINDOW_SECONDS", int(fallback.Window/time.Second))) * time.Second,
	}
}

func getEnvNonNegativeInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value < 0 {
		return fallback
	}
	return value
}

func getEnvInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func getEnvFloat(key string, fallback float64) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv(key)), 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func getEnv(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}

	return fallback
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			origins = append(origins, trimmed)
		}
	}

	return origins
}
