package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/iamroockie/plinth"
	"github.com/iamroockie/plinth/middleware"

	"github.com/iamroockie/pulsewatch/internal/adapter/postgres"
	"github.com/iamroockie/pulsewatch/internal/config"
)

func Run(cfg config.Config, log *slog.Logger) error {
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	startupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := postgres.NewPool(startupCtx, cfg.Postgres.DSN())
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	svr := &http.Server{
		Addr:              cfg.HTTP.Addr(),
		Handler:           router(log, map[string]plinth.CheckFunc{"postgres": pool.Ping}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       time.Minute,
	}
	svrErr := make(chan error, 1)
	go func() {
		log.Info("http server running", "addr", svr.Addr)
		if err := svr.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			svrErr <- err
		}
	}()

	select {
	case err := <-svrErr:
		return fmt.Errorf("serve http: %w", err)
	case <-runCtx.Done():
		stop()
	}

	log.Info("shutdown started")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := svr.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}

	log.Info("shutdown completed")

	return nil
}

func router(log *slog.Logger, checks map[string]plinth.CheckFunc) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /healthz", plinth.Healthz())
	mux.Handle("GET /readyz", plinth.Readyz(2*time.Second, checks))

	mw := middleware.Chain(
		middleware.RequestID(),
		middleware.RequestLog(log, "/healthz", "/readyz"),
		middleware.ErrorLog(log),
		middleware.Recover(),
	)

	return mw(plinth.JSONMux(mux))
}
