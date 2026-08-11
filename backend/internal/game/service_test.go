package game

import (
	"context"
	"errors"
	"strings"
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

type scriptedModelClient struct {
	questionResponses []*llm.GenerateQuestionsResponse
	questionErrors    []error
	boardResponses    []*llm.GenerateBoardResponse
	boardErrors       []error
	questionCalls     int
	boardCalls        int
}

type costlyQuestionClient struct {
	boardCalls int
}

type judgeModelClient struct {
	fixedBoardModelClient
	responses []*llm.JudgeGuessResponse
	errors    []error
	calls     int
}

type denyProviderGate struct{ calls int }

func (gate *denyProviderGate) AllowProvider(string) (bool, time.Duration) {
	gate.calls++
	return false, time.Minute
}

func (client *judgeModelClient) JudgeGuess(ctx context.Context, _ llm.JudgeGuessRequest) (*llm.JudgeGuessResponse, error) {
	index := client.calls
	client.calls++
	if index < len(client.errors) && client.errors[index] != nil {
		if errors.Is(client.errors[index], context.DeadlineExceeded) {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return nil, client.errors[index]
	}
	if index < len(client.responses) {
		return client.responses[index], nil
	}
	return nil, errors.New("no scripted judge response")
}

type timeoutModelClient struct{}

func (timeoutModelClient) GenerateQuestions(ctx context.Context, _ llm.GenerateQuestionsRequest) (*llm.GenerateQuestionsResponse, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (timeoutModelClient) GenerateBoard(_ context.Context, _ llm.GenerateBoardRequest) (*llm.GenerateBoardResponse, error) {
	panic("board generation must not run after question timeout")
}

func (client *costlyQuestionClient) GenerateQuestions(_ context.Context, _ llm.GenerateQuestionsRequest) (*llm.GenerateQuestionsResponse, error) {
	return &llm.GenerateQuestionsResponse{
		Questions: []models.Question{generatedQuestion("costly", "Name something costly.")},
		Metadata:  llm.CallMetadata{Provider: "paid", Model: "gpt-5.6-luna", PromptVersion: "v1", EstimatedCostUSD: 0.11},
	}, nil
}

func (client *costlyQuestionClient) GenerateBoard(_ context.Context, _ llm.GenerateBoardRequest) (*llm.GenerateBoardResponse, error) {
	client.boardCalls++
	return &llm.GenerateBoardResponse{Board: validGeneratedBoard()}, nil
}

func (client fixedBoardModelClient) GenerateQuestions(_ context.Context, _ llm.GenerateQuestionsRequest) (*llm.GenerateQuestionsResponse, error) {
	return &llm.GenerateQuestionsResponse{Questions: []models.Question{{
		ID: "question-1", Text: "Name something.", Locale: "en", Category: "party",
	}}}, nil
}

func (client fixedBoardModelClient) GenerateBoard(_ context.Context, _ llm.GenerateBoardRequest) (*llm.GenerateBoardResponse, error) {
	return &llm.GenerateBoardResponse{Board: client.board}, nil
}

func (client *scriptedModelClient) GenerateQuestions(_ context.Context, _ llm.GenerateQuestionsRequest) (*llm.GenerateQuestionsResponse, error) {
	index := client.questionCalls
	client.questionCalls++
	if index < len(client.questionErrors) && client.questionErrors[index] != nil {
		return nil, client.questionErrors[index]
	}
	if index < len(client.questionResponses) {
		return client.questionResponses[index], nil
	}
	return nil, errors.New("no scripted question response")
}

func (client *scriptedModelClient) GenerateBoard(_ context.Context, _ llm.GenerateBoardRequest) (*llm.GenerateBoardResponse, error) {
	index := client.boardCalls
	client.boardCalls++
	if index < len(client.boardErrors) && client.boardErrors[index] != nil {
		return nil, client.boardErrors[index]
	}
	if index < len(client.boardResponses) {
		return client.boardResponses[index], nil
	}
	return nil, errors.New("no scripted board response")
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
			TotalRounds: 5,
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

func TestSemanticJudgeRunsOnlyForDeterministicMissesAndStaysAdvisory(t *testing.T) {
	t.Parallel()
	board := validGeneratedBoard()
	client := &judgeModelClient{
		fixedBoardModelClient: fixedBoardModelClient{board: board},
		responses: []*llm.JudgeGuessResponse{{
			Confidence: 0.91, RationaleCategory: "paraphrase",
			Metadata: llm.CallMetadata{Provider: "paid", Model: "gpt-5.6-luna", PromptVersion: "judge-v1"},
		}},
	}
	repository := NewInMemoryRoomRepository()
	service := NewRoomService(repository, client)
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{RoomName: "Judge room", HostDisplayName: "Host"})
	if err != nil {
		t.Fatal(err)
	}
	_, exactPlayer, _ := service.JoinRoom(context.Background(), JoinRoomInput{Code: room.Code, DisplayName: "Exact"})
	_, semanticPlayer, _ := service.JoinRoom(context.Background(), JoinRoomInput{Code: room.Code, DisplayName: "Semantic"})
	started, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	round := started.CurrentGame.CurrentRound
	answerID := round.Board.Answers[0].ID
	client.responses[0].SuggestedPredictionAnswerID = &answerID
	if _, err := service.SubmitGuess(context.Background(), SubmitGuessInput{
		Code: room.Code, RoundID: round.ID, PlayerToken: exactPlayer.Token, Answer: round.Board.Answers[0].CanonicalAnswer,
	}); err != nil {
		t.Fatal(err)
	}
	if client.calls != 0 {
		t.Fatalf("judge calls after deterministic hit = %d, want 0", client.calls)
	}
	updated, err := service.SubmitGuess(context.Background(), SubmitGuessInput{
		Code: room.Code, RoundID: round.ID, PlayerToken: semanticPlayer.Token, Answer: "a semantic equivalent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if client.calls != 1 {
		t.Fatalf("judge calls = %d, want 1", client.calls)
	}
	for _, guess := range updated.CurrentGame.CurrentRound.Guesses {
		if guess.PlayerID == semanticPlayer.ID && (guess.MatchedPredictionAnswerID != nil || guess.ScoreAwarded != 0) {
			t.Fatalf("semantic suggestion changed authoritative guess: %#v", guess)
		}
	}
	if _, err := service.GetJudgeSuggestions(context.Background(), room.Code, round.ID, host.Token); !errors.Is(err, ErrRoundNotRevealed) {
		t.Fatalf("pre-reveal suggestions error = %v, want %v", err, ErrRoundNotRevealed)
	}
	if _, err := service.RevealRound(context.Background(), RevealRoundInput{Code: room.Code, RoundID: round.ID, PlayerToken: host.Token}); err != nil {
		t.Fatal(err)
	}
	suggestions, err := service.GetJudgeSuggestions(context.Background(), room.Code, round.ID, host.Token)
	if err != nil {
		t.Fatal(err)
	}
	if len(suggestions) != 1 || suggestions[0].SuggestedPredictionAnswerID == nil || suggestions[0].ConfidenceBand != "high" {
		t.Fatalf("unexpected suggestions: %#v", suggestions)
	}
}

func TestSemanticJudgeInvalidOutputRetriesAndLeavesMiss(t *testing.T) {
	t.Parallel()
	board := validGeneratedBoard()
	unknown := "not-on-board"
	client := &judgeModelClient{
		fixedBoardModelClient: fixedBoardModelClient{board: board},
		responses: []*llm.JudgeGuessResponse{
			{SuggestedPredictionAnswerID: &unknown, Confidence: 0.7, RationaleCategory: "synonym", Metadata: llm.CallMetadata{Provider: "paid", Model: "gpt-5.6-luna"}},
			{Confidence: 0.2, RationaleCategory: "ambiguous", Metadata: llm.CallMetadata{Provider: "paid", Model: "gpt-5.6-luna"}},
		},
	}
	repository := NewInMemoryRoomRepository()
	service := NewRoomService(repository, client)
	room, host, _ := service.CreateRoom(context.Background(), CreateRoomInput{RoomName: "Retry judge", HostDisplayName: "Host"})
	_, player, _ := service.JoinRoom(context.Background(), JoinRoomInput{Code: room.Code, DisplayName: "Player"})
	started, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	round := started.CurrentGame.CurrentRound
	if _, err := service.SubmitGuess(context.Background(), SubmitGuessInput{Code: room.Code, RoundID: round.ID, PlayerToken: player.Token, Answer: "miss"}); err != nil {
		t.Fatal(err)
	}
	if client.calls != 2 {
		t.Fatalf("judge calls = %d, want 2", client.calls)
	}
	audits, _ := repository.ListProviderAudits(context.Background(), room.Code)
	if audits[len(audits)-2].Outcome != "invalid_output" || audits[len(audits)-1].Outcome != "success" {
		t.Fatalf("unexpected judge audit outcomes: %#v", audits[len(audits)-2:])
	}
}

func TestConfidenceBandsUseDocumentedBoundaries(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		value float64
		want  string
	}{{0, "low"}, {0.599, "low"}, {0.60, "medium"}, {0.849, "medium"}, {0.85, "high"}, {1, "high"}} {
		if got := confidenceBand(test.value); got != test.want {
			t.Errorf("confidenceBand(%v) = %q, want %q", test.value, got, test.want)
		}
	}
}

func TestSemanticJudgeBudgetExhaustionSkipsProviderAndKeepsMiss(t *testing.T) {
	t.Parallel()
	client := &judgeModelClient{fixedBoardModelClient: fixedBoardModelClient{board: validGeneratedBoard()}}
	repository := NewInMemoryRoomRepository()
	service := NewRoomService(repository, client)
	service.SetModelPolicy(llm.Policy{
		AllowedQuestionModels: []string{"gpt-5.6-luna"}, AllowedPredictionModels: []string{"gpt-5.6-luna"},
		AllowedJudgeModels: []string{"gpt-5.6-luna"}, MaxCallsPerGame: 1, MaxEstimatedCostUSD: 0.10, MaxAttempts: 1,
	})
	room, host, _ := service.CreateRoom(context.Background(), CreateRoomInput{RoomName: "Budget judge", HostDisplayName: "Host"})
	_, player, _ := service.JoinRoom(context.Background(), JoinRoomInput{Code: room.Code, DisplayName: "Player"})
	started, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	repository.audits[room.Code] = append(repository.audits[room.Code], models.ProviderCallAudit{
		Provider: "paid", EstimatedCostUSD: 0.01,
	})
	round := started.CurrentGame.CurrentRound
	if _, err := service.SubmitGuess(context.Background(), SubmitGuessInput{
		Code: room.Code, RoundID: round.ID, PlayerToken: player.Token, Answer: "miss",
	}); err != nil {
		t.Fatal(err)
	}
	if client.calls != 0 {
		t.Fatalf("judge calls = %d, want 0 after budget exhaustion", client.calls)
	}
	audits, _ := repository.ListProviderAudits(context.Background(), room.Code)
	last := audits[len(audits)-1]
	if last.Outcome != "skipped" || last.ErrorCategory != "budget_exhausted" {
		t.Fatalf("unexpected budget audit: %#v", last)
	}
}

func TestSemanticJudgeTimeoutIsAuditedAndKeepsMiss(t *testing.T) {
	t.Parallel()
	client := &judgeModelClient{
		fixedBoardModelClient: fixedBoardModelClient{board: validGeneratedBoard()},
		errors:                []error{context.DeadlineExceeded},
	}
	repository := NewInMemoryRoomRepository()
	service := NewRoomService(repository, client)
	policy := llm.DefaultPolicy()
	policy.Timeout = time.Millisecond
	policy.MaxAttempts = 1
	service.SetModelPolicy(policy)
	room, host, _ := service.CreateRoom(context.Background(), CreateRoomInput{RoomName: "Timeout judge", HostDisplayName: "Host"})
	_, player, _ := service.JoinRoom(context.Background(), JoinRoomInput{Code: room.Code, DisplayName: "Player"})
	started, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	round := started.CurrentGame.CurrentRound
	updated, err := service.SubmitGuess(context.Background(), SubmitGuessInput{
		Code: room.Code, RoundID: round.ID, PlayerToken: player.Token, Answer: "miss",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.CurrentGame.CurrentRound.Guesses[0].ScoreAwarded != 0 {
		t.Fatal("timeout changed deterministic miss")
	}
	audits, _ := repository.ListProviderAudits(context.Background(), room.Code)
	if last := audits[len(audits)-1]; last.Outcome != "timeout" || last.ErrorCategory != "timeout" {
		t.Fatalf("unexpected timeout audit: %#v", last)
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

func TestJoinRoomRejectsPlayerAfterGameStarts(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName: "Closed lobby", HostDisplayName: "Host",
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}
	if _, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token}); err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}
	if _, _, err := service.JoinRoom(context.Background(), JoinRoomInput{Code: room.Code, DisplayName: "Late Player"}); !errors.Is(err, ErrRoomJoinClosed) {
		t.Fatalf("JoinRoom error = %v, want %v", err, ErrRoomJoinClosed)
	}
}

func TestCreateRoomValidatesMVPSettings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		settings models.RoomSettings
	}{
		{name: "unsupported mode", settings: models.RoomSettings{Mode: "cooperative"}},
		{name: "unsupported game kind", settings: models.RoomSettings{GameKind: "survey"}},
		{name: "open trivia sequential", settings: models.RoomSettings{Mode: models.GameModeSequential, GameKind: models.GameKindTriviaOpen}},
		{name: "choice trivia sequential", settings: models.RoomSettings{Mode: models.GameModeSequential, GameKind: models.GameKindTriviaChoice}},
		{name: "too many rounds", settings: models.RoomSettings{TotalRounds: 6}},
		{name: "timer too short", settings: models.RoomSettings{AnswerTimerSeconds: 14}},
		{name: "timer too long", settings: models.RoomSettings{AnswerTimerSeconds: 121}},
		{name: "unsupported locale", settings: models.RoomSettings{Locale: "ro"}},
		{name: "unsupported model", settings: models.RoomSettings{PredictionModel: "expensive-model"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := NewInMemoryRoomService()
			_, _, err := service.CreateRoom(context.Background(), CreateRoomInput{
				RoomName: "Settings Test", HostDisplayName: "Host", Settings: test.settings,
			})
			if !errors.Is(err, ErrRoomSettingsInvalid) {
				t.Fatalf("CreateRoom error = %v, want %v", err, ErrRoomSettingsInvalid)
			}
		})
	}
}

func TestCreateRoomDefaultsAndPreservesSupportedGameKinds(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	defaultRoom, _, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName: "Default rules", HostDisplayName: "Host",
	})
	if err != nil {
		t.Fatal(err)
	}
	if defaultRoom.Settings.GameKind != models.GameKindModelSays {
		t.Fatalf("default game kind = %q, want %q", defaultRoom.Settings.GameKind, models.GameKindModelSays)
	}

	tests := []struct {
		kind models.GameKind
		mode models.GameMode
	}{
		{models.GameKindModelSays, models.GameModeSequential},
		{models.GameKindTriviaOpen, models.GameModeSimultaneous},
		{models.GameKindTriviaOpen, models.GameModeTeams},
		{models.GameKindTriviaChoice, models.GameModeLivingRoom},
	}
	for _, test := range tests {
		t.Run(string(test.kind)+"_"+string(test.mode), func(t *testing.T) {
			room, _, createErr := service.CreateRoom(context.Background(), CreateRoomInput{
				RoomName: "Rules room", HostDisplayName: "Host",
				Settings: models.RoomSettings{GameKind: test.kind, Mode: test.mode},
			})
			if createErr != nil {
				t.Fatalf("CreateRoom: %v", createErr)
			}
			if room.Settings.GameKind != test.kind || room.Settings.Mode != test.mode {
				t.Fatalf("settings = %#v, want kind=%q mode=%q", room.Settings, test.kind, test.mode)
			}
		})
	}
}

