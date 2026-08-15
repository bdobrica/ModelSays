package llm

import (
	"context"
	"errors"

	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

var ErrNoBankContent = errors.New("no matching content bank item")

type BankStore interface {
	BankQuestions(context.Context, GenerateQuestionsRequest) ([]models.Question, error)
	BankBoard(context.Context, models.Question) (models.PredictionBoard, error)
	BankTrivia(context.Context, GenerateTriviaRequest) (models.Question, models.TriviaContent, error)
}

type BankModelClient struct {
	store    BankStore
	fallback ModelClient
}

func NewBankModelClient(store BankStore, fallback ModelClient) *BankModelClient {
	return &BankModelClient{store: store, fallback: fallback}
}

func (client *BankModelClient) GenerateQuestions(ctx context.Context, req GenerateQuestionsRequest) (*GenerateQuestionsResponse, error) {
	questions, err := client.store.BankQuestions(ctx, req)
	if err == nil {
		return &GenerateQuestionsResponse{Questions: questions, Metadata: bankMetadata("bank-v1")}, nil
	}
	if !errors.Is(err, ErrNoBankContent) {
		return nil, err
	}
	return client.fallback.GenerateQuestions(ctx, req)
}

func (client *BankModelClient) GenerateBoard(ctx context.Context, req GenerateBoardRequest) (*GenerateBoardResponse, error) {
	board, err := client.store.BankBoard(ctx, req.Question)
	if err == nil {
		return &GenerateBoardResponse{Board: board, Metadata: bankMetadata(req.PromptVersion)}, nil
	}
	if !errors.Is(err, ErrNoBankContent) {
		return nil, err
	}
	return client.fallback.GenerateBoard(ctx, req)
}

func (client *BankModelClient) GenerateTrivia(ctx context.Context, req GenerateTriviaRequest) (*GenerateTriviaResponse, error) {
	question, content, err := client.store.BankTrivia(ctx, req)
	if err == nil {
		return &GenerateTriviaResponse{Question: question, Content: content, Metadata: bankMetadata(TriviaPromptVersion)}, nil
	}
	if !errors.Is(err, ErrNoBankContent) {
		return nil, err
	}
	fallback, ok := client.fallback.(TriviaClient)
	if !ok {
		return nil, ErrNoBankContent
	}
	return fallback.GenerateTrivia(ctx, req)
}

func bankMetadata(promptVersion string) CallMetadata {
	return CallMetadata{Provider: "database", Model: "content-bank", PromptVersion: defaultString(promptVersion, "bank-v1")}
}

func (client *BankModelClient) JudgeGuess(ctx context.Context, req JudgeGuessRequest) (*JudgeGuessResponse, error) {
	if judge, ok := client.fallback.(JudgeClient); ok {
		return judge.JudgeGuess(ctx, req)
	}
	return nil, errors.New("judge is unavailable")
}
