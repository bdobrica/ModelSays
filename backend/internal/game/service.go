package game

import (
	"context"
	crand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bogdandobrica/modelsays/backend/internal/llm"
	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

var (
	ErrRoomNotFound             = errors.New("room not found")
	ErrDisplayNameInvalid       = errors.New("display name must be between 2 and 24 characters")
	ErrRoomNameInvalid          = errors.New("room name must be between 3 and 48 characters")
	ErrRoomCodeInvalid          = errors.New("room code must be 6 letters or digits")
	ErrDuplicatePlayer          = errors.New("display name already taken in this room")
	ErrRoomJoinClosed           = errors.New("game has started; new players cannot join")
	ErrRoomCodeConflict         = errors.New("room code already exists")
	ErrRoomSettingsInvalid      = errors.New("room settings are not supported")
	ErrUnauthorizedStart        = errors.New("only the host can start the game")
	ErrUnauthorizedReveal       = errors.New("only the host can reveal the round")
	ErrUnauthorizedAdvance      = errors.New("only the host can advance to the next round")
	ErrUnauthorizedOverride     = errors.New("only the host can override a match")
	ErrUnauthorizedAudit        = errors.New("only the host can view provider audits")
	ErrUnauthorizedJudgeReview  = errors.New("only the host can review judge suggestions")
	ErrGameAlreadyStarted       = errors.New("game already started")
	ErrGameAlreadyCompleted     = errors.New("game already completed")
	ErrNoQuestionsAvailable     = errors.New("no questions available for this room")
	ErrPlayerNotFound           = errors.New("player not found for token")
	ErrRoundNotFound            = errors.New("round not found")
	ErrRoundNotAcceptingGuesses = errors.New("round is not accepting guesses")
	ErrAnswerPhaseExpired       = errors.New("answer phase has expired")
	ErrRoundAlreadyRevealed     = errors.New("round already revealed")
	ErrRoundNotRevealed         = errors.New("round must be revealed before advancing")
	ErrGuessAlreadySubmitted    = errors.New("guess already submitted for this round")
	ErrGuessNotFound            = errors.New("guess not found")
	ErrPredictionAnswerNotFound = errors.New("prediction answer not found")
	ErrAmbiguousBoardAnswer     = errors.New("board contains a canonical answer or alias owned by multiple answers")
	ErrGeneratedContentInvalid  = errors.New("generated game content is invalid")
	ErrContentUnavailable       = errors.New("game content is temporarily unavailable; please try again")
	ErrAnswerInvalid            = errors.New("answer must be between 1 and 120 characters")
)

const (
	roomAlphabet             = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	generationAttempts       = 2 // retained as the documented/default retry count
	predictionAnswerCount    = 5
	maxQuestionTextRunes     = 240
	maxAnswerTextRunes       = 80
	maxAliasCount            = 8
	maxBoardMetadataRunes    = 80
	maxPredictionAnswerScore = 100
	minTotalRounds           = 1
	maxTotalRounds           = 5
	minAnswerTimerSeconds    = 15
	maxAnswerTimerSeconds    = 120
	defaultPredictionModel   = "gpt-4.1-mini"
)

type CreateRoomInput struct {
	RoomName        string
	HostDisplayName string
	Settings        models.RoomSettings
}

type JoinRoomInput struct {
	Code        string
	DisplayName string
}

type RecoverSessionInput struct {
	Code        string
	PlayerToken string
}

type StartGameInput struct {
	Code        string
	PlayerToken string
}

type SubmitGuessInput struct {
	Code        string
	RoundID     string
	PlayerToken string
	Answer      string
}

type RevealRoundInput struct {
	Code        string
	RoundID     string
	PlayerToken string
}

type NextRoundInput struct {
	Code        string
	PlayerToken string
}

type OverrideMatchInput struct {
	Code                      string
	RoundID                   string
	GuessID                   string
	PlayerToken               string
	MatchedPredictionAnswerID *string
	JudgeSuggestionID         string
}

type RoomService struct {
	repository      RoomRepository
	modelClient     llm.ModelClient
	fallbackClient  llm.ModelClient
	clock           Clock
	predictionModel string
	judgeModel      string
	modelPolicy     llm.Policy
	providerGate    ProviderGate
	providerObserve func(models.ProviderCallAudit)
}

type ProviderGate interface {
	AllowProvider(roomCode string) (bool, time.Duration)
}

type Clock interface {
	Now() time.Time
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

type GuessSubmission struct {
	ID                string
	PlayerID          string
	PlayerDisplayName string
	RawAnswer         string
	CreatedAt         time.Time
	ScoreEventID      string
}

type GuessOverride struct {
	GuessID                   string
	MatchedPredictionAnswerID *string
	ScoreEventID              string
	CreatedAt                 time.Time
	JudgeSuggestionID         string
	ReviewDecision            string
}

type RoomRepository interface {
	CreateRoom(ctx context.Context, room models.Room) error
	GetRoom(ctx context.Context, code string) (models.Room, error)
	AddPlayer(ctx context.Context, code string, player models.Player) (models.Room, error)
	StartGame(ctx context.Context, code string, gameState models.Game) (models.Room, error)
	SubmitGuess(ctx context.Context, code string, roundID string, submission GuessSubmission, clock Clock) (models.Room, error)
	RevealRound(ctx context.Context, code string, roundID string, transition models.RoundTransition) (models.Room, error)
	AdvanceGame(ctx context.Context, code string, gameState models.Game, nextRound *models.Round) (models.Room, error)
	OverrideGuess(ctx context.Context, code string, roundID string, override GuessOverride) (models.Room, error)
	ListProviderAudits(ctx context.Context, code string) ([]models.ProviderCallAudit, error)
	StoreJudgeEvaluation(ctx context.Context, suggestion models.JudgeSuggestion, audits []models.ProviderCallAudit) error
	ListJudgeSuggestions(ctx context.Context, code string, roundID string) ([]models.JudgeSuggestion, error)
	AppendRoomEvent(ctx context.Context, code string, eventType models.RoomEventType, occurredAt time.Time) (models.RoomEvent, error)
	ListRoomEvents(ctx context.Context, code string, afterRevision int64, limit int) ([]models.RoomEvent, error)
}

func NewRoomService(repository RoomRepository, modelClient llm.ModelClient) *RoomService {
	return NewRoomServiceWithClock(repository, modelClient, systemClock{})
}

func NewRoomServiceWithClock(repository RoomRepository, modelClient llm.ModelClient, clock Clock) *RoomService {
	if modelClient == nil {
		modelClient = llm.NewStaticModelClient()
	}
	if clock == nil {
		clock = systemClock{}
	}

	return &RoomService{
		repository:      repository,
		modelClient:     modelClient,
		fallbackClient:  llm.NewStaticModelClient(),
		clock:           clock,
		predictionModel: defaultPredictionModel,
		judgeModel:      defaultPredictionModel,
		modelPolicy:     llm.DefaultPolicy(),
	}
}

func (service *RoomService) SetJudgeModel(model string) {
	if model = strings.TrimSpace(model); model != "" {
		service.judgeModel = model
	}
}

func (service *RoomService) SetModelPolicy(policy llm.Policy) {
	service.modelPolicy = policy.Normalize()
}

func (service *RoomService) SetProviderGate(gate ProviderGate) {
	service.providerGate = gate
}

func (service *RoomService) SetProviderObserver(observer func(models.ProviderCallAudit)) {
	service.providerObserve = observer
}

func (service *RoomService) SetPredictionModel(model string) {
	if model = strings.TrimSpace(model); model != "" {
		service.predictionModel = model
	}
}

func NewInMemoryRoomService() *RoomService {
	return NewRoomService(NewInMemoryRoomRepository(), llm.NewStaticModelClient())
}

func (service *RoomService) CreateRoom(ctx context.Context, input CreateRoomInput) (models.Room, models.Player, error) {
	roomName := strings.TrimSpace(input.RoomName)
	hostDisplayName := strings.TrimSpace(input.HostDisplayName)

	if validateBoundedText(roomName, 3, 48) != nil {
		return models.Room{}, models.Player{}, ErrRoomNameInvalid
	}

	if err := validateDisplayName(hostDisplayName); err != nil {
		return models.Room{}, models.Player{}, err
	}

	settings := normalizeSettings(input.Settings)
	if err := service.validateSettings(settings); err != nil {
		return models.Room{}, models.Player{}, err
	}
	now := service.clock.Now()
	host := models.Player{
		ID:          newID(),
		DisplayName: hostDisplayName,
		IsHost:      true,
		JoinedAt:    now,
		Token:       newToken(),
	}
	for attempts := 0; attempts < 5; attempts++ {
		room := models.Room{
			Code:      randomAlphabeticCode(6),
			Name:      roomName,
			Status:    models.RoomStatusLobby,
			Settings:  settings,
			Players:   []models.Player{host},
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := service.repository.CreateRoom(ctx, room); err != nil {
			if errors.Is(err, ErrRoomCodeConflict) {
				continue
			}

			return models.Room{}, models.Player{}, err
		}

		return cloneRoom(room), host, nil
	}

	return models.Room{}, models.Player{}, fmt.Errorf("unable to create room after repeated code collisions")
}

func (service *RoomService) RecoverSession(ctx context.Context, input RecoverSessionInput) (models.Room, models.Player, error) {
	code, err := normalizeRoomCode(input.Code)
	if err != nil {
		return models.Room{}, models.Player{}, err
	}

	room, err := service.repository.GetRoom(ctx, code)
	if err != nil {
		return models.Room{}, models.Player{}, err
	}

	player, ok := findPlayerByToken(room.Players, strings.TrimSpace(input.PlayerToken))
	if !ok {
		return models.Room{}, models.Player{}, ErrPlayerNotFound
	}

	return room, player, nil
}

func (service *RoomService) GetRoom(ctx context.Context, code string) (models.Room, error) {
	code, err := normalizeRoomCode(code)
	if err != nil {
		return models.Room{}, err
	}
	return service.repository.GetRoom(ctx, code)
}

func (service *RoomService) AuthenticateEventSubscription(ctx context.Context, code, playerToken string) error {
	normalizedCode, err := normalizeRoomCode(code)
	if err != nil {
		return err
	}
	if strings.TrimSpace(playerToken) == "" {
		return ErrPlayerNotFound
	}
	room, err := service.repository.GetRoom(ctx, normalizedCode)
	if err != nil {
		return err
	}
	if _, ok := findPlayerByToken(room.Players, strings.TrimSpace(playerToken)); ok {
		return nil
	}
	return ErrPlayerNotFound
}

func (service *RoomService) ListRoomEvents(ctx context.Context, code string, afterRevision int64, limit int) ([]models.RoomEvent, error) {
	normalizedCode, err := normalizeRoomCode(code)
	if err != nil {
		return nil, err
	}
	return service.repository.ListRoomEvents(ctx, normalizedCode, afterRevision, limit)
}

func (service *RoomService) publishRoomEvent(ctx context.Context, room models.Room, eventType models.RoomEventType) models.Room {
	event, err := service.repository.AppendRoomEvent(ctx, room.Code, eventType, service.clock.Now())
	if err == nil {
		room.Revision = event.RoomRevision
	}
	return room
}

func (service *RoomService) GetProviderAudits(ctx context.Context, code string, playerToken string) ([]models.ProviderCallAudit, error) {
	code, err := normalizeRoomCode(code)
	if err != nil {
		return nil, err
	}
	room, err := service.repository.GetRoom(ctx, code)
	if err != nil {
		return nil, err
	}
	if !isHostToken(room.Players, strings.TrimSpace(playerToken)) {
		return nil, ErrUnauthorizedAudit
	}
	return service.repository.ListProviderAudits(ctx, code)
}

func (service *RoomService) GetJudgeSuggestions(ctx context.Context, code string, roundID string, playerToken string) ([]models.JudgeSuggestion, error) {
	code, err := normalizeRoomCode(code)
	if err != nil {
		return nil, err
	}
	room, err := service.repository.GetRoom(ctx, code)
	if err != nil {
		return nil, err
	}
	if !isHostToken(room.Players, strings.TrimSpace(playerToken)) {
		return nil, ErrUnauthorizedJudgeReview
	}
	round, err := currentRound(room, strings.TrimSpace(roundID))
	if err != nil {
		return nil, err
	}
	if round.Status != models.RoundStatusRevealed {
		return nil, ErrRoundNotRevealed
	}
	return service.repository.ListJudgeSuggestions(ctx, code, round.ID)
}

func (service *RoomService) JoinRoom(ctx context.Context, input JoinRoomInput) (models.Room, models.Player, error) {
	code, err := normalizeRoomCode(input.Code)
	if err != nil {
		return models.Room{}, models.Player{}, err
	}
	displayName := strings.TrimSpace(input.DisplayName)
	if err := validateDisplayName(displayName); err != nil {
		return models.Room{}, models.Player{}, err
	}

	player := models.Player{
		ID:          newID(),
		DisplayName: displayName,
		IsHost:      false,
		JoinedAt:    service.clock.Now(),
		Token:       newToken(),
	}

	room, err := service.repository.AddPlayer(ctx, code, player)
	if err != nil {
		return models.Room{}, models.Player{}, err
	}

	room = service.publishRoomEvent(ctx, room, models.RoomEventPlayerJoined)
	return room, player, nil
}

func (service *RoomService) StartGame(ctx context.Context, input StartGameInput) (models.Room, error) {
	code, err := normalizeRoomCode(input.Code)
	if err != nil {
		return models.Room{}, err
	}
	playerToken := strings.TrimSpace(input.PlayerToken)

	room, err := service.repository.GetRoom(ctx, code)
	if err != nil {
		return models.Room{}, err
	}

	if room.Status != models.RoomStatusLobby || room.CurrentGame != nil {
		return models.Room{}, ErrGameAlreadyStarted
	}

	if !isHostToken(room.Players, playerToken) {
		return models.Room{}, ErrUnauthorizedStart
	}

	now := service.clock.Now()
	gameID := newID()
	roundID := newID()
	roundSeed, err := service.generateRoundScoped(ctx, room.Code, gameID, roundID, room.Settings, 1, nil, now, nil)
	if err != nil {
		return models.Room{}, err
	}

	gameState := models.Game{
		ID:                gameID,
		Status:            models.GameStatusInProgress,
		Mode:              room.Settings.Mode,
		TotalRounds:       room.Settings.TotalRounds,
		CurrentRoundIndex: 1,
		Scoreboard:        initialScoreboard(room.Players),
		CreatedAt:         now,
		StartedAt:         now,
		CurrentRound: &models.Round{
			ID:                   roundID,
			RoundIndex:           roundSeed.RoundIndex,
			Status:               models.RoundStatusAnswering,
			Question:             roundSeed.Question,
			BoardHash:            roundSeed.Board.BoardHash,
			Board:                &roundSeed.Board,
			AnswerPhaseStartedAt: now,
			AnswerPhaseEndsAt:    now.Add(time.Duration(room.Settings.AnswerTimerSeconds) * time.Second),
			CreatedAt:            now,
			ProviderAudits:       roundSeed.Audits,
		},
	}

	updated, err := service.repository.StartGame(ctx, code, gameState)
	if err == nil {
		updated = service.publishRoomEvent(ctx, updated, models.RoomEventGameStarted)
	}
	return updated, err
}

func (service *RoomService) SubmitGuess(ctx context.Context, input SubmitGuessInput) (models.Room, error) {
	code, err := normalizeRoomCode(input.Code)
	if err != nil {
		return models.Room{}, err
	}
	roundID := strings.TrimSpace(input.RoundID)
	answer := strings.TrimSpace(input.Answer)
	if validateBoundedText(answer, 1, 120) != nil {
		return models.Room{}, ErrAnswerInvalid
	}

	room, err := service.repository.GetRoom(ctx, code)
	if err != nil {
		return models.Room{}, err
	}

	player, ok := findPlayerByToken(room.Players, strings.TrimSpace(input.PlayerToken))
	if !ok {
		return models.Room{}, ErrPlayerNotFound
	}

	round, err := currentRound(room, roundID)
	if err != nil {
		return models.Room{}, err
	}
	if round.Status != models.RoundStatusAnswering {
		return models.Room{}, ErrRoundNotAcceptingGuesses
	}
	now := service.clock.Now()
	if !now.Before(round.AnswerPhaseEndsAt) {
		return models.Room{}, ErrAnswerPhaseExpired
	}
	for _, existingGuess := range round.Guesses {
		if existingGuess.PlayerID == player.ID {
			return models.Room{}, ErrGuessAlreadySubmitted
		}
	}
	if round.Board == nil {
		return models.Room{}, fmt.Errorf("prediction board missing for round")
	}

	updated, err := service.repository.SubmitGuess(ctx, code, round.ID, GuessSubmission{
		ID:                newID(),
		PlayerID:          player.ID,
		PlayerDisplayName: player.DisplayName,
		RawAnswer:         answer,
		CreatedAt:         now,
		ScoreEventID:      newID(),
	}, service.clock)
	if err != nil {
		return models.Room{}, err
	}
	updated = service.publishRoomEvent(ctx, updated, models.RoomEventSubmissionProgress)
	submittedRound := updated.CurrentGame.CurrentRound
	var submitted models.Guess
	for _, guess := range submittedRound.Guesses {
		if guess.PlayerID == player.ID {
			submitted = guess
			break
		}
	}
	if submitted.ID == "" || submitted.MatchedPredictionAnswerID != nil {
		return updated, nil
	}
	if err := service.evaluateSemanticMiss(ctx, updated, submitted); err != nil {
		return models.Room{}, err
	}
	return updated, nil
}

func (service *RoomService) RevealRound(ctx context.Context, input RevealRoundInput) (models.Room, error) {
	code, err := normalizeRoomCode(input.Code)
	if err != nil {
		return models.Room{}, err
	}
	roundID := strings.TrimSpace(input.RoundID)
	room, err := service.repository.GetRoom(ctx, code)
	if err != nil {
		return models.Room{}, err
	}
	if !isHostToken(room.Players, strings.TrimSpace(input.PlayerToken)) {
		return models.Room{}, ErrUnauthorizedReveal
	}

	round, err := currentRound(room, roundID)
	if err != nil {
		return models.Room{}, err
	}
	if round.Status == models.RoundStatusRevealed {
		return models.Room{}, ErrRoundAlreadyRevealed
	}
	if round.Status != models.RoundStatusAnswering {
		return models.Room{}, ErrRoundNotAcceptingGuesses
	}

	now := service.clock.Now()
	return service.repository.RevealRound(ctx, code, round.ID, models.RoundTransition{
		ID: newID(), RoomCode: code, GameID: room.CurrentGame.ID, RoundID: round.ID,
		Action: "reveal", Actor: models.RoundTransitionActorHost,
		Reason: "host_reveal", CreatedAt: now,
	})
}

func (service *RoomService) NextRound(ctx context.Context, input NextRoundInput) (models.Room, error) {
	code, err := normalizeRoomCode(input.Code)
	if err != nil {
		return models.Room{}, err
	}
	room, err := service.repository.GetRoom(ctx, code)
	if err != nil {
		return models.Room{}, err
	}
	if !isHostToken(room.Players, strings.TrimSpace(input.PlayerToken)) {
		return models.Room{}, ErrUnauthorizedAdvance
	}
	if room.CurrentGame == nil || room.CurrentGame.CurrentRound == nil {
		return models.Room{}, ErrRoundNotFound
	}
	if room.CurrentGame.Status == models.GameStatusCompleted {
		return models.Room{}, ErrGameAlreadyCompleted
	}
	if room.CurrentGame.CurrentRound.Status != models.RoundStatusRevealed {
		return models.Room{}, ErrRoundNotRevealed
	}

	now := service.clock.Now()
	if room.CurrentGame.CurrentRoundIndex >= room.CurrentGame.TotalRounds {
		completedGame := cloneGame(room.CurrentGame)
		completedGame.Status = models.GameStatusCompleted
		completedGame.EndedAt = &now
		updated, err := service.repository.AdvanceGame(ctx, code, *completedGame, nil)
		if err == nil {
			updated = service.publishRoomEvent(ctx, updated, models.RoomEventGameCompleted)
		}
		return updated, err
	}

	nextRoundIndex := room.CurrentGame.CurrentRoundIndex + 1
	priorAudits, err := service.repository.ListProviderAudits(ctx, code)
	if err != nil {
		return models.Room{}, err
	}
	roundID := newID()
	roundSeed, err := service.generateRoundScoped(ctx, room.Code, room.CurrentGame.ID, roundID, room.Settings, nextRoundIndex, []string{room.CurrentGame.CurrentRound.Question.Text}, now, priorAudits)
	if err != nil {
		return models.Room{}, err
	}

	nextRound := &models.Round{
		ID:                   roundID,
		RoundIndex:           nextRoundIndex,
		Status:               models.RoundStatusAnswering,
		Question:             roundSeed.Question,
		BoardHash:            roundSeed.Board.BoardHash,
		Board:                &roundSeed.Board,
		AnswerPhaseStartedAt: now,
		AnswerPhaseEndsAt:    now.Add(time.Duration(room.Settings.AnswerTimerSeconds) * time.Second),
		CreatedAt:            now,
		ProviderAudits:       roundSeed.Audits,
	}

	nextGame := cloneGame(room.CurrentGame)
	nextGame.Status = models.GameStatusInProgress
	nextGame.CurrentRoundIndex = nextRoundIndex
	nextGame.CurrentRound = nextRound
	resetSubmissionFlags(nextGame.Scoreboard)

	updated, err := service.repository.AdvanceGame(ctx, code, *nextGame, nextRound)
	if err == nil {
		updated = service.publishRoomEvent(ctx, updated, models.RoomEventRoundStarted)
	}
	return updated, err
}

func (service *RoomService) OverrideMatch(ctx context.Context, input OverrideMatchInput) (models.Room, error) {
	code, err := normalizeRoomCode(input.Code)
	if err != nil {
		return models.Room{}, err
	}
	room, err := service.repository.GetRoom(ctx, code)
	if err != nil {
		return models.Room{}, err
	}
	if !isHostToken(room.Players, strings.TrimSpace(input.PlayerToken)) {
		return models.Room{}, ErrUnauthorizedOverride
	}
	if room.CurrentGame == nil || room.CurrentGame.CurrentRound == nil {
		return models.Room{}, ErrRoundNotFound
	}
	round, err := currentRound(room, strings.TrimSpace(input.RoundID))
	if err != nil {
		return models.Room{}, err
	}
	if round.Status != models.RoundStatusRevealed {
		return models.Room{}, ErrRoundNotRevealed
	}

	guessID := strings.TrimSpace(input.GuessID)
	if _, ok := findGuess(round.Guesses, guessID); !ok {
		return models.Room{}, ErrGuessNotFound
	}

	if input.MatchedPredictionAnswerID != nil {
		_, ok := findPredictionAnswer(round.Board, *input.MatchedPredictionAnswerID)
		if !ok {
			return models.Room{}, ErrPredictionAnswerNotFound
		}
	}

	suggestionID := strings.TrimSpace(input.JudgeSuggestionID)
	decision := reviewDecision(input.MatchedPredictionAnswerID)
	if suggestionID != "" {
		suggestions, err := service.repository.ListJudgeSuggestions(ctx, code, round.ID)
		if err != nil {
			return models.Room{}, err
		}
		found := false
		for _, suggestion := range suggestions {
			if suggestion.ID != suggestionID || suggestion.GuessID != guessID {
				continue
			}
			found = true
			if sameOptionalID(suggestion.SuggestedPredictionAnswerID, input.MatchedPredictionAnswerID) {
				if input.MatchedPredictionAnswerID == nil {
					decision = "retained_miss"
				} else {
					decision = "accepted_suggestion"
				}
			}
			break
		}
		if !found {
			return models.Room{}, ErrGuessNotFound
		}
	}

	updated, err := service.repository.OverrideGuess(ctx, code, round.ID, GuessOverride{
		GuessID:                   guessID,
		MatchedPredictionAnswerID: input.MatchedPredictionAnswerID,
		ScoreEventID:              newID(),
		CreatedAt:                 service.clock.Now(),
		JudgeSuggestionID:         suggestionID,
		ReviewDecision:            decision,
	})
	if err == nil {
		updated = service.publishRoomEvent(ctx, updated, models.RoomEventScoreChanged)
	}
	return updated, err
}

func sameOptionalID(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func reviewDecision(answerID *string) string {
	if answerID == nil {
		return "retained_miss"
	}
	return "selected_answer"
}

func (service *RoomService) evaluateSemanticMiss(ctx context.Context, room models.Room, guess models.Guess) error {
	round := room.CurrentGame.CurrentRound
	priorAudits, err := service.repository.ListProviderAudits(ctx, room.Code)
	if err != nil {
		return err
	}
	priorCalls, priorCost := providerUsage(priorAudits)
	now := service.clock.Now()
	suggestion := models.JudgeSuggestion{
		ID: newID(), RoomCode: room.Code, GameID: room.CurrentGame.ID, RoundID: round.ID, GuessID: guess.ID,
		Model: service.judgeModel, PromptVersion: "judge-v1", Outcome: "unavailable", CreatedAt: now,
		RationaleCategory: "unavailable", ConfidenceBand: "none",
	}
	var audit models.ProviderCallAudit
	audits := make([]models.ProviderCallAudit, 0, service.modelPolicy.MaxAttempts)
	if !service.modelPolicy.AllowsJudge(service.judgeModel) {
		audit = service.newSkippedJudgeAudit(room, "model_not_allowed", now)
		suggestion.Outcome = "unavailable"
		return service.repository.StoreJudgeEvaluation(ctx, suggestion, []models.ProviderCallAudit{audit})
	}
	if priorCalls >= service.modelPolicy.MaxCallsPerGame || priorCost >= service.modelPolicy.MaxEstimatedCostUSD {
		audit = service.newSkippedJudgeAudit(room, "budget_exhausted", now)
		suggestion.Outcome = "budget_exhausted"
		return service.repository.StoreJudgeEvaluation(ctx, suggestion, []models.ProviderCallAudit{audit})
	}
	judge, ok := service.modelClient.(llm.JudgeClient)
	if !ok {
		audit = service.newSkippedJudgeAudit(room, "provider_absent", now)
		return service.repository.StoreJudgeEvaluation(ctx, suggestion, []models.ProviderCallAudit{audit})
	}
	var response *llm.JudgeGuessResponse
	var callErr error
	for attempt := 1; attempt <= service.modelPolicy.MaxAttempts; attempt++ {
		if service.providerGate != nil {
			if allowed, _ := service.providerGate.AllowProvider(room.Code); !allowed {
				audit = service.newSkippedJudgeAudit(room, "circuit_breaker", service.clock.Now())
				suggestion.Outcome = "budget_exhausted"
				audits = append(audits, audit)
				break
			}
		}
		callCtx, cancel := llm.WithTimeout(ctx, service.modelPolicy.Timeout)
		started := service.clock.Now()
		response, callErr = judge.JudgeGuess(callCtx, llm.JudgeGuessRequest{
			Question: round.Question, Board: *round.Board, Guess: guess.RawAnswer,
			JudgeModel: service.judgeModel, PromptVersion: "judge-v1",
		})
		cancel()
		metadata := metadataJudge(response, callErr)
		attemptAudits := []models.ProviderCallAudit{}
		service.appendProviderAudit(&attemptAudits, room.Code, room.CurrentGame.ID, round.ID, "semantic_judging", "primary", attempt, started, metadata, callErr)
		audit = attemptAudits[0]
		if callErr == nil && validateJudgeResponse(response, round.Board) == nil {
			break
		}
		if callErr == nil {
			audit.Outcome = "invalid_output"
			audit.ErrorCategory = "validation"
		}
		audits = append(audits, audit)
		response = nil
		if attempt < service.modelPolicy.MaxAttempts {
			combinedAudits := append(append([]models.ProviderCallAudit(nil), priorAudits...), audits...)
			currentCalls, currentCost := providerUsage(combinedAudits)
			if currentCalls >= service.modelPolicy.MaxCallsPerGame || currentCost >= service.modelPolicy.MaxEstimatedCostUSD {
				break
			}
		}
	}
	if response != nil {
		audits = append(audits, audit)
		suggestion.Model = response.Metadata.Model
		suggestion.SuggestedPredictionAnswerID = response.SuggestedPredictionAnswerID
		suggestion.Confidence = response.Confidence
		suggestion.ConfidenceBand = confidenceBand(response.Confidence)
		suggestion.RationaleCategory = response.RationaleCategory
		if response.SuggestedPredictionAnswerID == nil {
			suggestion.Outcome = "miss"
		} else {
			suggestion.Outcome = "suggestion"
		}
	} else {
		suggestion.Outcome = audit.Outcome
	}
	return service.repository.StoreJudgeEvaluation(ctx, suggestion, audits)
}

func metadataJudge(response *llm.JudgeGuessResponse, err error) llm.CallMetadata {
	if response != nil {
		return response.Metadata
	}
	return llm.MetadataFromError(err)
}

func validateJudgeResponse(response *llm.JudgeGuessResponse, board *models.PredictionBoard) error {
	if response == nil || response.Confidence < 0 || response.Confidence > 1 {
		return ErrGeneratedContentInvalid
	}
	validRationale := map[string]bool{"synonym": true, "abbreviation": true, "paraphrase": true, "broader_or_narrower": true, "ambiguous": true, "unrelated": true}
	if !validRationale[response.RationaleCategory] {
		return ErrGeneratedContentInvalid
	}
	if response.SuggestedPredictionAnswerID != nil {
		if _, ok := findPredictionAnswer(board, *response.SuggestedPredictionAnswerID); !ok {
			return ErrGeneratedContentInvalid
		}
	}
	return nil
}

func confidenceBand(confidence float64) string {
	if confidence >= 0.85 {
		return "high"
	}
	if confidence >= 0.60 {
		return "medium"
	}
	return "low"
}

func (service *RoomService) newSkippedJudgeAudit(room models.Room, category string, now time.Time) models.ProviderCallAudit {
	return models.ProviderCallAudit{
		ID: newID(), RoomCode: room.Code, GameID: room.CurrentGame.ID, RoundID: room.CurrentGame.CurrentRound.ID,
		Purpose: "semantic_judging", Model: service.judgeModel, PromptVersion: "judge-v1", Outcome: "skipped",
		Path: "deterministic_fallback", ErrorCategory: category, RetentionClass: "provider_audit_30d",
		StartedAt: now, CompletedAt: now,
	}
}

func normalizeSettings(settings models.RoomSettings) models.RoomSettings {
	if settings.Mode == "" {
		settings.Mode = models.GameModeSimultaneous
	}
	if settings.TotalRounds == 0 {
		settings.TotalRounds = 5
	}
	if settings.AnswerTimerSeconds == 0 {
		settings.AnswerTimerSeconds = 45
	}
	if strings.TrimSpace(settings.Locale) == "" {
		settings.Locale = "en"
	}
	settings.Locale = strings.TrimSpace(settings.Locale)
	if strings.TrimSpace(settings.PredictionModel) == "" {
		settings.PredictionModel = defaultPredictionModel
	}
	settings.PredictionModel = strings.TrimSpace(settings.PredictionModel)

	return settings
}

func validateDisplayName(displayName string) error {
	if validateBoundedText(displayName, 2, 24) != nil {
		return ErrDisplayNameInvalid
	}

	return nil
}

func (service *RoomService) validateSettings(settings models.RoomSettings) error {
	if settings.Mode != models.GameModeSimultaneous {
		return fmt.Errorf("%w: mode must be simultaneous", ErrRoomSettingsInvalid)
	}
	if settings.TotalRounds < minTotalRounds || settings.TotalRounds > maxTotalRounds {
		return fmt.Errorf("%w: total rounds must be between %d and %d", ErrRoomSettingsInvalid, minTotalRounds, maxTotalRounds)
	}
	if settings.AnswerTimerSeconds < minAnswerTimerSeconds || settings.AnswerTimerSeconds > maxAnswerTimerSeconds {
		return fmt.Errorf("%w: answer timer must be between %d and %d seconds", ErrRoomSettingsInvalid, minAnswerTimerSeconds, maxAnswerTimerSeconds)
	}
	if settings.Locale != "en" {
		return fmt.Errorf("%w: locale must be en", ErrRoomSettingsInvalid)
	}
	if settings.PredictionModel != service.predictionModel {
		return fmt.Errorf("%w: prediction model must be %s", ErrRoomSettingsInvalid, service.predictionModel)
	}
	return nil
}

func validateBoundedText(value string, minimum int, maximum int) error {
	count := utf8.RuneCountInString(value)
	if count < minimum || count > maximum {
		return ErrRoomSettingsInvalid
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return ErrRoomSettingsInvalid
		}
	}
	return nil
}

func normalizeRoomCode(code string) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if utf8.RuneCountInString(code) != 6 {
		return "", ErrRoomCodeInvalid
	}
	for _, character := range code {
		if !strings.ContainsRune(roomAlphabet, character) {
			return "", ErrRoomCodeInvalid
		}
	}
	return code, nil
}

func cloneRoom(room models.Room) models.Room {
	clonedPlayers := append([]models.Player(nil), room.Players...)
	room.Players = clonedPlayers
	room.CurrentGame = cloneGame(room.CurrentGame)
	return room
}

func cloneGame(gameState *models.Game) *models.Game {
	if gameState == nil {
		return nil
	}

	clonedGame := *gameState
	if gameState.EndedAt != nil {
		endedAt := *gameState.EndedAt
		clonedGame.EndedAt = &endedAt
	}
	clonedGame.Scoreboard = append([]models.ScoreboardEntry(nil), gameState.Scoreboard...)
	if gameState.CurrentRound != nil {
		clonedRound := *gameState.CurrentRound
		clonedRound.Guesses = append([]models.Guess(nil), gameState.CurrentRound.Guesses...)
		if gameState.CurrentRound.Board != nil {
			clonedBoard := *gameState.CurrentRound.Board
			clonedBoard.Answers = append([]models.PredictionAnswer(nil), gameState.CurrentRound.Board.Answers...)
			for index := range clonedBoard.Answers {
				clonedBoard.Answers[index].Aliases = append([]string(nil), gameState.CurrentRound.Board.Answers[index].Aliases...)
			}
			clonedRound.Board = &clonedBoard
		}
		if gameState.CurrentRound.RevealStartedAt != nil {
			revealStartedAt := *gameState.CurrentRound.RevealStartedAt
			clonedRound.RevealStartedAt = &revealStartedAt
		}
		clonedGame.CurrentRound = &clonedRound
	}

	return &clonedGame
}

func isHostToken(players []models.Player, playerToken string) bool {
	if playerToken == "" {
		return false
	}

	for _, player := range players {
		if player.IsHost && player.Token == playerToken {
			return true
		}
	}

	return false
}

func findPlayerByToken(players []models.Player, playerToken string) (models.Player, bool) {
	for _, player := range players {
		if player.Token == playerToken {
			return player, true
		}
	}

	return models.Player{}, false
}

func currentRound(room models.Room, roundID string) (*models.Round, error) {
	if room.CurrentGame == nil || room.CurrentGame.CurrentRound == nil {
		return nil, ErrRoundNotFound
	}
	if room.CurrentGame.CurrentRound.ID != roundID {
		return nil, ErrRoundNotFound
	}

	return room.CurrentGame.CurrentRound, nil
}

func prepareBoard(board models.PredictionBoard, question models.Question, now time.Time) models.PredictionBoard {
	board.ID = newID()
	board.CreatedAt = now
	for index := range board.Answers {
		board.Answers[index].ID = newID()
		board.Answers[index].CreatedAt = now
	}
	board.BoardHash = computeBoardHash(question, board.Answers, board.Provider, board.ModelName, board.PromptVersion)
	return board
}

func computeBoardHash(question models.Question, answers []models.PredictionAnswer, provider string, modelName string, promptVersion string) string {
	var builder strings.Builder
	builder.WriteString(question.Text)
	builder.WriteString("|")
	builder.WriteString(provider)
	builder.WriteString("|")
	builder.WriteString(modelName)
	builder.WriteString("|")
	builder.WriteString(promptVersion)
	for _, answer := range answers {
		builder.WriteString("|")
		builder.WriteString(answer.CanonicalAnswer)
		builder.WriteString("|")
		builder.WriteString(strings.Join(answer.Aliases, ","))
		builder.WriteString("|")
		builder.WriteString(fmt.Sprintf("%d:%d", answer.Rank, answer.Score))
	}

	sum := sha256.Sum256([]byte(builder.String()))
	return hex.EncodeToString(sum[:])[:12]
}

func normalizeAnswer(value string) string {
	mapped := strings.Map(func(r rune) rune {
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r):
			return unicode.ToLower(r)
		case unicode.IsSpace(r):
			return ' '
		default:
			return -1
		}
	}, value)

	return strings.Join(strings.Fields(mapped), " ")
}

