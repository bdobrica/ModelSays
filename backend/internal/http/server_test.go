package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/config"
	"github.com/bogdandobrica/modelsays/backend/internal/game"
	"github.com/bogdandobrica/modelsays/backend/internal/models"
	"github.com/bogdandobrica/modelsays/backend/internal/security"
)

func TestObservabilityAddsRequestIDMetricsAndRedactsSecrets(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	cfg := config.Config{MetricsToken: "metrics-secret"}
	server := NewServer(cfg, logger, game.NewInMemoryRoomService())
	request := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(`{"playerToken":"never-log-token","answer":"hidden guess"}`))
	request.Header.Set("X-Request-ID", "request-123")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Header().Get("X-Request-ID") != "request-123" {
		t.Fatalf("request id = %q", response.Header().Get("X-Request-ID"))
	}
	for _, secret := range []string{"never-log-token", "hidden guess", "metrics-secret"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("log leaked %q: %s", secret, logs.String())
		}
	}

	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("metrics without token = %d", unauthorized.Code)
	}
	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRequest.Header.Set("Authorization", "Bearer metrics-secret")
	metricsResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(metricsResponse, metricsRequest)
	if metricsResponse.Code != http.StatusOK || !strings.Contains(metricsResponse.Body.String(), "modelsays_http_requests_total") {
		t.Fatalf("metrics response = %d %s", metricsResponse.Code, metricsResponse.Body.String())
	}
	for _, secret := range []string{"request-123", "never-log-token", "ROOMAA"} {
		if strings.Contains(metricsResponse.Body.String(), secret) {
			t.Fatalf("metrics leaked %q", secret)
		}
	}
}

