package llm

import (
	"strings"
	"testing"
)

func TestPolicyAllowlistAndDefaults(t *testing.T) {
	policy := (Policy{AllowedPredictionModels: []string{"approved"}, AllowedJudgeModels: []string{"judge"}}).Normalize()
	if !policy.AllowsPrediction("approved") || policy.AllowsPrediction("expensive") {
		t.Fatal("prediction allowlist did not enforce exact configured models")
	}
	if !policy.AllowsJudge("judge") || policy.AllowsJudge("unapproved") {
		t.Fatal("judge allowlist did not enforce exact configured models")
	}
	if policy.Timeout <= 0 || policy.MaxAttempts != 2 || policy.MaxCallsPerGame <= 0 {
		t.Fatalf("policy defaults were not applied: %#v", policy)
	}
}

func TestRedactRawResponse(t *testing.T) {
	raw := `{"authorization":"Bearer secret-value","key":"sk-secret","answer":"hidden"}`
	redacted := RedactRawResponse(raw, 48)
	if strings.Contains(redacted, "secret-value") || strings.Contains(redacted, "sk-secret") {
		t.Fatalf("redaction leaked a secret: %q", redacted)
	}
	if len(redacted) <= 48 || !strings.Contains(redacted, "truncated") {
		t.Fatalf("expected explicit bounded truncation, got %q", redacted)
	}
}