func TestNamesUseRuneBoundsAndRejectControlCharacters(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	if _, _, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName: "Joc de seară", HostDisplayName: "玩家",
	}); err != nil {
		t.Fatalf("valid Unicode names returned error: %v", err)
	}
	if _, _, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName: "Bad\nRoom", HostDisplayName: "Host",
	}); !errors.Is(err, ErrRoomNameInvalid) {
		t.Fatalf("control-character room name error = %v, want %v", err, ErrRoomNameInvalid)
	}
	if _, _, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName: "Valid Room", HostDisplayName: "Bad\tName",
	}); !errors.Is(err, ErrDisplayNameInvalid) {
		t.Fatalf("control-character display name error = %v, want %v", err, ErrDisplayNameInvalid)
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
			TotalRounds:        5,
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

func TestLivingRoomHostDisplayIsNotAPlayer(t *testing.T) {
	t.Parallel()
	service := NewInMemoryRoomService()
	room, display, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName: "Living room", HostDisplayName: "TV",
		Settings: models.RoomSettings{Mode: models.GameModeLivingRoom, TotalRounds: 2, AnswerTimerSeconds: 30, Locale: "en"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if display.Role != models.PlayerRoleHostDisplay {
		t.Fatalf("display role = %q", display.Role)
	}
	if _, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: display.Token}); !errors.Is(err, ErrNotEnoughParticipants) {
		t.Fatalf("start with no participants = %v", err)
	}
	for _, name := range []string{"Ana", "Mihai"} {
		if _, _, err := service.JoinRoom(context.Background(), JoinRoomInput{Code: room.Code, DisplayName: name}); err != nil {
			t.Fatal(err)
		}
	}
	started, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: display.Token})
	if err != nil {
		t.Fatal(err)
	}
	if len(started.CurrentGame.Scoreboard) != 2 || len(started.CurrentGame.PreparedRounds) != 1 {
		t.Fatalf("scoreboard/prepared rounds = %d/%d", len(started.CurrentGame.Scoreboard), len(started.CurrentGame.PreparedRounds))
	}
	if _, err := service.SubmitGuess(context.Background(), SubmitGuessInput{
		Code: room.Code, RoundID: started.CurrentGame.CurrentRound.ID, PlayerToken: display.Token, Answer: "Apple",
	}); !errors.Is(err, ErrHostDisplayCannotGuess) {
		t.Fatalf("display guess = %v", err)
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
	canonical, alias, expectedScore := curatedTopAnswer(t, startedRoom.CurrentGame.CurrentRound.Question.ID)
	_ = canonical

	updatedRoom, err := service.SubmitGuess(context.Background(), SubmitGuessInput{
		Code:        room.Code,
		RoundID:     startedRoom.CurrentGame.CurrentRound.ID,
		PlayerToken: player.Token,
		Answer:      alias,
	})
	if err != nil {
		t.Fatalf("SubmitGuess returned error: %v", err)
	}

	if len(updatedRoom.CurrentGame.CurrentRound.Guesses) != 1 {
		t.Fatalf("expected one guess, got %d", len(updatedRoom.CurrentGame.CurrentRound.Guesses))
	}
	if updatedRoom.CurrentGame.CurrentRound.Guesses[0].ScoreAwarded != expectedScore {
		t.Fatalf("expected alias match score %d, got %d", expectedScore, updatedRoom.CurrentGame.CurrentRound.Guesses[0].ScoreAwarded)
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

func TestStartGameFallsBackFromAmbiguousAliasOwnership(t *testing.T) {
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

	started, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatalf("expected curated fallback, got %v", err)
	}
	if started.CurrentGame.CurrentRound.Board.Provider != "static" {
		t.Fatalf("expected static fallback provider, got %q", started.CurrentGame.CurrentRound.Board.Provider)
	}
}

func TestValidatePredictionBoard(t *testing.T) {
	t.Parallel()

	valid := validGeneratedBoard()
	tests := []struct {
		name   string
		mutate func(*models.PredictionBoard)
	}{
		{name: "valid"},
		{name: "too few answers", mutate: func(board *models.PredictionBoard) { board.Answers = board.Answers[:4] }},
		{name: "too many answers", mutate: func(board *models.PredictionBoard) { board.Answers = append(board.Answers, board.Answers[4]) }},
		{name: "duplicate canonicals", mutate: func(board *models.PredictionBoard) {
			board.Answers[1].CanonicalAnswer = board.Answers[0].CanonicalAnswer
		}},
		{name: "ambiguous aliases", mutate: func(board *models.PredictionBoard) { board.Answers[1].Aliases = []string{board.Answers[0].Aliases[0]} }},
		{name: "duplicate ranks", mutate: func(board *models.PredictionBoard) { board.Answers[1].Rank = 1 }},
		{name: "unordered ranks", mutate: func(board *models.PredictionBoard) {
			board.Answers[0].Rank, board.Answers[1].Rank = 2, 1
		}},
		{name: "non descending scores", mutate: func(board *models.PredictionBoard) { board.Answers[1].Score = board.Answers[0].Score }},
		{name: "zero score", mutate: func(board *models.PredictionBoard) { board.Answers[4].Score = 0 }},
		{name: "empty canonical", mutate: func(board *models.PredictionBoard) { board.Answers[0].CanonicalAnswer = " " }},
		{name: "oversized canonical", mutate: func(board *models.PredictionBoard) {
			board.Answers[0].CanonicalAnswer = strings.Repeat("x", maxAnswerTextRunes+1)
		}},
		{name: "empty alias", mutate: func(board *models.PredictionBoard) { board.Answers[0].Aliases = []string{""} }},
		{name: "missing provider", mutate: func(board *models.PredictionBoard) { board.Provider = "" }},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			board := valid
			board.Answers = append([]models.PredictionAnswer(nil), valid.Answers...)
			if test.mutate != nil {
				test.mutate(&board)
			}
			err := validatePredictionBoard(board)
			if test.mutate == nil && err != nil {
				t.Fatalf("valid board rejected: %v", err)
			}
			if test.mutate != nil && !errors.Is(err, ErrGeneratedContentInvalid) {
				t.Fatalf("expected ErrGeneratedContentInvalid, got %v", err)
			}
		})
	}
}