func matchGuess(answers []models.PredictionAnswer, normalizedAnswer string) (*string, int) {
	for _, answer := range answers {
		if normalizeAnswer(answer.CanonicalAnswer) == normalizedAnswer {
			matchedID := answer.ID
			return &matchedID, answer.Score
		}
		for _, alias := range answer.Aliases {
			if normalizeAnswer(alias) == normalizedAnswer {
				matchedID := answer.ID
				return &matchedID, answer.Score
			}
		}
	}

	return nil, 0
}

func validateMatchingAliases(answers []models.PredictionAnswer) error {
	owners := make(map[string]int)
	for answerIndex, answer := range answers {
		phrases := append([]string{answer.CanonicalAnswer}, answer.Aliases...)
		for _, phrase := range phrases {
			normalized := normalizeAnswer(phrase)
			if normalized == "" {
				continue
			}
			if owner, exists := owners[normalized]; exists && owner != answerIndex {
				return fmt.Errorf("%w: %q", ErrAmbiguousBoardAnswer, normalized)
			}
			owners[normalized] = answerIndex
		}
	}

	return nil
}

func answerAlreadyClaimed(guesses []models.Guess, matchedAnswerID string, skipGuessID string) bool {
	for _, guess := range guesses {
		if guess.ID == skipGuessID {
			continue
		}
		if guess.MatchedPredictionAnswerID != nil && *guess.MatchedPredictionAnswerID == matchedAnswerID && guess.ScoreAwarded > 0 {
			return true
		}
	}

	return false
}

