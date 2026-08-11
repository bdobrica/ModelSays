package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

func TestGenerateBoardRequestsStrictSchemaAndRecordsActualMetadata(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body openAIChatRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		format, ok := body.ResponseFormat.(map[string]any)
		if !ok || format["type"] != "json_schema" {
			t.Fatalf("expected json_schema response format, got %#v", body.ResponseFormat)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"req_test"}},
			Body: io.NopCloser(strings.NewReader(
				`{"usage":{"prompt_tokens":1000,"completion_tokens":500},"choices":[{"message":{"role":"assistant","content":"{\"provider\":\"invented\",\"modelName\":\"invented\",\"promptVersion\":\"invented\",\"answers\":[{\"canonicalAnswer\":\"one\",\"aliases\":[\"first\"],\"rank\":1,\"score\":50},{\"canonicalAnswer\":\"two\",\"aliases\":[\"second\"],\"rank\":2,\"score\":40},{\"canonicalAnswer\":\"three\",\"aliases\":[\"third\"],\"rank\":3,\"score\":30},{\"canonicalAnswer\":\"four\",\"aliases\":[\"fourth\"],\"rank\":4,\"score\":20},{\"canonicalAnswer\":\"five\",\"aliases\":[\"fifth\"],\"rank\":5,\"score\":10}]}"}}]}`,
			)),
		}, nil
	})

	client := NewOpenAIModelClient("test-key", ClientDefaults{})
	client.http = &http.Client{Transport: transport}
	response, err := client.GenerateBoard(context.Background(), GenerateBoardRequest{
		Question:        generatedTestQuestion(),
		PredictionModel: "actual-model",
		PromptVersion:   "v7",
	})
	if err != nil {
		t.Fatalf("GenerateBoard returned error: %v", err)
	}
	if response.Board.Provider != "openai" || response.Board.ModelName != "actual-model" || response.Board.PromptVersion != "v7" {
		t.Fatalf("expected actual request metadata, got provider=%q model=%q prompt=%q", response.Board.Provider, response.Board.ModelName, response.Board.PromptVersion)
	}
	if response.Metadata.RequestID != "req_test" || response.Metadata.InputTokens != 1000 || response.Metadata.OutputTokens != 500 {
		t.Fatalf("provider usage metadata was not captured: %#v", response.Metadata)
	}
}

func TestJudgeGuessUsesFrozenAnswerIDsAndStrictOutput(t *testing.T) {
	t.Parallel()
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body openAIChatRequest
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(body.Messages[1].Content, `"id":"answer-1"`) ||
			!strings.Contains(body.Messages[1].Content, `"guess":"digital money"`) {
			t.Fatalf("judge prompt does not contain bounded frozen input: %s", body.Messages[1].Content)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"X-Request-Id": []string{"judge_req"}},
			Body: io.NopCloser(strings.NewReader(
				`{"usage":{"prompt_tokens":50,"completion_tokens":10},"choices":[{"message":{"role":"assistant","content":"{\"answerId\":\"answer-1\",\"confidence\":0.88,\"rationaleCategory\":\"paraphrase\"}"}}]}`,
			)),
		}, nil
	})
	client := NewOpenAIModelClient("test-key", ClientDefaults{JudgeModel: "gpt-5.6-luna"})
	client.http = &http.Client{Transport: transport}
	answerID := "answer-1"
	response, err := client.JudgeGuess(context.Background(), JudgeGuessRequest{
		Question: generatedTestQuestion(),
		Board: models.PredictionBoard{Answers: []models.PredictionAnswer{{
			ID: answerID, CanonicalAnswer: "cryptocurrency", Aliases: []string{"crypto"},
		}}},
		Guess: "digital money", PromptVersion: "judge-v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.SuggestedPredictionAnswerID == nil || *response.SuggestedPredictionAnswerID != answerID ||
		response.Confidence != 0.88 || response.Metadata.RequestID != "judge_req" {
		t.Fatalf("unexpected judge response: %#v", response)
	}
}

func TestGenerateTriviaUsesAtomicVersionedStrictSchemas(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		kind    models.GameKind
		content string
		options int
	}{
		{models.GameKindTriviaOpen, `{"question":"What is the largest planet?","locale":"en","category":"general_knowledge","canonicalAnswer":"Jupiter","acceptedAliases":[],"baseScore":100,"explanation":"Jupiter is largest.","source":"reviewed reference","options":[],"correctOptionId":""}`, 0},
		{models.GameKindTriviaChoice, `{"question":"What is the largest planet?","locale":"en","category":"general_knowledge","canonicalAnswer":"Jupiter","acceptedAliases":[],"baseScore":100,"explanation":"Jupiter is largest.","source":"reviewed reference","options":[{"id":"o1","label":"Mars"},{"id":"o2","label":"Venus"},{"id":"o3","label":"Jupiter"},{"id":"o4","label":"Saturn"}],"correctOptionId":"o3"}`, 4},
	} {
		test := test
		t.Run(string(test.kind), func(t *testing.T) {
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				var body openAIChatRequest
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				format := body.ResponseFormat.(map[string]any)
				schema := format["json_schema"].(map[string]any)["schema"].(map[string]any)
				if schema["additionalProperties"] != false || !strings.Contains(body.Messages[1].Content, "complete frozen solution atomically") {
					t.Fatalf("trivia request was not atomic and strict: %#v", body)
				}
				response := `{"usage":{"prompt_tokens":40,"completion_tokens":30},"choices":[{"message":{"role":"assistant","content":` + strconv.Quote(test.content) + `}}]}`
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Request-Id": []string{"trivia_req"}}, Body: io.NopCloser(strings.NewReader(response))}, nil
			})
			client := NewOpenAIModelClient("test-key", ClientDefaults{PredictionModel: "gpt-5.6-luna"})
			client.http = &http.Client{Transport: transport}
			result, err := client.GenerateTrivia(context.Background(), GenerateTriviaRequest{Kind: test.kind, Locale: "en", PromptVersion: TriviaPromptVersion})
			if err != nil {
				t.Fatal(err)
			}
			if result.Content.Kind != test.kind || len(result.Content.Options) != test.options || result.Metadata.PromptVersion != TriviaPromptVersion || result.Metadata.RequestID != "trivia_req" {
				t.Fatalf("unexpected trivia result: %#v", result)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func generatedTestQuestion() models.Question {
	return models.Question{ID: "question", Text: "Name something.", Locale: "en", Category: "party"}
}
