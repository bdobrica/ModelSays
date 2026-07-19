// Package baseline contains the repeatable, local-only public-beta measurement
// harness. It exercises the real HTTP and PostgreSQL paths with curated content.
package baseline

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/config"
	"github.com/bogdandobrica/modelsays/backend/internal/db"
	"github.com/bogdandobrica/modelsays/backend/internal/game"
	httpapi "github.com/bogdandobrica/modelsays/backend/internal/http"
	"github.com/bogdandobrica/modelsays/backend/internal/llm"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	schemaPrefix = "modelsays_baseline_"
	pollInterval = 3 * time.Second
)

type Options struct {
	DatabaseURL  string
	ClientDist   string
	PlayerCounts []int
}

type Report struct {
	SchemaVersion  int              `json:"schemaVersion"`
	Harness        string           `json:"harness"`
	PollIntervalMS int64            `json:"pollIntervalMs"`
	ClientBundle   ClientBundle     `json:"clientBundle"`
	Workloads      []WorkloadResult `json:"workloads"`
	Notes          []string         `json:"notes"`
}

type ClientBundle struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
}

type WorkloadResult struct {
	Players                         int            `json:"players"`
	HTTPRequests                    int            `json:"httpRequests"`
	PollRequests                    int            `json:"pollRequests"`
	PollRequestsPerMinute           int            `json:"pollRequestsPerMinute"`
	ResponseBytes                   int64          `json:"responseBytes"`
	AverageResponseBytes            int64          `json:"averageResponseBytes"`
	BackendLatency                  LatencySummary `json:"backendLatency"`
	MutationToVisibleStateLatencyMS float64        `json:"mutationToVisibleStateLatencyMs"`
	DatabaseQueries                 int64          `json:"databaseQueries"`
	DatabaseQueryTimeMS             float64        `json:"databaseQueryTimeMs"`
}

type LatencySummary struct {
	MinMS float64 `json:"minMs"`
	P50MS float64 `json:"p50Ms"`
	P95MS float64 `json:"p95Ms"`
	MaxMS float64 `json:"maxMs"`
}

type queryTrace struct {
	mu       sync.Mutex
	queries  int64
	duration time.Duration
}

type traceStartedAt struct{}

func (trace *queryTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, traceStartedAt{}, time.Now())
}

func (trace *queryTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryEndData) {
	startedAt, _ := ctx.Value(traceStartedAt{}).(time.Time)
	trace.mu.Lock()
	trace.queries++
	trace.duration += time.Since(startedAt)
	trace.mu.Unlock()
}

func (trace *queryTrace) snapshot() (int64, time.Duration) {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	return trace.queries, trace.duration
}

type measurement struct {
	requests      int
	polls         int
	responseBytes int64
	latencies     []time.Duration
}

type response struct {
	status int
	body   []byte
	value  map[string]any
}

func Run(ctx context.Context, options Options) (Report, error) {
	if strings.TrimSpace(options.DatabaseURL) == "" {
		return Report{}, fmt.Errorf("database URL is required")
	}
	if len(options.PlayerCounts) == 0 {
		options.PlayerCounts = []int{3, 8, 12}
	}
	bundle, err := measureBundle(options.ClientDist)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		SchemaVersion:  1,
		Harness:        "postgresql-http-curated-v1",
		PollIntervalMS: pollInterval.Milliseconds(),
		ClientBundle:   bundle,
		Notes: []string{
			"Durations are wall-clock observations and vary by host; request, byte, and query counts should remain deterministic.",
			"Polling volume is projected from one GET per player every three seconds; the harness does not sleep between polls.",
			"No external provider calls are made.",
		},
	}
	for _, players := range options.PlayerCounts {
		if players < 3 {
			return Report{}, fmt.Errorf("player count %d must be at least 3", players)
		}
		result, err := runWorkload(ctx, options.DatabaseURL, players)
		if err != nil {
			return Report{}, fmt.Errorf("%d-player workload: %w", players, err)
		}
		report.Workloads = append(report.Workloads, result)
	}
	return report, nil
}

