package pool_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/domain/domaintest"
	"github.com/iamroockie/pulsewatch/internal/pool"
)

func discardLog() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func levelAndMessageLog(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey || a.Key == slog.MessageKey {
				return a
			}

			return slog.Attr{}
		},
	}))
}

func newClaim(t *testing.T) domain.Claim {
	t.Helper()

	return domain.Claim{
		Monitor: domaintest.NewMonitor(t, time.Now()),
		Until:   time.Now().Add(time.Minute),
	}
}

func newClaims(t *testing.T, n int) []domain.Claim {
	t.Helper()

	claims := make([]domain.Claim, 0, n)
	for range n {
		claims = append(claims, newClaim(t))
	}

	return claims
}

func TestPoolLimitsConcurrency(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const size = 2
		release := make(chan struct{})
		var running atomic.Int32
		runner := NewMockRunner(gomock.NewController(t))
		runner.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
			func(context.Context, domain.Claim) error {
				running.Add(1)
				<-release
				running.Add(-1)

				return nil
			},
		).Times(size + 1)
		workers := pool.NewPool(size, runner, discardLog())
		submitted := make(chan struct{})

		go func() {
			for _, c := range newClaims(t, size+1) {
				workers.Go(c)
			}
			close(submitted)
		}()
		synctest.Wait()

		assert.Equal(t, int32(size), running.Load())
		assert.Zero(t, workers.Free())
		close(release)
		<-submitted
		require.NoError(t, workers.Shutdown(t.Context()))
		assert.Equal(t, size, workers.Free())
	})
}

func TestPoolSignalsFreedSlot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runner := NewMockRunner(gomock.NewController(t))
		runner.EXPECT().Run(gomock.Any(), gomock.Any()).Return(nil).Times(3)
		workers := pool.NewPool(3, runner, discardLog())

		for _, c := range newClaims(t, 3) {
			workers.Go(c)
		}
		synctest.Wait()

		assert.Len(t, workers.Freed(), 1)
		require.NoError(t, workers.Shutdown(t.Context()))
	})
}

func TestPoolShutdownWaitsForRunningChecks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const checkDuration = time.Minute
		var checkErr error
		runner := NewMockRunner(gomock.NewController(t))
		runner.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, _ domain.Claim) error {
				time.Sleep(checkDuration)
				checkErr = ctx.Err()

				return nil
			},
		)
		workers := pool.NewPool(1, runner, discardLog())
		workers.Go(newClaim(t))
		start := time.Now()

		err := workers.Shutdown(t.Context())

		require.NoError(t, err)
		assert.Equal(t, checkDuration, time.Since(start))
		assert.NoError(t, checkErr)
	})
}

func TestPoolShutdownCancelsChecksAfterDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const deadline = 5 * time.Second
		var checkErr error
		runner := NewMockRunner(gomock.NewController(t))
		runner.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, _ domain.Claim) error {
				<-ctx.Done()
				checkErr = ctx.Err()

				return checkErr
			},
		)
		workers := pool.NewPool(1, runner, discardLog())
		workers.Go(newClaim(t))
		ctx, cancel := context.WithTimeout(t.Context(), deadline)
		t.Cleanup(cancel)
		start := time.Now()

		err := workers.Shutdown(ctx)

		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, deadline, time.Since(start))
		assert.ErrorIs(t, checkErr, context.Canceled)
	})
}

func TestPoolLogsRunErrors(t *testing.T) {
	tests := map[string]struct {
		err  error
		want string
	}{
		"success is silent": {
			err:  nil,
			want: "",
		},
		"interruption is a warning": {
			err:  fmt.Errorf("check monitor: %w", context.Canceled),
			want: "level=WARN msg=\"check interrupted\"\n",
		},
		"failure is an error": {
			err:  errors.New("storage is down"),
			want: "level=ERROR msg=\"check failed\"\n",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			runner := NewMockRunner(gomock.NewController(t))
			runner.EXPECT().Run(gomock.Any(), gomock.Any()).Return(test.err)
			workers := pool.NewPool(1, runner, levelAndMessageLog(&buf))

			workers.Go(newClaim(t))

			require.NoError(t, workers.Shutdown(t.Context()))
			assert.Equal(t, test.want, buf.String())
		})
	}
}

func TestPoolRecoversPanickingCheck(t *testing.T) {
	var buf bytes.Buffer
	runner := NewMockRunner(gomock.NewController(t))
	runner.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
		func(context.Context, domain.Claim) error {
			panic("broken check")
		},
	)
	workers := pool.NewPool(1, runner, levelAndMessageLog(&buf))

	workers.Go(newClaim(t))

	require.NoError(t, workers.Shutdown(t.Context()))
	assert.Equal(t, "level=ERROR msg=\"check panicked\"\n", buf.String())
	assert.Equal(t, 1, workers.Free())
}
