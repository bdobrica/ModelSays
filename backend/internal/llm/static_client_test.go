package llm

import (
	"context"
	"errors"
	"testing"
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
	if len(response.Questions) != 3 {
		t.Fatalf("question count = %d, want 3", len(response.Questions))
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