func TestValidateQuestion(t *testing.T) {
	t.Parallel()

	valid := generatedQuestion("question-1", "Name something useful.")
	tests := []struct {
		name     string
		question models.Question
		excluded []string
		wantErr  bool
	}{
		{name: "valid", question: valid},
		{name: "empty text", question: generatedQuestion("question-1", " "), wantErr: true},
		{name: "oversized text", question: generatedQuestion("question-1", strings.Repeat("x", maxQuestionTextRunes+1)), wantErr: true},
		{name: "missing id", question: generatedQuestion("", "Name something."), wantErr: true},
		{name: "wrong locale", question: models.Question{ID: "question-1", Text: "Ceva.", Locale: "ro", Category: "party"}, wantErr: true},
		{name: "repeated question", question: valid, excluded: []string{"NAME... SOMETHING USEFUL!"}, wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			err := validateQuestion(test.question, "en", test.excluded)
			if test.wantErr && !errors.Is(err, ErrGeneratedContentInvalid) {
				t.Fatalf("expected ErrGeneratedContentInvalid, got %v", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("valid question rejected: %v", err)
			}
		})
	}
}

func TestGenerateRoundRetriesInvalidModelOutput(t *testing.T) {
	t.Parallel()

	validQuestion := generatedQuestion("question-model", "Name something useful.")
	invalid := validGeneratedBoard()
	invalid.Answers = invalid.Answers[:4]
	client := &scriptedModelClient{
		questionResponses: []*llm.GenerateQuestionsResponse{
			{Questions: []models.Question{validQuestion}},
			{Questions: []models.Question{validQuestion}},
		},
		boardResponses: []*llm.GenerateBoardResponse{
			{Board: invalid},
			{Board: validGeneratedBoard()},
		},
	}
	service := NewRoomService(NewInMemoryRoomRepository(), client)
	round, err := service.generateRound(context.Background(), models.RoomSettings{Locale: "en"}, 1, nil, time.Now())
	if err != nil {
		t.Fatalf("generateRound returned error: %v", err)
	}
	if client.boardCalls != 2 || round.Board.Provider != "test" {
		t.Fatalf("expected second model board after retry, calls=%d provider=%q", client.boardCalls, round.Board.Provider)
	}
}