func ResolveGuess(submission GuessSubmission, answers []models.PredictionAnswer, guesses []models.Guess) models.Guess {
	normalizedAnswer := normalizeAnswer(submission.RawAnswer)
	matchedAnswerID, scoreAwarded := matchGuess(answers, normalizedAnswer)
	duplicate := false
	if matchedAnswerID != nil && answerAlreadyClaimed(guesses, *matchedAnswerID, "") {
		scoreAwarded = 0
		duplicate = true
	}

	return models.Guess{
		ID:                        submission.ID,
		PlayerID:                  submission.PlayerID,
		PlayerDisplayName:         submission.PlayerDisplayName,
		RawAnswer:                 submission.RawAnswer,
		NormalizedAnswer:          normalizedAnswer,
		MatchedPredictionAnswerID: matchedAnswerID,
		ScoreAwarded:              scoreAwarded,
		Duplicate:                 duplicate,
		CreatedAt:                 submission.CreatedAt,
	}
}

func ResolveOverride(override GuessOverride, board *models.PredictionBoard, guesses []models.Guess) (models.Guess, int, error) {
	guess, ok := findGuess(guesses, override.GuessID)
	if !ok {
		return models.Guess{}, 0, ErrGuessNotFound
	}

	newScore := 0
	duplicate := false
	if override.MatchedPredictionAnswerID != nil {
		answer, ok := findPredictionAnswer(board, *override.MatchedPredictionAnswerID)
		if !ok {
			return models.Guess{}, 0, ErrPredictionAnswerNotFound
		}
		if answerAlreadyClaimed(guesses, answer.ID, guess.ID) {
			duplicate = true
		} else {
			newScore = answer.Score
		}
	}

	delta := newScore - guess.ScoreAwarded
	guess.MatchedPredictionAnswerID = override.MatchedPredictionAnswerID
	guess.ScoreAwarded = newScore
	guess.Duplicate = duplicate
	return guess, delta, nil
}

