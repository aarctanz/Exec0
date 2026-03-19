package logger

import (
	"context"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// Init configures the global zerolog logger based on environment.
// "local" or "development" uses pretty console output; everything else uses JSON.
func Init(env string) {
	zerolog.TimeFieldFormat = time.RFC3339Nano

	var logger zerolog.Logger
	if env == "local" || env == "development" {
		logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "15:04:05.000"}).
			With().Timestamp().Caller().Logger()
	} else {
		logger = zerolog.New(os.Stdout).
			With().Timestamp().Logger()
	}

	zerolog.DefaultContextLogger = &logger
}

// nopLogger is a fallback logger that discards output, used when Init hasn't been called.
var nopLogger = zerolog.Nop()

// FromContext returns the logger stored in the context, or the global logger.
// Falls back to a nop logger if Init hasn't been called (e.g., in tests).
func FromContext(ctx context.Context) *zerolog.Logger {
	l := zerolog.Ctx(ctx)
	if l.GetLevel() == zerolog.Disabled {
		if zerolog.DefaultContextLogger != nil {
			return zerolog.DefaultContextLogger
		}
		return &nopLogger
	}
	return l
}

// WithContext stores a logger in the context.
func WithContext(ctx context.Context, l zerolog.Logger) context.Context {
	return l.WithContext(ctx)
}
