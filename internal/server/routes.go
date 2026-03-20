package server

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/aarctanz/Exec0/internal/handlers"
	"github.com/aarctanz/Exec0/internal/middleware"
	"github.com/aarctanz/Exec0/internal/services"
)

func SetupRoutes(svc *services.Services, corsOrigins []string, redisAddr string) http.Handler {
	mux := http.NewServeMux()

	languages := handlers.NewLanguagesHandler(svc.LanguagesService)
	submissions := handlers.NewSubmissionsHandler(svc.SubmissionsService)

	mux.HandleFunc("GET /health", handlers.Health)

	mux.HandleFunc("GET /languages", languages.List)
	mux.HandleFunc("GET /languages/{id}", languages.Get)

	mux.HandleFunc("GET /submissions", submissions.List)
	mux.HandleFunc("POST /submissions", submissions.Create)
	mux.HandleFunc("GET /submissions/{id}", submissions.Get)

	// Queue monitoring API
	monitoring := handlers.NewMonitoringHandler(redisAddr)
	mux.HandleFunc("GET /monitoring/queues", monitoring.Queues)
	mux.HandleFunc("GET /monitoring/history", monitoring.History)

	// Prometheus metrics
	mux.Handle("GET /metrics", promhttp.Handler())

	// Middleware chain: recovery → metrics → otelhttp → logging → CORS → router
	var handler http.Handler = mux
	handler = middleware.CORS(corsOrigins)(handler)
	handler = middleware.Logging(handler)
	handler = otelhttp.NewHandler(handler, "http.request")
	handler = middleware.Metrics(handler)
	handler = middleware.Recovery(handler)

	return handler
}
