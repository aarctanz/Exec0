package server

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/aarctanz/Exec0/internal/handlers"
	"github.com/aarctanz/Exec0/internal/middleware"
	"github.com/aarctanz/Exec0/internal/services"
)

type RouteResources struct {
	Handler    http.Handler
	Health     *handlers.HealthHandler
	Monitoring *handlers.MonitoringHandler
}

func SetupRoutes(pool *pgxpool.Pool, svc *services.Services, allowedIPs []string, redisAddr string) *RouteResources {
	mux := http.NewServeMux()

	health := handlers.NewHealthHandler(pool, redisAddr)
	languages := handlers.NewLanguagesHandler(svc.LanguagesService)
	submissions := handlers.NewSubmissionsHandler(svc.SubmissionsService)

	mux.HandleFunc("GET /health", health.Check)

	mux.HandleFunc("GET /languages", languages.List)
	mux.HandleFunc("GET /languages/{id}", languages.Get)

	mux.HandleFunc("GET /submissions", submissions.List)
	mux.HandleFunc("POST /submissions", submissions.Create)
	mux.HandleFunc("POST /submissions/batch", submissions.CreateBatch)
	mux.HandleFunc("GET /submissions/{id}", submissions.Get)

	// Queue monitoring API
	monitoring := handlers.NewMonitoringHandler(redisAddr)
	mux.HandleFunc("GET /monitoring/queues", monitoring.Queues)
	mux.HandleFunc("GET /monitoring/history", monitoring.History)

	// Prometheus metrics
	mux.Handle("GET /metrics", promhttp.Handler())

	// Middleware chain: recovery → metrics → otelhttp → logging → IP allowlist → router
	var handler http.Handler = mux
	if len(allowedIPs) > 0 {
		handler = middleware.IPAllowlist(allowedIPs)(handler)
	}
	handler = middleware.Logging(handler)
	handler = otelhttp.NewHandler(handler, "http.request")
	handler = middleware.Metrics(handler)
	handler = middleware.Recovery(handler)

	return &RouteResources{
		Handler:    handler,
		Health:     health,
		Monitoring: monitoring,
	}
}
