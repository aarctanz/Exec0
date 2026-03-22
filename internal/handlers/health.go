package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/aarctanz/Exec0/internal/util"
)

type HealthHandler struct {
	pool        *pgxpool.Pool
	redisClient *redis.Client
}

func NewHealthHandler(pool *pgxpool.Pool, redisAddr string) *HealthHandler {
	return &HealthHandler{
		pool: pool,
		redisClient: redis.NewClient(&redis.Options{
			Addr: redisAddr,
		}),
	}
}

func (h *HealthHandler) Close() error {
	return h.redisClient.Close()
}

func (h *HealthHandler) Check(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()

	result := map[string]any{
		"status": "ok",
		"services": map[string]string{
			"postgres": "up",
			"redis":    "up",
		},
	}

	services := result["services"].(map[string]string)
	healthy := true

	if err := h.pool.Ping(ctx); err != nil {
		services["postgres"] = "down"
		healthy = false
	}

	if err := h.redisClient.Ping(ctx).Err(); err != nil {
		services["redis"] = "down"
		healthy = false
	}

	if !healthy {
		result["status"] = "degraded"
		util.JSON(w, http.StatusServiceUnavailable, result)
		return
	}

	util.JSON(w, http.StatusOK, result)
}
