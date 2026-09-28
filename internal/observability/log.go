package observability

import (
	"log/slog"
	"os"
)

func NewLogger(env string, level slog.Level) *slog.Logger {
	opts := slog.HandlerOptions{Level: level}

	var handler slog.Handler
	switch env {
	case "local":
		opts.AddSource = true
		handler = slog.NewTextHandler(os.Stdout, &opts)
	default:
		handler = slog.NewJSONHandler(os.Stdout, &opts)
	}

	return slog.New(handler).With("env", env)
}
