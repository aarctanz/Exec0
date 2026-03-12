package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/aarctanz/Exec0/internal/config"
	"github.com/aarctanz/Exec0/internal/database"
)

type Server struct {
	Config     *config.Config
	DB         *database.Database
	httpServer *http.Server
}

func New(cfg *config.Config) (*Server, error) {
	db, err := database.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	return &Server{
		Config: cfg,
		DB:     db,
	}, nil
}

func (s *Server) SetupHTTPServer(handler http.Handler) {
	s.httpServer = &http.Server{
		Addr:         ":" + s.Config.Server.Port,
		Handler:      handler,
		ReadTimeout:  time.Duration(s.Config.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.Config.Server.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(s.Config.Server.IdleTimeout) * time.Second,
	}
}

func (s *Server) Start() error {
	if s.httpServer == nil {
		return errors.New("HTTP server not initialized")
	}

	log.Printf("Port: %v\n", s.Config.Server.Port)
	log.Printf("env: %v\n", s.Config.Primary.Env)
	log.Println("Server started")

	return s.httpServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown HTTP server: %w", err)
	}

	return nil
}
