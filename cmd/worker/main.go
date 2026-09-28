package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/iamroockie/pulsewatch/internal/app"
	"github.com/iamroockie/pulsewatch/internal/config"
	"github.com/iamroockie/pulsewatch/internal/observability"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("run failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load[config.Worker]()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	log := observability.NewLogger(cfg.AppEnv, cfg.LogLevel).With("app", "worker")
	slog.SetDefault(log)

	return app.RunWorker(cfg, log)
}
