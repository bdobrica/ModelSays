package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/bogdandobrica/modelsays/backend/internal/llm"
	"github.com/bogdandobrica/modelsays/backend/internal/models"
	"github.com/jackc/pgx/v5"
)

type bankPayload struct {
	CanonicalAnswer string                    `json:"canonicalAnswer"`
	AcceptedAliases []string                  `json:"acceptedAliases"`
	BaseScore       int                       `json:"baseScore"`
	Explanation     string                    `json:"explanation"`
	Source          string                    `json:"source"`
	Options         []models.TriviaOption     `json:"options"`
	CorrectOptionID string                    `json:"correctOptionId"`
	Answers         []models.PredictionAnswer `json:"answers"`
}

func (repository *PostgresRoomRepository) BankQuestions(ctx context.Context, req llm.GenerateQuestionsRequest) ([]models.Question, error) {
	count := req.Count
	if count < 1 {
		count = 1
	}
	excluded := lowerTrimmed(req.ExcludedText)
	rows, err := repository.pool.Query(ctx, `
		SELECT id, question_text, locale, category
		FROM content_bank_items
		WHERE enabled AND game_kind='model_says' AND locale=$1
		  AND NOT (lower(btrim(question_text)) = ANY($2::text[]))
		ORDER BY random() LIMIT $3`, strings.TrimSpace(req.Locale), excluded, count)
	if err != nil {
		return nil, fmt.Errorf("select model-says bank questions: %w", err)
	}
	defer rows.Close()
	questions := make([]models.Question, 0, count)
	for rows.Next() {
		var question models.Question
		if err := rows.Scan(&question.ID, &question.Text, &question.Locale, &question.Category); err != nil {
			return nil, err
		}
		questions = append(questions, question)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(questions) == 0 {
		return nil, llm.ErrNoBankContent
	}
	return questions, nil
}

func (repository *PostgresRoomRepository) BankBoard(ctx context.Context, question models.Question) (models.PredictionBoard, error) {
	var encoded []byte
	err := repository.pool.QueryRow(ctx, `
		SELECT payload_jsonb FROM content_bank_items
		WHERE enabled AND game_kind='model_says' AND id=$1`, question.ID).Scan(&encoded)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.PredictionBoard{}, llm.ErrNoBankContent
	}
	if err != nil {
		return models.PredictionBoard{}, fmt.Errorf("select model-says bank board: %w", err)
	}
	var payload bankPayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return models.PredictionBoard{}, fmt.Errorf("decode model-says bank board: %w", err)
	}
	return models.PredictionBoard{Provider: "database", ModelName: "content-bank", PromptVersion: "bank-v1", Answers: payload.Answers}, nil
}

func (repository *PostgresRoomRepository) BankTrivia(ctx context.Context, req llm.GenerateTriviaRequest) (models.Question, models.TriviaContent, error) {
	excluded := lowerTrimmed(req.ExcludedText)
	var question models.Question
	var encoded []byte
	err := repository.pool.QueryRow(ctx, `
		SELECT id, question_text, locale, category, payload_jsonb
		FROM content_bank_items
		WHERE enabled AND game_kind=$1 AND locale=$2
		  AND NOT (lower(btrim(question_text)) = ANY($3::text[]))
		ORDER BY random() LIMIT 1`, string(req.Kind), strings.TrimSpace(req.Locale), excluded).Scan(
		&question.ID, &question.Text, &question.Locale, &question.Category, &encoded)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Question{}, models.TriviaContent{}, llm.ErrNoBankContent
	}
	if err != nil {
		return models.Question{}, models.TriviaContent{}, fmt.Errorf("select trivia bank item: %w", err)
	}
	var payload bankPayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return models.Question{}, models.TriviaContent{}, fmt.Errorf("decode trivia bank item: %w", err)
	}
	content := models.TriviaContent{Version: models.TriviaContentVersion, Kind: req.Kind,
		CanonicalAnswer: payload.CanonicalAnswer, AcceptedAliases: payload.AcceptedAliases,
		BaseScore: payload.BaseScore, Explanation: payload.Explanation, Source: payload.Source,
		Options: payload.Options, CorrectOptionID: payload.CorrectOptionID}
	return question, content, nil
}

func lowerTrimmed(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
			result = append(result, value)
		}
	}
	return result
}
