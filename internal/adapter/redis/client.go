package redis

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"
)

func NewClient(ctx context.Context, addr, password string) (*goredis.Client, error) {
	client := goredis.NewClient(&goredis.Options{
		Addr:                  addr,
		Password:              password,
		ContextTimeoutEnabled: true,
	})

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	return client, nil
}
