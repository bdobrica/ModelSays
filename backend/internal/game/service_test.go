package game

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/llm"
	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

type fakeClock struct {
	mu  sync.RWMutex
	now time.Time
}

type fixedBoardModelClient struct {
	board models.PredictionBoard
}

func (client fixedBoardModelClient) GenerateQuestions(_ context.Context, _ llm.GenerateQuestionsRequest) (*llm.GenerateQuestionsResponse, error) {
	return &llm.GenerateQuestionsResponse{Questions: []models.Question{{
		ID: "question-1", Text: "Name something.", Locale: "en", Category: "party",
	}}}, nil
}

func (client fixedBoardModelClient) GenerateBoard(_ context.Context, _ llm.GenerateBoardRequest) (*llm.GenerateBoardResponse, error) {
	return &llm.GenerateBoardResponse{Board: client.board}, nil
}

func (clock *fakeClock) Now() time.Time {
	clock.mu.RLock()
	defer clock.mu.RUnlock()
	return clock.now
}

func (clock *fakeClock) Set(now time.Time) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = now
}

func TestCreateRoomAndJoinRoom(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName:        "Friday Night",
		HostDisplayName: "Bogdan",
		Settings: models.RoomSettings{
			Mode:        models.GameModeSimultaneous,
			TotalRounds: 6,
		},
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}

	if room.Code == "" {
		t.Fatal("expected room code to be generated")
	}
	if len(room.Players) != 1 {
		t.Fatalf("expected one host player, got %d", len(room.Players))
	}
	if !host.IsHost {
		t.Fatal("expected returned player to be host")
	}

	joinedRoom, player, err := service.JoinRoom(context.Background(), JoinRoomInput{
		Code:        room.Code,
		DisplayName: "Ana",
	})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}

	if player.IsHost {
		t.Fatal("expected joined player to not be host")
	}
	if len(joinedRoom.Players) != 2 {
		t.Fatalf("expected two players after join, got %d", len(joinedRoom.Players))
	}
}

func TestJoinRoomRejectsDuplicateNames(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	room, _, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName:        "Friday Night",
		HostDisplayName: "Bogdan",
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}

	_, _, err = service.JoinRoom(context.Background(), JoinRoomInput{
		Code:        room.Code,
		DisplayName: "bogdan",
	})
	if err != ErrDuplicatePlayer {
		t.Fatalf("expected ErrDuplicatePlayer, got %v", err)
	}
}

func TestStartGameCreatesCurrentRound(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName:        "Friday Night",
		HostDisplayName: "Bogdan",
		Settings: models.RoomSettings{
			Mode:               models.GameModeSimultaneous,
			TotalRounds:        6,
			AnswerTimerSeconds: 30,
			Locale:             "en",
		},
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}

	startedRoom, err := service.StartGame(context.Background(), StartGameInput{
		Code:        room.Code,
		PlayerToken: host.Token,
	})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}

	if startedRoom.Status != models.RoomStatusInGame {
		t.Fatalf("expected room status %q, got %q", models.RoomStatusInGame, startedRoom.Status)
	}
	if startedRoom.CurrentGame == nil {
		t.Fatal("expected current game to be present")
	}
	if startedRoom.CurrentGame.CurrentRound == nil {
		t.Fatal("expected current round to be present")
	}
	if startedRoom.CurrentGame.CurrentRound.RoundIndex != 1 {
		t.Fatalf("expected round index 1, got %d", startedRoom.CurrentGame.CurrentRound.RoundIndex)
	}
	if startedRoom.CurrentGame.CurrentRound.Question.Text == "" {
		t.Fatal("expected current round question to be populated")
	}
	if !startedRoom.CurrentGame.CurrentRound.AnswerPhaseEndsAt.After(startedRoom.CurrentGame.CurrentRound.AnswerPhaseStartedAt) {
		t.Fatal("expected answer phase end to be after start")
	}
}

func TestStartGameRequiresHostToken(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	room, _, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName:        "Friday Night",
		HostDisplayName: "Bogdan",
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}

	_, player, err := service.JoinRoom(context.Background(), JoinRoomInput{
		Code:        room.Code,
		DisplayName: "Ana",
	})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}

	_, err = service.StartGame(context.Background(), StartGameInput{
		Code:        room.Code,
		PlayerToken: player.Token,
	})
	if err != ErrUnauthorizedStart {
		t.Fatalf("expected ErrUnauthorizedStart, got %v", err)
	}
}

