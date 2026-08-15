package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bogdandobrica/modelsays/backend/internal/config"
	"github.com/bogdandobrica/modelsays/backend/internal/db"
	"github.com/bogdandobrica/modelsays/backend/internal/game"
	httpapi "github.com/bogdandobrica/modelsays/backend/internal/http"
	"github.com/bogdandobrica/modelsays/backend/internal/llm"
	"github.com/bogdandobrica/modelsays/backend/internal/models"
	"github.com/bogdandobrica/modelsays/backend/internal/ops"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		logger.Error("invalid configuration", "error", err)
		os.Exit(1)
	}
	if !cfg.ModelPolicy.AllowsQuestion(cfg.DefaultModels.Question) || !cfg.ModelPolicy.AllowsPrediction(cfg.DefaultModels.Prediction) || !cfg.ModelPolicy.AllowsJudge(cfg.DefaultModels.Judge) {
		logger.Error("default model is not present in its server allowlist")
		os.Exit(1)
	}
	modelClient := llm.ModelClient(llm.NewStaticModelClient())
	if cfg.OpenAIAPIKey != "" {
		modelClient = llm.NewOpenAIModelClient(cfg.OpenAIAPIKey, llm.ClientDefaults{
			QuestionModel:   cfg.DefaultModels.Question,
			PredictionModel: cfg.DefaultModels.Prediction,
			JudgeModel:      cfg.DefaultModels.Judge,
		})
		logger.Info("using openai model client")
	} else {
		logger.Warn("OPENAI_API_KEY not set, using static model client")
	}

	roomRepository := game.RoomRepository(game.NewInMemoryRoomRepository())
	var dueRepository game.DueRoundRepository
	var postgresRepository *db.PostgresRoomRepository
	if cfg.DatabaseURL == "" {
		logger.Warn("DATABASE_URL not set, using in-memory room repository")
	} else {
		pool, err := db.OpenPostgresPool(context.Background(), cfg.DatabaseURL)
		if err != nil {
			logger.Error("postgres connection failed", "error", err)
			os.Exit(1)
		}
		defer pool.Close()

		postgresRepository = db.NewPostgresRoomRepository(pool)
		postgresRepository.SetLivingRoomRevealPause(cfg.LivingRoomRevealPause)
		roomRepository = postgresRepository
		dueRepository = postgresRepository
		logger.Info("using postgres room repository")
		modelClient = llm.NewBankModelClient(postgresRepository, modelClient)
		logger.Info("database content banks enabled")
	}

	roomService := game.NewRoomService(roomRepository, modelClient)
	roomService.SetPredictionModel(cfg.DefaultModels.Prediction)
	roomService.SetJudgeModel(cfg.DefaultModels.Judge)
	roomService.SetModelPolicy(cfg.ModelPolicy)
	roomService.SetAvailableLocales(cfg.AvailableLocales)
	server := httpapi.NewServer(cfg, logger, roomService)
	roomService.SetProviderObserver(func(audit models.ProviderCallAudit) {
		server.Metrics().Inc("modelsays_provider_calls_total", "purpose", audit.Purpose, "outcome", audit.Outcome, "path", audit.Path)
		server.Metrics().Add("modelsays_provider_tokens_total", uint64(max(audit.InputTokens, 0)), "direction", "input", "purpose", audit.Purpose)
		server.Metrics().Add("modelsays_provider_tokens_total", uint64(max(audit.OutputTokens, 0)), "direction", "output", "purpose", audit.Purpose)
		server.Metrics().Observe("modelsays_provider_estimated_cost_usd", audit.EstimatedCostUSD, []float64{.001, .005, .01, .025, .05, .1}, "purpose", audit.Purpose)
		logger.Info("provider outcome", "purpose", audit.Purpose, "outcome", audit.Outcome, "path", audit.Path,
			"latency_ms", audit.LatencyMillis, "input_tokens", audit.InputTokens, "output_tokens", audit.OutputTokens,
			"estimated_cost_usd", audit.EstimatedCostUSD)
	})
	if cfg.OpenAIAPIKey != "" {
		roomService.SetProviderGate(server.AbuseController())
	}
	deadlineWorker := game.NewDeadlineWorker(dueRepository, nil, game.DeadlineWorkerConfig{
		Enabled: cfg.AutoRevealEnabled, GracePeriod: cfg.AutoRevealGrace,
		PollInterval: cfg.TransitionPoll, BatchSize: cfg.TransitionBatchSize,
	})
	deadlineWorker.SetObserver(func(processed int, lag time.Duration, err error) {
		outcome := "success"
		if err != nil {
			outcome = "error"
			logger.Error("deadline transition pass", "outcome", outcome, "error", err)
		} else if processed > 0 {
			logger.Info("deadline transition pass", "outcome", outcome, "processed", processed, "duration_ms", lag.Milliseconds())
		}
		server.Metrics().Inc("modelsays_transition_passes_total", "outcome", outcome)
		server.Metrics().Observe("modelsays_transition_pass_duration_seconds", lag.Seconds(), ops.HTTPDurationBuckets, "outcome", outcome)
	})
	server.SetReadinessCheck(func() error {
		if postgresRepository != nil {
			if err := postgresRepository.Ready(context.Background()); err != nil {
				return err
			}
			acquired, idle, max := postgresRepository.PoolStats()
			server.Metrics().Set("modelsays_database_pool_connections", float64(acquired), "state", "acquired")
			server.Metrics().Set("modelsays_database_pool_connections", float64(idle), "state", "idle")
			server.Metrics().Set("modelsays_database_pool_max_connections", float64(max))
			activeRooms, err := postgresRepository.ActiveRoomCount(context.Background())
			if err != nil {
				return err
			}
			server.Metrics().Set("modelsays_active_rooms", float64(activeRooms))
		} else if cfg.AppEnv == "production" {
			return game.ErrDeadlineTransitionsUnavailable
		}
		return deadlineWorker.Ready()
	})

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cfg.AutoRevealEnabled && dueRepository != nil {
		go deadlineWorker.Run(ctx)
		logger.Info("automatic round reveal enabled", "grace", cfg.AutoRevealGrace, "poll_interval", cfg.TransitionPoll)
	} else if cfg.AutoRevealEnabled {
		logger.Warn("automatic round reveal unavailable without PostgreSQL; readiness will fail")
	} else {
		logger.Warn("automatic round reveal disabled; host reveal remains available")
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("http server failed", "error", err)
			os.Exit(1)
		}
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}
	for !deadlineWorker.Drained() {
		select {
		case <-shutdownCtx.Done():
			logger.Error("transition worker drain timed out")
			os.Exit(1)
		case <-time.After(10 * time.Millisecond):
		}
	}

	logger.Info("http server stopped")
}
