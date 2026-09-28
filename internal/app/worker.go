package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iamroockie/pulsewatch/internal/adapter/httpcheck"
	"github.com/iamroockie/pulsewatch/internal/adapter/postgres"
	"github.com/iamroockie/pulsewatch/internal/config"
	"github.com/iamroockie/pulsewatch/internal/pool"
	"github.com/iamroockie/pulsewatch/internal/schedule"
	"github.com/iamroockie/pulsewatch/internal/service"
)

func RunWorker(cfg config.Worker, log *slog.Logger) error {
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := connectPostgres(cfg.Postgres.DSN())
	if err != nil {
		return err
	}
	defer db.Close()

	retryBackoff := httpcheck.Backoff{Base: 500 * time.Millisecond, Max: 5 * time.Second}
	checker := httpcheck.NewChecker(retryBackoff)
	checks := service.NewChecks(postgres.NewChecks(db), checker, now, retryBackoff.Max)
	workers := pool.NewPool(cfg.WorkerCount, checks, log)

	log.Info("scheduler running", "workers", cfg.WorkerCount)
	schedule.NewScheduler(checks, workers, time.Second, log).Run(runCtx)
	stop()

	log.Info("shutdown started")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := workers.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("drain workers: %w", err)
	}

	log.Info("shutdown completed")

	return nil
}