func findGuess(guesses []models.Guess, guessID string) (models.Guess, bool) {
	for _, guess := range guesses {
		if guess.ID == guessID {
			return guess, true
		}
	}

	return models.Guess{}, false
}

func findPredictionAnswer(board *models.PredictionBoard, answerID string) (models.PredictionAnswer, bool) {
	if board == nil {
		return models.PredictionAnswer{}, false
	}

	for _, answer := range board.Answers {
		if answer.ID == answerID {
			return answer, true
		}
	}

	return models.PredictionAnswer{}, false
}

func initialScoreboard(players []models.Player) []models.ScoreboardEntry {
	entries := make([]models.ScoreboardEntry, 0, len(players))
	for _, player := range players {
		entries = append(entries, models.ScoreboardEntry{
			PlayerID:       player.ID,
			DisplayName:    player.DisplayName,
			Score:          0,
			IsHost:         player.IsHost,
			SubmissionMade: false,
		})
	}

	return entries
}

type generatedRound struct {
	RoundIndex int
	Question   models.Question
	Board      models.PredictionBoard
	Audits     []models.ProviderCallAudit
}

func (service *RoomService) generateRound(ctx context.Context, settings models.RoomSettings, roundIndex int, excludedQuestions []string, now time.Time) (generatedRound, error) {
	return service.generateRoundScoped(ctx, "", "", "", settings, roundIndex, excludedQuestions, now, nil)
}

