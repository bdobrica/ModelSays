package httpapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/config"
	"github.com/bogdandobrica/modelsays/backend/internal/game"
	"github.com/bogdandobrica/modelsays/backend/internal/models"
	"github.com/bogdandobrica/modelsays/backend/internal/ops"
	"github.com/bogdandobrica/modelsays/backend/internal/security"
)

const maxRequestBodyBytes = 16 * 1024

type Server struct {
	config                 config.Config
	logger                 *slog.Logger
	roomService            *game.RoomService
	mux                    *http.ServeMux
	activeEventConnections atomic.Int64
	readinessCheck         func() error
	abuse                  *security.Controller
	metrics                *ops.Metrics
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

type passTurnRequest struct {
	PlayerToken string `json:"playerToken"`
}

type nextRoundRequest struct {
	PlayerToken string `json:"playerToken"`
}

type playAgainRequest struct {
	PlayerToken string `json:"playerToken"`
}

type createTeamRequest struct {
	PlayerToken string `json:"playerToken"`
	Name        string `json:"name"`
}

type assignTeamRequest struct {
	PlayerToken string `json:"playerToken"`
	TeamID      string `json:"teamId"`
}

type overrideMatchRequest struct {
	PlayerToken               string  `json:"playerToken"`
	RoundID                   string  `json:"roundId"`
	GuessID                   string  `json:"guessId"`
	MatchedPredictionAnswerID *string `json:"matchedPredictionAnswerId"`
	JudgeSuggestionID         string  `json:"judgeSuggestionId"`
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
	Teams       []models.Team       `json:"teams,omitempty"`
	CurrentGame *publicGame         `json:"currentGame,omitempty"`
	CreatedAt   time.Time           `json:"createdAt"`
	UpdatedAt   time.Time           `json:"updatedAt"`
	Revision    int64               `json:"revision"`
}

type publicPlayer struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	IsHost      bool      `json:"isHost"`
	JoinedAt    time.Time `json:"joinedAt"`
	TeamID      string    `json:"teamId,omitempty"`
}