func TestStartGameRejectsSecondStart(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName:        "Friday Night",
		HostDisplayName: "Bogdan",
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}

	_, err = service.StartGame(context.Background(), StartGameInput{
		Code:        room.Code,
		PlayerToken: host.Token,
	})
	if err != nil {
		t.Fatalf("first StartGame returned error: %v", err)
	}

	_, err = service.StartGame(context.Background(), StartGameInput{
		Code:        room.Code,
		PlayerToken: host.Token,
	})
	if err != ErrGameAlreadyStarted {
		t.Fatalf("expected ErrGameAlreadyStarted, got %v", err)
	}
}

func TestSubmitGuessMatchesAliasAndScores(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName:        "Friday Night",
		HostDisplayName: "Bogdan",
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}

	_, player, err := service.JoinRoom(context.Background(), JoinRoomInput{
		Code:        room.Code,
		DisplayName: "Ana",
	})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}

	startedRoom, err := service.StartGame(context.Background(), StartGameInput{
		Code:        room.Code,
		PlayerToken: host.Token,
	})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}

	updatedRoom, err := service.SubmitGuess(context.Background(), SubmitGuessInput{
		Code:        room.Code,
		RoundID:     startedRoom.CurrentGame.CurrentRound.ID,
		PlayerToken: player.Token,
		Answer:      "bitcoin",
	})
	if err != nil {
		t.Fatalf("SubmitGuess returned error: %v", err)
	}

	if len(updatedRoom.CurrentGame.CurrentRound.Guesses) != 1 {
		t.Fatalf("expected one guess, got %d", len(updatedRoom.CurrentGame.CurrentRound.Guesses))
	}
	if updatedRoom.CurrentGame.CurrentRound.Guesses[0].ScoreAwarded != 50 {
		t.Fatalf("expected alias match score 50, got %d", updatedRoom.CurrentGame.CurrentRound.Guesses[0].ScoreAwarded)
	}
	if len(updatedRoom.CurrentGame.Scoreboard) < 2 {
		t.Fatalf("expected scoreboard entries for both players, got %d", len(updatedRoom.CurrentGame.Scoreboard))
	}
}

func TestNormalizeAnswerAndExactMatching(t *testing.T) {
	t.Parallel()

	answerID := "answer-1"
	answers := []models.PredictionAnswer{{
		ID:              answerID,
		CanonicalAnswer: "Crème brûlée",
		Aliases:         []string{"French dessert"},
		Score:           40,
	}}
	tests := []struct {
		name           string
		input          string
		wantNormalized string
		wantMatch      bool
	}{
		{name: "capitalization", input: "CRÈME BRÛLÉE", wantNormalized: "crème brûlée", wantMatch: true},
		{name: "repeated whitespace", input: "  crème\t\n brûlée  ", wantNormalized: "crème brûlée", wantMatch: true},
		{name: "punctuation", input: "Crème, brûlée!!!", wantNormalized: "crème brûlée", wantMatch: true},
		{name: "accented characters remain significant", input: "creme brulee", wantNormalized: "creme brulee", wantMatch: false},
		{name: "alias", input: "FRENCH... DESSERT", wantNormalized: "french dessert", wantMatch: true},
		{name: "semantic equivalent is not inferred", input: "custard", wantNormalized: "custard", wantMatch: false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			normalized := normalizeAnswer(test.input)
			if normalized != test.wantNormalized {
				t.Fatalf("normalizeAnswer(%q) = %q, want %q", test.input, normalized, test.wantNormalized)
			}
			matchedID, score := matchGuess(answers, normalized)
			if !test.wantMatch {
				if matchedID != nil || score != 0 {
					t.Fatalf("expected no exact canonical/alias match, got id=%v score=%d", matchedID, score)
				}
				return
			}
			if matchedID == nil || *matchedID != answerID || score != 40 {
				t.Fatalf("expected answer %q with score 40, got id=%v score=%d", answerID, matchedID, score)
			}
		})
	}
}