func TestGenerateRoundFallsBackAfterProviderErrors(t *testing.T) {
	t.Parallel()

	client := &scriptedModelClient{questionErrors: []error{errors.New("temporary failure"), errors.New("temporary failure")}}
	service := NewRoomService(NewInMemoryRoomRepository(), client)
	round, err := service.generateRound(context.Background(), models.RoomSettings{Locale: "en"}, 1, nil, time.Now())
	if err != nil {
		t.Fatalf("generateRound returned error: %v", err)
	}
	if client.questionCalls != generationAttempts || round.Board.Provider != "static" {
		t.Fatalf("expected bounded retries then static fallback, calls=%d provider=%q", client.questionCalls, round.Board.Provider)
	}
}

func TestProviderAuditsRecordRetryAndZeroCostFallback(t *testing.T) {
	primary := &scriptedModelClient{questionErrors: []error{errors.New("failed"), errors.New("failed")}}
	service := NewRoomService(NewInMemoryRoomRepository(), primary)
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{RoomName: "Audited game", HostDisplayName: "Host"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token}); err != nil {
		t.Fatal(err)
	}
	audits, err := service.GetProviderAudits(context.Background(), room.Code, host.Token)
	if err != nil {
		t.Fatal(err)
	}
	if len(audits) != 4 {
		t.Fatalf("audit count = %d, want two failures plus two fallback calls", len(audits))
	}
	if audits[0].Attempt != 1 || audits[1].Attempt != 2 {
		t.Fatalf("retry attempts not attributable: %#v", audits[:2])
	}
	for _, audit := range audits[2:] {
		if audit.Path != "curated_fallback" || audit.Provider != "static" || audit.EstimatedCostUSD != 0 {
			t.Fatalf("fallback audit is not distinct and zero-cost: %#v", audit)
		}
	}
	if _, err := service.GetProviderAudits(context.Background(), room.Code, "not-host"); !errors.Is(err, ErrUnauthorizedAudit) {
		t.Fatalf("non-host audit error = %v, want %v", err, ErrUnauthorizedAudit)
	}
}

