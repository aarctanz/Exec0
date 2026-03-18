package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/aarctanz/Exec0/internal/config"
	"github.com/aarctanz/Exec0/internal/database/queries"
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

	srv, err := server.New(cfg)
	if err != nil {
		panic("failed to create server: " + err.Error())
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
			log.Fatal("failed to start server")
		}
	}()

	<-ctx.Done()
	ctx, cancel := context.WithTimeout(context.Background(), DefaultContextTimeout*time.Second)

	if err = srv.Shutdown(ctx); err != nil {
		log.Fatal("server forced to shutdown")
	}
	stop()
	cancel()

	log.Print("server exited properly")
}