func runWorkload(ctx context.Context, databaseURL string, playerCount int) (result WorkloadResult, runErr error) {
	admin, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return result, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer admin.Close()

	schema := fmt.Sprintf("%s%d_%d", schemaPrefix, playerCount, time.Now().UnixNano())
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		return result, fmt.Errorf("create isolated schema: %w", err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`); err != nil && runErr == nil {
			runErr = fmt.Errorf("drop isolated schema: %w", err)
		}
	}()

	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return result, fmt.Errorf("parse database URL: %w", err)
	}
	trace := &queryTrace{}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = schema
	poolConfig.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return result, fmt.Errorf("open isolated pool: %w", err)
	}
	defer pool.Close()
	if err := applyMigrations(ctx, pool); err != nil {
		return result, err
	}
	startQueries, startQueryTime := trace.snapshot()

	repository := db.NewPostgresRoomRepository(pool)
	service := game.NewRoomService(repository, llm.NewStaticModelClient())
	server := httpapi.NewServer(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), service)
	handler := server.Handler()
	metrics := &measurement{}

	created, err := call(handler, metrics, false, http.MethodPost, "/api/rooms", map[string]any{
		"roomName":        "Baseline room",
		"hostDisplayName": "Player 1",
		"settings": map[string]any{
			"mode": "simultaneous", "totalRounds": 1, "answerTimerSeconds": 15,
			"locale": "en", "predictionModel": "gpt-5.6-luna", "teamSafeMode": false,
		},
	})
	if err != nil {
		return result, err
	}
	code := nestedString(created.value, "room", "code")
	tokens := []string{nestedString(created.value, "player", "token")}
	for index := 2; index <= playerCount; index++ {
		joined, err := call(handler, metrics, false, http.MethodPost, "/api/rooms/"+code+"/join", map[string]any{
			"displayName": fmt.Sprintf("Player %d", index),
		})
		if err != nil {
			return result, err
		}
		tokens = append(tokens, nestedString(joined.value, "player", "token"))
	}
	started, err := call(handler, metrics, false, http.MethodPost, "/api/rooms/"+code+"/start", map[string]any{"playerToken": tokens[0]})
	if err != nil {
		return result, err
	}
	roundID := nestedString(started.value, "room", "currentGame", "currentRound", "id")

	for range tokens {
		if _, err := call(handler, metrics, true, http.MethodGet, "/api/rooms/"+code, nil); err != nil {
			return result, err
		}
	}
	var visibilityTotal time.Duration
	answers := []string{"crypto", "quantum physics", "taxes", "artificial intelligence", "wine"}
	for index, token := range tokens {
		mutationStarted := time.Now()
		if _, err := call(handler, metrics, false, http.MethodPost, "/api/rooms/"+code+"/rounds/"+roundID+"/guesses", map[string]any{
			"playerToken": token,
			"answer":      answers[index%len(answers)],
		}); err != nil {
			return result, err
		}
		polled, err := call(handler, metrics, true, http.MethodGet, "/api/rooms/"+code, nil)
		if err != nil {
			return result, err
		}
		if submittedCount(polled.value) != index+1 {
			return result, fmt.Errorf("poll observed %d submissions, want %d", submittedCount(polled.value), index+1)
		}
		visibilityTotal += time.Since(mutationStarted)
	}
	if _, err := call(handler, metrics, false, http.MethodPost, "/api/rooms/"+code+"/rounds/"+roundID+"/reveal", map[string]any{"playerToken": tokens[0]}); err != nil {
		return result, err
	}
	for range tokens {
		if _, err := call(handler, metrics, true, http.MethodGet, "/api/rooms/"+code, nil); err != nil {
			return result, err
		}
	}

	endQueries, endQueryTime := trace.snapshot()
	result = WorkloadResult{
		Players:                         playerCount,
		HTTPRequests:                    metrics.requests,
		PollRequests:                    metrics.polls,
		PollRequestsPerMinute:           playerCount * int(time.Minute/pollInterval),
		ResponseBytes:                   metrics.responseBytes,
		AverageResponseBytes:            metrics.responseBytes / int64(metrics.requests),
		BackendLatency:                  summarize(metrics.latencies),
		MutationToVisibleStateLatencyMS: milliseconds(visibilityTotal / time.Duration(playerCount)),
		DatabaseQueries:                 endQueries - startQueries,
		DatabaseQueryTimeMS:             milliseconds(endQueryTime - startQueryTime),
	}
	return result, nil
}

func call(handler http.Handler, metrics *measurement, poll bool, method, path string, payload any) (response, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return response{}, err
		}
		body = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, body)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	startedAt := time.Now()
	handler.ServeHTTP(recorder, request)
	elapsed := time.Since(startedAt)
	metrics.requests++
	if poll {
		metrics.polls++
	}
	metrics.responseBytes += int64(recorder.Body.Len())
	metrics.latencies = append(metrics.latencies, elapsed)
	if recorder.Code < 200 || recorder.Code >= 300 {
		return response{}, fmt.Errorf("%s %s returned %d: %s", method, path, recorder.Code, recorder.Body.String())
	}
	var value map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &value); err != nil {
		return response{}, fmt.Errorf("decode %s %s response: %w", method, path, err)
	}
	return response{status: recorder.Code, body: recorder.Body.Bytes(), value: value}, nil
}

func submittedCount(value map[string]any) int {
	players, _ := nested(value, "room", "currentGame", "scoreboard").([]any)
	count := 0
	for _, raw := range players {
		player, _ := raw.(map[string]any)
		if submitted, _ := player["submissionMade"].(bool); submitted {
			count++
		}
	}
	return count
}

func nestedString(value map[string]any, path ...string) string {
	result, _ := nested(value, path...).(string)
	return result
}

func nested(value map[string]any, path ...string) any {
	var current any = value
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[key]
	}
	return current
}

func summarize(values []time.Duration) LatencySummary {
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return LatencySummary{
		MinMS: milliseconds(sorted[0]),
		P50MS: milliseconds(percentile(sorted, 0.50)),
		P95MS: milliseconds(percentile(sorted, 0.95)),
		MaxMS: milliseconds(sorted[len(sorted)-1]),
	}
}

func percentile(values []time.Duration, fraction float64) time.Duration {
	index := int(math.Ceil(float64(len(values))*fraction)) - 1
	if index < 0 {
		index = 0
	}
	return values[index]
}

func milliseconds(value time.Duration) float64 {
	return math.Round(float64(value)/float64(time.Millisecond)*1000) / 1000
}

func measureBundle(root string) (ClientBundle, error) {
	var result ClientBundle
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		result.Files++
		result.Bytes += info.Size()
		return nil
	})
	if err != nil {
		return result, fmt.Errorf("measure client bundle %q: %w", root, err)
	}
	if result.Files == 0 {
		return result, fmt.Errorf("client bundle %q is empty; build it first", root)
	}
	return result, nil
}

func applyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		return fmt.Errorf("locate baseline source for migrations")
	}
	migrationRoot := filepath.Join(filepath.Dir(sourceFile), "..", "..", "migrations")
	paths, err := filepath.Glob(filepath.Join(migrationRoot, "*.sql"))
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return fmt.Errorf("no migrations found under %s", migrationRoot)
	}
	for _, path := range paths {
		contents, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", path, err)
		}
		upSQL := strings.TrimPrefix(strings.Split(string(contents), "-- +goose Down")[0], "-- +goose Up")
		if _, err := pool.Exec(ctx, upSQL); err != nil {
			return fmt.Errorf("apply migration %s: %w", filepath.Base(path), err)
		}
	}
	return nil
}