func (service *RoomService) generateRoundScoped(ctx context.Context, roomCode string, gameID string, roundID string, settings models.RoomSettings, roundIndex int, excludedQuestions []string, now time.Time, priorAudits []models.ProviderCallAudit) (generatedRound, error) {
	if strings.TrimSpace(settings.PredictionModel) == "" {
		settings.PredictionModel = service.predictionModel
	}
	if !service.modelPolicy.AllowsPrediction(settings.PredictionModel) {
		return generatedRound{}, fmt.Errorf("%w: %s", llm.ErrModelNotAllowed, settings.PredictionModel)
	}
	audits := make([]models.ProviderCallAudit, 0, service.modelPolicy.MaxAttempts*2+2)
	priorCalls, priorSpent := providerUsage(priorAudits)
	paidCalls, spent := priorCalls, priorSpent
	var generationErr error
	for attempt := 1; attempt <= service.modelPolicy.MaxAttempts && paidCalls < service.modelPolicy.MaxCallsPerGame && spent < service.modelPolicy.MaxEstimatedCostUSD; attempt++ {
		round, err := service.generateRoundWithClient(ctx, service.modelClient, settings, roundIndex, excludedQuestions, now, roomCode, gameID, roundID, "primary", attempt, service.modelPolicy.MaxCallsPerGame-priorCalls, service.modelPolicy.MaxEstimatedCostUSD-priorSpent, &audits)
		newCalls, newSpent := providerUsage(audits)
		paidCalls, spent = priorCalls+newCalls, priorSpent+newSpent
		if err == nil {
			round.Audits = audits
			return round, nil
		}
		generationErr = errors.Join(generationErr, err)
	}

	round, fallbackErr := service.generateRoundWithClient(ctx, service.fallbackClient, settings, roundIndex, excludedQuestions, now, roomCode, gameID, roundID, "curated_fallback", 1, 2, service.modelPolicy.MaxEstimatedCostUSD, &audits)
	if fallbackErr == nil {
		round.Audits = audits
		return round, nil
	}

	return generatedRound{}, fmt.Errorf("%w: %v", ErrContentUnavailable, errors.Join(generationErr, fallbackErr))
}