func TestReplayHTTPProjectionExcludesPrivateFields(t *testing.T) {
	service := game.NewInMemoryRoomService()
	ctx := context.Background()
	room, host, err := service.CreateRoom(ctx, game.CreateRoomInput{
		RoomName: "Replay privacy", HostDisplayName: "Host",
		Settings: models.RoomSettings{TotalRounds: 1, AnswerTimerSeconds: 30},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, player, err := service.JoinRoom(ctx, game.JoinRoomInput{Code: room.Code, DisplayName: "Player"})
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.StartGame(ctx, game.StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	answer := started.CurrentGame.CurrentRound.Board.Answers[0]
	if _, err := service.SubmitGuess(ctx, game.SubmitGuessInput{
		Code: room.Code, RoundID: started.CurrentGame.CurrentRound.ID,
		PlayerToken: player.Token, Answer: answer.CanonicalAnswer,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RevealRound(ctx, game.RevealRoundInput{
		Code: room.Code, RoundID: started.CurrentGame.CurrentRound.ID, PlayerToken: host.Token,
	}); err != nil {
		t.Fatal(err)
	}
	completed, err := service.NextRound(ctx, game.NextRoundInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/replays/"+completed.CurrentGame.ReplayID, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("replay status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, private := range []string{host.Token, player.Token, `"aliases"`, `"normalizedAnswer"`, `"provider"`, `"modelName"`, `"promptVersion"`, `"providerAudits"`, `"rawResponse"`} {
		if strings.Contains(body, private) {
			t.Fatalf("replay leaked %q: %s", private, body)
		}
	}
	for _, public := range []string{answer.CanonicalAnswer, player.DisplayName, `"scoreDeltas"`} {
		if !strings.Contains(body, public) {
			t.Fatalf("replay omitted %q: %s", public, body)
		}
	}
}

func TestAbuseLimitsReturnDeterministicMetadataAndExcludeHealth(t *testing.T) {
	cfg := config.Config{Abuse: security.DefaultConfig()}
	cfg.Abuse.Create = security.Policy{Limit: 1, Window: time.Minute}
	server := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), game.NewInMemoryRoomService())
	payload := `{"roomName":"Party","hostDisplayName":"Host","settings":{"mode":"simultaneous","totalRounds":1,"answerTimerSeconds":30,"locale":"en","predictionModel":"gpt-4.1-mini","teamSafeMode":true}}`

	first := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(payload))
	first.RemoteAddr = "203.0.113.5:1234"
	firstResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(firstResponse, first)
	if firstResponse.Code != http.StatusCreated {
		t.Fatalf("first create status = %d body=%s", firstResponse.Code, firstResponse.Body.String())
	}

	second := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(payload))
	second.RemoteAddr = first.RemoteAddr
	secondResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(secondResponse, second)
	if secondResponse.Code != http.StatusTooManyRequests || secondResponse.Header().Get("Retry-After") != "60" {
		t.Fatalf("limited status=%d headers=%v body=%s", secondResponse.Code, secondResponse.Header(), secondResponse.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(secondResponse.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "rate_limited" || body["scope"] != "create" || body["retryAfterSeconds"] != float64(60) {
		t.Fatalf("rate response = %#v", body)
	}

	for index := 0; index < 3; index++ {
		response := httptest.NewRecorder()
		server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("health request %d was limited: %d", index, response.Code)
		}
	}
}

func TestModerationRejectsBeforeMutation(t *testing.T) {
	service := game.NewInMemoryRoomService()
	server := NewServer(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	request := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(
		`{"roomName":"Party","hostDisplayName":"nigger","settings":{"mode":"simultaneous","totalRounds":1,"answerTimerSeconds":30,"locale":"en","predictionModel":"gpt-4.1-mini","teamSafeMode":true}}`,
	))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "party-safe") {
		t.Fatalf("moderation status=%d body=%s", response.Code, response.Body.String())
	}

	room, host, err := service.CreateRoom(context.Background(), game.CreateRoomInput{RoomName: "Safe party", HostDisplayName: "Host"})
	if err != nil {
		t.Fatal(err)
	}
	started, err := service.StartGame(context.Background(), game.StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	guess := fmt.Sprintf(`{"playerToken":%q,"answer":"nigger"}`, host.Token)
	guessRequest := httptest.NewRequest(http.MethodPost, "/api/rooms/"+room.Code+"/rounds/"+started.CurrentGame.CurrentRound.ID+"/guesses", strings.NewReader(guess))
	guessResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(guessResponse, guessRequest)
	if guessResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("answer moderation status=%d body=%s", guessResponse.Code, guessResponse.Body.String())
	}
	reloaded, err := service.GetRoom(context.Background(), room.Code)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.CurrentGame.CurrentRound.Guesses) != 0 {
		t.Fatalf("moderated answer mutated guesses: %#v", reloaded.CurrentGame.CurrentRound.Guesses)
	}
}

type streamingResponseWriter struct {
	header http.Header
	status chan int
	writes chan string
}

func newStreamingResponseWriter() *streamingResponseWriter {
	return &streamingResponseWriter{header: make(http.Header), status: make(chan int, 1), writes: make(chan string, 32)}
}

func (writer *streamingResponseWriter) Header() http.Header { return writer.header }
func (writer *streamingResponseWriter) WriteHeader(status int) {
	select {
	case writer.status <- status:
	default:
	}
}
func (writer *streamingResponseWriter) Write(payload []byte) (int, error) {
	writer.writes <- string(append([]byte(nil), payload...))
	return len(payload), nil
}
func (writer *streamingResponseWriter) Flush() {}

type failingStreamingResponseWriter struct {
	header http.Header
	writes int
}

func (writer *failingStreamingResponseWriter) Header() http.Header { return writer.header }
func (writer *failingStreamingResponseWriter) WriteHeader(int)     {}
func (writer *failingStreamingResponseWriter) Flush()              {}
func (writer *failingStreamingResponseWriter) Write(payload []byte) (int, error) {
	writer.writes++
	if writer.writes > 1 {
		return 0, errors.New("slow consumer disconnected")
	}
	return len(payload), nil
}

func TestRoomEventsAuthenticateReplayAndRemainContentFree(t *testing.T) {
	service := game.NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), game.CreateRoomInput{RoomName: "Live room", HostDisplayName: "Host"})
	if err != nil {
		t.Fatal(err)
	}
	otherRoom, otherHost, err := service.CreateRoom(context.Background(), game.CreateRoomInput{RoomName: "Other room", HostDisplayName: "Other Host"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		CORSAllowedOrigins:  []string{"http://localhost:5173"},
		EventPollInterval:   time.Millisecond,
		EventHeartbeat:      10 * time.Millisecond,
		EventMaxConnections: 4,
	}
	server := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), service)

	for name, configure := range map[string]func(*http.Request){
		"missing":    func(*http.Request) {},
		"invalid":    func(r *http.Request) { r.Header.Set("X-Player-Token", "wrong") },
		"wrong room": func(r *http.Request) { r.Header.Set("X-Player-Token", otherHost.Token) },
		"query credential": func(r *http.Request) {
			r.URL.RawQuery = "playerToken=" + host.Token
			r.Header.Set("X-Player-Token", host.Token)
		},
		"wrong origin": func(r *http.Request) {
			r.Header.Set("X-Player-Token", host.Token)
			r.Header.Set("Origin", "https://evil.example")
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/rooms/"+room.Code+"/events", nil)
			configure(request)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code == http.StatusOK {
				t.Fatalf("unauthorized stream returned 200")
			}
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/rooms/"+room.Code+"/events", nil).WithContext(ctx)
	request.Header.Set("X-Player-Token", host.Token)
	stream := newStreamingResponseWriter()
	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(stream, request)
		close(done)
	}()
	if status := <-stream.status; status != http.StatusOK {
		t.Fatalf("event status = %d", status)
	}
	if connected := <-stream.writes; connected != ": connected\n\n" {
		t.Fatalf("initial stream payload = %q", connected)
	}
	joined, _, err := service.JoinRoom(context.Background(), game.JoinRoomInput{Code: room.Code, DisplayName: "Player"})
	if err != nil {
		t.Fatal(err)
	}
	if joined.Revision != 1 {
		t.Fatalf("mutation response revision = %d, want 1", joined.Revision)
	}
	var data string
	for data == "" {
		select {
		case chunk := <-stream.writes:
			for _, line := range strings.Split(chunk, "\n") {
				if strings.HasPrefix(line, "data: ") {
					data = strings.TrimSpace(strings.TrimPrefix(line, "data: "))
				}
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for room event")
		}
	}
	var event models.RoomEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		t.Fatal(err)
	}
	if event.Version != 1 || event.Type != models.RoomEventPlayerJoined || event.RoomRevision != 1 {
		t.Fatalf("unexpected event: %+v", event)
	}
	for _, secret := range []string{host.Token, "Player", "board", "guess", "score"} {
		if strings.Contains(data, secret) {
			t.Fatalf("event leaked %q: %s", secret, data)
		}
	}

	replayed, err := service.ListRoomEvents(context.Background(), room.Code, 0, 100)
	if err != nil || len(replayed) != 1 || replayed[0].ID != event.ID {
		t.Fatalf("durable replay = %+v, err=%v", replayed, err)
	}
	none, err := service.ListRoomEvents(context.Background(), room.Code, event.RoomRevision, 100)
	if err != nil || len(none) != 0 {
		t.Fatalf("resume returned %+v, err=%v", none, err)
	}
	isolated, err := service.ListRoomEvents(context.Background(), otherRoom.Code, 0, 100)
	if err != nil || len(isolated) != 0 {
		t.Fatalf("other room received %+v, err=%v", isolated, err)
	}
	cancel()
	<-done
}

