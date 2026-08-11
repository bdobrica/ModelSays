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

const TriviaPromptVersion = "trivia-v1"

type GenerateTriviaRequest struct {
	Kind            models.GameKind
	Locale          string
	Category        string
	RoundIndex      int
	TeamSafeMode    bool
	ExcludedText    []string
	PredictionModel string
	PromptVersion   string
}

type GenerateTriviaResponse struct {
	Question models.Question
	Content  models.TriviaContent
	Metadata CallMetadata
}

type JudgeGuessRequest struct {
	Question      models.Question
	Board         models.PredictionBoard
	Guess         string
	JudgeModel    string
	PromptVersion string
}

type JudgeGuessResponse struct {
	SuggestedPredictionAnswerID *string
	Confidence                  float64
	RationaleCategory           string
	Metadata                    CallMetadata
}

type ClientDefaults struct {
	QuestionModel   string
	PredictionModel string
	JudgeModel      string
}

type ModelClient interface {
	GenerateQuestions(ctx context.Context, req GenerateQuestionsRequest) (*GenerateQuestionsResponse, error)
	GenerateBoard(ctx context.Context, req GenerateBoardRequest) (*GenerateBoardResponse, error)
}

type TriviaClient interface {
	GenerateTrivia(ctx context.Context, req GenerateTriviaRequest) (*GenerateTriviaResponse, error)
}

type JudgeClient interface {
	JudgeGuess(ctx context.Context, req JudgeGuessRequest) (*JudgeGuessResponse, error)
}