func providerUsage(audits []models.ProviderCallAudit) (int, float64) {
	calls := 0
	cost := 0.0
	for _, audit := range audits {
		if audit.Provider != "" && audit.Provider != "static" {
			calls++
			cost += audit.EstimatedCostUSD
		}
	}
	return calls, cost
}

func (service *RoomService) generateRoundWithClient(ctx context.Context, client llm.ModelClient, settings models.RoomSettings, roundIndex int, excludedQuestions []string, now time.Time, roomCode string, gameID string, roundID string, path string, attempt int, remainingCalls int, remainingCost float64, audits *[]models.ProviderCallAudit) (generatedRound, error) {
	if path == "primary" && service.providerGate != nil {
		if allowed, _ := service.providerGate.AllowProvider(roomCode); !allowed {
			*audits = append(*audits, models.ProviderCallAudit{
				ID: newID(), RoomCode: roomCode, GameID: gameID, RoundID: roundID,
				Purpose: "question_generation", Provider: "circuit_breaker", Model: settings.PredictionModel,
				PromptVersion: "v1", Outcome: "budget_exhausted", ErrorCategory: "circuit_breaker",
				Path: path, Attempt: attempt, StartedAt: now, CompletedAt: now, RetentionClass: "provider_audit_30d",
			})
			return generatedRound{}, llm.ErrBudgetExhausted
		}
	}
	callCtx, cancel := llm.WithTimeout(ctx, service.modelPolicy.Timeout)
	started := service.clock.Now()
	questionResponse, err := client.GenerateQuestions(callCtx, llm.GenerateQuestionsRequest{
		Locale:       settings.Locale,
		Category:     "party",
		Count:        1,
		RoundIndex:   roundIndex,
		TeamSafeMode: settings.TeamSafeMode,
		ExcludedText: excludedQuestions,
	})
	cancel()
	service.appendProviderAudit(audits, roomCode, gameID, roundID, "question_generation", path, attempt, started, metadataQuestions(questionResponse, err), err)
	if err != nil {
		return generatedRound{}, err
	}
	if path == "primary" {
		calls, cost := providerUsage(*audits)
		if calls >= remainingCalls || cost >= remainingCost {
			return generatedRound{}, llm.ErrBudgetExhausted
		}
	}
	if questionResponse == nil || len(questionResponse.Questions) == 0 {
		markLastAuditInvalid(audits)
		return generatedRound{}, ErrNoQuestionsAvailable
	}
	if len(questionResponse.Questions) != 1 {
		markLastAuditInvalid(audits)
		return generatedRound{}, fmt.Errorf("%w: question generator must return exactly one question", ErrGeneratedContentInvalid)
	}

	question := questionResponse.Questions[0]
	if err := validateQuestion(question, settings.Locale, excludedQuestions); err != nil {
		markLastAuditInvalid(audits)
		return generatedRound{}, err
	}
	if question.CreatedAt.IsZero() {
		question.CreatedAt = now
	}

	if path == "primary" && service.providerGate != nil {
		if allowed, _ := service.providerGate.AllowProvider(roomCode); !allowed {
			*audits = append(*audits, models.ProviderCallAudit{
				ID: newID(), RoomCode: roomCode, GameID: gameID, RoundID: roundID,
				Purpose: "board_generation", Provider: "circuit_breaker", Model: settings.PredictionModel,
				PromptVersion: "v1", Outcome: "budget_exhausted", ErrorCategory: "circuit_breaker",
				Path: path, Attempt: attempt, StartedAt: service.clock.Now(), CompletedAt: service.clock.Now(), RetentionClass: "provider_audit_30d",
			})
			return generatedRound{}, llm.ErrBudgetExhausted
		}
	}
	callCtx, cancel = llm.WithTimeout(ctx, service.modelPolicy.Timeout)
	started = service.clock.Now()
	boardResponse, err := client.GenerateBoard(callCtx, llm.GenerateBoardRequest{
		Question:        question,
		PredictionModel: settings.PredictionModel,
		TeamSafeMode:    settings.TeamSafeMode,
		PromptVersion:   "v1",
	})
	cancel()
	service.appendProviderAudit(audits, roomCode, gameID, roundID, "board_generation", path, attempt, started, metadataBoard(boardResponse, err), err)
	if err != nil {
		return generatedRound{}, err
	}
	if boardResponse == nil {
		markLastAuditInvalid(audits)
		return generatedRound{}, fmt.Errorf("board generator returned no response")
	}
	if err := validatePredictionBoard(boardResponse.Board); err != nil {
		markLastAuditInvalid(audits)
		return generatedRound{}, err
	}

	board := prepareBoard(boardResponse.Board, question, now)
	return generatedRound{RoundIndex: roundIndex, Question: question, Board: board}, nil
}

