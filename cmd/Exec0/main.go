package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/aarctanz/Exec0/internal/config"
	"github.com/aarctanz/Exec0/internal/database/queries"
	"github.com/aarctanz/Exec0/internal/logger"
	"github.com/aarctanz/Exec0/internal/queue"
	"github.com/aarctanz/Exec0/internal/server"
	"github.com/aarctanz/Exec0/internal/services"
)

const DefaultContextTimeout = 30

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		panic("failed to load config: " + err.Error())
	}

	logger.Init(cfg.Primary.Env)

	srv, err := server.New(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create server")
	}

	queueClient := queue.NewClient(cfg.Redis.Address)
	defer queueClient.Close()

	queries := queries.New(srv.DB.Pool)
	svc := services.New(queries, queueClient)

	handler := server.SetupRoutes(svc, cfg.Server.CORSAllowedOrigins)
	srv.SetupHTTPServer(handler)

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
