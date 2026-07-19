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
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))
	cfg := config.Load()
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
	if cfg.DatabaseURL == "" {
		logger.Warn("DATABASE_URL not set, using in-memory room repository")
	} else {
		pool, err := db.OpenPostgresPool(context.Background(), cfg.DatabaseURL)
		if err != nil {
			logger.Error("postgres connection failed", "error", err)
			os.Exit(1)
		}
		defer pool.Close()

		roomRepository = db.NewPostgresRoomRepository(pool)
		logger.Info("using postgres room repository")
	}

	roomService := game.NewRoomService(roomRepository, modelClient)
	roomService.SetPredictionModel(cfg.DefaultModels.Prediction)
	roomService.SetJudgeModel(cfg.DefaultModels.Judge)
	roomService.SetModelPolicy(cfg.ModelPolicy)
	server := httpapi.NewServer(cfg, logger, roomService)

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
		os.Exit(1)
	}

	logger.Info("http server stopped")
}
