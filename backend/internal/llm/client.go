package llm

import (
	"context"

	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

type GenerateQuestionsRequest struct {
	Locale       string
	Category     string
	Count        int
	RoundIndex   int
	TeamSafeMode bool
	ExcludedText []string
}

type GenerateQuestionsResponse struct {
	Questions []models.Question
	Metadata  CallMetadata
}

type GenerateBoardRequest struct {
	Question        models.Question
	PredictionModel string
	TeamSafeMode    bool
	PromptVersion   string
}

type GenerateBoardResponse struct {
	Board    models.PredictionBoard
	Metadata CallMetadata
}

type ClientDefaults struct {
	QuestionModel   string
	PredictionModel string
}

type ModelClient interface {
	GenerateQuestions(ctx context.Context, req GenerateQuestionsRequest) (*GenerateQuestionsResponse, error)
	GenerateBoard(ctx context.Context, req GenerateBoardRequest) (*GenerateBoardResponse, error)
}