type publicGame struct {
	ID                string                       `json:"id"`
	ReplayID          string                       `json:"replayId,omitempty"`
	Status            models.GameStatus            `json:"status"`
	Mode              models.GameMode              `json:"mode"`
	TotalRounds       int                          `json:"totalRounds"`
	CurrentRoundIndex int                          `json:"currentRoundIndex"`
	CurrentRound      *publicRound                 `json:"currentRound,omitempty"`
	Scoreboard        []models.ScoreboardEntry     `json:"scoreboard,omitempty"`
	TeamScoreboard    []models.TeamScoreboardEntry `json:"teamScoreboard,omitempty"`
	CreatedAt         time.Time                    `json:"createdAt"`
	StartedAt         time.Time                    `json:"startedAt"`
	EndedAt           *time.Time                   `json:"endedAt,omitempty"`
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
	TurnOrder            []string                `json:"turnOrder,omitempty"`
	CurrentTurnIndex     *int                    `json:"currentTurnIndex,omitempty"`
	TurnEndsAt           *time.Time              `json:"turnEndsAt,omitempty"`
	CreatedAt            time.Time               `json:"createdAt"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func NewServer(cfg config.Config, logger *slog.Logger, roomService *game.RoomService) *Server {
	if cfg.EventPollInterval <= 0 {
		cfg.EventPollInterval = 250 * time.Millisecond
	}
	if cfg.EventHeartbeat <= 0 {
		cfg.EventHeartbeat = 15 * time.Second
	}
	if cfg.EventMaxConnections <= 0 {
		cfg.EventMaxConnections = 100
	}
	if cfg.EventWriteTimeout <= 0 {
		cfg.EventWriteTimeout = 5 * time.Second
	}
	server := &Server{
		config:      cfg,
		logger:      logger,
		roomService: roomService,
		mux:         http.NewServeMux(),
		abuse:       security.NewController(cfg.Abuse),
		metrics:     ops.NewMetrics(),
	}
	server.abuse.SetObserver(func(scope string, allowed bool) {
		decision := "allowed"
		if !allowed {
			decision = "rejected"
			logger.Warn("rate limit decision", "scope", scope, "decision", decision)
		}
		server.metrics.Inc("modelsays_limiter_decisions_total", "scope", scope, "decision", decision)
	})

	server.routes()
	return server
}

func (server *Server) Handler() http.Handler {
	return server.withObservability(server.withCORS(server.withAbuseLimits(server.mux)))
}

func (server *Server) AbuseController() *security.Controller { return server.abuse }
func (server *Server) Metrics() *ops.Metrics                 { return server.metrics }

func (server *Server) SetReadinessCheck(check func() error) {
	server.readinessCheck = check
}

func (server *Server) routes() {
	server.mux.HandleFunc("GET /healthz", server.handleHealth)
	server.mux.HandleFunc("GET /readyz", server.handleReady)
	server.mux.HandleFunc("GET /metrics", server.handleMetrics)
	server.mux.HandleFunc("POST /api/rooms", server.handleCreateRoom)
	server.mux.HandleFunc("GET /api/replays/", server.handleReplay)
	server.mux.HandleFunc("GET /api/rooms/", server.handleRoomRoutes)
	server.mux.HandleFunc("POST /api/rooms/", server.handleRoomRoutes)
}

func (server *Server) handleMetrics(writer http.ResponseWriter, request *http.Request) {
	if server.config.MetricsToken != "" && request.Header.Get("Authorization") != "Bearer "+server.config.MetricsToken {
		writeError(writer, http.StatusUnauthorized, "metrics authorization required")
		return
	}
	writer.Header().Set("Content-Type", "text/plain; version=0.0.4")
	server.metrics.Set("modelsays_event_connections", float64(server.activeEventConnections.Load()))
	server.metrics.WritePrometheus(writer)
}

func (server *Server) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
}

func (server *Server) handleReady(writer http.ResponseWriter, _ *http.Request) {
	if server.readinessCheck != nil {
		if err := server.readinessCheck(); err != nil {
			writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
			return
		}
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ready"})
}

func (server *Server) handleCreateRoom(writer http.ResponseWriter, request *http.Request) {
	var payload createRoomRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	if server.abuse.Moderated(payload.RoomName) || server.abuse.Moderated(payload.HostDisplayName) {
		writeError(writer, http.StatusUnprocessableEntity, "room and display names must be party-safe")
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
	case request.Method == http.MethodGet && len(parts) == 2 && parts[1] == "events":
		server.handleRoomEvents(writer, request, code)
	case request.Method == http.MethodGet && len(parts) == 2 && parts[1] == "provider-audits":
		server.handleProviderAudits(writer, request, code)
	case request.Method == http.MethodGet && len(parts) == 4 && parts[1] == "rounds" && parts[3] == "judge-suggestions":
		server.handleJudgeSuggestions(writer, request, code, parts[2])
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "join":
		server.handleJoinRoom(writer, request, code)
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "session":
		server.handleRecoverSession(writer, request, code)
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "start":
		server.handleStartGame(writer, request, code)
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "next-round":
		server.handleNextRound(writer, request, code)
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "play-again":
		server.handlePlayAgain(writer, request, code)
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "teams":
		server.handleCreateTeam(writer, request, code)
	case request.Method == http.MethodPost && len(parts) == 4 && parts[1] == "players" && parts[3] == "team":
		server.handleAssignTeam(writer, request, code, parts[2])
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "override-match":
		server.handleOverrideMatch(writer, request, code)
	case request.Method == http.MethodPost && len(parts) == 4 && parts[1] == "rounds" && parts[3] == "guesses":
		server.handleSubmitGuess(writer, request, code, parts[2])
	case request.Method == http.MethodPost && len(parts) == 4 && parts[1] == "rounds" && parts[3] == "reveal":
		server.handleRevealRound(writer, request, code, parts[2])
	case request.Method == http.MethodPost && len(parts) == 4 && parts[1] == "rounds" && parts[3] == "pass":
		server.handlePassTurn(writer, request, code, parts[2])
	default:
		writeError(writer, http.StatusNotFound, "room route not found")
	}
}

func (server *Server) handlePassTurn(writer http.ResponseWriter, request *http.Request, code, roundID string) {
	var payload passTurnRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	room, err := server.roomService.PassTurn(request.Context(), game.PassTurnInput{Code: code, RoundID: roundID, PlayerToken: payload.PlayerToken})
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, roomResponse{Room: projectRoom(room)})
}

func (server *Server) handleCreateTeam(writer http.ResponseWriter, request *http.Request, code string) {
	var payload createTeamRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	room, err := server.roomService.CreateTeam(request.Context(), game.CreateTeamInput{Code: code, PlayerToken: payload.PlayerToken, Name: payload.Name})
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, roomResponse{Room: projectRoom(room)})
}

func (server *Server) handleAssignTeam(writer http.ResponseWriter, request *http.Request, code, playerID string) {
	var payload assignTeamRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	room, err := server.roomService.AssignTeam(request.Context(), game.AssignTeamInput{Code: code, PlayerToken: payload.PlayerToken, PlayerID: playerID, TeamID: payload.TeamID})
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, roomResponse{Room: projectRoom(room)})
}

func (server *Server) handleReplay(writer http.ResponseWriter, request *http.Request) {
	replayID := strings.Trim(strings.TrimPrefix(request.URL.Path, "/api/replays/"), "/")
	if replayID == "" || strings.Contains(replayID, "/") {
		writeError(writer, http.StatusNotFound, game.ErrReplayNotFound.Error())
		return
	}
	replay, err := server.roomService.GetReplay(request.Context(), replayID)
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"replay": replay})
}

func (server *Server) handleRoomEvents(writer http.ResponseWriter, request *http.Request, code string) {
	if request.URL.Query().Has("token") || request.URL.Query().Has("playerToken") {
		writeError(writer, http.StatusBadRequest, "event credentials must not be sent in the URL")
		return
	}
	if !server.isAllowedOrigin(request.Header.Get("Origin")) {
		writeError(writer, http.StatusForbidden, "origin is not allowed")
		return
	}
	if !server.allowPlayerAction(writer, "event", code, request.Header.Get("X-Player-Token"), server.abuse.Config().EventIP) {
		return
	}
	if server.activeEventConnections.Add(1) > int64(server.config.EventMaxConnections) {
		server.activeEventConnections.Add(-1)
		writer.Header().Set("Retry-After", "5")
		writeError(writer, http.StatusServiceUnavailable, "event connection limit reached")
		return
	}
	defer server.activeEventConnections.Add(-1)
	server.metrics.Inc("modelsays_event_subscriptions_total", "resume", strconv.FormatBool(request.Header.Get("Last-Event-ID") != ""))

	if err := server.roomService.AuthenticateEventSubscription(request.Context(), code, request.Header.Get("X-Player-Token")); err != nil {
		server.writeDomainError(writer, err)
		return
	}
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeError(writer, http.StatusInternalServerError, "streaming is unavailable")
		return
	}
	afterRevision := int64(0)
	if value := strings.TrimSpace(request.Header.Get("Last-Event-ID")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed < 0 {
			writeError(writer, http.StatusBadRequest, "Last-Event-ID must be a non-negative room revision")
			return
		}
		afterRevision = parsed
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache, no-store")
	writer.Header().Set("Connection", "keep-alive")
	writer.Header().Set("X-Accel-Buffering", "no")
	writer.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(writer, ": connected\n\n")
	flusher.Flush()

	pollTicker := time.NewTicker(server.config.EventPollInterval)
	heartbeatTicker := time.NewTicker(server.config.EventHeartbeat)
	defer pollTicker.Stop()
	defer heartbeatTicker.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case <-heartbeatTicker.C:
			_ = http.NewResponseController(writer).SetWriteDeadline(time.Now().Add(server.config.EventWriteTimeout))
			if _, err := io.WriteString(writer, ": heartbeat\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-pollTicker.C:
			events, err := server.roomService.ListRoomEvents(request.Context(), code, afterRevision, 100)
			if err != nil {
				return
			}
			for _, event := range events {
				payload, err := json.Marshal(event)
				if err != nil {
					return
				}
				_ = http.NewResponseController(writer).SetWriteDeadline(time.Now().Add(server.config.EventWriteTimeout))
				if _, err := fmt.Fprintf(writer, "id: %d\nevent: room_invalidation\ndata: %s\n\n", event.RoomRevision, payload); err != nil {
					return
				}
				afterRevision = event.RoomRevision
			}
			if len(events) > 0 {
				flusher.Flush()
			}
		}
	}
}

type responseRecorder struct {
	http.ResponseWriter
	status int
}

func (recorder *responseRecorder) WriteHeader(status int) {
	if recorder.status != 0 {
		return
	}
	recorder.status = status
	recorder.ResponseWriter.WriteHeader(status)
}

func (recorder *responseRecorder) Write(body []byte) (int, error) {
	if recorder.status == 0 {
		recorder.WriteHeader(http.StatusOK)
	}
	return recorder.ResponseWriter.Write(body)
}

func (recorder *responseRecorder) Unwrap() http.ResponseWriter { return recorder.ResponseWriter }
func (recorder *responseRecorder) Flush() {
	_ = http.NewResponseController(recorder.ResponseWriter).Flush()
}

func (server *Server) withObservability(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requestID := strings.TrimSpace(request.Header.Get("X-Request-ID"))
		if !validRequestID(requestID) {
			var value [16]byte
			_, _ = rand.Read(value[:])
			requestID = hex.EncodeToString(value[:])
		}
		writer.Header().Set("X-Request-ID", requestID)
		recorder := &responseRecorder{ResponseWriter: writer}
		started := time.Now()
		next.ServeHTTP(recorder, request)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		route := metricRoute(request)
		statusClass := strconv.Itoa(status/100) + "xx"
		elapsed := time.Since(started)
		server.metrics.Inc("modelsays_http_requests_total", "method", request.Method, "route", route, "status_class", statusClass)
		server.metrics.Observe("modelsays_http_request_duration_seconds", elapsed.Seconds(), ops.HTTPDurationBuckets, "route", route)
		server.logger.Info("http request", "request_id", requestID, "method", request.Method, "route", route, "status", status, "duration_ms", elapsed.Milliseconds())
	})
}

func validRequestID(value string) bool {
	if len(value) < 8 || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') &&
			(character < '0' || character > '9') && character != '-' && character != '_' {
			return false
		}
	}
	return true
}

func metricRoute(request *http.Request) string {
	switch {
	case request.URL.Path == "/healthz":
		return "health"
	case request.URL.Path == "/readyz":
		return "ready"
	case request.URL.Path == "/metrics":
		return "metrics"
	case request.URL.Path == "/api/rooms" && request.Method == http.MethodPost:
		return "room_create"
	case strings.HasPrefix(request.URL.Path, "/api/replays/"):
		return "replay"
	case strings.HasSuffix(request.URL.Path, "/events"):
		return "room_events"
	case strings.Contains(request.URL.Path, "/rounds/"):
		return "round_action"
	case strings.HasPrefix(request.URL.Path, "/api/rooms/"):
		return "room"
	default:
		return "not_found"
	}
}

func (server *Server) handleJudgeSuggestions(writer http.ResponseWriter, request *http.Request, code string, roundID string) {
	suggestions, err := server.roomService.GetJudgeSuggestions(request.Context(), code, roundID, request.Header.Get("X-Player-Token"))
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"suggestions": suggestions})
}

func (server *Server) handleProviderAudits(writer http.ResponseWriter, request *http.Request, code string) {
	audits, err := server.roomService.GetProviderAudits(request.Context(), code, request.Header.Get("X-Player-Token"))
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"audits": audits})
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
	if server.abuse.Moderated(payload.DisplayName) {
		writeError(writer, http.StatusUnprocessableEntity, "display name must be party-safe")
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
	if !server.allowPlayerAction(writer, "start", code, payload.PlayerToken, server.abuse.Config().PlayerAction) {
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
	if server.abuse.Moderated(payload.Answer) {
		writeError(writer, http.StatusUnprocessableEntity, "answer must be party-safe")
		return
	}
	if !server.allowPlayerAction(writer, "guess", code, payload.PlayerToken, server.abuse.Config().GuessPlayer) {
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
	if !server.allowPlayerAction(writer, "reveal", code, payload.PlayerToken, server.abuse.Config().PlayerAction) {
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
	if !server.allowPlayerAction(writer, "next-round", code, payload.PlayerToken, server.abuse.Config().PlayerAction) {
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

func (server *Server) handlePlayAgain(writer http.ResponseWriter, request *http.Request, code string) {
	var payload playAgainRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	if !server.allowPlayerAction(writer, "play-again", code, payload.PlayerToken, server.abuse.Config().PlayerAction) {
		return
	}
	room, player, err := server.roomService.PlayAgain(request.Context(), game.PlayAgainInput{
		Code: code, PlayerToken: payload.PlayerToken,
	})
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}
	writeJSON(writer, http.StatusCreated, roomResponse{Room: projectRoom(room), Player: &player})
}

func (server *Server) handleOverrideMatch(writer http.ResponseWriter, request *http.Request, code string) {
	var payload overrideMatchRequest
	if err := decodeJSONBody(writer, request, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "invalid JSON payload")
		return
	}
	if !server.allowPlayerAction(writer, "override", code, payload.PlayerToken, server.abuse.Config().PlayerAction) {
		return
	}

	room, err := server.roomService.OverrideMatch(request.Context(), game.OverrideMatchInput{
		Code:                      code,
		RoundID:                   payload.RoundID,
		GuessID:                   payload.GuessID,
		PlayerToken:               payload.PlayerToken,
		MatchedPredictionAnswerID: payload.MatchedPredictionAnswerID,
		JudgeSuggestionID:         payload.JudgeSuggestionID,
	})
	if err != nil {
		server.writeDomainError(writer, err)
		return
	}

	writeJSON(writer, http.StatusOK, roomResponse{Room: projectRoom(room)})
}

func (server *Server) allowPlayerAction(writer http.ResponseWriter, action, room, token string, policy security.Policy) bool {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	roomKey := strings.ToUpper(strings.TrimSpace(room))
	key := roomKey + ":" + hex.EncodeToString(sum[:16])
	if allowed, retry := server.abuse.Allow("room-"+action, roomKey, server.abuse.Config().PlayerAction); !allowed {
		writeRateLimit(writer, "room_action", retry)
		return false
	}
	allowed, retry := server.abuse.Allow("player-"+action, key, policy)
	if !allowed {
		writeRateLimit(writer, "player_action", retry)
	}
	return allowed
}

func (server *Server) withAbuseLimits(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/healthz" || request.URL.Path == "/readyz" || request.Method == http.MethodOptions {
			next.ServeHTTP(writer, request)
			return
		}
		client := server.abuse.ClientKey(request)
		cfg := server.abuse.Config()
		if allowed, retry := server.abuse.Allow("ip", client, cfg.IP); !allowed {
			writeRateLimit(writer, "client", retry)
			return
		}
		action, room, policy := requestLimit(request, cfg)
		if action != "" {
			key := client
			if room != "" && action != "join-ip" {
				key = strings.ToUpper(room)
			}
			if allowed, retry := server.abuse.Allow(action, key, policy); !allowed {
				writeRateLimit(writer, action, retry)
				return
			}
			if action == "join-ip" {
				if allowed, retry := server.abuse.Allow("join-room", strings.ToUpper(room), cfg.JoinRoom); !allowed {
					writeRateLimit(writer, "join-room", retry)
					return
				}
			}
			if action == "event-room" {
				if allowed, retry := server.abuse.Allow("event-ip", client, cfg.EventIP); !allowed {
					writeRateLimit(writer, "event-ip", retry)
					return
				}
			}
		}
		next.ServeHTTP(writer, request)
	})
}

func requestLimit(request *http.Request, cfg security.Config) (string, string, security.Policy) {
	if request.Method == http.MethodPost && request.URL.Path == "/api/rooms" {
		return "create", "", cfg.Create
	}
	trimmed := strings.Trim(strings.TrimPrefix(request.URL.Path, "/api/rooms/"), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 || parts[0] == "" {
		return "", "", security.Policy{}
	}
	room := parts[0]
	switch {
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "join":
		return "join-ip", room, cfg.JoinIP
	case request.Method == http.MethodPost && len(parts) == 2 && parts[1] == "session":
		return "session-ip", "", cfg.Lookup
	case request.Method == http.MethodGet && len(parts) == 2 && parts[1] == "events":
		return "event-room", room, cfg.EventRoom
	case request.Method == http.MethodPost && len(parts) == 4 && parts[3] == "guesses":
		return "guess-room", room, cfg.GuessRoom
	case request.Method == http.MethodGet:
		return "lookup", "", cfg.Lookup
	default:
		return "", "", security.Policy{}
	}
}

func (server *Server) writeDomainError(writer http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, game.ErrRoomNotFound):
		writeError(writer, http.StatusNotFound, err.Error())
	case errors.Is(err, game.ErrReplayNotFound):
		writeError(writer, http.StatusNotFound, err.Error())
	case errors.Is(err, game.ErrRoundNotFound):
		writeError(writer, http.StatusNotFound, err.Error())
	case errors.Is(err, game.ErrUnauthorizedStart):
		writeError(writer, http.StatusForbidden, err.Error())
	case errors.Is(err, game.ErrUnauthorizedReveal), errors.Is(err, game.ErrUnauthorizedAdvance), errors.Is(err, game.ErrUnauthorizedOverride), errors.Is(err, game.ErrUnauthorizedAudit), errors.Is(err, game.ErrUnauthorizedJudgeReview), errors.Is(err, game.ErrUnauthorizedPlayAgain), errors.Is(err, game.ErrUnauthorizedTeams), errors.Is(err, game.ErrPlayerNotFound):
		writeError(writer, http.StatusForbidden, err.Error())
	case errors.Is(err, game.ErrTeamNotFound):
		writeError(writer, http.StatusNotFound, err.Error())
	case errors.Is(err, game.ErrGameAlreadyStarted), errors.Is(err, game.ErrGameAlreadyCompleted), errors.Is(err, game.ErrRoomJoinClosed):
		writeError(writer, http.StatusConflict, err.Error())
	case errors.Is(err, game.ErrRoundNotAcceptingGuesses), errors.Is(err, game.ErrAnswerPhaseExpired), errors.Is(err, game.ErrRoundAlreadyRevealed), errors.Is(err, game.ErrRoundNotRevealed), errors.Is(err, game.ErrGuessAlreadySubmitted), errors.Is(err, game.ErrReplayNotReady), errors.Is(err, game.ErrNotPlayersTurn):
		writeError(writer, http.StatusConflict, err.Error())
	case errors.Is(err, game.ErrContentUnavailable):
		writeError(writer, http.StatusServiceUnavailable, game.ErrContentUnavailable.Error())
	case errors.Is(err, game.ErrDisplayNameInvalid), errors.Is(err, game.ErrRoomNameInvalid), errors.Is(err, game.ErrRoomCodeInvalid), errors.Is(err, game.ErrRoomSettingsInvalid), errors.Is(err, game.ErrDuplicatePlayer), errors.Is(err, game.ErrAnswerInvalid), errors.Is(err, game.ErrPredictionAnswerNotFound), errors.Is(err, game.ErrGuessNotFound), errors.Is(err, game.ErrTeamNameInvalid), errors.Is(err, game.ErrTeamConfigurationInvalid):
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
			writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Player-Token")
			writer.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		}

		if request.Method == http.MethodOptions {
			writer.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(writer, request)
	})
}

func (server *Server) isAllowedOrigin(origin string) bool {
	if origin == "" {
		return true
	}
	for _, allowed := range server.config.CORSAllowedOrigins {
		if origin == allowed {
			return true
		}
	}
	return false
}

func writeJSON(writer http.ResponseWriter, status int, payload any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(payload)
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, errorResponse{Error: message})
}

func writeRateLimit(writer http.ResponseWriter, scope string, retry time.Duration) {
	seconds := int((retry + time.Second - 1) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	writer.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSON(writer, http.StatusTooManyRequests, map[string]any{
		"error": "rate limit exceeded", "code": "rate_limited",
		"scope": scope, "retryAfterSeconds": seconds,
	})
}

func projectRoom(room models.Room) publicRoom {
	projected := publicRoom{
		Code:      room.Code,
		Name:      room.Name,
		Status:    room.Status,
		Settings:  room.Settings,
		Players:   make([]publicPlayer, 0, len(room.Players)),
		Teams:     append([]models.Team(nil), room.Teams...),
		CreatedAt: room.CreatedAt,
		UpdatedAt: room.UpdatedAt,
		Revision:  room.Revision,
	}
	for _, player := range room.Players {
		projected.Players = append(projected.Players, publicPlayer{
			ID:          player.ID,
			DisplayName: player.DisplayName,
			IsHost:      player.IsHost,
			JoinedAt:    player.JoinedAt,
			TeamID:      player.TeamID,
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
		TeamScoreboard:    append([]models.TeamScoreboardEntry(nil), gameState.TeamScoreboard...),
		CreatedAt:         gameState.CreatedAt,
		StartedAt:         gameState.StartedAt,
		EndedAt:           gameState.EndedAt,
	}
	if gameState.Status == models.GameStatusCompleted {
		projectedGame.ReplayID = gameState.ReplayID
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
		TurnOrder:            append([]string(nil), round.TurnOrder...),
		CurrentTurnIndex:     round.CurrentTurnIndex,
		TurnEndsAt:           round.TurnEndsAt,
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
	projectedGame.TeamScoreboard = projectTeamScores(room.Teams, room.Players, projectedGame.Scoreboard)
	if gameState.Mode == models.GameModeSequential {
		projectedGame.CurrentRound.Guesses = make([]models.Guess, 0, len(round.Guesses))
		for _, guess := range round.Guesses {
			projectedGame.CurrentRound.Guesses = append(projectedGame.CurrentRound.Guesses, models.Guess{
				ID: guess.ID, PlayerID: guess.PlayerID, PlayerDisplayName: guess.PlayerDisplayName,
				RawAnswer: guess.RawAnswer, CreatedAt: guess.CreatedAt,
			})
		}
	}

	return projected
}

func projectTeamScores(teams []models.Team, players []models.Player, scores []models.ScoreboardEntry) []models.TeamScoreboardEntry {
	playerTeams := make(map[string]string, len(players))
	for _, player := range players {
		playerTeams[player.ID] = player.TeamID
	}
	totals := make(map[string]int, len(teams))
	for _, score := range scores {
		totals[playerTeams[score.PlayerID]] += score.Score
	}
	result := make([]models.TeamScoreboardEntry, 0, len(teams))
	for _, team := range teams {
		result = append(result, models.TeamScoreboardEntry{TeamID: team.ID, Name: team.Name, Score: totals[team.ID]})
	}
	return result
}
