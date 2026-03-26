package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/aarctanz/Exec0/internal/config"
	"github.com/aarctanz/Exec0/internal/database"
	dbqueries "github.com/aarctanz/Exec0/internal/database/queries"
	"github.com/aarctanz/Exec0/internal/logger"
	"github.com/aarctanz/Exec0/internal/metrics"
	"github.com/aarctanz/Exec0/internal/queue"
	"github.com/aarctanz/Exec0/internal/queue/tasks"
	"github.com/aarctanz/Exec0/internal/services"
	"github.com/aarctanz/Exec0/internal/telemetry"
	"github.com/hibiken/asynq"
)

func getConcurrency(cfg config.WorkerConfig) int {
	if cfg.Concurrency > 0 {
		return cfg.Concurrency
	}
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
	metrics.RegisterWorker(prometheus.DefaultRegisterer)

	// OpenTelemetry tracing
	otelEndpoint := cfg.OTel.Endpoint
	if otelEndpoint == "" {
		otelEndpoint = "otel-collector:4317"
	}
	otelServiceName := cfg.OTel.ServiceName
	if otelServiceName == "" {
		otelServiceName = "worker"
	}
	shutdownTracer, err := telemetry.Init(context.Background(), otelServiceName, otelEndpoint)
	if err != nil {
		log.Warn().Err(err).Msg("failed to init OTel tracing, continuing without it")
	} else {
		log.Info().Str("otel_endpoint", otelEndpoint).Str("otel_service", otelServiceName).Msg("otel configured")
		defer shutdownTracer(context.Background())
	}

	db, err := database.New(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer db.Pool.Close()

	queries := dbqueries.New(db.Pool)
	executionConfig := config.DefaultExecutionConfig()
	executionService := services.NewExecutionService(queries, executionConfig)

	submissionHandler := func(ctx context.Context, t *asynq.Task) error {
		var payload tasks.SubmissionPayload
		if err := json.Unmarshal(t.Payload(), &payload); err != nil {
			return fmt.Errorf("failed to unmarshal submission payload: %w", err)
		}

		// Extract trace context propagated through the queue
		if len(payload.TraceCarrier) > 0 {
			ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(payload.TraceCarrier))
		}

		return executionService.Execute(ctx, payload.SubmissionID)
	}

	concurrency := getConcurrency(cfg.Worker)
	metrics.WorkerConcurrency.Set(float64(concurrency))

	srv := queue.NewServer(cfg.Redis.Address, concurrency, queries)
	mux := queue.NewServeMux(submissionHandler)

	log.Info().Int("concurrency", concurrency).Msg("starting worker")

	// Metrics HTTP server
	metricsPort := cfg.Worker.MetricsPort
	if metricsPort == "" {
		metricsPort = "9091"
	}
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", promhttp.Handler())
	metricsServer := &http.Server{Addr: ":" + metricsPort, Handler: metricsMux}
	go func() {
		log.Info().Str("port", metricsPort).Msg("starting worker metrics server")
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("worker metrics server error")
		}
	}()

	// Start asynq server in a goroutine
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
	metricsServer.Close()
	log.Info().Msg("worker stopped")
}
