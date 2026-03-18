package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/aarctanz/Exec0/internal/config"
	"github.com/aarctanz/Exec0/internal/database"
	dbqueries "github.com/aarctanz/Exec0/internal/database/queries"
	"github.com/aarctanz/Exec0/internal/queue"
	"github.com/aarctanz/Exec0/internal/queue/tasks"
	"github.com/aarctanz/Exec0/internal/services"
	"github.com/hibiken/asynq"
)

const defaultConcurrency = 10

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("failed to load config: " + err.Error())
	}

	db, err := database.New(cfg)
	if err != nil {
		log.Fatal("failed to connect to database: " + err.Error())
	}

	queries := dbqueries.New(db.Pool)
	executionService := services.NewExecutionService(queries)

	// Build the handler as a closure that delegates to ExecutionService.
	// This avoids circular imports: queue/tasks doesn't import services,
	// the wiring happens here in main.
	submissionHandler := func(ctx context.Context, t *asynq.Task) error {
		var payload tasks.SubmissionPayload
		if err := json.Unmarshal(t.Payload(), &payload); err != nil {
			return fmt.Errorf("failed to unmarshal submission payload: %w", err)
		}
		return executionService.Execute(ctx, payload.SubmissionID)
	}

	srv := queue.NewServer(cfg.Redis.Address, defaultConcurrency, queries)
	mux := queue.NewServeMux(submissionHandler)

	log.Println("Starting worker with concurrency", defaultConcurrency)

	if err := srv.Run(mux); err != nil {
		log.Fatal("failed to run worker: " + err.Error())
	}
}
