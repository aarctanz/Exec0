package database

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/aarctanz/Exec0/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Database struct {
	pool *pgxpool.Pool
}

const DatabasePingTimeout = 10

func New(cfg *config.Config) (*Database, error) {
	hostPort := net.JoinHostPort(cfg.Database.Host, strconv.Itoa(cfg.Database.Port))

	encodedPassword := url.QueryEscape(cfg.Database.Password)

	dsn := fmt.Sprintf("postgres://%s:%s@%s/%s?sslmode=%s",
		cfg.Database.User,
		encodedPassword,
		hostPort,
		cfg.Database.Name,
		cfg.Database.SSLMode,
	)

	pgxPoolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse pgx pool config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(context.Background(), pgxPoolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create pgx pool: %w", err)
	}

	database := &Database{pool: pool}

	ctx, cancel := context.WithTimeout(context.Background(), DatabasePingTimeout*time.Second)
	defer cancel()

	if err = database.pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Println("connected to database")
	return database, nil
}
