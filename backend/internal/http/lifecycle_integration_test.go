package httpapi

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/config"
	"github.com/bogdandobrica/modelsays/backend/internal/db"
	"github.com/bogdandobrica/modelsays/backend/internal/game"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresAPIFullLifecycleSurvivesReload(t *testing.T) {
	pool := lifecyclePool(t)
	clock := &fixedClock{now: time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)}
	newServer := func() *Server {
		repository := db.NewPostgresRoomRepository(pool)
		service := game.NewRoomServiceWithClock(repository, nil, clock)
		return NewServer(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	}
	server := newServer()

	created := performRoomRequest(t, server, http.MethodPost, "/api/rooms", `{
		"roomName":"Lifecycle smoke","hostDisplayName":"Host",
		"settings":{"mode":"simultaneous","totalRounds":2,"answerTimerSeconds":15,"locale":"en","predictionModel":"gpt-5.6-luna","teamSafeMode":false}
	}`)
	code := nestedString(t, created, "room", "code")
	hostToken := nestedString(t, created, "player", "token")
	joined := performRoomRequest(t, server, http.MethodPost, fmt.Sprintf("/api/rooms/%s/join", code), `{"displayName":"Player"}`)
	playerToken := nestedString(t, joined, "player", "token")
	secondJoined := performRoomRequest(t, server, http.MethodPost, fmt.Sprintf("/api/rooms/%s/join", code), `{"displayName":"Second player"}`)
	secondPlayerToken := nestedString(t, secondJoined, "player", "token")

	started := performRoomRequest(t, server, http.MethodPost, fmt.Sprintf("/api/rooms/%s/start", code), fmt.Sprintf(`{"playerToken":%q}`, hostToken))
	firstRoundID := nestedString(t, started, "room", "currentGame", "currentRound", "id")
	firstBoardHash := nestedString(t, started, "room", "currentGame", "currentRound", "boardHash")
	if round := nestedMap(t, started, "room", "currentGame", "currentRound"); round["board"] != nil || round["guesses"] != nil {
		t.Fatalf("answering response leaked board or guesses: %#v", round)
	}
	assertLifecycleSession(t, server, code, hostToken, "Host", firstRoundID, firstBoardHash, "answering")
	assertLifecycleSession(t, server, code, playerToken, "Player", firstRoundID, firstBoardHash, "answering")
	assertLifecycleSession(t, server, code, secondPlayerToken, "Second player", firstRoundID, firstBoardHash, "answering")

	submitted := performRoomRequest(
		t, server, http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/rounds/%s/guesses", code, firstRoundID),
		fmt.Sprintf(`{"playerToken":%q,"answer":"crypto"}`, playerToken),
	)
	if round := nestedMap(t, submitted, "room", "currentGame", "currentRound"); round["board"] != nil || round["guesses"] != nil {
		t.Fatalf("submission response leaked board or guesses: %#v", round)
	}
	performRoomRequest(
		t, server, http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/rounds/%s/guesses", code, firstRoundID),
		fmt.Sprintf(`{"playerToken":%q,"answer":"bitcoin"}`, secondPlayerToken),
	)

	deadline, err := time.Parse(time.RFC3339Nano, nestedString(t, started, "room", "currentGame", "currentRound", "answerPhaseEndsAt"))
	if err != nil {
		t.Fatalf("parse answer deadline: %v", err)
	}
	clock.now = deadline
	assertLifecycleStatus(
		t, server, http.StatusConflict, http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/rounds/%s/guesses", code, firstRoundID),
		fmt.Sprintf(`{"playerToken":%q,"answer":"late answer"}`, hostToken),
	)

	// Rebuild the repository, service, and HTTP server before finishing the game.
	server = newServer()
	revealed := performRoomRequest(
		t, server, http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/rounds/%s/reveal", code, firstRoundID),
		fmt.Sprintf(`{"playerToken":%q}`, hostToken),
	)
	firstRound := nestedMap(t, revealed, "room", "currentGame", "currentRound")
	guesses := nestedSlice(t, firstRound, "guesses")
	board := nestedMap(t, firstRound, "board")
	answers := nestedSlice(t, board, "answers")
	if len(guesses) != 2 || len(answers) == 0 {
		t.Fatalf("revealed state has guesses=%d answers=%d", len(guesses), len(answers))
	}
	assertLifecycleSession(t, server, code, secondPlayerToken, "Second player", firstRoundID, firstBoardHash, "revealed")

	playerID := nestedString(t, joined, "player", "id")
	secondPlayerID := nestedString(t, secondJoined, "player", "id")
	var duplicateGuessID string
	for _, rawGuess := range guesses {
		guess := rawGuess.(map[string]any)
		if guess["playerId"] == playerID {
			if guess["duplicate"] != false || guess["scoreAwarded"].(float64) <= 0 {
				t.Fatalf("first equivalent claim did not score: %#v", guess)
			}
		}
		if guess["playerId"] == secondPlayerID {
			if guess["duplicate"] != true || guess["scoreAwarded"].(float64) != 0 {
				t.Fatalf("second equivalent claim was not a zero-point duplicate: %#v", guess)
			}
			duplicateGuessID = guess["id"].(string)
		}
	}
	if duplicateGuessID == "" {
		t.Fatal("second player's duplicate guess was not found")
	}
	secondAnswerID := answers[1].(map[string]any)["id"].(string)
	overridden := performRoomRequest(
		t, server, http.MethodPost, fmt.Sprintf("/api/rooms/%s/override-match", code),
		fmt.Sprintf(
			`{"playerToken":%q,"roundId":%q,"guessId":%q,"matchedPredictionAnswerId":%q}`,
			hostToken, firstRoundID, duplicateGuessID, secondAnswerID,
		),
	)
	if score := scoreboardEntry(t, nestedMap(t, overridden, "room", "currentGame"), playerID)["score"].(float64); score <= 0 {
		t.Fatalf("first player's score = %v, want a positive score", score)
	}
	if score := scoreboardEntry(t, nestedMap(t, overridden, "room", "currentGame"), secondPlayerID)["score"].(float64); score <= 0 {
		t.Fatalf("host override score = %v, want a positive score", score)
	}

	next := performRoomRequest(
		t, server, http.MethodPost, fmt.Sprintf("/api/rooms/%s/next-round", code),
		fmt.Sprintf(`{"playerToken":%q}`, hostToken),
	)
	secondRoundID := nestedString(t, next, "room", "currentGame", "currentRound", "id")
	performRoomRequest(
		t, server, http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/rounds/%s/guesses", code, secondRoundID),
		fmt.Sprintf(`{"playerToken":%q,"answer":"another miss"}`, playerToken),
	)
	performRoomRequest(
		t, server, http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/rounds/%s/reveal", code, secondRoundID),
		fmt.Sprintf(`{"playerToken":%q}`, hostToken),
	)
	completed := performRoomRequest(
		t, server, http.MethodPost, fmt.Sprintf("/api/rooms/%s/next-round", code),
		fmt.Sprintf(`{"playerToken":%q}`, hostToken),
	)
	if status := nestedString(t, completed, "room", "currentGame", "status"); status != "completed" {
		t.Fatalf("game status = %q, want completed", status)
	}

	reloaded := performRoomRequest(t, newServer(), http.MethodGet, fmt.Sprintf("/api/rooms/%s", code), "")
	if status := nestedString(t, reloaded, "room", "currentGame", "status"); status != "completed" {
		t.Fatalf("reloaded game status = %q, want completed", status)
	}
	if score := scoreboardEntry(t, nestedMap(t, reloaded, "room", "currentGame"), playerID)["score"].(float64); score <= 0 {
		t.Fatalf("reloaded player score = %v, want persisted positive score", score)
	}
	if score := scoreboardEntry(t, nestedMap(t, reloaded, "room", "currentGame"), secondPlayerID)["score"].(float64); score <= 0 {
		t.Fatalf("reloaded second player score = %v, want persisted positive score", score)
	}
}

