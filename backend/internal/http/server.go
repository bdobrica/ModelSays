package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/config"
	"github.com/bogdandobrica/modelsays/backend/internal/game"
	"github.com/bogdandobrica/modelsays/backend/internal/models"
)

const maxRequestBodyBytes = 16 * 1024

type Server struct {
	config      config.Config
	logger      *slog.Logger
	roomService *game.RoomService
	mux         *http.ServeMux
}

type createRoomRequest struct {
	RoomName        string              `json:"roomName"`
	HostDisplayName string              `json:"hostDisplayName"`
	Settings        models.RoomSettings `json:"settings"`
}

type joinRoomRequest struct {
	DisplayName string `json:"displayName"`
}

type recoverSessionRequest struct {
	PlayerToken string `json:"playerToken"`
}

type startGameRequest struct {
	PlayerToken string `json:"playerToken"`
}

type submitGuessRequest struct {
	PlayerToken string `json:"playerToken"`
	Answer      string `json:"answer"`
}

type revealRoundRequest struct {
	PlayerToken string `json:"playerToken"`
}

type nextRoundRequest struct {
	PlayerToken string `json:"playerToken"`
}

type overrideMatchRequest struct {
	PlayerToken               string  `json:"playerToken"`
	RoundID                   string  `json:"roundId"`
	GuessID                   string  `json:"guessId"`
	MatchedPredictionAnswerID *string `json:"matchedPredictionAnswerId"`
}

type roomResponse struct {
	Room   publicRoom     `json:"room"`
	Player *models.Player `json:"player,omitempty"`
}

type publicRoom struct {
	Code        string              `json:"code"`
	Name        string              `json:"name"`
	Status      models.RoomStatus   `json:"status"`
	Settings    models.RoomSettings `json:"settings"`
	Players     []publicPlayer      `json:"players"`
	CurrentGame *publicGame         `json:"currentGame,omitempty"`
	CreatedAt   time.Time           `json:"createdAt"`
	UpdatedAt   time.Time           `json:"updatedAt"`
}

type publicPlayer struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	IsHost      bool      `json:"isHost"`
	JoinedAt    time.Time `json:"joinedAt"`
}

type publicGame struct {
	ID                string                   `json:"id"`
	Status            models.GameStatus        `json:"status"`
	Mode              models.GameMode          `json:"mode"`
	TotalRounds       int                      `json:"totalRounds"`
	CurrentRoundIndex int                      `json:"currentRoundIndex"`
	CurrentRound      *publicRound             `json:"currentRound,omitempty"`
	Scoreboard        []models.ScoreboardEntry `json:"scoreboard,omitempty"`
	CreatedAt         time.Time                `json:"createdAt"`
	StartedAt         time.Time                `json:"startedAt"`
	EndedAt           *time.Time               `json:"endedAt,omitempty"`
}

