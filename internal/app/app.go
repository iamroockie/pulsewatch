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

	"github.com/iamroockie/pulsewatch/internal/config"
)

func Run(cfg config.Config, log *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	svr := &http.Server{
		Addr:              cfg.HTTP.Addr(),
		Handler:           router(log),
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
	case <-ctx.Done():
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

func router(log *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	mux.Handle("GET /healthz", healthz())

	mw := middleware.Chain(
		middleware.RequestID(),
		middleware.RequestLog(log, "/healthz"),
		middleware.ErrorLog(log),
		middleware.Recover(),
	)

	return mw(plinth.JSONMux(mux))
}

func healthz() http.Handler {
	return plinth.RespondJSON(func(_ *http.Request) (*plinth.Response, error) {
		return plinth.NewResponse(http.StatusOK, map[string]string{"status": "ok"}), nil
	})
}
