package models

import "time"

type RoomStatus string

const (
	RoomStatusLobby  RoomStatus = "lobby"
	RoomStatusInGame RoomStatus = "in_game"
)

type GameMode string

const (
	GameModeSimultaneous GameMode = "simultaneous"
)

type RoomSettings struct {
	Mode               GameMode `json:"mode"`
	TotalRounds        int      `json:"totalRounds"`
	AnswerTimerSeconds int      `json:"answerTimerSeconds"`
	Locale             string   `json:"locale"`
	PredictionModel    string   `json:"predictionModel"`
	TeamSafeMode       bool     `json:"teamSafeMode"`
}

type Player struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	IsHost      bool      `json:"isHost"`
	JoinedAt    time.Time `json:"joinedAt"`
	Token       string    `json:"token,omitempty"`
}

type Room struct {
	Code        string       `json:"code"`
	Name        string       `json:"name"`
	Status      RoomStatus   `json:"status"`
	Settings    RoomSettings `json:"settings"`
	Players     []Player     `json:"players"`
	CurrentGame *Game        `json:"currentGame,omitempty"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}