func TestProviderBudgetExhaustionMakesNoFurtherPaidCall(t *testing.T) {
	primary := &costlyQuestionClient{}
	service := NewRoomService(NewInMemoryRoomRepository(), primary)
	service.SetModelPolicy(llm.Policy{
		AllowedPredictionModels: []string{"gpt-5.6-luna"},
		MaxAttempts:             2, MaxCallsPerGame: 20, MaxEstimatedCostUSD: 0.10,
	})
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{RoomName: "Budget fallback", HostDisplayName: "Host"})
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	if primary.boardCalls != 0 {
		t.Fatalf("paid board calls = %d, want zero after question exhausted budget", primary.boardCalls)
	}
	if started.CurrentGame.CurrentRound.Board.Provider != "static" {
		t.Fatalf("budget exhaustion did not preserve curated play: %#v", started.CurrentGame.CurrentRound.Board)
	}
}

func TestProviderCircuitMakesNoPaidCallAndFallsBackToCurated(t *testing.T) {
	client := &scriptedModelClient{}
	gate := &denyProviderGate{}
	service := NewRoomService(NewInMemoryRoomRepository(), client)
	service.SetProviderGate(gate)
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName: "Circuit fallback", HostDisplayName: "Host",
	})
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	if client.questionCalls != 0 || client.boardCalls != 0 {
		t.Fatalf("paid client was called: questions=%d boards=%d", client.questionCalls, client.boardCalls)
	}
	if gate.calls == 0 || started.CurrentGame.CurrentRound.Board.Provider != "static" {
		t.Fatalf("circuit calls=%d provider=%q", gate.calls, started.CurrentGame.CurrentRound.Board.Provider)
	}
	audits, err := service.GetProviderAudits(context.Background(), room.Code, host.Token)
	if err != nil {
		t.Fatal(err)
	}
	if len(audits) < 3 || audits[0].Outcome != "budget_exhausted" || audits[0].ErrorCategory != "circuit_breaker" {
		t.Fatalf("circuit and fallback audits = %#v", audits)
	}
}