func markLastAuditInvalid(audits *[]models.ProviderCallAudit) {
	if len(*audits) == 0 {
		return
	}
	last := len(*audits) - 1
	(*audits)[last].Outcome = "invalid_output"
	(*audits)[last].ErrorCategory = "validation"
}

func metadataQuestions(response *llm.GenerateQuestionsResponse, err error) llm.CallMetadata {
	if response != nil {
		return response.Metadata
	}
	return llm.MetadataFromError(err)
}

func metadataBoard(response *llm.GenerateBoardResponse, err error) llm.CallMetadata {
	if response != nil {
		return response.Metadata
	}
	return llm.MetadataFromError(err)
}

func (service *RoomService) appendProviderAudit(audits *[]models.ProviderCallAudit, roomCode string, gameID string, roundID string, purpose string, path string, attempt int, started time.Time, metadata llm.CallMetadata, callErr error) {
	completed := service.clock.Now()
	outcome := "success"
	category := ""
	if callErr != nil {
		outcome = "error"
		category = "provider_error"
		if errors.Is(callErr, context.DeadlineExceeded) {
			outcome = "timeout"
			category = "timeout"
		}
	}
	raw := ""
	if service.modelPolicy.CaptureRawResponses {
		raw = llm.RedactRawResponse(metadata.RawResponse, service.modelPolicy.MaxRawResponseBytes)
	}
	audit := models.ProviderCallAudit{
		ID: newID(), RoomCode: roomCode, GameID: gameID, RoundID: roundID,
		Purpose: purpose, Provider: metadata.Provider, Model: metadata.Model,
		PromptVersion: metadata.PromptVersion, RequestID: metadata.RequestID,
		Outcome: outcome, LatencyMillis: completed.Sub(started).Milliseconds(),
		InputTokens: metadata.InputTokens, OutputTokens: metadata.OutputTokens,
		EstimatedCostUSD: metadata.EstimatedCostUSD, Attempt: attempt, Path: path,
		ErrorCategory: category, RawResponse: raw, RetentionClass: "provider_audit_30d",
		StartedAt: started, CompletedAt: completed,
	}
	*audits = append(*audits, audit)
	if service.providerObserve != nil {
		service.providerObserve(audit)
	}
}