func TestValidateMatchingAliasesRejectsAmbiguousOwnership(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		answers []models.PredictionAnswer
		wantErr bool
	}{
		{
			name: "distinct normalized phrases",
			answers: []models.PredictionAnswer{
				{CanonicalAnswer: "Artificial intelligence", Aliases: []string{"AI"}},
				{CanonicalAnswer: "Machine learning", Aliases: []string{"ML"}},
			},
		},
		{
			name: "duplicate alias within one answer is harmless",
			answers: []models.PredictionAnswer{
				{CanonicalAnswer: "Artificial intelligence", Aliases: []string{"AI", "A.I."}},
			},
		},
		{
			name: "alias owned by two answers",
			answers: []models.PredictionAnswer{
				{CanonicalAnswer: "Artificial intelligence", Aliases: []string{"AI"}},
				{CanonicalAnswer: "AI assistant", Aliases: []string{"A.I."}},
			},
			wantErr: true,
		},
		{
			name: "canonical collides with another alias",
			answers: []models.PredictionAnswer{
				{CanonicalAnswer: "Crème brûlée"},
				{CanonicalAnswer: "Dessert", Aliases: []string{"CRÈME, BRÛLÉE!"}},
			},
			wantErr: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateMatchingAliases(test.answers)
			if test.wantErr && !errors.Is(err, ErrAmbiguousBoardAnswer) {
				t.Fatalf("expected ErrAmbiguousBoardAnswer, got %v", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("expected valid board matching phrases, got %v", err)
			}
		})
	}
}

func TestStartGameRejectsBoardWithAmbiguousAliasOwnership(t *testing.T) {
	t.Parallel()

	modelClient := fixedBoardModelClient{board: models.PredictionBoard{
		Provider: "test",
		Answers: []models.PredictionAnswer{
			{CanonicalAnswer: "Artificial intelligence", Aliases: []string{"AI"}, Rank: 1, Score: 50},
			{CanonicalAnswer: "AI assistant", Aliases: []string{"A.I."}, Rank: 2, Score: 30},
		},
	}}
	service := NewRoomService(NewInMemoryRoomRepository(), modelClient)
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName: "Ambiguous board", HostDisplayName: "Host",
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}

	_, err = service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if !errors.Is(err, ErrAmbiguousBoardAnswer) {
		t.Fatalf("expected ErrAmbiguousBoardAnswer, got %v", err)
	}
}

func TestSubmitGuessMatchesCanonicalAnswer(t *testing.T) {
	t.Parallel()

	service, room, _, player, startedRoom := startedGameWithPlayer(t)
	canonical := startedRoom.CurrentGame.CurrentRound.Board.Answers[0]

	updatedRoom, err := service.SubmitGuess(context.Background(), SubmitGuessInput{
		Code:        room.Code,
		RoundID:     startedRoom.CurrentGame.CurrentRound.ID,
		PlayerToken: player.Token,
		Answer:      canonical.CanonicalAnswer,
	})
	if err != nil {
		t.Fatalf("SubmitGuess returned error: %v", err)
	}

	guess := updatedRoom.CurrentGame.CurrentRound.Guesses[0]
	if guess.MatchedPredictionAnswerID == nil || *guess.MatchedPredictionAnswerID != canonical.ID {
		t.Fatalf("expected canonical answer %q to match %q", canonical.CanonicalAnswer, canonical.ID)
	}
	if guess.ScoreAwarded != canonical.Score || guess.Duplicate {
		t.Fatalf("expected canonical score %d without duplicate, got score=%d duplicate=%t", canonical.Score, guess.ScoreAwarded, guess.Duplicate)
	}
}

func TestSubmitGuessMissScoresZero(t *testing.T) {
	t.Parallel()

	service, room, _, player, startedRoom := startedGameWithPlayer(t)
	updatedRoom, err := service.SubmitGuess(context.Background(), SubmitGuessInput{
		Code:        room.Code,
		RoundID:     startedRoom.CurrentGame.CurrentRound.ID,
		PlayerToken: player.Token,
		Answer:      "definitely not on this board",
	})
	if err != nil {
		t.Fatalf("SubmitGuess returned error: %v", err)
	}

	guess := updatedRoom.CurrentGame.CurrentRound.Guesses[0]
	if guess.MatchedPredictionAnswerID != nil || guess.ScoreAwarded != 0 || guess.Duplicate {
		t.Fatalf("expected a zero-score miss, got match=%v score=%d duplicate=%t", guess.MatchedPredictionAnswerID, guess.ScoreAwarded, guess.Duplicate)
	}
}