func TestProviderTimeoutIsAuditedBeforeCuratedFallback(t *testing.T) {
	service := NewRoomService(NewInMemoryRoomRepository(), timeoutModelClient{})
	service.SetModelPolicy(llm.Policy{
		AllowedPredictionModels: []string{"gpt-5.6-luna"},
		Timeout:                 time.Millisecond, MaxAttempts: 1, MaxCallsPerGame: 20, MaxEstimatedCostUSD: 0.10,
	})
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{RoomName: "Timeout fallback", HostDisplayName: "Host"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token}); err != nil {
		t.Fatal(err)
	}
	audits, err := service.GetProviderAudits(context.Background(), room.Code, host.Token)
	if err != nil {
		t.Fatal(err)
	}
	if len(audits) != 3 || audits[0].Outcome != "timeout" || audits[0].ErrorCategory != "timeout" {
		t.Fatalf("timeout audit followed by fallback = %#v", audits)
	}
}

func TestGenerateRoundReturnsErrorWhenModelAndFallbackFail(t *testing.T) {
	t.Parallel()

	primary := &scriptedModelClient{questionErrors: []error{errors.New("primary failed"), errors.New("primary failed")}}
	fallback := &scriptedModelClient{questionErrors: []error{errors.New("fallback failed")}}
	service := NewRoomService(NewInMemoryRoomRepository(), primary)
	service.fallbackClient = fallback
	_, err := service.generateRound(context.Background(), models.RoomSettings{Locale: "en"}, 1, nil, time.Now())
	if !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("expected combined terminal generation error, got %v", err)
	}
}

func TestStaticGameUsesFiveUniqueQuestions(t *testing.T) {
	t.Parallel()

	service := NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName: "Five rounds", HostDisplayName: "Host",
		Settings: models.RoomSettings{TotalRounds: 5, Locale: "en"},
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}
	room, err = service.StartGame(context.Background(), StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}
	seen := map[string]struct{}{}
	for roundIndex := 1; roundIndex <= 5; roundIndex++ {
		text := room.CurrentGame.CurrentRound.Question.Text
		if _, exists := seen[text]; exists {
			t.Fatalf("question repeated in round %d: %q", roundIndex, text)
		}
		seen[text] = struct{}{}
		room, err = service.RevealRound(context.Background(), RevealRoundInput{
			Code: room.Code, RoundID: room.CurrentGame.CurrentRound.ID, PlayerToken: host.Token,
		})
		if err != nil {
			t.Fatalf("RevealRound %d returned error: %v", roundIndex, err)
		}
		if roundIndex < 5 {
			room, err = service.NextRound(context.Background(), NextRoundInput{Code: room.Code, PlayerToken: host.Token})
			if err != nil {
				t.Fatalf("NextRound %d returned error: %v", roundIndex, err)
			}
		}
	}
}

func generatedQuestion(id string, text string) models.Question {
	return models.Question{ID: id, Text: text, Locale: "en", Category: "party"}
}

