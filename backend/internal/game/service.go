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

	"github.com/bogdandobrica/modelsays/backend/internal/llm"
	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

var (
	ErrRoomNotFound             = errors.New("room not found")
	ErrDisplayNameInvalid       = errors.New("display name must be between 2 and 24 characters")
	ErrRoomNameInvalid          = errors.New("room name must be between 3 and 48 characters")
	ErrDuplicatePlayer          = errors.New("display name already taken in this room")
	ErrRoomCodeConflict         = errors.New("room code already exists")
	ErrUnauthorizedStart        = errors.New("only the host can start the game")
	ErrUnauthorizedReveal       = errors.New("only the host can reveal the round")
	ErrUnauthorizedAdvance      = errors.New("only the host can advance to the next round")
	ErrUnauthorizedOverride     = errors.New("only the host can override a match")
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
	ErrAnswerInvalid            = errors.New("answer must be between 1 and 120 characters")
)

const roomAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

type CreateRoomInput struct {
	RoomName        string
	HostDisplayName string
	Settings        models.RoomSettings
}

type JoinRoomInput struct {
	Code        string
	DisplayName string
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
}

type RoomService struct {
	repository  RoomRepository
	modelClient llm.ModelClient
	clock       Clock
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
}

type RoomRepository interface {
	CreateRoom(ctx context.Context, room models.Room) error
	GetRoom(ctx context.Context, code string) (models.Room, error)
	AddPlayer(ctx context.Context, code string, player models.Player) (models.Room, error)
	StartGame(ctx context.Context, code string, gameState models.Game) (models.Room, error)
	SubmitGuess(ctx context.Context, code string, roundID string, submission GuessSubmission, clock Clock) (models.Room, error)
	RevealRound(ctx context.Context, code string, roundID string, revealStartedAt time.Time) (models.Room, error)
	AdvanceGame(ctx context.Context, code string, gameState models.Game, nextRound *models.Round) (models.Room, error)
	OverrideGuess(ctx context.Context, code string, roundID string, override GuessOverride) (models.Room, error)
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
		repository:  repository,
		modelClient: modelClient,
		clock:       clock,
	}
}

func NewInMemoryRoomService() *RoomService {
	return NewRoomService(NewInMemoryRoomRepository(), llm.NewStaticModelClient())
}