type publicRound struct {
	ID                   string                  `json:"id"`
	RoundIndex           int                     `json:"roundIndex"`
	Status               models.RoundStatus      `json:"status"`
	Question             models.Question         `json:"question"`
	BoardHash            string                  `json:"boardHash"`
	Board                *models.PredictionBoard `json:"board,omitempty"`
	Guesses              []models.Guess          `json:"guesses,omitempty"`
	AnswerPhaseStartedAt time.Time               `json:"answerPhaseStartedAt"`
	AnswerPhaseEndsAt    time.Time               `json:"answerPhaseEndsAt"`
	RevealStartedAt      *time.Time              `json:"revealStartedAt,omitempty"`
	CreatedAt            time.Time               `json:"createdAt"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewServer(cfg config.Config, logger *slog.Logger, roomService *game.RoomService) *Server {
	server := &Server{
		config:      cfg,
		logger:      logger,
		roomService: roomService,
		mux:         http.NewServeMux(),
	}

	server.routes()
	return server
}

func (server *Server) Handler() http.Handler {
	return server.withCORS(server.mux)
}

func (server *Server) routes() {
	server.mux.HandleFunc("GET /healthz", server.handleHealth)
	server.mux.HandleFunc("POST /api/rooms", server.handleCreateRoom)
	server.mux.HandleFunc("GET /api/rooms/", server.handleRoomRoutes)
	server.mux.HandleFunc("POST /api/rooms/", server.handleRoomRoutes)
}

func (server *Server) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) handleCreateRoom(writer http.ResponseWriter, request *http.Request) {
	var payload createRoomRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	room, host, err := server.roomService.CreateRoom(request.Context(), game.CreateRoomInput{
		RoomName:        payload.RoomName,
		HostDisplayName: payload.HostDisplayName,
		Settings:        payload.Settings,
	})
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}

	writeJSON(writer, http.StatusCreated, roomResponse{Room: projectRoom(room), Player: &host})
}

func (server *Server) handleRoomRoutes(writer http.ResponseWriter, request *http.Request) {
	trimmed := strings.TrimPrefix(request.URL.Path, "/api/rooms/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(writer, http.StatusNotFound, "room route not found")
		return
	}

	code := parts[0]
	switch {
	case request.Method == http.MethodGet && len(parts) == 1:
		server.handleGetRoom(writer, request, code)
	case request.Method == http.MethodGet && len(parts) == 2 && parts[1] == "state":
		server.handleGetRoom(writer, request, code)
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "join":
		server.handleJoinRoom(writer, request, code)
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "session":
		server.handleRecoverSession(writer, request, code)
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "start":
		server.handleStartGame(writer, request, code)
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "next-round":
		server.handleNextRound(writer, request, code)
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "override-match":
		server.handleOverrideMatch(writer, request, code)
	case request.Method == http.MethodPost && len(parts) == 4 && parts[1] == "rounds" && parts[3] == "guesses":
		server.handleSubmitGuess(writer, request, code, parts[2])
	case request.Method == http.MethodPost && len(parts) == 4 && parts[1] == "rounds" && parts[3] == "reveal":
		server.handleRevealRound(writer, request, code, parts[2])
	default:
		writeError(writer, http.StatusNotFound, "room route not found")
	}
}

func (server *Server) handleRecoverSession(writer http.ResponseWriter, request *http.Request, code string) {
	var payload recoverSessionRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	room, player, err := server.roomService.RecoverSession(request.Context(), game.RecoverSessionInput{
		Code:        code,
		PlayerToken: payload.PlayerToken,
	})
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, roomResponse{Room: projectRoom(room), Player: &player})
}

func (server *Server) handleGetRoom(writer http.ResponseWriter, request *http.Request, code string) {
	room, err := server.roomService.GetRoom(request.Context(), code)
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, roomResponse{Room: projectRoom(room)})
}

func (server *Server) handleJoinRoom(writer http.ResponseWriter, request *http.Request, code string) {
	var payload joinRoomRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	room, player, err := server.roomService.JoinRoom(request.Context(), game.JoinRoomInput{
		Code:        code,
		DisplayName: payload.DisplayName,
	})
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}

	writeJSON(writer, http.StatusCreated, roomResponse{Room: projectRoom(room), Player: &player})
}

func (server *Server) handleStartGame(writer http.ResponseWriter, request *http.Request, code string) {
	var payload startGameRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	room, err := server.roomService.StartGame(request.Context(), game.StartGameInput{
		Code:        code,
		PlayerToken: payload.PlayerToken,
	})
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, roomResponse{Room: projectRoom(room)})
}

func (server *Server) handleSubmitGuess(writer http.ResponseWriter, request *http.Request, code string, roundID string) {
	var payload submitGuessRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	room, err := server.roomService.SubmitGuess(request.Context(), game.SubmitGuessInput{
		Code:        code,
		RoundID:     roundID,
		PlayerToken: payload.PlayerToken,
		Answer:      payload.Answer,
	})
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}

	writeJSON(writer, http.StatusCreated, roomResponse{Room: projectRoom(room)})
}

func (server *Server) handleRevealRound(writer http.ResponseWriter, request *http.Request, code string, roundID string) {
	var payload revealRoundRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	room, err := server.roomService.RevealRound(request.Context(), game.RevealRoundInput{
		Code:        code,
		RoundID:     roundID,
		PlayerToken: payload.PlayerToken,
	})
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, roomResponse{Room: projectRoom(room)})
}

func (server *Server) handleNextRound(writer http.ResponseWriter, request *http.Request, code string) {
	var payload nextRoundRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	room, err := server.roomService.NextRound(request.Context(), game.NextRoundInput{
		Code:        code,
		PlayerToken: payload.PlayerToken,
	})
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, roomResponse{Room: projectRoom(room)})
}

func (server *Server) handleOverrideMatch(writer http.ResponseWriter, request *http.Request, code string) {
	var payload overrideMatchRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	room, err := server.roomService.OverrideMatch(request.Context(), game.OverrideMatchInput{
		Code:                      code,
		RoundID:                   payload.RoundID,
		GuessID:                   payload.GuessID,
		PlayerToken:               payload.PlayerToken,
		MatchedPredictionAnswerID: payload.MatchedPredictionAnswerID,
	})
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, roomResponse{Room: projectRoom(room)})
}

func (server *Server) writeDomainError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, game.ErrRoomNotFound):
		writeError(writer, http.StatusNotFound, err.Error())
	case errors.Is(err, game.ErrRoundNotFound):
		writeError(writer, http.StatusNotFound, err.Error())
	case errors.Is(err, game.ErrUnauthorizedStart):
		writeError(writer, http.StatusForbidden, err.Error())
	case errors.Is(err, game.ErrUnauthorizedReveal), errors.Is(err, game.ErrUnauthorizedAdvance), errors.Is(err, game.ErrUnauthorizedOverride), errors.Is(err, game.ErrPlayerNotFound):
		writeError(writer, http.StatusForbidden, err.Error())
	case errors.Is(err, game.ErrGameAlreadyStarted), errors.Is(err, game.ErrGameAlreadyCompleted), errors.Is(err, game.ErrRoomJoinClosed):
		writeError(writer, http.StatusConflict, err.Error())
	case errors.Is(err, game.ErrRoundNotAcceptingGuesses), errors.Is(err, game.ErrAnswerPhaseExpired), errors.Is(err, game.ErrRoundAlreadyRevealed), errors.Is(err, game.ErrRoundNotRevealed), errors.Is(err, game.ErrGuessAlreadySubmitted):
		writeError(writer, http.StatusConflict, err.Error())
	case errors.Is(err, game.ErrContentUnavailable):
		writeError(writer, http.StatusServiceUnavailable, game.ErrContentUnavailable.Error())
	case errors.Is(err, game.ErrDisplayNameInvalid), errors.Is(err, game.ErrRoomNameInvalid), errors.Is(err, game.ErrRoomCodeInvalid), errors.Is(err, game.ErrRoomSettingsInvalid), errors.Is(err, game.ErrDuplicatePlayer), errors.Is(err, game.ErrAnswerInvalid), errors.Is(err, game.ErrPredictionAnswerNotFound), errors.Is(err, game.ErrGuessNotFound):
		writeError(writer, http.StatusBadRequest, err.Error())
	default:
		server.logger.Error("unexpected request failure", "error", err)
		writeError(writer, http.StatusInternalServerError, "internal server error")
	}
}

func decodeJSONBody(writer http.ResponseWriter, request *http.Request, destination any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func (server *Server) withCORS(next http.Handler) http.Handler {
	allowedOrigins := make(map[string]struct{}, len(server.config.CORSAllowedOrigins))
	for _, origin := range server.config.CORSAllowedOrigins {
		allowedOrigins[origin] = struct{}{}
	}

	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		origin := request.Header.Get("Origin")
		if origin != "" {
			if _, ok := allowedOrigins[origin]; ok {
				writer.Header().Set("Access-Control-Allow-Origin", origin)
				writer.Header().Set("Vary", "Origin")
			}
			writer.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			writer.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		}

		if request.Method == http.MethodOptions {
			writer.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(writer, request)
	})
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, errorResponse{Error: message})
}

func projectRoom(room models.Room) publicRoom {
	projected := publicRoom{
		Code:      room.Code,
		Name:      room.Name,
		Status:    room.Status,
		Settings:  room.Settings,
		Players:   make([]publicPlayer, 0, len(room.Players)),
		CreatedAt: room.CreatedAt,
		UpdatedAt: room.UpdatedAt,
	}
	for _, player := range room.Players {
		projected.Players = append(projected.Players, publicPlayer{
			ID:          player.ID,
			DisplayName: player.DisplayName,
			IsHost:      player.IsHost,
			JoinedAt:    player.JoinedAt,
		})
	}

	if room.CurrentGame == nil {
		return projected
	}

	gameState := room.CurrentGame
	projectedGame := &publicGame{
		ID:                gameState.ID,
		Status:            gameState.Status,
		Mode:              gameState.Mode,
		TotalRounds:       gameState.TotalRounds,
		CurrentRoundIndex: gameState.CurrentRoundIndex,
		Scoreboard:        append([]models.ScoreboardEntry(nil), gameState.Scoreboard...),
		CreatedAt:         gameState.CreatedAt,
		StartedAt:         gameState.StartedAt,
		EndedAt:           gameState.EndedAt,
	}
	projected.CurrentGame = projectedGame
	if gameState.CurrentRound == nil {
		return projected
	}

	round := gameState.CurrentRound
	projectedGame.CurrentRound = &publicRound{
		ID:                   round.ID,
		RoundIndex:           round.RoundIndex,
		Status:               round.Status,
		Question:             round.Question,
		BoardHash:            round.BoardHash,
		AnswerPhaseStartedAt: round.AnswerPhaseStartedAt,
		AnswerPhaseEndsAt:    round.AnswerPhaseEndsAt,
		RevealStartedAt:      round.RevealStartedAt,
		CreatedAt:            round.CreatedAt,
	}
	if round.Status == models.RoundStatusRevealed {
		projectedGame.CurrentRound.Board = round.Board
		projectedGame.CurrentRound.Guesses = round.Guesses
		return projected
	}

	currentRoundScores := make(map[string]int, len(round.Guesses))
	for _, guess := range round.Guesses {
		currentRoundScores[guess.PlayerID] += guess.ScoreAwarded
	}
	for index := range projectedGame.Scoreboard {
		projectedGame.Scoreboard[index].Score -= currentRoundScores[projectedGame.Scoreboard[index].PlayerID]
	}

	return projected
}
