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
	"uuid"

	"github.com/iamroockie/plinth"

	"github.com/iamroockie/pulsewatch/internal/adapter/postgres"
	"github.com/iamroockie/pulsewatch/internal/config"
	"github.com/iamroockie/pulsewatch/internal/observability"
	"github.com/iamroockie/pulsewatch/internal/service"
	"github.com/iamroockie/pulsewatch/internal/transport/rest"
)

func RunAPI(cfg config.API, log *slog.Logger) error {
	runCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := connectPostgres(cfg.Postgres.DSN())
	if err != nil {
		return err
	}
	defer db.Close()

	monitorRepo := postgres.NewMonitors(db)
	monitors := service.NewMonitors(monitorRepo, now, uuid.NewV7)
	history := service.NewHistory(monitorRepo, postgres.NewChecks(db), now)
	probes := map[string]plinth.CheckFunc{"postgres": db.Ping}
	reg := observability.NewRegistry()

	svr := &http.Server{
		Addr:              cfg.HTTP.Addr(),
		Handler:           rest.NewRouter(log, reg, probes, monitors, history),
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
