package app

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

func now() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}
