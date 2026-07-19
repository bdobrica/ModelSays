package db_test

import (
	"context"
	"errors"
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
	"github.com/bogdandobrica/modelsays/backend/internal/llm"
	"github.com/bogdandobrica/modelsays/backend/internal/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

type semanticJudgeClient struct {
	*llm.StaticModelClient
}

func (client semanticJudgeClient) JudgeGuess(_ context.Context, request llm.JudgeGuessRequest) (*llm.JudgeGuessResponse, error) {
	answerID := request.Board.Answers[0].ID
	return &llm.JudgeGuessResponse{
		SuggestedPredictionAnswerID: &answerID,
		Confidence:                  0.93, RationaleCategory: "paraphrase",
		Metadata: llm.CallMetadata{Provider: "mock", Model: "gpt-4.1-mini", PromptVersion: "judge-v1"},
	}, nil
}

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

func TestPostgresTeamTotalsEqualScoreEventsAfterConcurrentPlayAndReload(t *testing.T) {
	pool := integrationPool(t)
	service := game.NewRoomService(db.NewPostgresRoomRepository(pool), nil)
	ctx := context.Background()
	room, host, err := service.CreateRoom(ctx, game.CreateRoomInput{RoomName: "Team audit", HostDisplayName: "Host",
		Settings: models.RoomSettings{Mode: models.GameModeTeams, TotalRounds: 1, AnswerTimerSeconds: 30}})
	if err != nil {
		t.Fatal(err)
	}
	room, guest, err := service.JoinRoom(ctx, game.JoinRoomInput{Code: room.Code, DisplayName: "Guest"})
	if err != nil {
		t.Fatal(err)
	}
	room, err = service.CreateTeam(ctx, game.CreateTeamInput{Code: room.Code, PlayerToken: host.Token, Name: "Blue"})
	if err != nil {
		t.Fatal(err)
	}
	room, err = service.CreateTeam(ctx, game.CreateTeamInput{Code: room.Code, PlayerToken: host.Token, Name: "Gold"})
	if err != nil {
		t.Fatal(err)
	}
	if room, err = service.AssignTeam(ctx, game.AssignTeamInput{Code: room.Code, PlayerToken: host.Token, PlayerID: host.ID, TeamID: room.Teams[0].ID}); err != nil {
		t.Fatal(err)
	}
	if room, err = service.AssignTeam(ctx, game.AssignTeamInput{Code: room.Code, PlayerToken: host.Token, PlayerID: guest.ID, TeamID: room.Teams[1].ID}); err != nil {
		t.Fatal(err)
	}
	room, err = service.StartGame(ctx, game.StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	round := room.CurrentGame.CurrentRound
	answers := round.Board.Answers[:2]
	var group sync.WaitGroup
	group.Add(2)
	for index, player := range []models.Player{host, guest} {
		go func(player models.Player, answer string) {
			defer group.Done()
			if _, submitErr := service.SubmitGuess(ctx, game.SubmitGuessInput{Code: room.Code, RoundID: round.ID, PlayerToken: player.Token, Answer: answer}); submitErr != nil {
				t.Errorf("submit guess: %v", submitErr)
			}
		}(player, answers[index].CanonicalAnswer)
	}
	group.Wait()
	reloaded, err := db.NewPostgresRoomRepository(pool).GetRoom(ctx, room.Code)
	if err != nil {
		t.Fatal(err)
	}
	eventTotals := make(map[string]int)
	rows, err := pool.Query(ctx, `SELECT p.team_id, SUM(s.delta) FROM score_events s JOIN players p ON p.id=s.player_id WHERE s.game_id=$1 GROUP BY p.team_id`, room.CurrentGame.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var teamID string
		var total int
		if err := rows.Scan(&teamID, &total); err != nil {
			t.Fatal(err)
		}
		eventTotals[teamID] = total
	}
	for _, team := range reloaded.CurrentGame.TeamScoreboard {
		if team.Score != eventTotals[team.TeamID] {
			t.Fatalf("team %s score=%d event sum=%d", team.Name, team.Score, eventTotals[team.TeamID])
		}
	}
}

func TestReplayMigrationDownAndUp(t *testing.T) {
	pool := integrationPool(t)
	path := migrationPath(t, "000010_game_replays.sql")
	executeMigrationSection(t, pool, path, false)
	if indexExists(t, pool, "games_replay_id_idx") {
		t.Fatal("replay index still exists after migration down")
	}
	executeMigrationSection(t, pool, path, true)
	if !indexExists(t, pool, "games_replay_id_idx") {
		t.Fatal("replay index missing after migration up")
	}
}

func TestTeamModeMigrationDownAndUp(t *testing.T) {
	pool := integrationPool(t)
	path := migrationPath(t, "000011_team_mode.sql")
	executeMigrationSection(t, pool, path, false)
	executeMigrationSection(t, pool, path, true)
}

func TestPostgresReplayAndPlayAgainIsolation(t *testing.T) {
	pool := integrationPool(t)
	service := game.NewRoomService(db.NewPostgresRoomRepository(pool), nil)
	ctx := context.Background()
	room, host, err := service.CreateRoom(ctx, game.CreateRoomInput{
		RoomName: "Replay isolation", HostDisplayName: "Host",
		Settings: models.RoomSettings{TotalRounds: 1, AnswerTimerSeconds: 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, player, err := service.JoinRoom(ctx, game.JoinRoomInput{Code: room.Code, DisplayName: "Player"})
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.StartGame(ctx, game.StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	answer := started.CurrentGame.CurrentRound.Board.Answers[0]
	if _, err := service.SubmitGuess(ctx, game.SubmitGuessInput{
		Code: room.Code, RoundID: started.CurrentGame.CurrentRound.ID,
		PlayerToken: player.Token, Answer: answer.CanonicalAnswer,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RevealRound(ctx, game.RevealRoundInput{
		Code: room.Code, RoundID: started.CurrentGame.CurrentRound.ID, PlayerToken: host.Token,
	}); err != nil {
		t.Fatal(err)
	}
	completed, err := service.NextRound(ctx, game.NextRoundInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	reloadedService := game.NewRoomService(db.NewPostgresRoomRepository(pool), nil)
	replay, err := reloadedService.GetReplay(ctx, completed.CurrentGame.ReplayID)
	if err != nil || len(replay.Rounds) != 1 || replay.Rankings[0].PlayerID != player.ID {
		t.Fatalf("reloaded replay=%#v err=%v", replay, err)
	}
	fresh, freshHost, err := reloadedService.PlayAgain(ctx, game.PlayAgainInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Code == room.Code || freshHost.Token == host.Token || fresh.CurrentGame != nil {
		t.Fatal("fresh lifecycle reused original state")
	}
	_, freshPlayer, err := reloadedService.JoinRoom(ctx, game.JoinRoomInput{Code: fresh.Code, DisplayName: "Player"})
	if err != nil {
		t.Fatal(err)
	}
	secondStarted, err := reloadedService.StartGame(ctx, game.StartGameInput{Code: fresh.Code, PlayerToken: freshHost.Token})
	if err != nil {
		t.Fatal(err)
	}
	if secondStarted.CurrentGame.ID == completed.CurrentGame.ID ||
		secondStarted.CurrentGame.CurrentRound.ID == completed.CurrentGame.CurrentRound.ID ||
		len(secondStarted.CurrentGame.CurrentRound.Guesses) != 0 ||
		secondStarted.CurrentGame.Scoreboard[0].Score != 0 {
		t.Fatal("second game inherited original game, round, guesses, or scores")
	}
	secondAnswer := secondStarted.CurrentGame.CurrentRound.Board.Answers[0]
	if _, err := reloadedService.SubmitGuess(ctx, game.SubmitGuessInput{
		Code: fresh.Code, RoundID: secondStarted.CurrentGame.CurrentRound.ID,
		PlayerToken: freshPlayer.Token, Answer: secondAnswer.CanonicalAnswer,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := reloadedService.RevealRound(ctx, game.RevealRoundInput{
		Code: fresh.Code, RoundID: secondStarted.CurrentGame.CurrentRound.ID, PlayerToken: freshHost.Token,
	}); err != nil {
		t.Fatal(err)
	}
	secondCompleted, err := reloadedService.NextRound(ctx, game.NextRoundInput{Code: fresh.Code, PlayerToken: freshHost.Token})
	if err != nil {
		t.Fatal(err)
	}
	finalService := game.NewRoomService(db.NewPostgresRoomRepository(pool), nil)
	secondReplay, err := finalService.GetReplay(ctx, secondCompleted.CurrentGame.ReplayID)
	if err != nil || len(secondReplay.Rounds) != 1 || secondReplay.Rankings[0].PlayerID != freshPlayer.ID {
		t.Fatalf("second replay=%#v err=%v", secondReplay, err)
	}
	originalReplay, err := finalService.GetReplay(ctx, completed.CurrentGame.ReplayID)
	if err != nil || originalReplay.Rankings[0].PlayerID != player.ID {
		t.Fatalf("original replay changed after second game: %#v err=%v", originalReplay, err)
	}
}

func TestPostgresDueRevealIsExactlyOnceAcrossWorkersAndRestart(t *testing.T) {
	pool := integrationPool(t)
	first := db.NewPostgresRoomRepository(pool)
	second := db.NewPostgresRoomRepository(pool)
	service := game.NewRoomService(first, nil)
	ctx := context.Background()
	room, host, err := service.CreateRoom(ctx, game.CreateRoomInput{
		RoomName: "Durable reveal", HostDisplayName: "Host",
		Settings: models.RoomSettings{Mode: models.GameModeSimultaneous, TotalRounds: 1,
			AnswerTimerSeconds: 15, Locale: "en", PredictionModel: "gpt-4.1-mini"},
	})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	started, err := service.StartGame(ctx, game.StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatalf("StartGame: %v", err)
	}
	dueAt := started.CurrentGame.CurrentRound.AnswerPhaseEndsAt
	count, err := first.RevealDueRounds(ctx, dueAt.Add(-time.Nanosecond), dueAt.Add(-time.Nanosecond), 25)
	if err != nil || count != 0 {
		t.Fatalf("before-cutoff delivery = (%d, %v), want (0, nil)", count, err)
	}
	lockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin simulated crashed worker: %v", err)
	}
	if _, err := lockTx.Exec(ctx, `SELECT id FROM rounds WHERE id = $1 FOR UPDATE`, started.CurrentGame.CurrentRound.ID); err != nil {
		t.Fatalf("lock round for simulated crash: %v", err)
	}
	count, err = second.RevealDueRounds(ctx, dueAt, dueAt, 25)
	if err != nil || count != 0 {
		t.Fatalf("locked delivery = (%d, %v), want skipped", count, err)
	}
	if err := lockTx.Rollback(ctx); err != nil {
		t.Fatalf("release simulated crashed-worker lock: %v", err)
	}
	start := make(chan struct{})
	results := make(chan int, 2)
	errs := make(chan error, 2)
	for _, repository := range []*db.PostgresRoomRepository{first, second} {
		go func(repository *db.PostgresRoomRepository) {
			<-start
			count, revealErr := repository.RevealDueRounds(ctx, dueAt, dueAt, 25)
			results <- count
			errs <- revealErr
		}(repository)
	}
	close(start)
	total := 0
	for range 2 {
		total += <-results
		if err := <-errs; err != nil {
			t.Fatalf("RevealDueRounds: %v", err)
		}
	}
	if total != 1 {
		t.Fatalf("transition count = %d, want exactly 1", total)
	}
	reloaded := db.NewPostgresRoomRepository(pool)
	count, err = reloaded.RevealDueRounds(ctx, dueAt.Add(time.Hour), dueAt.Add(time.Hour), 25)
	if err != nil || count != 0 {
		t.Fatalf("restart delivery = (%d, %v), want (0, nil)", count, err)
	}
	reloadedRoom, err := reloaded.GetRoom(ctx, room.Code)
	if err != nil {
		t.Fatalf("GetRoom: %v", err)
	}
	if reloadedRoom.CurrentGame.CurrentRound.Status != models.RoundStatusRevealed {
		t.Fatalf("round status = %s, want revealed", reloadedRoom.CurrentGame.CurrentRound.Status)
	}
	var transitions, events int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM round_transitions WHERE round_id = $1 AND actor = 'scheduler' AND reason = 'answer_deadline_elapsed'`,
		started.CurrentGame.CurrentRound.ID).Scan(&transitions); err != nil {
		t.Fatalf("count transitions: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM room_events WHERE room_code = $1 AND event_type = 'round_revealed'`,
		room.Code).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if transitions != 1 || events != 1 {
		t.Fatalf("transitions/events = %d/%d, want 1/1", transitions, events)
	}
}

func TestPostgresDueRevealRepresentativeBatchMeetsLagBudget(t *testing.T) {
	pool := integrationPool(t)
	repository := db.NewPostgresRoomRepository(pool)
	service := game.NewRoomService(repository, nil)
	ctx := context.Background()
	var latestDeadline time.Time
	for index := range 12 {
		room, host, err := service.CreateRoom(ctx, game.CreateRoomInput{
			RoomName: fmt.Sprintf("Transition load %d", index), HostDisplayName: "Host",
			Settings: models.RoomSettings{Mode: models.GameModeSimultaneous, TotalRounds: 1,
				AnswerTimerSeconds: 15, Locale: "en", PredictionModel: "gpt-4.1-mini"},
		})
		if err != nil {
			t.Fatalf("CreateRoom %d: %v", index, err)
		}
		started, err := service.StartGame(ctx, game.StartGameInput{Code: room.Code, PlayerToken: host.Token})
		if err != nil {
			t.Fatalf("StartGame %d: %v", index, err)
		}
		if started.CurrentGame.CurrentRound.AnswerPhaseEndsAt.After(latestDeadline) {
			latestDeadline = started.CurrentGame.CurrentRound.AnswerPhaseEndsAt
		}
	}
	startedAt := time.Now()
	count, err := repository.RevealDueRounds(ctx, latestDeadline, latestDeadline, 25)
	elapsed := time.Since(startedAt)
	if err != nil {
		t.Fatalf("RevealDueRounds: %v", err)
	}
	if count != 12 {
		t.Fatalf("revealed count = %d, want 12", count)
	}
	// The default 250 ms discovery interval leaves 750 ms of the one-second
	// PB-00 deadline-lag budget for the representative transition transaction.
	if elapsed >= 750*time.Millisecond {
		t.Fatalf("12-room transition transaction took %s, budget is <750ms", elapsed)
	}
}

func TestPostgresManualAndAutomaticRevealRaceHasOneWinner(t *testing.T) {
	pool := integrationPool(t)
	repository := db.NewPostgresRoomRepository(pool)
	service := game.NewRoomService(repository, nil)
	ctx := context.Background()
	room, host, err := service.CreateRoom(ctx, game.CreateRoomInput{
		RoomName: "Reveal race", HostDisplayName: "Host",
		Settings: models.RoomSettings{Mode: models.GameModeSimultaneous, TotalRounds: 1,
			AnswerTimerSeconds: 15, Locale: "en", PredictionModel: "gpt-4.1-mini"},
	})
	if err != nil {
		t.Fatalf("CreateRoom: %v", err)
	}
	started, err := service.StartGame(ctx, game.StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatalf("StartGame: %v", err)
	}
	round := started.CurrentGame.CurrentRound
	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		<-start
		_, revealErr := service.RevealRound(ctx, game.RevealRoundInput{
			Code: room.Code, RoundID: round.ID, PlayerToken: host.Token,
		})
		errs <- revealErr
	}()
	go func() {
		<-start
		_, revealErr := repository.RevealDueRounds(ctx, round.AnswerPhaseEndsAt, round.AnswerPhaseEndsAt, 25)
		errs <- revealErr
	}()
	close(start)
	for range 2 {
		if raceErr := <-errs; raceErr != nil && !errors.Is(raceErr, game.ErrRoundAlreadyRevealed) {
			t.Fatalf("reveal race error = %v", raceErr)
		}
	}
	var transitions, events int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM round_transitions WHERE round_id = $1`, round.ID).Scan(&transitions); err != nil {
		t.Fatalf("count transitions: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM room_events WHERE room_code = $1 AND event_type = 'round_revealed'`, room.Code).Scan(&events); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if transitions != 1 || events != 1 {
		t.Fatalf("transitions/events = %d/%d, want 1/1", transitions, events)
	}
}

func TestPostgresRoomEventsSurviveRepositoryReconstruction(t *testing.T) {
	pool := integrationPool(t)
	service := game.NewRoomService(db.NewPostgresRoomRepository(pool), nil)
	ctx := context.Background()
	room, _, err := service.CreateRoom(ctx, game.CreateRoomInput{RoomName: "Durable events", HostDisplayName: "Host"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.JoinRoom(ctx, game.JoinRoomInput{Code: room.Code, DisplayName: "Player"}); err != nil {
		t.Fatal(err)
	}
	reconstructed := game.NewRoomService(db.NewPostgresRoomRepository(pool), nil)
	events, err := reconstructed.ListRoomEvents(ctx, room.Code, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != models.RoomEventPlayerJoined || events[0].RoomRevision != 1 {
		t.Fatalf("reconstructed events = %+v", events)
	}
	refetched, err := reconstructed.GetRoom(ctx, room.Code)
	if err != nil || refetched.Revision != events[0].RoomRevision {
		t.Fatalf("refetched revision = %d, err=%v", refetched.Revision, err)
	}
}

func TestRoomEventsMigrationDownAndUp(t *testing.T) {
	pool := integrationPool(t)
	executeMigrationSection(t, pool, migrationPath(t, "000008_room_events.sql"), false)
	executeMigrationSection(t, pool, migrationPath(t, "000008_room_events.sql"), true)
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('room_events') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("room_events table missing after migration up")
	}
}

func TestRoundTransitionsMigrationDownAndUp(t *testing.T) {
	pool := integrationPool(t)
	path := migrationPath(t, "000009_round_transitions.sql")
	executeMigrationSection(t, pool, path, false)
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('round_transitions') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatalf("query transition table after down: %v", err)
	}
	if exists {
		t.Fatal("round_transitions still exists after down migration")
	}
	executeMigrationSection(t, pool, path, true)
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('round_transitions') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatalf("query transition table after up: %v", err)
	}
	if !exists || !indexExists(t, pool, "rounds_due_answering_idx") {
		t.Fatal("transition table or due index missing after up migration")
	}
}

func TestPostgresJudgeSuggestionReloadAndAuditableAcceptance(t *testing.T) {
	pool := integrationPool(t)
	repository := db.NewPostgresRoomRepository(pool)
	service := game.NewRoomService(repository, semanticJudgeClient{StaticModelClient: llm.NewStaticModelClient()})
	ctx := context.Background()
	room, host, err := service.CreateRoom(ctx, game.CreateRoomInput{RoomName: "Judge persistence", HostDisplayName: "Host"})
	if err != nil {
		t.Fatal(err)
	}
	_, player, _ := service.JoinRoom(ctx, game.JoinRoomInput{Code: room.Code, DisplayName: "Player"})
	started, err := service.StartGame(ctx, game.StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	round := started.CurrentGame.CurrentRound
	if _, err := service.SubmitGuess(ctx, game.SubmitGuessInput{
		Code: room.Code, RoundID: round.ID, PlayerToken: player.Token, Answer: "a reasonable paraphrase",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RevealRound(ctx, game.RevealRoundInput{Code: room.Code, RoundID: round.ID, PlayerToken: host.Token}); err != nil {
		t.Fatal(err)
	}
	suggestions, err := db.NewPostgresRoomRepository(pool).ListJudgeSuggestions(ctx, room.Code, round.ID)
	if err != nil || len(suggestions) != 1 {
		t.Fatalf("reloaded suggestions = %#v, err=%v", suggestions, err)
	}
	suggestion := suggestions[0]
	updated, err := service.OverrideMatch(ctx, game.OverrideMatchInput{
		Code: room.Code, RoundID: round.ID, GuessID: suggestion.GuessID, PlayerToken: host.Token,
		MatchedPredictionAnswerID: suggestion.SuggestedPredictionAnswerID, JudgeSuggestionID: suggestion.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if totalScore(updated) != round.Board.Answers[0].Score {
		t.Fatalf("score after suggestion acceptance = %d", totalScore(updated))
	}
	reloaded, err := repository.ListJudgeSuggestions(ctx, room.Code, round.ID)
	if err != nil || reloaded[0].ReviewedAt == nil || reloaded[0].ReviewDecision != "accepted_suggestion" {
		t.Fatalf("review was not persisted: %#v, err=%v", reloaded, err)
	}
	var eventCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM score_events
		WHERE game_id = $1 AND round_id = $2 AND reason = 'host_override_match'
	`, started.CurrentGame.ID, round.ID).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("override score event count=%d err=%v", eventCount, err)
	}
}

func TestPostgresRejectsJoinAfterGameStarts(t *testing.T) {
	pool := integrationPool(t)
	service := game.NewRoomService(db.NewPostgresRoomRepository(pool), nil)
	ctx := context.Background()

	room, host, err := service.CreateRoom(ctx, game.CreateRoomInput{RoomName: "Closed lobby", HostDisplayName: "Host"})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}
	if _, err := service.StartGame(ctx, game.StartGameInput{Code: room.Code, PlayerToken: host.Token}); err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}
	if _, _, err := service.JoinRoom(ctx, game.JoinRoomInput{Code: room.Code, DisplayName: "Late Player"}); !errors.Is(err, game.ErrRoomJoinClosed) {
		t.Fatalf("JoinRoom error = %v, want %v", err, game.ErrRoomJoinClosed)
	}
}

func TestPostgresRejectsSubmissionThatCrossesDeadlineWaitingForLock(t *testing.T) {
	pool := integrationPool(t)
	repository := db.NewPostgresRoomRepository(pool)
	startedAt := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	clock := &integrationClock{now: startedAt}
	service := game.NewRoomServiceWithClock(repository, nil, clock)
	ctx := context.Background()

	room, host, err := service.CreateRoom(ctx, game.CreateRoomInput{
		RoomName: "Deadline lock", HostDisplayName: "Host", Settings: models.RoomSettings{AnswerTimerSeconds: 30},
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}
	_, player, err := service.JoinRoom(ctx, game.JoinRoomInput{Code: room.Code, DisplayName: "Player"})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}
	startedRoom, err := service.StartGame(ctx, game.StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}
	round := startedRoom.CurrentGame.CurrentRound
	clock.Set(round.AnswerPhaseEndsAt.Add(-time.Nanosecond))

	lockTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin round lock transaction: %v", err)
	}
	defer lockTx.Rollback(ctx)
	if _, err := lockTx.Exec(ctx, `SELECT id FROM rounds WHERE id = $1 FOR UPDATE`, round.ID); err != nil {
		t.Fatalf("lock round: %v", err)
	}

	result := make(chan error, 1)
	go func() {
		_, submitErr := service.SubmitGuess(ctx, game.SubmitGuessInput{
			Code: room.Code, RoundID: round.ID, PlayerToken: player.Token, Answer: round.Board.Answers[0].CanonicalAnswer,
		})
		result <- submitErr
	}()

	waitForBlockedRoundLock(t, pool)
	clock.Set(round.AnswerPhaseEndsAt)
	if err := lockTx.Commit(ctx); err != nil {
		t.Fatalf("release round lock: %v", err)
	}

	select {
	case err := <-result:
		if err != game.ErrAnswerPhaseExpired {
			t.Fatalf("expected ErrAnswerPhaseExpired after crossing deadline, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for blocked submission")
	}

	var guessCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM guesses WHERE round_id = $1`, round.ID).Scan(&guessCount); err != nil {
		t.Fatalf("count guesses: %v", err)
	}
	if guessCount != 0 {
		t.Fatalf("expected no persisted guess after expiry, got %d", guessCount)
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

func TestPostgresProviderAuditHistoryIsRoomScoped(t *testing.T) {
	pool := integrationPool(t)
	repository := db.NewPostgresRoomRepository(pool)
	service := game.NewRoomService(repository, nil)
	ctx := context.Background()

	firstRoom, firstHost, err := service.CreateRoom(ctx, game.CreateRoomInput{RoomName: "First audit room", HostDisplayName: "Host One"})
	if err != nil {
		t.Fatal(err)
	}
	secondRoom, secondHost, err := service.CreateRoom(ctx, game.CreateRoomInput{RoomName: "Second audit room", HostDisplayName: "Host Two"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartGame(ctx, game.StartGameInput{Code: firstRoom.Code, PlayerToken: firstHost.Token}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartGame(ctx, game.StartGameInput{Code: secondRoom.Code, PlayerToken: secondHost.Token}); err != nil {
		t.Fatal(err)
	}

	firstAudits, err := db.NewPostgresRoomRepository(pool).ListProviderAudits(ctx, firstRoom.Code)
	if err != nil {
		t.Fatal(err)
	}
	if len(firstAudits) != 2 {
		t.Fatalf("first room audit count = %d, want 2", len(firstAudits))
	}
	for _, audit := range firstAudits {
		if audit.RoomCode != firstRoom.Code || audit.Provider != "static" || audit.EstimatedCostUSD != 0 || audit.RawResponse != "" {
			t.Fatalf("unexpected isolated fallback audit: %#v", audit)
		}
	}
	secondAudits, err := repository.ListProviderAudits(ctx, secondRoom.Code)
	if err != nil {
		t.Fatal(err)
	}
	if len(secondAudits) != 2 || secondAudits[0].GameID == firstAudits[0].GameID {
		t.Fatalf("second room audit history crossed room boundary: %#v", secondAudits)
	}
}

func TestProviderAuditMigrationUpDown(t *testing.T) {
	pool := integrationPool(t)
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('provider_call_audits') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("provider_call_audits table missing after migration up")
	}
	executeMigrationSection(t, pool, migrationPath(t, "000006_provider_call_audits.sql"), false)
	executeMigrationSection(t, pool, migrationPath(t, "000006_provider_call_audits.sql"), true)
}

func TestJudgeSuggestionMigrationUpDown(t *testing.T) {
	pool := integrationPool(t)
	var exists bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('judge_suggestions') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("judge_suggestions table missing after migration up")
	}
	executeMigrationSection(t, pool, migrationPath(t, "000007_judge_suggestions.sql"), false)
	executeMigrationSection(t, pool, migrationPath(t, "000007_judge_suggestions.sql"), true)
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

type integrationClock struct {
	mu  sync.RWMutex
	now time.Time
}

func (clock *integrationClock) Now() time.Time {
	clock.mu.RLock()
	defer clock.mu.RUnlock()
	return clock.now
}

func (clock *integrationClock) Set(now time.Time) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = now
}

func waitForBlockedRoundLock(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		err := pool.QueryRow(context.Background(), `
			SELECT EXISTS (
				SELECT 1
				FROM pg_stat_activity
				WHERE datname = current_database()
				  AND wait_event_type = 'Lock'
				  AND query LIKE '%lock round for guess submission%'
			)
		`).Scan(&blocked)
		if err != nil {
			t.Fatalf("query blocked round submission: %v", err)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("submission did not block on the round lock")
}
