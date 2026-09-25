package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/iamroockie/pulsewatch/internal/app"
	"github.com/iamroockie/pulsewatch/internal/config"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("run failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logOpts := slog.HandlerOptions{Level: cfg.LogLevel}
	var sloghandler slog.Handler
	switch cfg.AppEnv {
	case "local":
		logOpts.AddSource = true
		sloghandler = slog.NewTextHandler(os.Stdout, &logOpts)
	default:
		sloghandler = slog.NewJSONHandler(os.Stdout, &logOpts)
	}
	log := slog.New(sloghandler).With("env", cfg.AppEnv)
	slog.SetDefault(log)

	return app.Run(cfg, log)
}
