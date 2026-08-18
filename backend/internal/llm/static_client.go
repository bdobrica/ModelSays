package llm

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"

	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

type StaticModelClient struct {
	randomIndex func(int) (int, error)
}

func (client *StaticModelClient) JudgeGuess(_ context.Context, req JudgeGuessRequest) (*JudgeGuessResponse, error) {
	return &JudgeGuessResponse{
		Confidence:        0,
		RationaleCategory: "unrelated",
		Metadata: CallMetadata{
			Provider:      "static",
			Model:         "curated-bank",
			PromptVersion: defaultString(req.PromptVersion, "judge-v1"),
		},
	}, nil
}

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
		{
			question: models.Question{
				ID: "question-en-004", Text: "Name something people check twice before leaving home.", Locale: "en", Category: "party",
			},
			board: models.PredictionBoard{
				Provider: "static", ModelName: "curated-bank", PromptVersion: "v1",
				Answers: []models.PredictionAnswer{
					{CanonicalAnswer: "their keys", Aliases: []string{"keys", "house keys"}, Rank: 1, Score: 50},
					{CanonicalAnswer: "their phone", Aliases: []string{"phone", "mobile phone"}, Rank: 2, Score: 40},
					{CanonicalAnswer: "the door lock", Aliases: []string{"locked door", "front door"}, Rank: 3, Score: 30},
					{CanonicalAnswer: "their wallet", Aliases: []string{"wallet", "purse"}, Rank: 4, Score: 20},
					{CanonicalAnswer: "the stove", Aliases: []string{"oven", "cooker"}, Rank: 5, Score: 10},
				},
			},
		},
		{
			question: models.Question{
				ID: "question-en-005", Text: "Name something that makes a meeting feel longer.", Locale: "en", Category: "party",
			},
			board: models.PredictionBoard{
				Provider: "static", ModelName: "curated-bank", PromptVersion: "v1",
				Answers: []models.PredictionAnswer{
					{CanonicalAnswer: "no clear agenda", Aliases: []string{"no agenda", "unclear agenda"}, Rank: 1, Score: 50},
					{CanonicalAnswer: "a long presentation", Aliases: []string{"presentation", "too many slides"}, Rank: 2, Score: 40},
					{CanonicalAnswer: "repeated discussion", Aliases: []string{"repeating points", "going in circles"}, Rank: 3, Score: 30},
					{CanonicalAnswer: "technical problems", Aliases: []string{"tech issues", "connection problems"}, Rank: 4, Score: 20},
					{CanonicalAnswer: "being hungry", Aliases: []string{"hunger", "missing lunch"}, Rank: 5, Score: 10},
				},
			},
		},
		{
			question: models.Question{ID: "question-en-006", Text: "Name something people often forget when packing for a trip.", Locale: "en", Category: "party"},
			board: models.PredictionBoard{Provider: "static", ModelName: "curated-bank", PromptVersion: "v1", Answers: []models.PredictionAnswer{
				{CanonicalAnswer: "toothbrush", Aliases: []string{"their toothbrush"}, Rank: 1, Score: 50},
				{CanonicalAnswer: "phone charger", Aliases: []string{"charger", "charging cable"}, Rank: 2, Score: 40},
				{CanonicalAnswer: "underwear", Aliases: []string{"clean underwear"}, Rank: 3, Score: 30},
				{CanonicalAnswer: "sunscreen", Aliases: []string{"sun cream", "sunblock"}, Rank: 4, Score: 20},
				{CanonicalAnswer: "medication", Aliases: []string{"medicine", "prescriptions"}, Rank: 5, Score: 10},
			}},
		},
		{
			question: models.Question{ID: "question-en-007", Text: "Name something that can ruin a good night's sleep.", Locale: "en", Category: "party"},
			board: models.PredictionBoard{Provider: "static", ModelName: "curated-bank", PromptVersion: "v1", Answers: []models.PredictionAnswer{
				{CanonicalAnswer: "noise", Aliases: []string{"loud neighbors", "snoring"}, Rank: 1, Score: 50},
				{CanonicalAnswer: "stress", Aliases: []string{"worry", "anxiety"}, Rank: 2, Score: 40},
				{CanonicalAnswer: "a bad mattress", Aliases: []string{"uncomfortable bed", "mattress"}, Rank: 3, Score: 30},
				{CanonicalAnswer: "room temperature", Aliases: []string{"too hot", "too cold"}, Rank: 4, Score: 20},
				{CanonicalAnswer: "screen time", Aliases: []string{"phone", "late-night scrolling"}, Rank: 5, Score: 10},
			}},
		},
		{
			question: models.Question{ID: "question-en-008", Text: "Name something people do while waiting in a long line.", Locale: "en", Category: "party"},
			board: models.PredictionBoard{Provider: "static", ModelName: "curated-bank", PromptVersion: "v1", Answers: []models.PredictionAnswer{
				{CanonicalAnswer: "check their phone", Aliases: []string{"use their phone", "scroll"}, Rank: 1, Score: 50},
				{CanonicalAnswer: "talk", Aliases: []string{"chat", "have a conversation"}, Rank: 2, Score: 40},
				{CanonicalAnswer: "people-watch", Aliases: []string{"watch people", "people watching"}, Rank: 3, Score: 30},
				{CanonicalAnswer: "complain", Aliases: []string{"grumble"}, Rank: 4, Score: 20},
				{CanonicalAnswer: "listen to music", Aliases: []string{"music", "wear headphones"}, Rank: 5, Score: 10},
			}},
		},
		{
			question: models.Question{ID: "question-en-009", Text: "Name something that is difficult to do quietly.", Locale: "en", Category: "party"},
			board: models.PredictionBoard{Provider: "static", ModelName: "curated-bank", PromptVersion: "v1", Answers: []models.PredictionAnswer{
				{CanonicalAnswer: "sneeze", Aliases: []string{"sneezing"}, Rank: 1, Score: 50},
				{CanonicalAnswer: "eat crunchy food", Aliases: []string{"eat chips", "crunch food"}, Rank: 2, Score: 40},
				{CanonicalAnswer: "laugh", Aliases: []string{"laughing"}, Rank: 3, Score: 30},
				{CanonicalAnswer: "open a snack bag", Aliases: []string{"open a packet", "open chips"}, Rank: 4, Score: 20},
				{CanonicalAnswer: "move furniture", Aliases: []string{"drag a chair", "moving furniture"}, Rank: 5, Score: 10},
			}},
		},
		{
			question: models.Question{ID: "question-en-010", Text: "Name something people save for a special occasion.", Locale: "en", Category: "party"},
			board: models.PredictionBoard{Provider: "static", ModelName: "curated-bank", PromptVersion: "v1", Answers: []models.PredictionAnswer{
				{CanonicalAnswer: "a bottle of wine", Aliases: []string{"wine", "champagne"}, Rank: 1, Score: 50},
				{CanonicalAnswer: "nice clothes", Aliases: []string{"special outfit", "formal clothes"}, Rank: 2, Score: 40},
				{CanonicalAnswer: "money", Aliases: []string{"savings", "cash"}, Rank: 3, Score: 30},
				{CanonicalAnswer: "fancy dishes", Aliases: []string{"fine china", "best plates"}, Rank: 4, Score: 20},
				{CanonicalAnswer: "a gift", Aliases: []string{"present"}, Rank: 5, Score: 10},
			}},
		},
	},
}

