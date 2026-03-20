package middleware

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/aarctanz/Exec0/internal/logger"
)

// formatDuration returns a human-readable duration with appropriate units.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%.0fµs", float64(d.Microseconds()))
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

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
		logCtx := logger.FromContext(r.Context()).With().
			Str("request_id", requestID)

		// Correlate logs with OTel traces
		if sc := trace.SpanFromContext(r.Context()).SpanContext(); sc.HasTraceID() {
			logCtx = logCtx.
				Str("trace_id", sc.TraceID().String()).
				Str("span_id", sc.SpanID().String())
		}

		l := logCtx.Logger()
		ctx := logger.WithContext(r.Context(), l)

		// Expose request_id in response headers for client-side correlation
		w.Header().Set("X-Request-ID", requestID)

		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r.WithContext(ctx))

		l.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", sw.status).
			Str("duration", formatDuration(time.Since(start))).
			Msg("request completed")
	})
}