func (service *RoomService) CreateRoom(ctx context.Context, input CreateRoomInput) (models.Room, models.Player, error) {
	roomName := strings.TrimSpace(input.RoomName)
	hostDisplayName := strings.TrimSpace(input.HostDisplayName)

	if len(roomName) < 3 || len(roomName) > 48 {
		return models.Room{}, models.Player{}, ErrRoomNameInvalid
	}

	if err := validateDisplayName(hostDisplayName); err != nil {
		return models.Room{}, models.Player{}, err
	}

	settings := normalizeSettings(input.Settings)
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

func (service *RoomService) GetRoom(ctx context.Context, code string) (models.Room, error) {
	return service.repository.GetRoom(ctx, strings.ToUpper(strings.TrimSpace(code)))
}

func (service *RoomService) JoinRoom(ctx context.Context, input JoinRoomInput) (models.Room, models.Player, error) {
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

	room, err := service.repository.AddPlayer(ctx, strings.ToUpper(strings.TrimSpace(input.Code)), player)
	if err != nil {
		return models.Room{}, models.Player{}, err
	}

	return room, player, nil
}

func (service *RoomService) StartGame(ctx context.Context, input StartGameInput) (models.Room, error) {
	code := strings.ToUpper(strings.TrimSpace(input.Code))
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
	roundSeed, err := service.generateRound(ctx, room.Settings, 1, now)
	if err != nil {
		return models.Room{}, err
	}

	gameState := models.Game{
		ID:                newID(),
		Status:            models.GameStatusInProgress,
		Mode:              room.Settings.Mode,
		TotalRounds:       room.Settings.TotalRounds,
		CurrentRoundIndex: 1,
		Scoreboard:        initialScoreboard(room.Players),
		CreatedAt:         now,
		StartedAt:         now,
		CurrentRound: &models.Round{
			ID:                   newID(),
			RoundIndex:           roundSeed.RoundIndex,
			Status:               models.RoundStatusAnswering,
			Question:             roundSeed.Question,
			BoardHash:            roundSeed.Board.BoardHash,
			Board:                &roundSeed.Board,
			AnswerPhaseStartedAt: now,
			AnswerPhaseEndsAt:    now.Add(time.Duration(room.Settings.AnswerTimerSeconds) * time.Second),
			CreatedAt:            now,
		},
	}

	return service.repository.StartGame(ctx, code, gameState)
}

func (service *RoomService) SubmitGuess(ctx context.Context, input SubmitGuessInput) (models.Room, error) {
	code := strings.ToUpper(strings.TrimSpace(input.Code))
	roundID := strings.TrimSpace(input.RoundID)
	answer := strings.TrimSpace(input.Answer)
	if len(answer) < 1 || len(answer) > 120 {
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

	return service.repository.SubmitGuess(ctx, code, round.ID, GuessSubmission{
		ID:                newID(),
		PlayerID:          player.ID,
		PlayerDisplayName: player.DisplayName,
		RawAnswer:         answer,
		CreatedAt:         now,
		ScoreEventID:      newID(),
	}, service.clock)
}

func (service *RoomService) RevealRound(ctx context.Context, input RevealRoundInput) (models.Room, error) {
	code := strings.ToUpper(strings.TrimSpace(input.Code))
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

	return service.repository.RevealRound(ctx, code, round.ID, service.clock.Now())
}

func (service *RoomService) NextRound(ctx context.Context, input NextRoundInput) (models.Room, error) {
	code := strings.ToUpper(strings.TrimSpace(input.Code))
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
		return service.repository.AdvanceGame(ctx, code, *completedGame, nil)
	}

	nextRoundIndex := room.CurrentGame.CurrentRoundIndex + 1
	roundSeed, err := service.generateRound(ctx, room.Settings, nextRoundIndex, now)
	if err != nil {
		return models.Room{}, err
	}

	nextRound := &models.Round{
		ID:                   newID(),
		RoundIndex:           nextRoundIndex,
		Status:               models.RoundStatusAnswering,
		Question:             roundSeed.Question,
		BoardHash:            roundSeed.Board.BoardHash,
		Board:                &roundSeed.Board,
		AnswerPhaseStartedAt: now,
		AnswerPhaseEndsAt:    now.Add(time.Duration(room.Settings.AnswerTimerSeconds) * time.Second),
		CreatedAt:            now,
	}

	nextGame := cloneGame(room.CurrentGame)
	nextGame.Status = models.GameStatusInProgress
	nextGame.CurrentRoundIndex = nextRoundIndex
	nextGame.CurrentRound = nextRound
	resetSubmissionFlags(nextGame.Scoreboard)

	return service.repository.AdvanceGame(ctx, code, *nextGame, nextRound)
}

func (service *RoomService) OverrideMatch(ctx context.Context, input OverrideMatchInput) (models.Room, error) {
	code := strings.ToUpper(strings.TrimSpace(input.Code))
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

	return service.repository.OverrideGuess(ctx, code, round.ID, GuessOverride{
		GuessID:                   guessID,
		MatchedPredictionAnswerID: input.MatchedPredictionAnswerID,
		ScoreEventID:              newID(),
		CreatedAt:                 service.clock.Now(),
	})
}

func normalizeSettings(settings models.RoomSettings) models.RoomSettings {
	if settings.Mode == "" {
		settings.Mode = models.GameModeSimultaneous
	}
	if settings.TotalRounds <= 0 {
		settings.TotalRounds = 5
	}
	if settings.AnswerTimerSeconds <= 0 {
		settings.AnswerTimerSeconds = 45
	}
	if strings.TrimSpace(settings.Locale) == "" {
		settings.Locale = "en"
	}
	if strings.TrimSpace(settings.PredictionModel) == "" {
		settings.PredictionModel = "gpt-4.1-mini"
	}

	return settings
}

func validateDisplayName(displayName string) error {
	if len(displayName) < 2 || len(displayName) > 24 {
		return ErrDisplayNameInvalid
	}

	return nil
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
}

func (service *RoomService) generateRound(ctx context.Context, settings models.RoomSettings, roundIndex int, now time.Time) (generatedRound, error) {
	questionResponse, err := service.modelClient.GenerateQuestions(ctx, llm.GenerateQuestionsRequest{
		Locale:       settings.Locale,
		Category:     "party",
		Count:        1,
		RoundIndex:   roundIndex,
		TeamSafeMode: settings.TeamSafeMode,
	})
	if err != nil {
		return generatedRound{}, err
	}
	if questionResponse == nil || len(questionResponse.Questions) == 0 {
		return generatedRound{}, ErrNoQuestionsAvailable
	}

	question := questionResponse.Questions[0]
	if question.CreatedAt.IsZero() {
		question.CreatedAt = now
	}

	boardResponse, err := service.modelClient.GenerateBoard(ctx, llm.GenerateBoardRequest{
		Question:        question,
		PredictionModel: settings.PredictionModel,
		TeamSafeMode:    settings.TeamSafeMode,
		PromptVersion:   "v1",
	})
	if err != nil {
		return generatedRound{}, err
	}
	if boardResponse == nil {
		return generatedRound{}, fmt.Errorf("board generator returned no response")
	}

	board := prepareBoard(boardResponse.Board, question, now)
	return generatedRound{RoundIndex: roundIndex, Question: question, Board: board}, nil
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
