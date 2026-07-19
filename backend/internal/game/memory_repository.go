package game

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

type InMemoryRoomRepository struct {
	mu    sync.RWMutex
	rooms map[string]*models.Room
}

func NewInMemoryRoomRepository() *InMemoryRoomRepository {
	return &InMemoryRoomRepository{rooms: make(map[string]*models.Room)}
}

func (repository *InMemoryRoomRepository) CreateRoom(_ context.Context, room models.Room) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	if _, exists := repository.rooms[room.Code]; exists {
		return ErrRoomCodeConflict
	}

	cloned := cloneRoom(room)
	repository.rooms[room.Code] = &cloned
	return nil
}

func (repository *InMemoryRoomRepository) GetRoom(_ context.Context, code string) (models.Room, error) {
	repository.mu.RLock()
	room, ok := repository.rooms[code]
	repository.mu.RUnlock()
	if !ok {
		return models.Room{}, ErrRoomNotFound
	}

	return cloneRoom(*room), nil
}

func (repository *InMemoryRoomRepository) AddPlayer(_ context.Context, code string, player models.Player) (models.Room, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	room, ok := repository.rooms[code]
	if !ok {
		return models.Room{}, ErrRoomNotFound
	}

	for _, existingPlayer := range room.Players {
		if normalizeDisplayName(existingPlayer.DisplayName) == normalizeDisplayName(player.DisplayName) {
			return models.Room{}, ErrDuplicatePlayer
		}
	}

	room.Players = append(room.Players, player)
	room.UpdatedAt = time.Now().UTC()

	return cloneRoom(*room), nil
}

func (repository *InMemoryRoomRepository) StartGame(_ context.Context, code string, gameState models.Game) (models.Room, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	room, ok := repository.rooms[code]
	if !ok {
		return models.Room{}, ErrRoomNotFound
	}

	if room.Status != models.RoomStatusLobby || room.CurrentGame != nil {
		return models.Room{}, ErrGameAlreadyStarted
	}

	room.Status = models.RoomStatusInGame
	room.CurrentGame = cloneGame(&gameState)
	room.UpdatedAt = time.Now().UTC()

	return cloneRoom(*room), nil
}

func (repository *InMemoryRoomRepository) SubmitGuess(_ context.Context, code string, roundID string, submission GuessSubmission, clock Clock) (models.Room, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	room, ok := repository.rooms[code]
	if !ok {
		return models.Room{}, ErrRoomNotFound
	}
	if room.CurrentGame == nil || room.CurrentGame.CurrentRound == nil || room.CurrentGame.CurrentRound.ID != roundID {
		return models.Room{}, ErrRoundNotFound
	}

	round := room.CurrentGame.CurrentRound
	if round.Status != models.RoundStatusAnswering {
		return models.Room{}, ErrRoundNotAcceptingGuesses
	}
	if !clock.Now().Before(round.AnswerPhaseEndsAt) {
		return models.Room{}, ErrAnswerPhaseExpired
	}
	for _, existingGuess := range round.Guesses {
		if existingGuess.PlayerID == submission.PlayerID {
			return models.Room{}, ErrGuessAlreadySubmitted
		}
	}
	if round.Board == nil {
		return models.Room{}, ErrPredictionAnswerNotFound
	}

	guess := ResolveGuess(submission, round.Board.Answers, round.Guesses)
	round.Guesses = append(round.Guesses, guess)
	applyGuessToScoreboard(room.CurrentGame, guess)
	room.UpdatedAt = time.Now().UTC()

	return cloneRoom(*room), nil
}

func (repository *InMemoryRoomRepository) RevealRound(_ context.Context, code string, roundID string, revealStartedAt time.Time) (models.Room, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	room, ok := repository.rooms[code]
	if !ok {
		return models.Room{}, ErrRoomNotFound
	}
	if room.CurrentGame == nil || room.CurrentGame.CurrentRound == nil || room.CurrentGame.CurrentRound.ID != roundID {
		return models.Room{}, ErrRoundNotFound
	}

	round := room.CurrentGame.CurrentRound
	if round.Status == models.RoundStatusRevealed {
		return models.Room{}, ErrRoundAlreadyRevealed
	}

	round.Status = models.RoundStatusRevealed
	round.RevealStartedAt = &revealStartedAt
	room.UpdatedAt = revealStartedAt

	return cloneRoom(*room), nil
}

func (repository *InMemoryRoomRepository) AdvanceGame(_ context.Context, code string, gameState models.Game, nextRound *models.Round) (models.Room, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	room, ok := repository.rooms[code]
	if !ok {
		return models.Room{}, ErrRoomNotFound
	}
	if room.CurrentGame == nil {
		return models.Room{}, ErrRoundNotFound
	}

	room.CurrentGame.Status = gameState.Status
	room.CurrentGame.CurrentRoundIndex = gameState.CurrentRoundIndex
	room.CurrentGame.Scoreboard = append([]models.ScoreboardEntry(nil), gameState.Scoreboard...)
	room.CurrentGame.EndedAt = gameState.EndedAt
	if nextRound != nil {
		room.CurrentGame.CurrentRound = cloneGame(&models.Game{CurrentRound: nextRound}).CurrentRound
	}
	room.UpdatedAt = time.Now().UTC()

	return cloneRoom(*room), nil
}

func (repository *InMemoryRoomRepository) OverrideGuess(_ context.Context, code string, roundID string, override GuessOverride) (models.Room, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	room, ok := repository.rooms[code]
	if !ok {
		return models.Room{}, ErrRoomNotFound
	}
	if room.CurrentGame == nil || room.CurrentGame.CurrentRound == nil || room.CurrentGame.CurrentRound.ID != roundID {
		return models.Room{}, ErrRoundNotFound
	}

	round := room.CurrentGame.CurrentRound
	guess, delta, err := ResolveOverride(override, round.Board, round.Guesses)
	if err != nil {
		return models.Room{}, err
	}

	for index := range room.CurrentGame.CurrentRound.Guesses {
		if room.CurrentGame.CurrentRound.Guesses[index].ID == guess.ID {
			room.CurrentGame.CurrentRound.Guesses[index] = guess
			break
		}
	}

	if delta != 0 {
		for index := range room.CurrentGame.Scoreboard {
			if room.CurrentGame.Scoreboard[index].PlayerID == guess.PlayerID {
				room.CurrentGame.Scoreboard[index].Score += delta
				break
			}
		}
	}

	room.UpdatedAt = time.Now().UTC()
	return cloneRoom(*room), nil
}

func applyGuessToScoreboard(gameState *models.Game, guess models.Guess) {
	if gameState == nil {
		return
	}

	updated := false
	for index := range gameState.Scoreboard {
		if gameState.Scoreboard[index].PlayerID == guess.PlayerID {
			gameState.Scoreboard[index].Score += guess.ScoreAwarded
			gameState.Scoreboard[index].SubmissionMade = true
			updated = true
			break
		}
	}

	if !updated {
		gameState.Scoreboard = append(gameState.Scoreboard, models.ScoreboardEntry{
			PlayerID:       guess.PlayerID,
			DisplayName:    guess.PlayerDisplayName,
			Score:          guess.ScoreAwarded,
			SubmissionMade: true,
		})
	}
}

func normalizeDisplayName(displayName string) string {
	return strings.ToLower(strings.TrimSpace(displayName))
}
