package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
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
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"choices":[{"message":{"role":"assistant","content":"{\"provider\":\"invented\",\"modelName\":\"invented\",\"promptVersion\":\"invented\",\"answers\":[{\"canonicalAnswer\":\"one\",\"aliases\":[\"first\"],\"rank\":1,\"score\":50},{\"canonicalAnswer\":\"two\",\"aliases\":[\"second\"],\"rank\":2,\"score\":40},{\"canonicalAnswer\":\"three\",\"aliases\":[\"third\"],\"rank\":3,\"score\":30},{\"canonicalAnswer\":\"four\",\"aliases\":[\"fourth\"],\"rank\":4,\"score\":20},{\"canonicalAnswer\":\"five\",\"aliases\":[\"fifth\"],\"rank\":5,\"score\":10}]}"}}]}`,
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
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func generatedTestQuestion() models.Question {
	return models.Question{ID: "question", Text: "Name something.", Locale: "en", Category: "party"}
}
