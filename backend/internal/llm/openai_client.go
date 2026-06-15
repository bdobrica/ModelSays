package llm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

type OpenAIModelClient struct {
	apiKey   string
	defaults ClientDefaults
	http     *http.Client
	baseURL  string
}

type openAIChatRequest struct {
	Model          string            `json:"model"`
	Messages       []openAIMessage   `json:"messages"`
	ResponseFormat map[string]string `json:"response_format,omitempty"`
	Temperature    float64           `json:"temperature,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIChatResponse struct {
	Choices []struct {
		Message openAIMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type generatedQuestionsPayload struct {
	Questions []struct {
		Text     string `json:"text"`
		Locale   string `json:"locale"`
		Category string `json:"category"`
	} `json:"questions"`
}

type generatedBoardPayload struct {
	Provider      string `json:"provider"`
	ModelName     string `json:"modelName"`
	PromptVersion string `json:"promptVersion"`
	Answers       []struct {
		CanonicalAnswer string   `json:"canonicalAnswer"`
		Aliases         []string `json:"aliases"`
		Rank            int      `json:"rank"`
		Score           int      `json:"score"`
	} `json:"answers"`
}

func NewOpenAIModelClient(apiKey string, defaults ClientDefaults) *OpenAIModelClient {
	return &OpenAIModelClient{
		apiKey:   strings.TrimSpace(apiKey),
		defaults: defaults,
		http: &http.Client{
			Timeout: 25 * time.Second,
		},
		baseURL: "https://api.openai.com/v1/chat/completions",
	}
}

func (client *OpenAIModelClient) GenerateQuestions(ctx context.Context, req GenerateQuestionsRequest) (*GenerateQuestionsResponse, error) {
	model := client.defaults.QuestionModel
	if model == "" {
		model = "gpt-4.1-mini"
	}

	prompt := fmt.Sprintf("Return JSON with shape {\"questions\":[{\"text\":string,\"locale\":string,\"category\":string}]}. Generate %d broad, party-safe questions for a game where players guess an AI answer board. Locale: %s. Category: %s. Team safe mode: %t. Vary by round index %d. Avoid hateful, sexual, medical, legal, financial, personal, or workplace-sensitive topics.", maxInt(req.Count, 1), req.Locale, defaultString(req.Category, "party"), req.TeamSafeMode, req.RoundIndex)

	body, err := client.completeJSON(ctx, model, prompt)
	if err != nil {
		return nil, err
	}

	var payload generatedQuestionsPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode question payload: %w", err)
	}

	questions := make([]models.Question, 0, len(payload.Questions))
	for _, generated := range payload.Questions {
		text := strings.TrimSpace(generated.Text)
		if text == "" {
			continue
		}
		locale := defaultString(strings.TrimSpace(generated.Locale), defaultString(strings.TrimSpace(req.Locale), "en"))
		category := defaultString(strings.TrimSpace(generated.Category), defaultString(strings.TrimSpace(req.Category), "party"))
		questions = append(questions, models.Question{
			ID:       stableID(locale + "|" + text),
			Text:     text,
			Locale:   locale,
			Category: category,
		})
	}

	if len(questions) == 0 {
		return nil, fmt.Errorf("openai returned no usable questions")
	}

	return &GenerateQuestionsResponse{Questions: questions}, nil
}

func (client *OpenAIModelClient) GenerateBoard(ctx context.Context, req GenerateBoardRequest) (*GenerateBoardResponse, error) {
	model := defaultString(strings.TrimSpace(req.PredictionModel), client.defaults.PredictionModel)
	if model == "" {
		model = "gpt-4.1-mini"
	}

	prompt := fmt.Sprintf("Return JSON with shape {\"provider\":string,\"modelName\":string,\"promptVersion\":string,\"answers\":[{\"canonicalAnswer\":string,\"aliases\":string[],\"rank\":number,\"score\":number}]}. Generate a Family Feud style answer board for the question %q. Provide exactly 5 ranked answers with descending scores. Scores should be positive integers. Aliases should contain close synonyms only. Team safe mode: %t. Prompt version: %s.", req.Question.Text, req.TeamSafeMode, defaultString(req.PromptVersion, "v1"))

	body, err := client.completeJSON(ctx, model, prompt)
	if err != nil {
		return nil, err
	}

	var payload generatedBoardPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode board payload: %w", err)
	}

	answers := make([]models.PredictionAnswer, 0, len(payload.Answers))
	for index, generated := range payload.Answers {
		canonical := strings.TrimSpace(generated.CanonicalAnswer)
		if canonical == "" {
			continue
		}
		rank := generated.Rank
		if rank <= 0 {
			rank = index + 1
		}
		score := generated.Score
		if score <= 0 {
			score = maxInt(10, (len(payload.Answers)-index)*10)
		}
		aliases := make([]string, 0, len(generated.Aliases))
		for _, alias := range generated.Aliases {
			trimmed := strings.TrimSpace(alias)
			if trimmed != "" && !strings.EqualFold(trimmed, canonical) {
				aliases = append(aliases, trimmed)
			}
		}
		answers = append(answers, models.PredictionAnswer{
			CanonicalAnswer: canonical,
			Aliases:         aliases,
			Rank:            rank,
			Score:           score,
		})
	}

	if len(answers) == 0 {
		return nil, fmt.Errorf("openai returned no usable board answers")
	}

	board := models.PredictionBoard{
		Provider:      defaultString(strings.TrimSpace(payload.Provider), "openai"),
		ModelName:     defaultString(strings.TrimSpace(payload.ModelName), model),
		PromptVersion: defaultString(strings.TrimSpace(payload.PromptVersion), defaultString(req.PromptVersion, "v1")),
		Answers:       answers,
	}

	return &GenerateBoardResponse{Board: board}, nil
}

func (client *OpenAIModelClient) MatchGuess(_ context.Context, _ MatchGuessRequest) (*MatchGuessResponse, error) {
	return &MatchGuessResponse{}, nil
}

func (client *OpenAIModelClient) completeJSON(ctx context.Context, model string, prompt string) ([]byte, error) {
	reqBody, err := json.Marshal(openAIChatRequest{
		Model: model,
		Messages: []openAIMessage{
			{Role: "system", Content: "You are generating structured game data. Respond with valid JSON only and no markdown."},
			{Role: "user", Content: prompt},
		},
		ResponseFormat: map[string]string{"type": "json_object"},
		Temperature:    0.7,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal openai request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, client.baseURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create openai request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+client.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := client.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send openai request: %w", err)
	}
	defer httpResp.Body.Close()

	body, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("read openai response: %w", err)
	}

	var response openAIChatResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}
	if httpResp.StatusCode >= 400 {
		if response.Error != nil {
			return nil, fmt.Errorf("openai error: %s", response.Error.Message)
		}
		return nil, fmt.Errorf("openai error: status %d", httpResp.StatusCode)
	}
	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("openai returned no choices")
	}

	content := strings.TrimSpace(response.Choices[0].Message.Content)
	return []byte(content), nil
}

func stableID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func defaultString(value string, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}

	return fallback
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}

	return b
}