func TestRoomEventsHeartbeatAndConnectionLimit(t *testing.T) {
	service := game.NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), game.CreateRoomInput{RoomName: "Bounded streams", HostDisplayName: "Host"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{EventPollInterval: time.Millisecond, EventHeartbeat: time.Millisecond, EventMaxConnections: 1}
	server := NewServer(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodGet, "/api/rooms/"+room.Code+"/events", nil).WithContext(ctx)
	request.Header.Set("X-Player-Token", host.Token)
	stream := newStreamingResponseWriter()
	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(stream, request)
		close(done)
	}()
	<-stream.status
	<-stream.writes
	select {
	case heartbeat := <-stream.writes:
		if heartbeat != ": heartbeat\n\n" {
			t.Fatalf("heartbeat = %q", heartbeat)
		}
	case <-time.After(time.Second):
		t.Fatal("heartbeat was not sent")
	}

	second := httptest.NewRequest(http.MethodGet, "/api/rooms/"+room.Code+"/events", nil)
	second.Header.Set("X-Player-Token", host.Token)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, second)
	if response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") == "" {
		t.Fatalf("second connection = %d headers=%v", response.Code, response.Header())
	}
	cancel()
	<-done
}

func TestRoomEventsCleanUpFailedConsumer(t *testing.T) {
	service := game.NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), game.CreateRoomInput{RoomName: "Failed stream", HostDisplayName: "Host"})
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(
		config.Config{EventPollInterval: time.Millisecond, EventHeartbeat: time.Millisecond, EventMaxConnections: 1},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		service,
	)
	request := httptest.NewRequest(http.MethodGet, "/api/rooms/"+room.Code+"/events", nil)
	request.Header.Set("X-Player-Token", host.Token)
	writer := &failingStreamingResponseWriter{header: make(http.Header)}
	done := make(chan struct{})
	go func() {
		server.Handler().ServeHTTP(writer, request)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("failed consumer was not cleaned up")
	}
	if active := server.activeEventConnections.Load(); active != 0 {
		t.Fatalf("active connections = %d, want 0", active)
	}
}

