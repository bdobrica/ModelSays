package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

func TestStaticQuestionsUseRandomSelectionWithoutReplacement(t *testing.T) {
	t.Parallel()

	selections := []int{4, 2, 1}
	client := &StaticModelClient{randomIndex: func(limit int) (int, error) {
		selection := selections[0]
		selections = selections[1:]
		if selection >= limit {
			t.Fatalf("selection %d exceeds limit %d", selection, limit)
		}
		return selection, nil
	}}

	response, err := client.GenerateQuestions(context.Background(), GenerateQuestionsRequest{
		Locale: "en",
		Count:  3,
	})
	if err != nil {
		t.Fatalf("GenerateQuestions returned error: %v", err)
	}
	wantIDs := []string{"question-en-005", "question-en-004", "question-en-002"}
	for index, wantID := range wantIDs {
		if response.Questions[index].ID != wantID {
			t.Fatalf("question %d ID = %q, want %q", index, response.Questions[index].ID, wantID)
		}
	}
}

func TestCuratedTriviaBankHasFiveUniqueRoundsPerKind(t *testing.T) {
	t.Parallel()
	for _, kind := range []models.GameKind{models.GameKindTriviaOpen, models.GameKindTriviaChoice} {
		entries := curatedTriviaByKindAndLocale[kind]["en"]
		if len(entries) < 10 {
			t.Fatalf("%s curated count = %d, want at least 10", kind, len(entries))
		}
		questions := map[string]bool{}
		for _, entry := range entries {
			if questions[entry.question.Text] {
				t.Fatalf("%s duplicate question %q", kind, entry.question.Text)
			}
			questions[entry.question.Text] = true
			if entry.content.Kind != kind || entry.content.CanonicalAnswer == "" || entry.content.BaseScore != curatedTriviaBaseScore {
				t.Fatalf("invalid %s curated entry: %#v", kind, entry)
			}
			if kind == models.GameKindTriviaChoice && (len(entry.content.Options) != 4 || entry.content.CorrectOptionID == "") {
				t.Fatalf("invalid choice options: %#v", entry.content)
			}
		}
	}
}

func TestStaticTriviaSelectsWithoutRepeatingAndRejectsUnsupportedLocale(t *testing.T) {
	t.Parallel()
	client := &StaticModelClient{randomIndex: func(int) (int, error) { return 0, nil }}
	excluded := []string{}
	for round := 0; round < 10; round++ {
		response, err := client.GenerateTrivia(context.Background(), GenerateTriviaRequest{
			Kind: models.GameKindTriviaOpen, Locale: "en", ExcludedText: excluded,
		})
		if err != nil {
			t.Fatalf("round %d: %v", round+1, err)
		}
		if containsFold(excluded, response.Question.Text) {
			t.Fatalf("returned excluded question %q", response.Question.Text)
		}
		excluded = append(excluded, response.Question.Text)
	}
	if _, err := client.GenerateTrivia(context.Background(), GenerateTriviaRequest{Kind: models.GameKindTriviaOpen, Locale: "en", ExcludedText: excluded}); err == nil {
		t.Fatal("expected exhausted curated pool error")
	}
	if _, err := client.GenerateTrivia(context.Background(), GenerateTriviaRequest{Kind: models.GameKindTriviaOpen, Locale: "fr"}); err == nil {
		t.Fatal("expected unsupported locale error")
	}
}

func TestStaticTriviaReportsEntropyFailure(t *testing.T) {
	t.Parallel()
	wantErr := errors.New("entropy unavailable")
	client := &StaticModelClient{randomIndex: func(int) (int, error) { return 0, wantErr }}
	_, err := client.GenerateTrivia(context.Background(), GenerateTriviaRequest{Kind: models.GameKindTriviaChoice, Locale: "en"})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func TestStaticQuestionsExcludeQuestionsUsedEarlierInGame(t *testing.T) {
	t.Parallel()

	client := &StaticModelClient{randomIndex: func(int) (int, error) { return 0, nil }}
	response, err := client.GenerateQuestions(context.Background(), GenerateQuestionsRequest{
		Locale: "en",
		Count:  5,
		ExcludedText: []string{
			curatedRoundDataByLocale["en"][0].question.Text,
			curatedRoundDataByLocale["en"][2].question.Text,
		},
	})
	if err != nil {
		t.Fatalf("GenerateQuestions returned error: %v", err)
	}
	if len(response.Questions) != 5 {
		t.Fatalf("question count = %d, want 5", len(response.Questions))
	}
	for _, question := range response.Questions {
		if containsFold([]string{
			curatedRoundDataByLocale["en"][0].question.Text,
			curatedRoundDataByLocale["en"][2].question.Text,
		}, question.Text) {
			t.Fatalf("excluded question returned: %q", question.Text)
		}
	}
}

func TestStaticQuestionSelectionReportsRandomSourceFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("random source failed")
	client := &StaticModelClient{randomIndex: func(int) (int, error) { return 0, wantErr }}
	_, err := client.GenerateQuestions(context.Background(), GenerateQuestionsRequest{Locale: "en", Count: 1})
	if !errors.Is(err, wantErr) {
		t.Fatalf("GenerateQuestions error = %v, want wrapped %v", err, wantErr)
	}
}
