package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	goredis "github.com/redis/go-redis/v9"

	"github.com/iamroockie/pulsewatch/internal/adapter/postgres"
	"github.com/iamroockie/pulsewatch/internal/adapter/redis"
)

func connectPostgres(dsn string) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	return pool, nil
}

func connectRedis(addr, password string) (*goredis.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := redis.NewClient(ctx, addr, password)
	if err != nil {
		return nil, fmt.Errorf("connect redis: %w", err)
	}

	return client, nil
}

func serveMetrics(
	ctx context.Context,
	addr string,
	reg *prometheus.Registry,
	log *slog.Logger,
) (*http.Server, error) {
	ln, err := new(net.ListenConfig).Listen(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen metrics: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorLog:      slog.NewLogLogger(log.Handler(), slog.LevelError),
		ErrorHandling: promhttp.ContinueOnError,
	}))
	svr := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	go func() {
		log.Info("metrics server running", "addr", addr)
		if err := svr.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			log.Error("serve metrics", "error", err)
		}
	}()

	return svr, nil
}

func now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}