func TestRoomResponsesHideRoundOutcomeUntilReveal(t *testing.T) {
	t.Parallel()

	service := game.NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), game.CreateRoomInput{
		RoomName: "Secret outcomes", HostDisplayName: "Host",
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}
	_, player, err := service.JoinRoom(context.Background(), game.JoinRoomInput{
		Code: room.Code, DisplayName: "Player",
	})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}

	server := NewServer(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	lobby := performRoomRequest(t, server, http.MethodGet, fmt.Sprintf("/api/rooms/%s", room.Code), "")
	started := performRoomRequest(
		t,
		server,
		http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/start", room.Code),
		fmt.Sprintf(`{"playerToken":%q}`, host.Token),
	)
	roundID := nestedString(t, started, "room", "currentGame", "currentRound", "id")
	submitted := performRoomRequest(
		t,
		server,
		http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/rounds/%s/guesses", room.Code, roundID),
		fmt.Sprintf(`{"playerToken":%q,"answer":"bitcoin"}`, player.Token),
	)
	hostView := performRoomRequest(
		t,
		server,
		http.MethodGet,
		fmt.Sprintf("/api/rooms/%s?playerToken=%s", room.Code, host.Token),
		"",
	)
	playerView := performRoomRequest(
		t,
		server,
		http.MethodGet,
		fmt.Sprintf("/api/rooms/%s?playerToken=%s", room.Code, player.Token),
		"",
	)
	revealed := performRoomRequest(
		t,
		server,
		http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/rounds/%s/reveal", room.Code, roundID),
		fmt.Sprintf(`{"playerToken":%q}`, host.Token),
	)

	tests := []struct {
		name             string
		response         map[string]any
		wantCurrentGame  bool
		wantRoundSecrets bool
		wantScore        float64
		wantSubmitted    bool
	}{
		{name: "lobby", response: lobby},
		{name: "answering after start", response: started, wantCurrentGame: true},
		{name: "answering after scoring submission", response: submitted, wantCurrentGame: true, wantSubmitted: true},
		{name: "answering host view", response: hostView, wantCurrentGame: true, wantSubmitted: true},
		{name: "answering non-host view", response: playerView, wantCurrentGame: true, wantSubmitted: true},
		{name: "revealed", response: revealed, wantCurrentGame: true, wantRoundSecrets: true, wantScore: 50, wantSubmitted: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			roomPayload := nestedMap(t, test.response, "room")
			assertPublicPlayersHaveNoTokens(t, roomPayload)
			gamePayload, hasGame := roomPayload["currentGame"].(map[string]any)
			if hasGame != test.wantCurrentGame {
				t.Fatalf("currentGame presence = %v, want %v", hasGame, test.wantCurrentGame)
			}
			if !hasGame {
				return
			}

			roundPayload := nestedMap(t, gamePayload, "currentRound")
			_, hasBoard := roundPayload["board"]
			_, hasGuesses := roundPayload["guesses"]
			if hasBoard != test.wantRoundSecrets || hasGuesses != test.wantRoundSecrets {
				t.Fatalf("board/guesses presence = %v/%v, want %v/%v", hasBoard, hasGuesses, test.wantRoundSecrets, test.wantRoundSecrets)
			}

			playerScore := scoreboardEntry(t, gamePayload, player.ID)
			if score := playerScore["score"]; score != test.wantScore {
				t.Fatalf("public score = %v, want %v", score, test.wantScore)
			}
			if submitted := playerScore["submissionMade"]; submitted != test.wantSubmitted {
				t.Fatalf("submissionMade = %v, want %v", submitted, test.wantSubmitted)
			}

			if test.wantRoundSecrets {
				guess := nestedSlice(t, roundPayload, "guesses")[0].(map[string]any)
				for _, field := range []string{"rawAnswer", "normalizedAnswer", "matchedPredictionAnswerId", "scoreAwarded", "duplicate"} {
					if _, ok := guess[field]; !ok {
						t.Errorf("revealed guess does not include %q", field)
					}
				}
			}
		})
	}

	deleteNested(hostView, "room", "updatedAt")
	deleteNested(playerView, "room", "updatedAt")
	if !reflect.DeepEqual(hostView, playerView) {
		t.Fatal("host and non-host answering views have different secrecy projections")
	}
}

