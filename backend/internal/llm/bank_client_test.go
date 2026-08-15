package llm

import (
	"context"
	"testing"

	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

type fakeBankStore struct{ empty bool }

func (store fakeBankStore) BankQuestions(context.Context, GenerateQuestionsRequest) ([]models.Question, error) {
	if store.empty {
		return nil, ErrNoBankContent
	}
	return []models.Question{{ID: "bank-q", Text: "Întrebare?", Locale: "ro", Category: "General"}}, nil
}
func (store fakeBankStore) BankBoard(context.Context, models.Question) (models.PredictionBoard, error) {
	return models.PredictionBoard{}, ErrNoBankContent
}
func (store fakeBankStore) BankTrivia(context.Context, GenerateTriviaRequest) (models.Question, models.TriviaContent, error) {
	return models.Question{}, models.TriviaContent{}, ErrNoBankContent
}

func TestBankClientPrefersLoadedContent(t *testing.T) {
	client := NewBankModelClient(fakeBankStore{}, NewStaticModelClient())
	response, err := client.GenerateQuestions(context.Background(), GenerateQuestionsRequest{Locale: "ro", Count: 1})
	if err != nil || len(response.Questions) != 1 || response.Questions[0].ID != "bank-q" || response.Metadata.Provider != "database" {
		t.Fatalf("bank response = %#v, %v", response, err)
	}
}

func TestBankClientFallsBackWhenNoMatchingContentExists(t *testing.T) {
	client := NewBankModelClient(fakeBankStore{empty: true}, NewStaticModelClient())
	response, err := client.GenerateQuestions(context.Background(), GenerateQuestionsRequest{Locale: "en", Count: 1})
	if err != nil || len(response.Questions) != 1 || response.Metadata.Provider != "static" {
		t.Fatalf("fallback response = %#v, %v", response, err)
	}
}