func TestSubmitGuessRejectsDuplicateSubmission(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName:        "Friday Night",
		HostDisplayName: "Bogdan",
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}

	_, player, err := service.JoinRoom(context.Background(), JoinRoomInput{
		Code:        room.Code,
		DisplayName: "Ana",
	})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}

	startedRoom, err := service.StartGame(context.Background(), StartGameInput{
		Code:        room.Code,
		PlayerToken: host.Token,
	})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}

	_, err = service.SubmitGuess(context.Background(), SubmitGuessInput{
		Code:        room.Code,
		RoundID:     startedRoom.CurrentGame.CurrentRound.ID,
		PlayerToken: player.Token,
		Answer:      "bitcoin",
	})
	if err != nil {
		t.Fatalf("first SubmitGuess returned error: %v", err)
	}

	_, err = service.SubmitGuess(context.Background(), SubmitGuessInput{
		Code:        room.Code,
		RoundID:     startedRoom.CurrentGame.CurrentRound.ID,
		PlayerToken: player.Token,
		Answer:      "crypto",
	})
	if err != ErrGuessAlreadySubmitted {
		t.Fatalf("expected ErrGuessAlreadySubmitted, got %v", err)
	}
}

func TestSubmitGuessEnforcesAnswerDeadline(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		now     time.Time
		wantErr error
	}{
		{name: "just before deadline", now: startedAt.Add(30*time.Second - time.Nanosecond)},
		{name: "exactly at deadline", now: startedAt.Add(30 * time.Second), wantErr: ErrAnswerPhaseExpired},
		{name: "after deadline", now: startedAt.Add(30*time.Second + time.Nanosecond), wantErr: ErrAnswerPhaseExpired},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			clock := &fakeClock{now: startedAt}
			service := NewRoomServiceWithClock(NewInMemoryRoomRepository(), nil, clock)
			room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
				RoomName:        "Deadline room",
				HostDisplayName: "Host",
				Settings:        models.RoomSettings{AnswerTimerSeconds: 30},
			})
			if err != nil {
				t.Fatalf("CreateRoom returned error: %v", err)
			}
			_, player, err := service.JoinRoom(context.Background(), JoinRoomInput{Code: room.Code, DisplayName: "Player"})
			if err != nil {
				t.Fatalf("JoinRoom returned error: %v", err)
			}
			startedRoom, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token})
			if err != nil {
				t.Fatalf("StartGame returned error: %v", err)
			}

			clock.Set(test.now)
			updatedRoom, err := service.SubmitGuess(context.Background(), SubmitGuessInput{
				Code: room.Code, RoundID: startedRoom.CurrentGame.CurrentRound.ID, PlayerToken: player.Token, Answer: "bitcoin",
			})
			if err != test.wantErr {
				t.Fatalf("expected error %v, got %v", test.wantErr, err)
			}
			if test.wantErr == nil && len(updatedRoom.CurrentGame.CurrentRound.Guesses) != 1 {
				t.Fatalf("expected accepted guess just before deadline")
			}
		})
	}
}

func TestRevealRoundAllowsEarlyReveal(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: startedAt}
	service := NewRoomServiceWithClock(NewInMemoryRoomRepository(), nil, clock)
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName: "Early reveal", HostDisplayName: "Host", Settings: models.RoomSettings{AnswerTimerSeconds: 30},
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}
	startedRoom, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}

	clock.Set(startedAt.Add(time.Second))
	revealedRoom, err := service.RevealRound(context.Background(), RevealRoundInput{
		Code: room.Code, RoundID: startedRoom.CurrentGame.CurrentRound.ID, PlayerToken: host.Token,
	})
	if err != nil {
		t.Fatalf("RevealRound returned error before deadline: %v", err)
	}
	if revealedRoom.CurrentGame.CurrentRound.Status != models.RoundStatusRevealed {
		t.Fatalf("expected early reveal to reveal round")
	}
}

func TestRevealRoundTransitionsState(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName:        "Friday Night",
		HostDisplayName: "Bogdan",
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}

	startedRoom, err := service.StartGame(context.Background(), StartGameInput{
		Code:        room.Code,
		PlayerToken: host.Token,
	})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}

	revealedRoom, err := service.RevealRound(context.Background(), RevealRoundInput{
		Code:        room.Code,
		RoundID:     startedRoom.CurrentGame.CurrentRound.ID,
		PlayerToken: host.Token,
	})
	if err != nil {
		t.Fatalf("RevealRound returned error: %v", err)
	}

	if revealedRoom.CurrentGame.CurrentRound.Status != models.RoundStatusRevealed {
		t.Fatalf("expected round status %q, got %q", models.RoundStatusRevealed, revealedRoom.CurrentGame.CurrentRound.Status)
	}
	if revealedRoom.CurrentGame.CurrentRound.RevealStartedAt == nil {
		t.Fatal("expected reveal timestamp to be set")
	}
}

