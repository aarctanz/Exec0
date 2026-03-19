package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/aarctanz/Exec0/internal/config"
	"github.com/aarctanz/Exec0/internal/database"
	dbqueries "github.com/aarctanz/Exec0/internal/database/queries"
	"github.com/aarctanz/Exec0/internal/logger"
	"github.com/aarctanz/Exec0/internal/queue"
	"github.com/aarctanz/Exec0/internal/queue/tasks"
	"github.com/aarctanz/Exec0/internal/services"
	"github.com/hibiken/asynq"
)

func getConcurrency() int {
	n := runtime.NumCPU()
	if n < 2 {
		return 2
	}
	return n * 2
}

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger.Init(cfg.Primary.Env)

	db, err := database.New(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer db.Pool.Close()

	queries := dbqueries.New(db.Pool)
	executionService := services.NewExecutionService(queries)

	submissionHandler := func(ctx context.Context, t *asynq.Task) error {
		var payload tasks.SubmissionPayload
		if err := json.Unmarshal(t.Payload(), &payload); err != nil {
			return fmt.Errorf("failed to unmarshal submission payload: %w", err)
		}
		return executionService.Execute(ctx, payload.SubmissionID)
	}

	concurrency := getConcurrency()
	srv := queue.NewServer(cfg.Redis.Address, concurrency, queries)
	mux := queue.NewServeMux(submissionHandler)

	log.Info().Int("concurrency", concurrency).Msg("starting worker")

	// Start server in a goroutine
	go func() {
		if err := srv.Run(mux); err != nil {
			log.Fatal().Err(err).Msg("failed to run worker")
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit

	log.Info().Str("signal", sig.String()).Msg("shutting down worker, waiting for active tasks to finish")
	srv.Shutdown()
	log.Info().Msg("worker stopped")
}
