package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog/log"

	"github.com/aarctanz/Exec0/internal/config"
	"github.com/aarctanz/Exec0/internal/database/queries"
	"github.com/aarctanz/Exec0/internal/logger"
	"github.com/aarctanz/Exec0/internal/metrics"
	"github.com/aarctanz/Exec0/internal/queue"
	"github.com/aarctanz/Exec0/internal/server"
	"github.com/aarctanz/Exec0/internal/services"
	"github.com/aarctanz/Exec0/internal/telemetry"
)

const DefaultContextTimeout = 30

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		panic("failed to load config: " + err.Error())
	}

	logger.Init(cfg.Primary.Env)
	metrics.RegisterAPI(prometheus.DefaultRegisterer)

	// OpenTelemetry tracing
	otelEndpoint := cfg.OTel.Endpoint
	if otelEndpoint == "" {
		otelEndpoint = "otel-collector:4317"
	}
	otelServiceName := cfg.OTel.ServiceName
	if otelServiceName == "" {
		otelServiceName = "api"
	}
	shutdownTracer, err := telemetry.Init(context.Background(), otelServiceName, otelEndpoint)
	if err != nil {
		log.Warn().Err(err).Msg("failed to init OTel tracing, continuing without it")
	} else {
		log.Info().Str("otel_endpoint", otelEndpoint).Str("otel_service", otelServiceName).Msg("otel configured")
		defer shutdownTracer(context.Background())
	}

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create server")
	}

	queueClient := queue.NewClient(cfg.Redis.Address)
	defer queueClient.Close()

	queries := queries.New(srv.DB.Pool)
	svc := services.New(srv.DB.Pool, queries, queueClient)

	routes := server.SetupRoutes(srv.DB.Pool, svc, cfg.Server.AllowedIPs, cfg.Redis.Address)
	defer routes.Monitoring.Close()
	defer routes.Health.Close()
	srv.SetupHTTPServer(routes.Handler)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)

	go func() {
		if err = srv.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("failed to start server")
		}
	}()

	<-ctx.Done()
	ctx, cancel := context.WithTimeout(context.Background(), DefaultContextTimeout*time.Second)

	if err = srv.Shutdown(ctx); err != nil {
		log.Fatal().Err(err).Msg("server forced to shutdown")
	}
	stop()
	cancel()

	log.Info().Msg("server exited properly")
}
