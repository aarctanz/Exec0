package middleware

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/aarctanz/Exec0/internal/logger"
)

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

// Logging injects a request-scoped zerolog logger with a unique request_id
// into the context and logs each completed request.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := uuid.New().String()

		// Build a child logger with request-scoped fields
		l := logger.FromContext(r.Context()).With().
			Str("request_id", requestID).
			Logger()
		ctx := logger.WithContext(r.Context(), l)

		// Expose request_id in response headers for client-side correlation
		w.Header().Set("X-Request-ID", requestID)

		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r.WithContext(ctx))

		l.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", sw.status).
			Dur("duration", time.Since(start)).
			Msg("request completed")
	})
}