func TestNextRoundStartsNewRoundAndResetsSubmissionFlags(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName:        "Friday Night",
		HostDisplayName: "Bogdan",
		Settings: models.RoomSettings{
			TotalRounds:        2,
			AnswerTimerSeconds: 30,
		},
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}

	_, player, err := service.JoinRoom(context.Background(), JoinRoomInput{
		Code:        room.Code,
		DisplayName: "Ana",
	})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}

	startedRoom, err := service.StartGame(context.Background(), StartGameInput{
		Code:        room.Code,
		PlayerToken: host.Token,
	})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}

	_, err = service.SubmitGuess(context.Background(), SubmitGuessInput{
		Code:        room.Code,
		RoundID:     startedRoom.CurrentGame.CurrentRound.ID,
		PlayerToken: player.Token,
		Answer:      "bitcoin",
	})
	if err != nil {
		t.Fatalf("SubmitGuess returned error: %v", err)
	}

	revealedRoom, err := service.RevealRound(context.Background(), RevealRoundInput{
		Code:        room.Code,
		RoundID:     startedRoom.CurrentGame.CurrentRound.ID,
		PlayerToken: host.Token,
	})
	if err != nil {
		t.Fatalf("RevealRound returned error: %v", err)
	}

	nextRoundRoom, err := service.NextRound(context.Background(), NextRoundInput{
		Code:        room.Code,
		PlayerToken: host.Token,
	})
	if err != nil {
		t.Fatalf("NextRound returned error: %v", err)
	}

	if nextRoundRoom.CurrentGame.CurrentRound.RoundIndex != 2 {
		t.Fatalf("expected round index 2, got %d", nextRoundRoom.CurrentGame.CurrentRound.RoundIndex)
	}
	if nextRoundRoom.CurrentGame.CurrentRound.Status != models.RoundStatusAnswering {
		t.Fatalf("expected next round status %q, got %q", models.RoundStatusAnswering, nextRoundRoom.CurrentGame.CurrentRound.Status)
	}
	if nextRoundRoom.CurrentGame.CurrentRound.Question.ID == revealedRoom.CurrentGame.CurrentRound.Question.ID {
		t.Fatal("expected next round question to change")
	}
	for _, entry := range nextRoundRoom.CurrentGame.Scoreboard {
		if entry.SubmissionMade {
			t.Fatal("expected submission flags to reset for the next round")
		}
	}
}

func TestNextRoundCompletesGameAfterFinalRound(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName:        "Friday Night",
		HostDisplayName: "Bogdan",
		Settings: models.RoomSettings{
			TotalRounds:        1,
			AnswerTimerSeconds: 30,
		},
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}

	startedRoom, err := service.StartGame(context.Background(), StartGameInput{
		Code:        room.Code,
		PlayerToken: host.Token,
	})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}

	_, err = service.RevealRound(context.Background(), RevealRoundInput{
		Code:        room.Code,
		RoundID:     startedRoom.CurrentGame.CurrentRound.ID,
		PlayerToken: host.Token,
	})
	if err != nil {
		t.Fatalf("RevealRound returned error: %v", err)
	}

	completedRoom, err := service.NextRound(context.Background(), NextRoundInput{
		Code:        room.Code,
		PlayerToken: host.Token,
	})
	if err != nil {
		t.Fatalf("NextRound returned error: %v", err)
	}

	if completedRoom.CurrentGame.Status != models.GameStatusCompleted {
		t.Fatalf("expected completed game status, got %q", completedRoom.CurrentGame.Status)
	}
	if completedRoom.CurrentGame.EndedAt == nil {
		t.Fatal("expected endedAt to be set on game completion")
	}
}

