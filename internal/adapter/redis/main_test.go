package redis_test

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"

	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/iamroockie/pulsewatch/internal/adapter/redis"
)

const addrEnv = "PULSEWATCH_TEST_REDIS_ADDR"

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	flag.Parse()
	if testing.Short() {
		return m.Run()
	}

	ctx := context.Background()

	container, err := tcredis.Run(ctx, "redis:8.10.2-trixie")
	defer func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			fmt.Fprintln(os.Stderr, "terminate redis:", err)
		}
	}()
	if err != nil {
		fmt.Fprintln(os.Stderr, "start redis:", err)
		return 1
	}

	addr, err := container.Endpoint(ctx, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "redis address:", err)
		return 1
	}

	if err := os.Setenv(addrEnv, addr); err != nil {
		fmt.Fprintln(os.Stderr, "set redis address:", err)
		return 1
	}

	return m.Run()
}

func newClient(t *testing.T) *goredis.Client {
	t.Helper()

	if testing.Short() {
		t.Skip("redis adapter tests need docker")
	}

	client, err := redis.NewClient(t.Context(), os.Getenv(addrEnv), "")
	require.NoError(t, err)
	t.Cleanup(func() { client.Close() })

	return client
}
