package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/aarctanz/Exec0/internal/metrics"
)

// Metrics records Prometheus HTTP metrics for each request.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(sw, r.WithContext(r.Context()))

		route := normalizeRoute(r)
		method := r.Method
		statusClass := fmt.Sprintf("%dxx", sw.status/100)

		metrics.HTTPRequestsTotal.WithLabelValues(route, method, statusClass).Inc()
		metrics.HTTPRequestDuration.WithLabelValues(route, method).Observe(time.Since(start).Seconds())
	})
}

// normalizeRoute maps the request to a stable route pattern to avoid
// high-cardinality labels from path parameters like /submissions/123.
func normalizeRoute(r *http.Request) string {
	// Go 1.22+ stores the matched pattern
	if pat := r.Pattern; pat != "" {
		return pat
	}
	return r.URL.Path
}
