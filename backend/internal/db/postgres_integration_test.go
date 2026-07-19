package db_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/db"
	"github.com/bogdandobrica/modelsays/backend/internal/game"
	"github.com/bogdandobrica/modelsays/backend/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresAtomicAnswerClaimsAndReload(t *testing.T) {
	pool := integrationPool(t)
	repository := db.NewPostgresRoomRepository(pool)
	service := game.NewRoomService(repository, nil)
	ctx := context.Background()

	room, host, err := service.CreateRoom(ctx, game.CreateRoomInput{RoomName: "Atomic scoring", HostDisplayName: "Host"})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}
	_, playerOne, err := service.JoinRoom(ctx, game.JoinRoomInput{Code: room.Code, DisplayName: "Ana"})
	if err != nil {
		t.Fatalf("first JoinRoom returned error: %v", err)
	}
	_, playerTwo, err := service.JoinRoom(ctx, game.JoinRoomInput{Code: room.Code, DisplayName: "Radu"})
	if err != nil {
		t.Fatalf("second JoinRoom returned error: %v", err)
	}
	startedRoom, err := service.StartGame(ctx, game.StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}
	round := startedRoom.CurrentGame.CurrentRound
	answer := round.Board.Answers[0]
	aliases := []string{answer.CanonicalAnswer, answer.CanonicalAnswer}
	if len(answer.Aliases) > 0 {
		aliases[1] = answer.Aliases[0]
	}

	type result struct {
		room models.Room
		err  error
	}
	results := make(chan result, 2)
	var ready sync.WaitGroup
	ready.Add(2)
	start := make(chan struct{})
	submit := func(player models.Player, rawAnswer string) {
		ready.Done()
		<-start
		updatedRoom, submitErr := service.SubmitGuess(ctx, game.SubmitGuessInput{
			Code: room.Code, RoundID: round.ID, PlayerToken: player.Token, Answer: rawAnswer,
		})
		results <- result{room: updatedRoom, err: submitErr}
	}
	go submit(playerOne, aliases[0])
	go submit(playerTwo, aliases[1])
	ready.Wait()
	close(start)

	var finalRoom models.Room
	for range 2 {
		submission := <-results
		if submission.err != nil {
			t.Fatalf("concurrent SubmitGuess returned error: %v", submission.err)
		}
		finalRoom = submission.room
	}

	assertSingleClaim(t, finalRoom, answer.ID, answer.Score)

	var scoreEventCount int
	var scoreEventTotal int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(delta), 0)
		FROM score_events
		WHERE game_id = $1 AND round_id = $2
	`, startedRoom.CurrentGame.ID, round.ID).Scan(&scoreEventCount, &scoreEventTotal); err != nil {
		t.Fatalf("query score events: %v", err)
	}
	if scoreEventCount != 1 || scoreEventTotal != answer.Score {
		t.Fatalf("expected one score event totaling %d, got count=%d total=%d", answer.Score, scoreEventCount, scoreEventTotal)
	}

	reloadedRepository := db.NewPostgresRoomRepository(pool)
	reloadedRoom, err := reloadedRepository.GetRoom(ctx, room.Code)
	if err != nil {
		t.Fatalf("GetRoom after repository reload returned error: %v", err)
	}
	assertSingleClaim(t, reloadedRoom, answer.ID, answer.Score)
	if totalScore(reloadedRoom) != answer.Score {
		t.Fatalf("expected reconstructed scoreboard total %d, got %d", answer.Score, totalScore(reloadedRoom))
	}
}

func TestAtomicAnswerClaimMigrationUpDown(t *testing.T) {
	pool := integrationPool(t)

	if !indexExists(t, pool, "guesses_one_scoring_claim_per_answer_idx") {
		t.Fatal("expected atomic answer claim index after migration up")
	}
	executeMigrationSection(t, pool, migrationPath(t, "000005_atomic_answer_claims.sql"), false)
	if indexExists(t, pool, "guesses_one_scoring_claim_per_answer_idx") {
		t.Fatal("expected atomic answer claim index to be removed by migration down")
	}
	executeMigrationSection(t, pool, migrationPath(t, "000005_atomic_answer_claims.sql"), true)
	if !indexExists(t, pool, "guesses_one_scoring_claim_per_answer_idx") {
		t.Fatal("expected atomic answer claim index after reapplying migration up")
	}
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; skipping PostgreSQL integration test")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("create admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)

	schema := fmt.Sprintf("modelsays_test_%d", time.Now().UnixNano())
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
	})

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("create isolated test pool: %v", err)
	}
	t.Cleanup(pool.Close)

	matches, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil {
		t.Fatalf("list migrations: %v", err)
	}
	sort.Strings(matches)
	for _, path := range matches {
		executeMigrationSection(t, pool, path, true)
	}
	return pool
}

func executeMigrationSection(t *testing.T, pool *pgxpool.Pool, path string, up bool) {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", path, err)
	}
	parts := strings.Split(string(contents), "-- +goose Down")
	sql := strings.TrimPrefix(parts[0], "-- +goose Up")
	if !up {
		if len(parts) != 2 {
			t.Fatalf("migration %s has no down section", path)
		}
		sql = parts[1]
	}
	if _, err := pool.Exec(context.Background(), sql); err != nil {
		t.Fatalf("execute migration %s (up=%t): %v", filepath.Base(path), up, err)
	}
}

func migrationPath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("..", "..", "migrations", name)
}

func indexExists(t *testing.T, pool *pgxpool.Pool, indexName string) bool {
	t.Helper()
	var exists bool
	if err := pool.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1 FROM pg_indexes
			WHERE schemaname = current_schema() AND indexname = $1
		)
	`, indexName).Scan(&exists); err != nil {
		t.Fatalf("query index %q: %v", indexName, err)
	}
	return exists
}

func assertSingleClaim(t *testing.T, room models.Room, answerID string, score int) {
	t.Helper()
	positiveClaims := 0
	duplicateClaims := 0
	for _, guess := range room.CurrentGame.CurrentRound.Guesses {
		if guess.MatchedPredictionAnswerID == nil || *guess.MatchedPredictionAnswerID != answerID {
			continue
		}
		if guess.ScoreAwarded == score && !guess.Duplicate {
			positiveClaims++
		}
		if guess.ScoreAwarded == 0 && guess.Duplicate {
			duplicateClaims++
		}
	}
	if positiveClaims != 1 || duplicateClaims != 1 {
		t.Fatalf("expected one scoring claim and one duplicate, got scoring=%d duplicate=%d", positiveClaims, duplicateClaims)
	}
}

func totalScore(room models.Room) int {
	total := 0
	for _, entry := range room.CurrentGame.Scoreboard {
		total += entry.Score
	}
	return total
}