func TestDuplicateAnswerScoresZero(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName:        "Friday Night",
		HostDisplayName: "Bogdan",
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}

	_, playerOne, err := service.JoinRoom(context.Background(), JoinRoomInput{Code: room.Code, DisplayName: "Ana"})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}
	_, playerTwo, err := service.JoinRoom(context.Background(), JoinRoomInput{Code: room.Code, DisplayName: "Radu"})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}

	startedRoom, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}

	updatedRoom, err := service.SubmitGuess(context.Background(), SubmitGuessInput{Code: room.Code, RoundID: startedRoom.CurrentGame.CurrentRound.ID, PlayerToken: playerOne.Token, Answer: "bitcoin"})
	if err != nil {
		t.Fatalf("first SubmitGuess returned error: %v", err)
	}

	updatedRoom, err = service.SubmitGuess(context.Background(), SubmitGuessInput{Code: room.Code, RoundID: startedRoom.CurrentGame.CurrentRound.ID, PlayerToken: playerTwo.Token, Answer: "crypto"})
	if err != nil {
		t.Fatalf("second SubmitGuess returned error: %v", err)
	}

	if len(updatedRoom.CurrentGame.CurrentRound.Guesses) != 2 {
		t.Fatalf("expected two guesses, got %d", len(updatedRoom.CurrentGame.CurrentRound.Guesses))
	}
	if updatedRoom.CurrentGame.CurrentRound.Guesses[1].ScoreAwarded != 0 {
		t.Fatalf("expected duplicate guess to score 0, got %d", updatedRoom.CurrentGame.CurrentRound.Guesses[1].ScoreAwarded)
	}
	if !updatedRoom.CurrentGame.CurrentRound.Guesses[1].Duplicate {
		t.Fatal("expected duplicate guess to be flagged")
	}
}

func TestOverrideMatchAdjustsScore(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName:        "Friday Night",
		HostDisplayName: "Bogdan",
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}

	_, player, err := service.JoinRoom(context.Background(), JoinRoomInput{Code: room.Code, DisplayName: "Ana"})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}

	startedRoom, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}

	updatedRoom, err := service.SubmitGuess(context.Background(), SubmitGuessInput{Code: room.Code, RoundID: startedRoom.CurrentGame.CurrentRound.ID, PlayerToken: player.Token, Answer: "bitcoin"})
	if err != nil {
		t.Fatalf("SubmitGuess returned error: %v", err)
	}

	revealedRoom, err := service.RevealRound(context.Background(), RevealRoundInput{Code: room.Code, RoundID: startedRoom.CurrentGame.CurrentRound.ID, PlayerToken: host.Token})
	if err != nil {
		t.Fatalf("RevealRound returned error: %v", err)
	}

	guess := updatedRoom.CurrentGame.CurrentRound.Guesses[0]
	answerID := revealedRoom.CurrentGame.CurrentRound.Board.Answers[2].ID
	overriddenRoom, err := service.OverrideMatch(context.Background(), OverrideMatchInput{
		Code:                      room.Code,
		RoundID:                   startedRoom.CurrentGame.CurrentRound.ID,
		GuessID:                   guess.ID,
		PlayerToken:               host.Token,
		MatchedPredictionAnswerID: &answerID,
	})
	if err != nil {
		t.Fatalf("OverrideMatch returned error: %v", err)
	}

	if overriddenRoom.CurrentGame.CurrentRound.Guesses[0].ScoreAwarded != 25 {
		t.Fatalf("expected overridden score 25, got %d", overriddenRoom.CurrentGame.CurrentRound.Guesses[0].ScoreAwarded)
	}
}

func TestResolveOverrideSupportsHitToMissAndMissToHit(t *testing.T) {
	t.Parallel()

	answerID := "answer-1"
	board := &models.PredictionBoard{Answers: []models.PredictionAnswer{{ID: answerID, Score: 40}}}
	tests := []struct {
		name      string
		guess     models.Guess
		matchID   *string
		wantScore int
		wantDelta int
	}{
		{
			name:      "hit to miss",
			guess:     models.Guess{ID: "guess-1", MatchedPredictionAnswerID: &answerID, ScoreAwarded: 40},
			matchID:   nil,
			wantScore: 0,
			wantDelta: -40,
		},
		{
			name:      "miss to hit",
			guess:     models.Guess{ID: "guess-1"},
			matchID:   &answerID,
			wantScore: 40,
			wantDelta: 40,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			updated, delta, err := ResolveOverride(
				GuessOverride{GuessID: test.guess.ID, MatchedPredictionAnswerID: test.matchID},
				board,
				[]models.Guess{test.guess},
			)
			if err != nil {
				t.Fatalf("ResolveOverride returned error: %v", err)
			}
			if updated.ScoreAwarded != test.wantScore || delta != test.wantDelta || updated.Duplicate {
				t.Fatalf("got score=%d delta=%d duplicate=%t, want score=%d delta=%d duplicate=false", updated.ScoreAwarded, delta, updated.Duplicate, test.wantScore, test.wantDelta)
			}
		})
	}
}

