package httpapi

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/config"
	"github.com/bogdandobrica/modelsays/backend/internal/game"
	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

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

type fixedClock struct {
	now time.Time
}

func (clock *fixedClock) Now() time.Time {
	return clock.now
}
