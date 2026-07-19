package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/config"
	"github.com/bogdandobrica/modelsays/backend/internal/game"
	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

func TestRoomResponsesHideRoundOutcomeUntilReveal(t *testing.T) {
	t.Parallel()

	service := game.NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), game.CreateRoomInput{
		RoomName: "Secret outcomes", HostDisplayName: "Host",
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}
	_, player, err := service.JoinRoom(context.Background(), game.JoinRoomInput{
		Code: room.Code, DisplayName: "Player",
	})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}

	server := NewServer(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	lobby := performRoomRequest(t, server, http.MethodGet, fmt.Sprintf("/api/rooms/%s", room.Code), "")
	started := performRoomRequest(
		t,
		server,
		http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/start", room.Code),
		fmt.Sprintf(`{"playerToken":%q}`, host.Token),
	)
	roundID := nestedString(t, started, "room", "currentGame", "currentRound", "id")
	submitted := performRoomRequest(
		t,
		server,
		http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/rounds/%s/guesses", room.Code, roundID),
		fmt.Sprintf(`{"playerToken":%q,"answer":"bitcoin"}`, player.Token),
	)
	hostView := performRoomRequest(
		t,
		server,
		http.MethodGet,
		fmt.Sprintf("/api/rooms/%s?playerToken=%s", room.Code, host.Token),
		"",
	)
	playerView := performRoomRequest(
		t,
		server,
		http.MethodGet,
		fmt.Sprintf("/api/rooms/%s?playerToken=%s", room.Code, player.Token),
		"",
	)
	revealed := performRoomRequest(
		t,
		server,
		http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/rounds/%s/reveal", room.Code, roundID),
		fmt.Sprintf(`{"playerToken":%q}`, host.Token),
	)

	tests := []struct {
		name             string
		response         map[string]any
		wantCurrentGame  bool
		wantRoundSecrets bool
		wantScore        float64
		wantSubmitted    bool
	}{
		{name: "lobby", response: lobby},
		{name: "answering after start", response: started, wantCurrentGame: true},
		{name: "answering after scoring submission", response: submitted, wantCurrentGame: true, wantSubmitted: true},
		{name: "answering host view", response: hostView, wantCurrentGame: true, wantSubmitted: true},
		{name: "answering non-host view", response: playerView, wantCurrentGame: true, wantSubmitted: true},
		{name: "revealed", response: revealed, wantCurrentGame: true, wantRoundSecrets: true, wantScore: 50, wantSubmitted: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			roomPayload := nestedMap(t, test.response, "room")
			assertPublicPlayersHaveNoTokens(t, roomPayload)
			gamePayload, hasGame := roomPayload["currentGame"].(map[string]any)
			if hasGame != test.wantCurrentGame {
				t.Fatalf("currentGame presence = %v, want %v", hasGame, test.wantCurrentGame)
			}
			if !hasGame {
				return
			}

			roundPayload := nestedMap(t, gamePayload, "currentRound")
			_, hasBoard := roundPayload["board"]
			_, hasGuesses := roundPayload["guesses"]
			if hasBoard != test.wantRoundSecrets || hasGuesses != test.wantRoundSecrets {
				t.Fatalf("board/guesses presence = %v/%v, want %v/%v", hasBoard, hasGuesses, test.wantRoundSecrets, test.wantRoundSecrets)
			}

			playerScore := scoreboardEntry(t, gamePayload, player.ID)
			if score := playerScore["score"]; score != test.wantScore {
				t.Fatalf("public score = %v, want %v", score, test.wantScore)
			}
			if submitted := playerScore["submissionMade"]; submitted != test.wantSubmitted {
				t.Fatalf("submissionMade = %v, want %v", submitted, test.wantSubmitted)
			}

			if test.wantRoundSecrets {
				guess := nestedSlice(t, roundPayload, "guesses")[0].(map[string]any)
				for _, field := range []string{"rawAnswer", "normalizedAnswer", "matchedPredictionAnswerId", "scoreAwarded", "duplicate"} {
					if _, ok := guess[field]; !ok {
						t.Errorf("revealed guess does not include %q", field)
					}
				}
			}
		})
	}

	deleteNested(hostView, "room", "updatedAt")
	deleteNested(playerView, "room", "updatedAt")
	if !reflect.DeepEqual(hostView, playerView) {
		t.Fatal("host and non-host answering views have different secrecy projections")
	}
}

