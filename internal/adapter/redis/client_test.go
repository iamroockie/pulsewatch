package redis_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/iamroockie/pulsewatch/internal/adapter/redis"
)

func silentServer(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })

	go func() {
		var conns []net.Conn
		defer func() {
			for _, conn := range conns {
				conn.Close()
			}
		}()

		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conns = append(conns, conn)
		}
	}()

	return listener.Addr().String()
}

func TestNewClientGivesUpAtContextDeadline(t *testing.T) {
	addr := silentServer(t)
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()

	_, err := redis.NewClient(ctx, addr, "")
	elapsed := time.Since(start)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, elapsed, time.Second)
}
