package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/iamroockie/pulsewatch/internal/adapter/httpcheck"
	"github.com/iamroockie/pulsewatch/internal/adapter/postgres"
	"github.com/iamroockie/pulsewatch/internal/adapter/redis"
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

	rdb, err := connectRedis(cfg.Redis.Addr(), cfg.Redis.Password)
	if err != nil {
		return err
	}
	defer rdb.Close()

	retryBackoff := httpcheck.Backoff{Base: 500 * time.Millisecond, Max: 5 * time.Second}
	checker := httpcheck.NewChecker(retryBackoff)
	hosts := redis.NewHostLimiter(rdb, 5)
	checks := service.NewChecks(postgres.NewChecks(db), checker, hosts, now, retryBackoff.Max, log)
	workers := pool.NewPool(cfg.WorkerCount, checks, log)

	var retention sync.WaitGroup
	retention.Go(func() {
		schedule.NewRetention(checks, 30*24*time.Hour, time.Hour, log).Run(runCtx)
	})

	log.Info("scheduler running", "workers", cfg.WorkerCount)
	schedule.NewScheduler(checks, workers, time.Second, log).Run(runCtx)
	stop()
	retention.Wait()

	log.Info("shutdown started")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := workers.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("drain workers: %w", err)
	}

	log.Info("shutdown completed")

	return nil
}
