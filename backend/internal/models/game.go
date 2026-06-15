package models

import "time"

type GameStatus string

const (
	GameStatusInProgress GameStatus = "in_progress"
	GameStatusCompleted  GameStatus = "completed"
)

type RoundStatus string

const (
	RoundStatusAnswering RoundStatus = "answering"
	RoundStatusRevealed  RoundStatus = "revealed"
)

type PredictionAnswer struct {
	ID              string    `json:"id"`
	CanonicalAnswer string    `json:"canonicalAnswer"`
	Aliases         []string  `json:"aliases"`
	Rank            int       `json:"rank"`
	Score           int       `json:"score"`
	CreatedAt       time.Time `json:"createdAt"`
}

type PredictionBoard struct {
	ID            string             `json:"id"`
	Provider      string             `json:"provider"`
	ModelName     string             `json:"modelName"`
	PromptVersion string             `json:"promptVersion"`
	BoardHash     string             `json:"boardHash"`
	Answers       []PredictionAnswer `json:"answers"`
	CreatedAt     time.Time          `json:"createdAt"`
}

type Guess struct {
	ID                        string    `json:"id"`
	PlayerID                  string    `json:"playerId"`
	PlayerDisplayName         string    `json:"playerDisplayName"`
	RawAnswer                 string    `json:"rawAnswer"`
	NormalizedAnswer          string    `json:"normalizedAnswer"`
	MatchedPredictionAnswerID *string   `json:"matchedPredictionAnswerId,omitempty"`
	ScoreAwarded              int       `json:"scoreAwarded"`
	Duplicate                 bool      `json:"duplicate"`
	CreatedAt                 time.Time `json:"createdAt"`
}

type ScoreboardEntry struct {
	PlayerID       string `json:"playerId"`
	DisplayName    string `json:"displayName"`
	Score          int    `json:"score"`
	IsHost         bool   `json:"isHost"`
	SubmissionMade bool   `json:"submissionMade"`
}

type ScoreEvent struct {
	ID        string    `json:"id"`
	GameID    string    `json:"gameId"`
	RoundID   string    `json:"roundId"`
	PlayerID  string    `json:"playerId"`
	Delta     int       `json:"delta"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}

type Question struct {
	ID        string    `json:"id"`
	Text      string    `json:"text"`
	Locale    string    `json:"locale"`
	Category  string    `json:"category"`
	CreatedAt time.Time `json:"createdAt"`
}

type Round struct {
	ID                   string           `json:"id"`
	RoundIndex           int              `json:"roundIndex"`
	Status               RoundStatus      `json:"status"`
	Question             Question         `json:"question"`
	BoardHash            string           `json:"boardHash"`
	Board                *PredictionBoard `json:"board,omitempty"`
	Guesses              []Guess          `json:"guesses,omitempty"`
	AnswerPhaseStartedAt time.Time        `json:"answerPhaseStartedAt"`
	AnswerPhaseEndsAt    time.Time        `json:"answerPhaseEndsAt"`
	RevealStartedAt      *time.Time       `json:"revealStartedAt,omitempty"`
	CreatedAt            time.Time        `json:"createdAt"`
}

type Game struct {
	ID                string            `json:"id"`
	Status            GameStatus        `json:"status"`
	Mode              GameMode          `json:"mode"`
	TotalRounds       int               `json:"totalRounds"`
	CurrentRoundIndex int               `json:"currentRoundIndex"`
	CurrentRound      *Round            `json:"currentRound,omitempty"`
	Scoreboard        []ScoreboardEntry `json:"scoreboard,omitempty"`
	CreatedAt         time.Time         `json:"createdAt"`
	StartedAt         time.Time         `json:"startedAt"`
	EndedAt           *time.Time        `json:"endedAt,omitempty"`
}