func TestOverrideCannotCreateSecondScoringClaim(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{RoomName: "Friday Night", HostDisplayName: "Bogdan"})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}
	_, playerOne, err := service.JoinRoom(context.Background(), JoinRoomInput{Code: room.Code, DisplayName: "Ana"})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}
	_, playerTwo, err := service.JoinRoom(context.Background(), JoinRoomInput{Code: room.Code, DisplayName: "Radu"})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}
	startedRoom, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}
	roundID := startedRoom.CurrentGame.CurrentRound.ID
	claimedAnswer := startedRoom.CurrentGame.CurrentRound.Board.Answers[0]

	_, err = service.SubmitGuess(context.Background(), SubmitGuessInput{Code: room.Code, RoundID: roundID, PlayerToken: playerOne.Token, Answer: claimedAnswer.CanonicalAnswer})
	if err != nil {
		t.Fatalf("first SubmitGuess returned error: %v", err)
	}
	secondRoom, err := service.SubmitGuess(context.Background(), SubmitGuessInput{Code: room.Code, RoundID: roundID, PlayerToken: playerTwo.Token, Answer: "not a match"})
	if err != nil {
		t.Fatalf("second SubmitGuess returned error: %v", err)
	}
	revealedRoom, err := service.RevealRound(context.Background(), RevealRoundInput{Code: room.Code, RoundID: roundID, PlayerToken: host.Token})
	if err != nil {
		t.Fatalf("RevealRound returned error: %v", err)
	}

	secondGuess := findPlayerGuess(t, secondRoom.CurrentGame.CurrentRound.Guesses, playerTwo.ID)
	overriddenRoom, err := service.OverrideMatch(context.Background(), OverrideMatchInput{
		Code:                      room.Code,
		RoundID:                   roundID,
		GuessID:                   secondGuess.ID,
		PlayerToken:               host.Token,
		MatchedPredictionAnswerID: &claimedAnswer.ID,
	})
	if err != nil {
		t.Fatalf("OverrideMatch returned error: %v", err)
	}

	firstGuess := findPlayerGuess(t, overriddenRoom.CurrentGame.CurrentRound.Guesses, playerOne.ID)
	secondGuess = findPlayerGuess(t, overriddenRoom.CurrentGame.CurrentRound.Guesses, playerTwo.ID)
	if firstGuess.ScoreAwarded != claimedAnswer.Score {
		t.Fatalf("expected original claim to retain score %d, got %d", claimedAnswer.Score, firstGuess.ScoreAwarded)
	}
	if secondGuess.ScoreAwarded != 0 || !secondGuess.Duplicate {
		t.Fatalf("expected override to remain a zero-score duplicate, got score=%d duplicate=%t", secondGuess.ScoreAwarded, secondGuess.Duplicate)
	}
	if scoreForPlayer(overriddenRoom, playerTwo.ID) != 0 {
		t.Fatalf("expected overridden duplicate player's scoreboard to remain zero")
	}
	if revealedRoom.CurrentGame == nil {
		t.Fatal("expected revealed game")
	}
}

func startedGameWithPlayer(t *testing.T) (*RoomService, models.Room, models.Player, models.Player, models.Room) {
	t.Helper()

	service := NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{RoomName: "Friday Night", HostDisplayName: "Bogdan"})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}
	_, player, err := service.JoinRoom(context.Background(), JoinRoomInput{Code: room.Code, DisplayName: "Ana"})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}
	startedRoom, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}
	return service, room, host, player, startedRoom
}

func findPlayerGuess(t *testing.T, guesses []models.Guess, playerID string) models.Guess {
	t.Helper()
	for _, guess := range guesses {
		if guess.PlayerID == playerID {
			return guess
		}
	}
	t.Fatalf("guess for player %q not found", playerID)
	return models.Guess{}
}

func scoreForPlayer(room models.Room, playerID string) int {
	for _, entry := range room.CurrentGame.Scoreboard {
		if entry.PlayerID == playerID {
			return entry.Score
		}
	}
	return 0
}
