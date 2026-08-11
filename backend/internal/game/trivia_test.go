package game

import (
	"strings"
	"testing"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

func validTriviaContent(kind models.GameKind) models.TriviaContent {
	content := models.TriviaContent{
		Version: models.TriviaContentVersion, Kind: kind, CanonicalAnswer: "Paris",
		AcceptedAliases: []string{"City of Paris"}, BaseScore: 100,
		Explanation: "Paris is the capital of France.", Source: "Reviewed reference",
	}
	if kind == models.GameKindTriviaChoice {
		content.Options = []models.TriviaOption{{ID: "opt-a", Label: "Paris"}, {ID: "opt-b", Label: "Lyon"}, {ID: "opt-c", Label: "Nice"}, {ID: "opt-d", Label: "Lille"}}
		content.CorrectOptionID = "opt-a"
	}
	content.IntegrityHash = ComputeTriviaContentHash(content)
	return content
}

func TestResolveOpenTriviaGuessUsesOnlyFrozenNormalizedAnswers(t *testing.T) {
	content := validTriviaContent(models.GameKindTriviaOpen)
	tests := []struct {
		answer  string
		correct bool
	}{
		{"PARIS!!!", true}, {"  City\tof Paris ", true}, {"París", false}, {"Paris France", false}, {"Lyon", false},
	}
	for _, test := range tests {
		guess, err := ResolveTriviaGuess(GuessSubmission{ID: "g", PlayerID: "p", RawAnswer: test.answer, CreatedAt: time.Now()}, &content)
		if err != nil || guess.Correct == nil || *guess.Correct != test.correct {
			t.Fatalf("answer %q: guess=%#v err=%v", test.answer, guess, err)
		}
		wantScore := 0
		if test.correct {
			wantScore = content.BaseScore
		}
		if guess.ScoreAwarded != wantScore || guess.Duplicate {
			t.Fatalf("answer %q score=%d duplicate=%v", test.answer, guess.ScoreAwarded, guess.Duplicate)
		}
	}
}

func TestResolveChoiceTriviaGuessRequiresFrozenOptionID(t *testing.T) {
	content := validTriviaContent(models.GameKindTriviaChoice)
	for _, test := range []struct {
		id      string
		correct bool
	}{{"opt-a", true}, {"opt-b", false}} {
		guess, err := ResolveTriviaGuess(GuessSubmission{ID: "g", PlayerID: "p", SelectedOptionID: test.id}, &content)
		if err != nil || guess.Correct == nil || *guess.Correct != test.correct || guess.Duplicate {
			t.Fatalf("option %q: %#v err=%v", test.id, guess, err)
		}
	}
	if _, err := ResolveTriviaGuess(GuessSubmission{SelectedOptionID: "Paris"}, &content); err != ErrAnswerInvalid {
		t.Fatalf("label error = %v", err)
	}
	if _, err := ResolveTriviaGuess(GuessSubmission{SelectedOptionID: "unknown"}, &content); err != ErrAnswerInvalid {
		t.Fatalf("unknown error = %v", err)
	}
}

func TestResolveTriviaOverrideCreatesAuditableDeltaWithoutChangingContent(t *testing.T) {
	content := validTriviaContent(models.GameKindTriviaOpen)
	wrong := false
	guess := models.Guess{ID: "g", PlayerID: "p", Correct: &wrong}
	correct := true
	updated, delta, err := ResolveTriviaOverride(GuessOverride{GuessID: "g", Correct: &correct}, &content, []models.Guess{guess})
	if err != nil || delta != content.BaseScore || updated.Correct == nil || !*updated.Correct {
		t.Fatalf("override=%#v delta=%d err=%v", updated, delta, err)
	}
	if content.CanonicalAnswer != "Paris" {
		t.Fatal("override changed frozen content")
	}
}

func TestValidateTriviaContentBoundaries(t *testing.T) {
	for _, kind := range []models.GameKind{models.GameKindTriviaOpen, models.GameKindTriviaChoice} {
		if err := ValidateTriviaContent(validTriviaContent(kind)); err != nil {
			t.Fatalf("valid %s: %v", kind, err)
		}
	}
	tests := []struct {
		name   string
		mutate func(*models.TriviaContent)
	}{
		{"version", func(c *models.TriviaContent) { c.Version = 2 }},
		{"kind", func(c *models.TriviaContent) { c.Kind = models.GameKindModelSays }},
		{"empty canonical", func(c *models.TriviaContent) { c.CanonicalAnswer = " " }},
		{"long canonical", func(c *models.TriviaContent) { c.CanonicalAnswer = strings.Repeat("x", maxTriviaAnswerRunes+1) }},
		{"zero score", func(c *models.TriviaContent) { c.BaseScore = 0 }},
		{"too many aliases", func(c *models.TriviaContent) { c.AcceptedAliases = make([]string, maxTriviaAliases+1) }},
		{"duplicate alias", func(c *models.TriviaContent) { c.AcceptedAliases = []string{" PARIS! "} }},
		{"long explanation", func(c *models.TriviaContent) { c.Explanation = strings.Repeat("x", maxTriviaExplanationRunes+1) }},
		{"long source", func(c *models.TriviaContent) { c.Source = strings.Repeat("x", maxTriviaSourceRunes+1) }},
		{"three options", func(c *models.TriviaContent) { c.Options = c.Options[:3] }},
		{"empty option", func(c *models.TriviaContent) { c.Options[0].Label = " " }},
		{"duplicate option ID", func(c *models.TriviaContent) { c.Options[1].ID = c.Options[0].ID }},
		{"ambiguous labels", func(c *models.TriviaContent) { c.Options[1].Label = " PARIS!" }},
		{"unknown correct ID", func(c *models.TriviaContent) { c.CorrectOptionID = "unknown" }},
		{"open with options", func(c *models.TriviaContent) { c.Kind = models.GameKindTriviaOpen }},
		{"tampered hash", func(c *models.TriviaContent) { c.CanonicalAnswer = "Rome" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			content := validTriviaContent(models.GameKindTriviaChoice)
			test.mutate(&content)
			if test.name != "tampered hash" {
				content.IntegrityHash = ComputeTriviaContentHash(content)
			}
			if err := ValidateTriviaContent(content); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestProjectTriviaContentKeepsSolutionsPrivateUntilReveal(t *testing.T) {
	content := validTriviaContent(models.GameKindTriviaChoice)
	answering := ProjectTriviaContent(&content, false)
	if answering.CanonicalAnswer != "" || answering.CorrectOptionID != "" || len(answering.AcceptedAliases) != 0 || answering.Explanation != "" || answering.Source != "" {
		t.Fatalf("answering projection leaked solution: %#v", answering)
	}
	if len(answering.Options) != 4 || answering.Options[0].ID != "opt-a" {
		t.Fatalf("answering options lost: %#v", answering)
	}
	revealed := ProjectTriviaContent(&content, true)
	if revealed.CanonicalAnswer != "Paris" || revealed.CorrectOptionID != "opt-a" || len(revealed.AcceptedAliases) != 1 {
		t.Fatalf("reveal missing solution: %#v", revealed)
	}
}