func assertLifecycleSession(
	t *testing.T,
	server *Server,
	code string,
	playerToken string,
	displayName string,
	roundID string,
	boardHash string,
	roundStatus string,
) {
	t.Helper()
	session := performRoomRequest(
		t,
		server,
		http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/session", code),
		fmt.Sprintf(`{"playerToken":%q}`, playerToken),
	)
	if got := nestedString(t, session, "player", "displayName"); got != displayName {
		t.Fatalf("recovered display name = %q, want %q", got, displayName)
	}
	if got := nestedString(t, session, "room", "currentGame", "currentRound", "id"); got != roundID {
		t.Fatalf("recovered round = %q, want %q", got, roundID)
	}
	if got := nestedString(t, session, "room", "currentGame", "currentRound", "boardHash"); got != boardHash {
		t.Fatalf("recovered board hash = %q, want %q", got, boardHash)
	}
	if got := nestedString(t, session, "room", "currentGame", "currentRound", "status"); got != roundStatus {
		t.Fatalf("recovered round status = %q, want %q", got, roundStatus)
	}
}

func assertLifecycleStatus(t *testing.T, server *Server, want int, method string, path string, body string) {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(response, request)
	if response.Code != want {
		t.Fatalf("%s %s returned %d, want %d: %s", method, path, response.Code, want, response.Body.String())
	}
}

func lifecyclePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL lifecycle smoke test")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := fmt.Sprintf("modelsays_lifecycle_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create lifecycle schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
	})

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatalf("create lifecycle pool: %v", err)
	}
	t.Cleanup(pool.Close)

	migrations, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	sort.Strings(migrations)
	for _, path := range migrations {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", path, err)
		}
		upSQL := strings.TrimPrefix(strings.Split(string(contents), "-- +goose Down")[0], "-- +goose Up")
		if _, err := pool.Exec(ctx, upSQL); err != nil {
			t.Fatalf("apply migration %s: %v", filepath.Base(path), err)
		}
	}
	return pool
}