func validateQuestion(question models.Question, locale string, excluded []string) error {
	text := strings.TrimSpace(question.Text)
	if text == "" || utf8.RuneCountInString(text) > maxQuestionTextRunes {
		return fmt.Errorf("%w: question text must contain 1-%d characters", ErrGeneratedContentInvalid, maxQuestionTextRunes)
	}
	if strings.TrimSpace(question.ID) == "" || strings.TrimSpace(question.Locale) == "" || strings.TrimSpace(question.Category) == "" {
		return fmt.Errorf("%w: question id, locale, and category are required", ErrGeneratedContentInvalid)
	}
	if locale != "" && !strings.EqualFold(strings.TrimSpace(question.Locale), strings.TrimSpace(locale)) {
		return fmt.Errorf("%w: question locale %q does not match %q", ErrGeneratedContentInvalid, question.Locale, locale)
	}
	for _, previous := range excluded {
		if normalizeAnswer(previous) == normalizeAnswer(text) {
			return fmt.Errorf("%w: question was already used", ErrGeneratedContentInvalid)
		}
	}
	return nil
}

func validatePredictionBoard(board models.PredictionBoard) error {
	for label, value := range map[string]string{
		"provider": board.Provider, "model name": board.ModelName, "prompt version": board.PromptVersion,
	} {
		length := utf8.RuneCountInString(strings.TrimSpace(value))
		if length == 0 || length > maxBoardMetadataRunes {
			return fmt.Errorf("%w: %s must contain 1-%d characters", ErrGeneratedContentInvalid, label, maxBoardMetadataRunes)
		}
	}
	if len(board.Answers) != predictionAnswerCount {
		return fmt.Errorf("%w: board must contain exactly %d answers", ErrGeneratedContentInvalid, predictionAnswerCount)
	}

	canonicals := make(map[string]struct{}, len(board.Answers))
	ranks := make(map[int]struct{}, len(board.Answers))
	previousScore := maxPredictionAnswerScore + 1
	for index, answer := range board.Answers {
		canonical := strings.TrimSpace(answer.CanonicalAnswer)
		canonicalLength := utf8.RuneCountInString(canonical)
		if canonicalLength == 0 || canonicalLength > maxAnswerTextRunes {
			return fmt.Errorf("%w: answer %d canonical text must contain 1-%d characters", ErrGeneratedContentInvalid, index+1, maxAnswerTextRunes)
		}
		normalizedCanonical := normalizeAnswer(canonical)
		if _, exists := canonicals[normalizedCanonical]; exists {
			return fmt.Errorf("%w: duplicate canonical answer %q", ErrGeneratedContentInvalid, normalizedCanonical)
		}
		canonicals[normalizedCanonical] = struct{}{}
		if answer.Rank < 1 || answer.Rank > predictionAnswerCount {
			return fmt.Errorf("%w: answer rank must be between 1 and %d", ErrGeneratedContentInvalid, predictionAnswerCount)
		}
		if _, exists := ranks[answer.Rank]; exists {
			return fmt.Errorf("%w: duplicate answer rank %d", ErrGeneratedContentInvalid, answer.Rank)
		}
		ranks[answer.Rank] = struct{}{}
		if answer.Rank != index+1 {
			return fmt.Errorf("%w: answers must be ordered by rank", ErrGeneratedContentInvalid)
		}
		if answer.Score <= 0 || answer.Score > maxPredictionAnswerScore || answer.Score >= previousScore {
			return fmt.Errorf("%w: scores must be positive, at most %d, and strictly descending", ErrGeneratedContentInvalid, maxPredictionAnswerScore)
		}
		previousScore = answer.Score
		if len(answer.Aliases) > maxAliasCount {
			return fmt.Errorf("%w: answer %d has more than %d aliases", ErrGeneratedContentInvalid, index+1, maxAliasCount)
		}
		for _, alias := range answer.Aliases {
			length := utf8.RuneCountInString(strings.TrimSpace(alias))
			if length == 0 || length > maxAnswerTextRunes {
				return fmt.Errorf("%w: aliases must contain 1-%d characters", ErrGeneratedContentInvalid, maxAnswerTextRunes)
			}
		}
	}
	if err := validateMatchingAliases(board.Answers); err != nil {
		return fmt.Errorf("%w: %w", ErrGeneratedContentInvalid, err)
	}
	return nil
}

func resetSubmissionFlags(scoreboard []models.ScoreboardEntry) {
	for index := range scoreboard {
		scoreboard[index].SubmissionMade = false
	}
}

func randomAlphabeticCode(length int) string {
	buffer := make([]byte, length)
	fillRandom(buffer)

	code := make([]byte, length)
	for index, value := range buffer {
		code[index] = roomAlphabet[int(value)%len(roomAlphabet)]
	}

	return string(code)
}

func newID() string {
	buffer := make([]byte, 8)
	fillRandom(buffer)
	return hex.EncodeToString(buffer)
}

func newToken() string {
	buffer := make([]byte, 16)
	fillRandom(buffer)
	return hex.EncodeToString(buffer)
}

func fillRandom(buffer []byte) {
	if _, err := crand.Read(buffer); err == nil {
		return
	}

	panic(fmt.Errorf("random source unavailable"))
}