func validGeneratedBoard() models.PredictionBoard {
	return models.PredictionBoard{
		Provider: "test", ModelName: "test-model", PromptVersion: "v1",
		Answers: []models.PredictionAnswer{
			{CanonicalAnswer: "answer one", Aliases: []string{"first"}, Rank: 1, Score: 50},
			{CanonicalAnswer: "answer two", Aliases: []string{"second"}, Rank: 2, Score: 40},
			{CanonicalAnswer: "answer three", Aliases: []string{"third"}, Rank: 3, Score: 30},
			{CanonicalAnswer: "answer four", Aliases: []string{"fourth"}, Rank: 4, Score: 20},
			{CanonicalAnswer: "answer five", Aliases: []string{"fifth"}, Rank: 5, Score: 10},
		},
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
	canonical, alias, _ := curatedTopAnswer(t, startedRoom.CurrentGame.CurrentRound.Question.ID)

	updatedRoom, err := service.SubmitGuess(context.Background(), SubmitGuessInput{Code: room.Code, RoundID: startedRoom.CurrentGame.CurrentRound.ID, PlayerToken: playerOne.Token, Answer: canonical})
	if err != nil {
		t.Fatalf("first SubmitGuess returned error: %v", err)
	}

	updatedRoom, err = service.SubmitGuess(context.Background(), SubmitGuessInput{Code: room.Code, RoundID: startedRoom.CurrentGame.CurrentRound.ID, PlayerToken: playerTwo.Token, Answer: alias})
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

func curatedTopAnswer(t *testing.T, questionID string) (canonical string, alias string, score int) {
	t.Helper()
	answers := map[string]struct {
		canonical string
		alias     string
		score     int
	}{
		"question-en-001": {canonical: "cryptocurrency", alias: "bitcoin", score: 50},
		"question-en-002": {canonical: "airport food", alias: "airport snacks", score: 45},
		"question-en-003": {canonical: "dieting", alias: "diet", score: 40},
		"question-en-004": {canonical: "their keys", alias: "keys", score: 50},
		"question-en-005": {canonical: "no clear agenda", alias: "no agenda", score: 50},
	}
	answer, ok := answers[questionID]
	if !ok {
		t.Fatalf("no curated answer fixture for question %q", questionID)
	}
	return answer.canonical, answer.alias, answer.score
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
	expectedScore := revealedRoom.CurrentGame.CurrentRound.Board.Answers[2].Score
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

	if overriddenRoom.CurrentGame.CurrentRound.Guesses[0].ScoreAwarded != expectedScore {
		t.Fatalf("expected overridden score %d, got %d", expectedScore, overriddenRoom.CurrentGame.CurrentRound.Guesses[0].ScoreAwarded)
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

func TestCompletedReplayAndPlayAgainArePublicSafeAndIsolated(t *testing.T) {
	service := NewInMemoryRoomService()
	original, host, err := service.CreateRoom(context.Background(), CreateRoomInput{
		RoomName: "Replay room", HostDisplayName: "Host",
		Settings: models.RoomSettings{TotalRounds: 1, AnswerTimerSeconds: 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, player, err := service.JoinRoom(context.Background(), JoinRoomInput{Code: original.Code, DisplayName: "Player"})
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.StartGame(context.Background(), StartGameInput{Code: original.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	answer := started.CurrentGame.CurrentRound.Board.Answers[0]
	if _, err := service.SubmitGuess(context.Background(), SubmitGuessInput{
		Code: original.Code, RoundID: started.CurrentGame.CurrentRound.ID,
		PlayerToken: player.Token, Answer: answer.CanonicalAnswer,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RevealRound(context.Background(), RevealRoundInput{
		Code: original.Code, RoundID: started.CurrentGame.CurrentRound.ID, PlayerToken: host.Token,
	}); err != nil {
		t.Fatal(err)
	}
	completed, err := service.NextRound(context.Background(), NextRoundInput{Code: original.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	if completed.CurrentGame.ReplayID == "" {
		t.Fatal("completed game has no replay id")
	}
	replay, err := service.GetReplay(context.Background(), completed.CurrentGame.ReplayID)
	if err != nil {
		reloaded, _ := service.GetRoom(context.Background(), original.Code)
		t.Fatalf("%v (status=%v ended=%v replay=%q)", err, reloaded.CurrentGame.Status, reloaded.CurrentGame.EndedAt, reloaded.CurrentGame.ReplayID)
	}
	if len(replay.Rounds) != 1 || replay.Rounds[0].Question == "" || len(replay.Rounds[0].Board) != 5 {
		t.Fatalf("incomplete replay: %#v", replay)
	}
	if replay.Rankings[0].PlayerID != player.ID || replay.Rankings[0].Score != answer.Score {
		t.Fatalf("unexpected rankings: %#v", replay.Rankings)
	}
	if replay.GameKind != models.GameKindModelSays {
		t.Fatalf("replay game kind = %q", replay.GameKind)
	}

	fresh, freshHost, err := service.PlayAgain(context.Background(), PlayAgainInput{Code: original.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Code == original.Code || freshHost.ID == host.ID || freshHost.Token == host.Token {
		t.Fatal("play again reused original room, player, or credential")
	}
	if fresh.CurrentGame != nil || len(fresh.Players) != 1 || fresh.Status != models.RoomStatusLobby {
		t.Fatalf("fresh room inherited game state: %#v", fresh)
	}
	if fresh.Settings.GameKind != original.Settings.GameKind {
		t.Fatalf("play again game kind = %q, want %q", fresh.Settings.GameKind, original.Settings.GameKind)
	}
	originalReloaded, err := service.GetRoom(context.Background(), original.Code)
	if err != nil || originalReloaded.CurrentGame.Status != models.GameStatusCompleted {
		t.Fatalf("original game changed: room=%#v err=%v", originalReloaded, err)
	}
}

func TestTeamModeRequiresAssignmentsAndDerivesScores(t *testing.T) {
	service := NewInMemoryRoomService()
	ctx := context.Background()
	room, host, err := service.CreateRoom(ctx, CreateRoomInput{RoomName: "Team night", HostDisplayName: "Host",
		Settings: models.RoomSettings{Mode: models.GameModeTeams, TotalRounds: 1, AnswerTimerSeconds: 30}})
	if err != nil {
		t.Fatal(err)
	}
	room, guest, err := service.JoinRoom(ctx, JoinRoomInput{Code: room.Code, DisplayName: "Guest"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartGame(ctx, StartGameInput{Code: room.Code, PlayerToken: host.Token}); !errors.Is(err, ErrTeamConfigurationInvalid) {
		t.Fatalf("start without teams error = %v", err)
	}
	room, err = service.CreateTeam(ctx, CreateTeamInput{Code: room.Code, PlayerToken: host.Token, Name: "Blue"})
	if err != nil {
		t.Fatal(err)
	}
	room, err = service.CreateTeam(ctx, CreateTeamInput{Code: room.Code, PlayerToken: host.Token, Name: "Gold"})
	if err != nil {
		t.Fatal(err)
	}
	room, err = service.AssignTeam(ctx, AssignTeamInput{Code: room.Code, PlayerToken: host.Token, PlayerID: host.ID, TeamID: room.Teams[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	room, err = service.AssignTeam(ctx, AssignTeamInput{Code: room.Code, PlayerToken: host.Token, PlayerID: guest.ID, TeamID: room.Teams[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	room, err = service.StartGame(ctx, StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	answer := room.CurrentGame.CurrentRound.Board.Answers[0].CanonicalAnswer
	room, err = service.SubmitGuess(ctx, SubmitGuessInput{Code: room.Code, RoundID: room.CurrentGame.CurrentRound.ID, PlayerToken: host.Token, Answer: answer})
	if err != nil {
		t.Fatal(err)
	}
	if len(room.CurrentGame.TeamScoreboard) != 2 || room.CurrentGame.TeamScoreboard[0].Score != room.CurrentGame.Scoreboard[0].Score {
		t.Fatalf("team scores are not derived from player scores: %#v / %#v", room.CurrentGame.TeamScoreboard, room.CurrentGame.Scoreboard)
	}
	if _, err := service.AssignTeam(ctx, AssignTeamInput{Code: room.Code, PlayerToken: host.Token, PlayerID: guest.ID, TeamID: room.Teams[0].ID}); !errors.Is(err, ErrGameAlreadyStarted) {
		t.Fatalf("post-start assignment error = %v", err)
	}
}

func TestSequentialModeAdvancesTurnsAndRevealsAfterFinalAction(t *testing.T) {
	ctx := context.Background()
	service := NewInMemoryRoomService()
	room, host, err := service.CreateRoom(ctx, CreateRoomInput{
		RoomName: "Turn night", HostDisplayName: "Host",
		Settings: models.RoomSettings{Mode: models.GameModeSequential, TotalRounds: 2, AnswerTimerSeconds: 15},
	})
	if err != nil {
		t.Fatal(err)
	}
	room, guest, err := service.JoinRoom(ctx, JoinRoomInput{Code: room.Code, DisplayName: "Guest"})
	if err != nil {
		t.Fatal(err)
	}
	room, err = service.StartGame(ctx, StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	round := room.CurrentGame.CurrentRound
	if round.CurrentTurnIndex == nil || *round.CurrentTurnIndex != 0 || len(round.TurnOrder) != 2 || round.TurnOrder[0] != host.ID {
		t.Fatalf("unexpected initial turn state: %#v", round)
	}
	if _, err := service.SubmitGuess(ctx, SubmitGuessInput{Code: room.Code, RoundID: round.ID, PlayerToken: guest.Token, Answer: "early"}); !errors.Is(err, ErrNotPlayersTurn) {
		t.Fatalf("non-current submission error = %v", err)
	}
	answer := round.Board.Answers[0].CanonicalAnswer
	room, err = service.SubmitGuess(ctx, SubmitGuessInput{Code: room.Code, RoundID: round.ID, PlayerToken: host.Token, Answer: answer})
	if err != nil {
		t.Fatal(err)
	}
	if room.CurrentGame.CurrentRound.Status != models.RoundStatusAnswering || *room.CurrentGame.CurrentRound.CurrentTurnIndex != 1 {
		t.Fatalf("first submission did not advance: %#v", room.CurrentGame.CurrentRound)
	}
	room, err = service.PassTurn(ctx, PassTurnInput{Code: room.Code, RoundID: round.ID, PlayerToken: guest.Token})
	if err != nil {
		t.Fatal(err)
	}
	if room.CurrentGame.CurrentRound.Status != models.RoundStatusRevealed || room.CurrentGame.CurrentRound.CurrentTurnIndex != nil {
		t.Fatalf("final pass did not reveal: %#v", room.CurrentGame.CurrentRound)
	}
	room, err = service.NextRound(ctx, NextRoundInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	if room.CurrentGame.CurrentRound.RoundIndex != 2 || *room.CurrentGame.CurrentRound.CurrentTurnIndex != 0 || room.CurrentGame.CurrentRound.TurnOrder[0] != host.ID {
		t.Fatalf("next round did not restart turn order: %#v", room.CurrentGame.CurrentRound)
	}
}