func TestExpiredSubmissionReturnsStableConflictResponse(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{now: startedAt}
	service := game.NewRoomServiceWithClock(game.NewInMemoryRoomRepository(), nil, &clock)
	room, host, err := service.CreateRoom(context.Background(), game.CreateRoomInput{
		RoomName: "HTTP deadline", HostDisplayName: "Host", Settings: models.RoomSettings{AnswerTimerSeconds: 30},
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}
	_, player, err := service.JoinRoom(context.Background(), game.JoinRoomInput{Code: room.Code, DisplayName: "Player"})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}
	startedRoom, err := service.StartGame(context.Background(), game.StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}
	clock.now = startedRoom.CurrentGame.CurrentRound.AnswerPhaseEndsAt

	server := NewServer(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/rounds/%s/guesses", room.Code, startedRoom.CurrentGame.CurrentRound.ID),
		strings.NewReader(fmt.Sprintf(`{"playerToken":%q,"answer":"bitcoin"}`, player.Token)),
	)
	request.Header.Set("Content-Type", "application/json")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("expected JSON content type, got %q", contentType)
	}
	if body := strings.TrimSpace(response.Body.String()); body != `{"error":"answer phase has expired"}` {
		t.Fatalf("unexpected error response: %s", body)
	}
}

func performRoomRequest(t *testing.T, server *Server, method string, path string, body string) map[string]any {
	t.Helper()

	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(response, request)
	if response.Code < http.StatusOK || response.Code >= http.StatusMultipleChoices {
		t.Fatalf("%s %s returned %d: %s", method, path, response.Code, response.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %s %s response: %v", method, path, err)
	}
	return payload
}

func nestedMap(t *testing.T, payload map[string]any, keys ...string) map[string]any {
	t.Helper()

	current := payload
	for _, key := range keys {
		value, ok := current[key].(map[string]any)
		if !ok {
			t.Fatalf("%q is not an object in %#v", key, current)
		}
		current = value
	}
	return current
}

func nestedString(t *testing.T, payload map[string]any, keys ...string) string {
	t.Helper()

	parent := nestedMap(t, payload, keys[:len(keys)-1]...)
	value, ok := parent[keys[len(keys)-1]].(string)
	if !ok {
		t.Fatalf("%q is not a string in %#v", keys[len(keys)-1], parent)
	}
	return value
}

func nestedSlice(t *testing.T, payload map[string]any, key string) []any {
	t.Helper()

	value, ok := payload[key].([]any)
	if !ok {
		t.Fatalf("%q is not an array in %#v", key, payload)
	}
	return value
}

func assertPublicPlayersHaveNoTokens(t *testing.T, room map[string]any) {
	t.Helper()

	for _, value := range nestedSlice(t, room, "players") {
		player, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("player is not an object: %#v", value)
		}
		if _, ok := player["token"]; ok {
			t.Errorf("public player leaked token: %#v", player)
		}
	}
}

func scoreboardEntry(t *testing.T, gamePayload map[string]any, playerID string) map[string]any {
	t.Helper()

	for _, value := range nestedSlice(t, gamePayload, "scoreboard") {
		entry := value.(map[string]any)
		if entry["playerId"] == playerID {
			return entry
		}
	}
	t.Fatalf("scoreboard does not include player %q", playerID)
	return nil
}

func deleteNested(payload map[string]any, keys ...string) {
	parent := payload
	for _, key := range keys[:len(keys)-1] {
		parent, _ = parent[key].(map[string]any)
	}
	delete(parent, keys[len(keys)-1])
}

type fixedClock struct {
	now time.Time
}

func (clock *fixedClock) Now() time.Time {
	return clock.now
}