func TestProviderAuditsRequireHostHeaderAndStayOutOfRoomProjection(t *testing.T) {
	service := game.NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), game.CreateRoomInput{RoomName: "Private audits", HostDisplayName: "Host"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.StartGame(context.Background(), game.StartGameInput{Code: room.Code, PlayerToken: host.Token}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), service)

	public := performRoomRequest(t, server, http.MethodGet, "/api/rooms/"+room.Code, "")
	encoded, _ := json.Marshal(public)
	if strings.Contains(string(encoded), "providerAudits") || strings.Contains(string(encoded), "retentionClass") {
		t.Fatalf("public room projection leaked provider audit data: %s", encoded)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/rooms/"+room.Code+"/provider-audits", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated audit status = %d, want 403", response.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/api/rooms/"+room.Code+"/provider-audits", nil)
	request.Header.Set("X-Player-Token", host.Token)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"retentionClass":"provider_audit_30d"`) {
		t.Fatalf("host audit response = %d %s", response.Code, response.Body.String())
	}
}

func TestJudgeSuggestionsAreSecretUntilRevealAndHostOnly(t *testing.T) {
	service := game.NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), game.CreateRoomInput{RoomName: "Private judge", HostDisplayName: "Host"})
	if err != nil {
		t.Fatal(err)
	}
	_, player, _ := service.JoinRoom(context.Background(), game.JoinRoomInput{Code: room.Code, DisplayName: "Player"})
	started, err := service.StartGame(context.Background(), game.StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatal(err)
	}
	roundID := started.CurrentGame.CurrentRound.ID
	if _, err := service.SubmitGuess(context.Background(), game.SubmitGuessInput{
		Code: room.Code, RoundID: roundID, PlayerToken: player.Token, Answer: "definitely not on the board",
	}); err != nil {
		t.Fatal(err)
	}
	server := NewServer(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	public := performRoomRequest(t, server, http.MethodGet, "/api/rooms/"+room.Code, "")
	encoded, _ := json.Marshal(public)
	if strings.Contains(string(encoded), "suggestion") || strings.Contains(string(encoded), "confidence") {
		t.Fatalf("answering projection leaked judge data: %s", encoded)
	}
	path := fmt.Sprintf("/api/rooms/%s/rounds/%s/judge-suggestions", room.Code, roundID)
	for name, token := range map[string]string{"missing": "", "player": player.Token} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.Header.Set("X-Player-Token", token)
			response := httptest.NewRecorder()
			server.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", response.Code)
			}
		})
	}
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("X-Player-Token", host.Token)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("pre-reveal host status = %d, want 409", response.Code)
	}
	if _, err := service.RevealRound(context.Background(), game.RevealRoundInput{Code: room.Code, RoundID: roundID, PlayerToken: host.Token}); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("X-Player-Token", host.Token)
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"outcome":"miss"`) {
		t.Fatalf("revealed host response = %d %s", response.Code, response.Body.String())
	}
}

