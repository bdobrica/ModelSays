package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrModelNotAllowed = errors.New("model is not allowed")
var ErrBudgetExhausted = errors.New("provider budget exhausted")

type Policy struct {
	AllowedQuestionModels   []string
	AllowedPredictionModels []string
	Timeout                 time.Duration
	MaxAttempts             int
	MaxCallsPerGame         int
	MaxEstimatedCostUSD     float64
	CaptureRawResponses     bool
	MaxRawResponseBytes     int
}

func DefaultPolicy() Policy {
	return Policy{
		AllowedQuestionModels:   []string{"gpt-4.1-mini"},
		AllowedPredictionModels: []string{"gpt-4.1-mini"},
		Timeout:                 10 * time.Second,
		MaxAttempts:             2,
		MaxCallsPerGame:         20,
		MaxEstimatedCostUSD:     0.10,
		MaxRawResponseBytes:     4096,
	}
}

func (policy Policy) Normalize() Policy {
	defaults := DefaultPolicy()
	if policy.Timeout <= 0 {
		policy.Timeout = defaults.Timeout
	}
	if policy.MaxAttempts < 1 {
		policy.MaxAttempts = defaults.MaxAttempts
	}
	if policy.MaxCallsPerGame < 1 {
		policy.MaxCallsPerGame = defaults.MaxCallsPerGame
	}
	if policy.MaxEstimatedCostUSD <= 0 {
		policy.MaxEstimatedCostUSD = defaults.MaxEstimatedCostUSD
	}
	if policy.MaxRawResponseBytes < 1 {
		policy.MaxRawResponseBytes = defaults.MaxRawResponseBytes
	}
	return policy
}

func (policy Policy) AllowsQuestion(model string) bool {
	return containsModel(policy.AllowedQuestionModels, model)
}

func (policy Policy) AllowsPrediction(model string) bool {
	return containsModel(policy.AllowedPredictionModels, model)
}

func containsModel(allowed []string, model string) bool {
	for _, candidate := range allowed {
		if strings.EqualFold(strings.TrimSpace(candidate), strings.TrimSpace(model)) {
			return true
		}
	}
	return false
}

func WithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

type CallMetadata struct {
	Provider         string
	Model            string
	PromptVersion    string
	RequestID        string
	InputTokens      int
	OutputTokens     int
	EstimatedCostUSD float64
	RawResponse      string
}

type CallError struct {
	Metadata CallMetadata
	Err      error
}

func (err *CallError) Error() string { return err.Err.Error() }
func (err *CallError) Unwrap() error { return err.Err }

func MetadataFromError(err error) CallMetadata {
	var callErr *CallError
	if errors.As(err, &callErr) {
		return callErr.Metadata
	}
	return CallMetadata{}
}

func RedactRawResponse(raw string, maxBytes int) string {
	raw = strings.ReplaceAll(raw, "\x00", "")
	for _, marker := range []string{"sk-", "Bearer "} {
		for {
			start := strings.Index(raw, marker)
			if start < 0 {
				break
			}
			end := start + len(marker)
			for end < len(raw) && raw[end] != ' ' && raw[end] != '"' && raw[end] != '\n' {
				end++
			}
			raw = raw[:start] + "[REDACTED]" + raw[end:]
		}
	}
	if len(raw) > maxBytes {
		raw = raw[:maxBytes] + fmt.Sprintf("...[truncated %d bytes]", len(raw)-maxBytes)
	}
	return raw
}
