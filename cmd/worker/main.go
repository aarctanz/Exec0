package main

import (
	"log"

	"github.com/aarctanz/Exec0/internal/config"
	"github.com/aarctanz/Exec0/internal/queue"
)

const defaultConcurrency = 10

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatal("failed to load config: " + err.Error())
	}

	srv := queue.NewServer(cfg.Redis.Address, defaultConcurrency)
	mux := queue.NewServeMux()

	log.Println("Starting worker with concurrency", defaultConcurrency)

	if err := srv.Run(mux); err != nil {
		log.Fatal("failed to run worker: " + err.Error())
	}
}
