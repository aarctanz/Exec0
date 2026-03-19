package middleware

import (
	"encoding/json"
	"net/http"
	"runtime/debug"

	"github.com/aarctanz/Exec0/internal/logger"
)

func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				logger.FromContext(r.Context()).Error().
					Interface("error", err).
					Str("stack", string(debug.Stack())).
					Msg("panic recovered")
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				json.NewEncoder(w).Encode(map[string]string{"error": "internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
