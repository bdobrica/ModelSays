package models

import "time"

const RoomEventVersion = 1

type RoomEventType string

const (
	RoomEventPlayerJoined       RoomEventType = "player_joined"
	RoomEventTeamsChanged       RoomEventType = "teams_changed"
	RoomEventGameStarted        RoomEventType = "game_started"
	RoomEventSubmissionProgress RoomEventType = "submission_progress_changed"
	RoomEventRoundRevealed      RoomEventType = "round_revealed"
	RoomEventScoreChanged       RoomEventType = "score_changed"
	RoomEventRoundStarted       RoomEventType = "round_started"
	RoomEventGameCompleted      RoomEventType = "game_completed"
	RoomEventGameReset          RoomEventType = "game_reset"
)

// RoomEvent is deliberately an invalidation envelope. It never contains board,
// guess, score, player-token, or provider content.
type RoomEvent struct {
	Version      int           `json:"version"`
	ID           string        `json:"id"`
	RoomCode     string        `json:"roomCode"`
	Type         RoomEventType `json:"type"`
	RoomRevision int64         `json:"roomRevision"`
	OccurredAt   time.Time     `json:"occurredAt"`
}
