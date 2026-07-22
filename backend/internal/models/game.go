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
	RoundStatusPending   RoundStatus = "pending"
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

type TeamScoreboardEntry struct {
	TeamID string `json:"teamId"`
	Name   string `json:"name"`
	Score  int    `json:"score"`
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
	ID                   string              `json:"id"`
	RoundIndex           int                 `json:"roundIndex"`
	Status               RoundStatus         `json:"status"`
	Question             Question            `json:"question"`
	BoardHash            string              `json:"boardHash"`
	Board                *PredictionBoard    `json:"board,omitempty"`
	Guesses              []Guess             `json:"guesses,omitempty"`
	AnswerPhaseStartedAt time.Time           `json:"answerPhaseStartedAt"`
	AnswerPhaseEndsAt    time.Time           `json:"answerPhaseEndsAt"`
	RevealStartedAt      *time.Time          `json:"revealStartedAt,omitempty"`
	RevealPhaseEndsAt    *time.Time          `json:"revealPhaseEndsAt,omitempty"`
	CreatedAt            time.Time           `json:"createdAt"`
	ProviderAudits       []ProviderCallAudit `json:"-"`
	TurnOrder            []string            `json:"turnOrder,omitempty"`
	CurrentTurnIndex     *int                `json:"currentTurnIndex,omitempty"`
	TurnEndsAt           *time.Time          `json:"turnEndsAt,omitempty"`
}

type RoundTransitionActor string

const (
	RoundTransitionActorHost      RoundTransitionActor = "host"
	RoundTransitionActorScheduler RoundTransitionActor = "scheduler"
	RoundTransitionActorPlayer    RoundTransitionActor = "player"
)

type RoundTransition struct {
	ID        string               `json:"id"`
	RoomCode  string               `json:"roomCode"`
	GameID    string               `json:"gameId"`
	RoundID   string               `json:"roundId"`
	Action    string               `json:"action"`
	Actor     RoundTransitionActor `json:"actor"`
	Reason    string               `json:"reason"`
	CreatedAt time.Time            `json:"createdAt"`
	TurnIndex *int                 `json:"turnIndex,omitempty"`
	Revision  int64                `json:"revision,omitempty"`
}

type ProviderCallAudit struct {
	ID               string    `json:"id"`
	RoomCode         string    `json:"roomCode"`
	GameID           string    `json:"gameId"`
	RoundID          string    `json:"roundId,omitempty"`
	Purpose          string    `json:"purpose"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	PromptVersion    string    `json:"promptVersion"`
	RequestID        string    `json:"requestId,omitempty"`
	Outcome          string    `json:"outcome"`
	LatencyMillis    int64     `json:"latencyMillis"`
	InputTokens      int       `json:"inputTokens"`
	OutputTokens     int       `json:"outputTokens"`
	EstimatedCostUSD float64   `json:"estimatedCostUsd"`
	Attempt          int       `json:"attempt"`
	Path             string    `json:"path"`
	ErrorCategory    string    `json:"errorCategory,omitempty"`
	RawResponse      string    `json:"rawResponse,omitempty"`
	RetentionClass   string    `json:"retentionClass"`
	StartedAt        time.Time `json:"startedAt"`
	CompletedAt      time.Time `json:"completedAt"`
}

type JudgeSuggestion struct {
	ID                          string     `json:"id"`
	RoomCode                    string     `json:"roomCode"`
	GameID                      string     `json:"gameId"`
	RoundID                     string     `json:"roundId"`
	GuessID                     string     `json:"guessId"`
	SuggestedPredictionAnswerID *string    `json:"suggestedPredictionAnswerId,omitempty"`
	Confidence                  float64    `json:"confidence"`
	ConfidenceBand              string     `json:"confidenceBand"`
	RationaleCategory           string     `json:"rationaleCategory"`
	Model                       string     `json:"model"`
	PromptVersion               string     `json:"promptVersion"`
	Outcome                     string     `json:"outcome"`
	CreatedAt                   time.Time  `json:"createdAt"`
	ReviewedAt                  *time.Time `json:"reviewedAt,omitempty"`
	ReviewDecision              string     `json:"reviewDecision,omitempty"`
}

type Game struct {
	ID                string                `json:"id"`
	ReplayID          string                `json:"replayId,omitempty"`
	Status            GameStatus            `json:"status"`
	Mode              GameMode              `json:"mode"`
	TotalRounds       int                   `json:"totalRounds"`
	CurrentRoundIndex int                   `json:"currentRoundIndex"`
	CurrentRound      *Round                `json:"currentRound,omitempty"`
	Scoreboard        []ScoreboardEntry     `json:"scoreboard,omitempty"`
	TeamScoreboard    []TeamScoreboardEntry `json:"teamScoreboard,omitempty"`
	CreatedAt         time.Time             `json:"createdAt"`
	StartedAt         time.Time             `json:"startedAt"`
	EndedAt           *time.Time            `json:"endedAt,omitempty"`
	PreparedRounds    []Round               `json:"-"`
}

type ReplayRound struct {
	RoundIndex  int               `json:"roundIndex"`
	Question    string            `json:"question"`
	Board       []ReplayAnswer    `json:"board"`
	Guesses     []ReplayGuess     `json:"guesses"`
	ScoreDeltas []ScoreboardEntry `json:"scoreDeltas"`
}

type ReplayAnswer struct {
	ID              string `json:"id"`
	CanonicalAnswer string `json:"canonicalAnswer"`
	Rank            int    `json:"rank"`
	Score           int    `json:"score"`
}

type ReplayGuess struct {
	PlayerDisplayName         string  `json:"playerDisplayName"`
	RawAnswer                 string  `json:"rawAnswer"`
	MatchedPredictionAnswerID *string `json:"matchedPredictionAnswerId,omitempty"`
	ScoreAwarded              int     `json:"scoreAwarded"`
	Duplicate                 bool    `json:"duplicate"`
}

type ReplaySummary struct {
	ID           string                `json:"id"`
	RoomName     string                `json:"roomName"`
	Mode         GameMode              `json:"mode"`
	StartedAt    time.Time             `json:"startedAt"`
	EndedAt      time.Time             `json:"endedAt"`
	Rankings     []ScoreboardEntry     `json:"rankings"`
	TeamRankings []TeamScoreboardEntry `json:"teamRankings,omitempty"`
	Teams        []Team                `json:"teams,omitempty"`
	Rounds       []ReplayRound         `json:"rounds"`
}
