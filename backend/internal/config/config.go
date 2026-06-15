package config

import (
	"os"
	"strings"
)

type Config struct {
	AppEnv             string
	DatabaseURL        string
	OpenAIAPIKey       string
	HTTPAddr           string
	CORSAllowedOrigins []string
	DefaultModels      DefaultModels
}

type DefaultModels struct {
	Prediction string
	Judge      string
	Question   string
}

func Load() Config {
	return Config{
		AppEnv:             getEnv("APP_ENV", "development"),
		DatabaseURL:        getEnv("DATABASE_URL", ""),
		OpenAIAPIKey:       getEnv("OPENAI_API_KEY", ""),
		HTTPAddr:           getEnv("HTTP_ADDR", ":8080"),
		CORSAllowedOrigins: splitCSV(getEnv("CORS_ALLOWED_ORIGINS", "http://localhost:5173")),
		DefaultModels: DefaultModels{
			Prediction: getEnv("DEFAULT_PREDICTION_MODEL", "gpt-4.1-mini"),
			Judge:      getEnv("DEFAULT_JUDGE_MODEL", "gpt-4.1"),
			Question:   getEnv("DEFAULT_QUESTION_MODEL", "gpt-4.1-mini"),
		},
	}
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