func TestExpiredSubmissionReturnsStableConflictResponse(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.July, 19, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{now: startedAt}
	service := game.NewRoomServiceWithClock(game.NewInMemoryRoomRepository(), nil, &clock)
	room, host, err := service.CreateRoom(context.Background(), game.CreateRoomInput{
		RoomName: "HTTP deadline", HostDisplayName: "Host", Settings: models.RoomSettings{AnswerTimerSeconds: 30},
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}
	_, player, err := service.JoinRoom(context.Background(), game.JoinRoomInput{Code: room.Code, DisplayName: "Player"})
	if err != nil {
		t.Fatalf("JoinRoom returned error: %v", err)
	}
	startedRoom, err := service.StartGame(context.Background(), game.StartGameInput{Code: room.Code, PlayerToken: host.Token})
	if err != nil {
		t.Fatalf("StartGame returned error: %v", err)
	}
	clock.now = startedRoom.CurrentGame.CurrentRound.AnswerPhaseEndsAt

	server := NewServer(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/rounds/%s/guesses", room.Code, startedRoom.CurrentGame.CurrentRound.ID),
		strings.NewReader(fmt.Sprintf(`{"playerToken":%q,"answer":"bitcoin"}`, player.Token)),
	)
	request.Header.Set("Content-Type", "application/json")

	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d", http.StatusConflict, response.Code)
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json" {
		t.Fatalf("expected JSON content type, got %q", contentType)
	}
	if body := strings.TrimSpace(response.Body.String()); body != `{"error":"answer phase has expired"}` {
		t.Fatalf("unexpected error response: %s", body)
	}
}

func TestRecoverSessionValidatesTokenAndReturnsAuthoritativePlayer(t *testing.T) {
	t.Parallel()

	service := game.NewInMemoryRoomService()
	room, host, err := service.CreateRoom(context.Background(), game.CreateRoomInput{
		RoomName: "Reconnect room", HostDisplayName: "Host",
	})
	if err != nil {
		t.Fatalf("CreateRoom returned error: %v", err)
	}
	server := NewServer(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), service)

	recovered := performRoomRequest(
		t,
		server,
		http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/session", strings.ToLower(room.Code)),
		fmt.Sprintf(`{"playerToken":%q}`, host.Token),
	)
	player := nestedMap(t, recovered, "player")
	if player["id"] != host.ID || player["isHost"] != true || player["token"] != host.Token {
		t.Fatalf("recovered player = %#v, want authoritative host identity", player)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/rooms/%s/session", room.Code),
		strings.NewReader(`{"playerToken":"invalid"}`),
	)
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("invalid token status = %d, want %d; body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), game.ErrPlayerNotFound.Error()) {
		t.Fatalf("invalid token response = %s, want stable player-not-found error", recorder.Body.String())
	}
}

func TestCreateRoomRejectsInvalidSettingsAndMalformedBodies(t *testing.T) {
	t.Parallel()

	server := NewServer(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), game.NewInMemoryRoomService())
	valid := `{"roomName":"Friday Night","hostDisplayName":"Host","settings":{"mode":"simultaneous","totalRounds":1,"answerTimerSeconds":15,"locale":"en","predictionModel":"gpt-4.1-mini","teamSafeMode":false}}`
	tests := []struct {
		name string
		body string
	}{
		{name: "zero rounds after explicit invalid timer", body: strings.Replace(valid, `"totalRounds":1`, `"totalRounds":6`, 1)},
		{name: "timer below lower bound", body: strings.Replace(valid, `"answerTimerSeconds":15`, `"answerTimerSeconds":14`, 1)},
		{name: "timer above upper bound", body: strings.Replace(valid, `"answerTimerSeconds":15`, `"answerTimerSeconds":121`, 1)},
		{name: "unsupported mode", body: strings.Replace(valid, `"simultaneous"`, `"sequential"`, 1)},
		{name: "unsupported locale", body: strings.Replace(valid, `"locale":"en"`, `"locale":"ro"`, 1)},
		{name: "unsupported model", body: strings.Replace(valid, `"gpt-4.1-mini"`, `"other-model"`, 1)},
		{name: "unknown field", body: strings.Replace(valid, `"roomName"`, `"unexpected":true,"roomName"`, 1)},
		{name: "trailing JSON", body: valid + `{}`},
		{name: "oversized body", body: valid + strings.Repeat(" ", maxRequestBodyBytes)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/api/rooms", strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			server.Handler().ServeHTTP(response, request)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusBadRequest, response.Body.String())
			}
		})
	}
}

