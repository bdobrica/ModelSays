package llm

import (
	"context"
	"fmt"
	"strings"

	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

type StaticModelClient struct{}

type curatedRoundData struct {
	question models.Question
	board    models.PredictionBoard
}

var curatedRoundDataByLocale = map[string][]curatedRoundData{
	"en": {
		{
			question: models.Question{
				ID:       "question-en-001",
				Text:     "Name something people pretend to understand but probably do not.",
				Locale:   "en",
				Category: "party",
			},
			board: models.PredictionBoard{
				Provider:      "static",
				ModelName:     "curated-bank",
				PromptVersion: "v1",
				Answers: []models.PredictionAnswer{
					{CanonicalAnswer: "cryptocurrency", Aliases: []string{"crypto", "bitcoin"}, Rank: 1, Score: 50},
					{CanonicalAnswer: "quantum physics", Aliases: []string{"physics", "quantum"}, Rank: 2, Score: 35},
					{CanonicalAnswer: "taxes", Aliases: []string{"tax", "the tax code"}, Rank: 3, Score: 25},
					{CanonicalAnswer: "artificial intelligence", Aliases: []string{"ai", "machine learning"}, Rank: 4, Score: 15},
					{CanonicalAnswer: "wine", Aliases: []string{"wine tasting"}, Rank: 5, Score: 10},
				},
			},
		},
		{
			question: models.Question{
				ID:       "question-en-002",
				Text:     "Name something that feels expensive even when you know it should not be.",
				Locale:   "en",
				Category: "party",
			},
			board: models.PredictionBoard{
				Provider:      "static",
				ModelName:     "curated-bank",
				PromptVersion: "v1",
				Answers: []models.PredictionAnswer{
					{CanonicalAnswer: "airport food", Aliases: []string{"airport snacks", "airport sandwich"}, Rank: 1, Score: 45},
					{CanonicalAnswer: "movie popcorn", Aliases: []string{"cinema popcorn", "popcorn"}, Rank: 2, Score: 35},
					{CanonicalAnswer: "bottled water", Aliases: []string{"water bottle", "water"}, Rank: 3, Score: 25},
					{CanonicalAnswer: "printer ink", Aliases: []string{"ink cartridges", "ink"}, Rank: 4, Score: 15},
					{CanonicalAnswer: "concert tickets", Aliases: []string{"tickets", "gig tickets"}, Rank: 5, Score: 10},
				},
			},
		},
		{
			question: models.Question{
				ID:       "question-en-003",
				Text:     "Name a thing people always claim they will start next Monday.",
				Locale:   "en",
				Category: "party",
			},
			board: models.PredictionBoard{
				Provider:      "static",
				ModelName:     "curated-bank",
				PromptVersion: "v1",
				Answers: []models.PredictionAnswer{
					{CanonicalAnswer: "dieting", Aliases: []string{"diet", "eat healthy"}, Rank: 1, Score: 40},
					{CanonicalAnswer: "going to the gym", Aliases: []string{"gym", "exercise", "working out"}, Rank: 2, Score: 30},
					{CanonicalAnswer: "waking up early", Aliases: []string{"wake up early", "early mornings"}, Rank: 3, Score: 20},
					{CanonicalAnswer: "learning a language", Aliases: []string{"duolingo", "language lessons"}, Rank: 4, Score: 15},
					{CanonicalAnswer: "budgeting", Aliases: []string{"save money", "budget"}, Rank: 5, Score: 10},
				},
			},
		},
	},
}

func NewStaticModelClient() *StaticModelClient {
	return &StaticModelClient{}
}

func (client *StaticModelClient) GenerateQuestions(_ context.Context, req GenerateQuestionsRequest) (*GenerateQuestionsResponse, error) {
	locale := strings.TrimSpace(req.Locale)
	questionSet := curatedRoundDataByLocale[locale]
	if len(questionSet) == 0 {
		questionSet = curatedRoundDataByLocale["en"]
	}
	if len(questionSet) == 0 {
		return nil, fmt.Errorf("no curated questions available")
	}

	count := maxInt(req.Count, 1)
	offset := req.RoundIndex - 1
	if offset < 0 {
		offset = 0
	}

	questions := make([]models.Question, 0, count)
	for index := 0; index < count; index++ {
		selected := questionSet[(offset+index)%len(questionSet)].question
		questions = append(questions, selected)
	}

	return &GenerateQuestionsResponse{Questions: questions}, nil
}

func (client *StaticModelClient) GenerateBoard(_ context.Context, req GenerateBoardRequest) (*GenerateBoardResponse, error) {
	locale := strings.TrimSpace(req.Question.Locale)
	questionSet := curatedRoundDataByLocale[locale]
	if len(questionSet) == 0 {
		questionSet = curatedRoundDataByLocale["en"]
	}

	for _, entry := range questionSet {
		if entry.question.ID == req.Question.ID {
			board := entry.board
			board.ModelName = req.PredictionModel
			if board.ModelName == "" {
				board.ModelName = "curated-bank"
			}
			board.PromptVersion = defaultString(req.PromptVersion, board.PromptVersion)
			return &GenerateBoardResponse{Board: board}, nil
		}
	}

	return nil, fmt.Errorf("no curated board available for question %q", req.Question.ID)
}

func (client *StaticModelClient) MatchGuess(_ context.Context, _ MatchGuessRequest) (*MatchGuessResponse, error) {
	return &MatchGuessResponse{}, nil
}