func NewStaticModelClient() *StaticModelClient {
	return &StaticModelClient{randomIndex: cryptographicRandomIndex}
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

	available := make([]models.Question, 0, len(questionSet))
	for _, entry := range questionSet {
		if !containsFold(req.ExcludedText, entry.question.Text) {
			available = append(available, entry.question)
		}
	}

	count := minInt(maxInt(req.Count, 1), len(available))
	questions := make([]models.Question, 0, count)
	randomIndex := client.randomIndex
	if randomIndex == nil {
		randomIndex = cryptographicRandomIndex
	}
	for index := 0; index < count; index++ {
		selectedOffset, err := randomIndex(len(available) - index)
		if err != nil {
			return nil, fmt.Errorf("select curated question: %w", err)
		}
		selectedIndex := index + selectedOffset
		available[index], available[selectedIndex] = available[selectedIndex], available[index]
		questions = append(questions, available[index])
	}

	return &GenerateQuestionsResponse{Questions: questions, Metadata: CallMetadata{
		Provider: "static", Model: "curated-bank", PromptVersion: "v1",
	}}, nil
}

func cryptographicRandomIndex(limit int) (int, error) {
	if limit <= 0 {
		return 0, fmt.Errorf("random selection limit must be positive")
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return 0, err
	}
	return int(value.Int64()), nil
}

func minInt(left int, right int) int {
	if left < right {
		return left
	}
	return right
}

func containsFold(values []string, candidate string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
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
			board.Answers = append([]models.PredictionAnswer(nil), entry.board.Answers...)
			for index := range board.Answers {
				board.Answers[index].Aliases = append([]string(nil), entry.board.Answers[index].Aliases...)
			}
			board.ModelName = req.PredictionModel
			if board.ModelName == "" {
				board.ModelName = "curated-bank"
			}
			board.PromptVersion = defaultString(req.PromptVersion, board.PromptVersion)
			return &GenerateBoardResponse{Board: board, Metadata: CallMetadata{
				Provider: "static", Model: "curated-bank", PromptVersion: board.PromptVersion,
			}}, nil
		}
	}

	return nil, fmt.Errorf("no curated board available for question %q", req.Question.ID)
}