func TestReadinessReflectsTransitionWorker(t *testing.T) {
	server := NewServer(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), game.NewInMemoryRoomService())
	server.SetReadinessCheck(func() error { return game.ErrDeadlineTransitionsUnavailable })
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unready status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	server.SetReadinessCheck(func() error { return nil })
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("ready status = %d, want %d", response.Code, http.StatusOK)
	}
}

func TestCreateJoinStartHTTPFlowAndJoinClosure(t *testing.T) {
	t.Parallel()

	server := NewServer(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), game.NewInMemoryRoomService())
	created := performRoomRequest(t, server, http.MethodPost, "/api/rooms", `{
		"roomName":"Friday Night","hostDisplayName":"Host",
		"settings":{"mode":"simultaneous","totalRounds":5,"answerTimerSeconds":120,"locale":"en","predictionModel":"gpt-4.1-mini","teamSafeMode":true}
	}`)
	code := nestedString(t, created, "room", "code")
	hostToken := nestedString(t, created, "player", "token")
	performRoomRequest(t, server, http.MethodPost, fmt.Sprintf("/api/rooms/%s/join", code), `{"displayName":"Player"}`)
	performRoomRequest(t, server, http.MethodPost, fmt.Sprintf("/api/rooms/%s/start", code), fmt.Sprintf(`{"playerToken":%q}`, hostToken))

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/rooms/%s/join", code), strings.NewReader(`{"displayName":"Late Player"}`))
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("late join status = %d, want %d: %s", response.Code, http.StatusConflict, response.Body.String())
	}
	if body := strings.TrimSpace(response.Body.String()); body != `{"error":"game has started; new players cannot join"}` {
		t.Fatalf("late join response = %s", body)
	}
}

func performRoomRequest(t *testing.T, server *Server, method string, path string, body string) map[string]any {
	t.Helper()

	response := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(response, request)
	if response.Code < http.StatusOK || response.Code >= http.StatusMultipleChoices {
		t.Fatalf("%s %s returned %d: %s", method, path, response.Code, response.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode %s %s response: %v", method, path, err)
	}
	return payload
}

func nestedMap(t *testing.T, payload map[string]any, keys ...string) map[string]any {
	t.Helper()

	current := payload
	for _, key := range keys {
		value, ok := current[key].(map[string]any)
		if !ok {
			t.Fatalf("%q is not an object in %#v", key, current)
		}
		current = value
	}
	return current
}

func nestedString(t *testing.T, payload map[string]any, keys ...string) string {
	t.Helper()

	parent := nestedMap(t, payload, keys[:len(keys)-1]...)
	value, ok := parent[keys[len(keys)-1]].(string)
	if !ok {
		t.Fatalf("%q is not a string in %#v", keys[len(keys)-1], parent)
	}
	return value
}

func nestedSlice(t *testing.T, payload map[string]any, key string) []any {
	t.Helper()

	value, ok := payload[key].([]any)
	if !ok {
		t.Fatalf("%q is not an array in %#v", key, payload)
	}
	return value
}

func assertPublicPlayersHaveNoTokens(t *testing.T, room map[string]any) {
	t.Helper()

	for _, value := range nestedSlice(t, room, "players") {
		player, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("player is not an object: %#v", value)
		}
		if _, ok := player["token"]; ok {
			t.Errorf("public player leaked token: %#v", player)
		}
	}
}

func scoreboardEntry(t *testing.T, gamePayload map[string]any, playerID string) map[string]any {
	t.Helper()

	for _, value := range nestedSlice(t, gamePayload, "scoreboard") {
		entry := value.(map[string]any)
		if entry["playerId"] == playerID {
			return entry
		}
	}
	t.Fatalf("scoreboard does not include player %q", playerID)
	return nil
}

func deleteNested(payload map[string]any, keys ...string) {
	parent := payload
	for _, key := range keys[:len(keys)-1] {
		parent, _ = parent[key].(map[string]any)
	}
	delete(parent, keys[len(keys)-1])
}

type fixedClock struct {
	now time.Time
}

func (clock *fixedClock) Now() time.Time {
	return clock.now
}
