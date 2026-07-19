package game

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

type InMemoryRoomRepository struct {
	mu          sync.RWMutex
	rooms       map[string]*models.Room
	audits      map[string][]models.ProviderCallAudit
	suggestions map[string][]models.JudgeSuggestion
	events      map[string][]models.RoomEvent
	rounds      map[string][]models.Round
}

func NewInMemoryRoomRepository() *InMemoryRoomRepository {
	return &InMemoryRoomRepository{
		rooms: make(map[string]*models.Room), audits: make(map[string][]models.ProviderCallAudit),
		suggestions: make(map[string][]models.JudgeSuggestion),
		events:      make(map[string][]models.RoomEvent),
		rounds:      make(map[string][]models.Round),
	}
}

func (repository *InMemoryRoomRepository) AppendRoomEvent(_ context.Context, code string, eventType models.RoomEventType, occurredAt time.Time) (models.RoomEvent, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	room, ok := repository.rooms[code]
	if !ok {
		return models.RoomEvent{}, ErrRoomNotFound
	}
	room.Revision++
	event := models.RoomEvent{
		Version: models.RoomEventVersion, ID: newID(), RoomCode: code, Type: eventType,
		RoomRevision: room.Revision, OccurredAt: occurredAt,
	}
	repository.events[code] = append(repository.events[code], event)
	if len(repository.events[code]) > 1000 {
		repository.events[code] = append([]models.RoomEvent(nil), repository.events[code][len(repository.events[code])-1000:]...)
	}
	return event, nil
}

func (repository *InMemoryRoomRepository) ListRoomEvents(_ context.Context, code string, afterRevision int64, limit int) ([]models.RoomEvent, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	if _, ok := repository.rooms[code]; !ok {
		return nil, ErrRoomNotFound
	}
	if limit < 1 || limit > 100 {
		limit = 100
	}
	result := make([]models.RoomEvent, 0, limit)
	for _, event := range repository.events[code] {
		if event.RoomRevision > afterRevision {
			result = append(result, event)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
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

func (repository *InMemoryRoomRepository) GetRoomByReplayID(_ context.Context, replayID string) (models.Room, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	for _, room := range repository.rooms {
		if room.CurrentGame != nil && room.CurrentGame.ReplayID == replayID {
			return cloneRoom(*room), nil
		}
	}
	return models.Room{}, ErrReplayNotFound
}

func (repository *InMemoryRoomRepository) ListGameRounds(_ context.Context, gameID string) ([]models.Round, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	rounds := repository.rounds[gameID]
	result := make([]models.Round, 0, len(rounds)+1)
	for _, round := range rounds {
		result = append(result, *cloneGame(&models.Game{CurrentRound: &round}).CurrentRound)
	}
	for _, room := range repository.rooms {
		if room.CurrentGame != nil && room.CurrentGame.ID == gameID && room.CurrentGame.CurrentRound != nil {
			current := room.CurrentGame.CurrentRound
			if len(result) == 0 || result[len(result)-1].ID != current.ID {
				result = append(result, *cloneGame(&models.Game{CurrentRound: current}).CurrentRound)
			}
			break
		}
	}
	return result, nil
}

func (repository *InMemoryRoomRepository) AddPlayer(_ context.Context, code string, player models.Player) (models.Room, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()

	room, ok := repository.rooms[code]
	if !ok {
		return models.Room{}, ErrRoomNotFound
	}
	if room.Status != models.RoomStatusLobby || room.CurrentGame != nil {
		return models.Room{}, ErrRoomJoinClosed
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
	repository.audits[code] = append(repository.audits[code], gameState.CurrentRound.ProviderAudits...)
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

func (repository *InMemoryRoomRepository) RevealRound(_ context.Context, code string, roundID string, transition models.RoundTransition) (models.Room, error) {
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
	round.RevealStartedAt = &transition.CreatedAt
	room.UpdatedAt = transition.CreatedAt
	room.Revision++
	event := models.RoomEvent{Version: models.RoomEventVersion, ID: newID(), RoomCode: code,
		Type: models.RoomEventRoundRevealed, RoomRevision: room.Revision, OccurredAt: transition.CreatedAt}
	repository.events[code] = append(repository.events[code], event)

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
		if room.CurrentGame.CurrentRound != nil {
			repository.rounds[room.CurrentGame.ID] = append(repository.rounds[room.CurrentGame.ID], *cloneGame(&models.Game{CurrentRound: room.CurrentGame.CurrentRound}).CurrentRound)
		}
		room.CurrentGame.CurrentRound = cloneGame(&models.Game{CurrentRound: nextRound}).CurrentRound
		repository.audits[code] = append(repository.audits[code], nextRound.ProviderAudits...)
	}
	room.UpdatedAt = time.Now().UTC()

	return cloneRoom(*room), nil
}

func (repository *InMemoryRoomRepository) ListProviderAudits(_ context.Context, code string) ([]models.ProviderCallAudit, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	if _, ok := repository.rooms[code]; !ok {
		return nil, ErrRoomNotFound
	}
	return append([]models.ProviderCallAudit(nil), repository.audits[code]...), nil
}

func (repository *InMemoryRoomRepository) StoreJudgeEvaluation(_ context.Context, suggestion models.JudgeSuggestion, audits []models.ProviderCallAudit) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if _, ok := repository.rooms[suggestion.RoomCode]; !ok {
		return ErrRoomNotFound
	}
	repository.suggestions[suggestion.RoomCode] = append(repository.suggestions[suggestion.RoomCode], suggestion)
	repository.audits[suggestion.RoomCode] = append(repository.audits[suggestion.RoomCode], audits...)
	return nil
}

func (repository *InMemoryRoomRepository) ListJudgeSuggestions(_ context.Context, code string, roundID string) ([]models.JudgeSuggestion, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	if _, ok := repository.rooms[code]; !ok {
		return nil, ErrRoomNotFound
	}
	result := make([]models.JudgeSuggestion, 0)
	for _, suggestion := range repository.suggestions[code] {
		if suggestion.RoundID == roundID {
			result = append(result, suggestion)
		}
	}
	return result, nil
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
	if override.JudgeSuggestionID != "" {
		for index := range repository.suggestions[code] {
			suggestion := &repository.suggestions[code][index]
			if suggestion.ID == override.JudgeSuggestionID && suggestion.GuessID == override.GuessID {
				reviewedAt := override.CreatedAt
				suggestion.ReviewedAt = &reviewedAt
				suggestion.ReviewDecision = override.ReviewDecision
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
