package game

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/llm"
	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

type scriptedTriviaClient struct {
	*llm.StaticModelClient
	responses []*llm.GenerateTriviaResponse
	errors    []error
	calls     int
}

func (client *scriptedTriviaClient) GenerateTrivia(_ context.Context, _ llm.GenerateTriviaRequest) (*llm.GenerateTriviaResponse, error) {
	index := client.calls
	client.calls++
	if index < len(client.errors) && client.errors[index] != nil {
		return nil, client.errors[index]
	}
	if index >= len(client.responses) {
		return nil, errors.New("no scripted trivia response")
	}
	return client.responses[index], nil
}

func generatedTriviaResponse(kind models.GameKind) *llm.GenerateTriviaResponse {
	content := models.TriviaContent{Version: models.TriviaContentVersion, Kind: kind, CanonicalAnswer: "Jupiter", BaseScore: 100}
	if kind == models.GameKindTriviaChoice {
		content.Options = []models.TriviaOption{{ID: "o1", Label: "Mars"}, {ID: "o2", Label: "Jupiter"}, {ID: "o3", Label: "Venus"}, {ID: "o4", Label: "Saturn"}}
		content.CorrectOptionID = "o2"
	}
	return &llm.GenerateTriviaResponse{
		Question: models.Question{ID: "generated-trivia", Text: "What is the largest planet?", Locale: "en", Category: "general_knowledge"},
		Content:  content,
		Metadata: llm.CallMetadata{Provider: "test", Model: "gpt-5.6-luna", PromptVersion: llm.TriviaPromptVersion, InputTokens: 20, OutputTokens: 30, EstimatedCostUSD: .001},
	}
}

func TestGenerateTriviaRoundValidatesAndFreezesAtomicResponse(t *testing.T) {
	t.Parallel()
	for _, kind := range []models.GameKind{models.GameKindTriviaOpen, models.GameKindTriviaChoice} {
		kind := kind
		t.Run(string(kind), func(t *testing.T) {
			client := &scriptedTriviaClient{StaticModelClient: llm.NewStaticModelClient(), responses: []*llm.GenerateTriviaResponse{generatedTriviaResponse(kind)}}
			service := NewRoomService(NewInMemoryRoomRepository(), client)
			round, err := service.generateRound(context.Background(), models.RoomSettings{GameKind: kind, Locale: "en", PredictionModel: "gpt-5.6-luna"}, 1, nil, time.Now())
			if err != nil {
				t.Fatal(err)
			}
			if round.Board != nil || round.Trivia == nil || round.Trivia.IntegrityHash == "" {
				t.Fatalf("unexpected generated trivia round: %#v", round)
			}
			if err := ValidateTriviaContent(*round.Trivia); err != nil {
				t.Fatalf("frozen content invalid: %v", err)
			}
			if len(round.Audits) != 1 || round.Audits[0].Purpose != "trivia_generation" || round.Audits[0].PromptVersion != llm.TriviaPromptVersion {
				t.Fatalf("audit = %#v", round.Audits)
			}
		})
	}
}

func TestGenerateTriviaRetriesInvalidThenFallsBackToUnusedCuratedContent(t *testing.T) {
	t.Parallel()
	invalid := generatedTriviaResponse(models.GameKindTriviaOpen)
	invalid.Content.AcceptedAliases = []string{"JUPITER"}
	client := &scriptedTriviaClient{StaticModelClient: llm.NewStaticModelClient(), responses: []*llm.GenerateTriviaResponse{invalid, invalid}}
	service := NewRoomService(NewInMemoryRoomRepository(), client)
	round, err := service.generateRound(context.Background(), models.RoomSettings{GameKind: models.GameKindTriviaOpen, Locale: "en", PredictionModel: "gpt-5.6-luna"}, 2,
		[]string{"What is the largest planet in our solar system?"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 2 || round.Trivia == nil || round.Question.Text == "What is the largest planet in our solar system?" {
		t.Fatalf("retry/fallback result = calls %d, round %#v", client.calls, round)
	}
	if len(round.Audits) != 3 || round.Audits[0].Outcome != "invalid_output" || round.Audits[2].Path != "curated_fallback" || round.Audits[2].Provider != "static" {
		t.Fatalf("audits = %#v", round.Audits)
	}
}

func TestGenerateTriviaBudgetExhaustionFallsBackWithoutAnotherPaidAttempt(t *testing.T) {
	t.Parallel()
	response := generatedTriviaResponse(models.GameKindTriviaChoice)
	response.Metadata.EstimatedCostUSD = .10
	client := &scriptedTriviaClient{StaticModelClient: llm.NewStaticModelClient(), responses: []*llm.GenerateTriviaResponse{response}}
	service := NewRoomService(NewInMemoryRoomRepository(), client)
	round, err := service.generateRound(context.Background(), models.RoomSettings{GameKind: models.GameKindTriviaChoice, Locale: "en", PredictionModel: "gpt-5.6-luna"}, 1, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 1 || round.Trivia == nil || len(round.Audits) != 2 || round.Audits[1].Path != "curated_fallback" {
		t.Fatalf("budget fallback = calls %d, round %#v", client.calls, round)
	}
}

func TestGenerateTriviaTimeoutIsAuditedBeforeFallback(t *testing.T) {
	t.Parallel()
	client := &scriptedTriviaClient{StaticModelClient: llm.NewStaticModelClient(), errors: []error{context.DeadlineExceeded}}
	service := NewRoomService(NewInMemoryRoomRepository(), client)
	service.SetModelPolicy(llm.Policy{AllowedPredictionModels: []string{"gpt-5.6-luna"}, MaxAttempts: 1, MaxCallsPerGame: 20, MaxEstimatedCostUSD: .10})
	round, err := service.generateRound(context.Background(), models.RoomSettings{GameKind: models.GameKindTriviaOpen, Locale: "en", PredictionModel: "gpt-5.6-luna"}, 1, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if round.Trivia == nil || len(round.Audits) != 2 || round.Audits[0].Outcome != "timeout" || round.Audits[0].ErrorCategory != "timeout" || round.Audits[1].Path != "curated_fallback" {
		t.Fatalf("timeout fallback audits = %#v", round.Audits)
	}
}
