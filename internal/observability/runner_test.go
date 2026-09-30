package observability_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/observability"
)

func TestInstrumentRunnerTracksBusyWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := prometheus.NewRegistry()
		release := make(chan struct{})
		next := NewMockRunner(gomock.NewController(t))
		next.EXPECT().Run(gomock.Any(), gomock.Any()).DoAndReturn(
			func(context.Context, domain.Claim) error {
				<-release

				return nil
			},
		).Times(2)
		runner := observability.InstrumentRunner(next, 4, reg)
		claims := []domain.Claim{newClaim(t, time.Now()), newClaim(t, time.Now())}
		running := map[string]float64{
			"pulsewatch_check_lag_seconds_count": 2,
			"pulsewatch_check_lag_seconds_sum":   0,
			"pulsewatch_workers":                 4,
			"pulsewatch_workers_busy":            2,
		}
		finished := map[string]float64{
			"pulsewatch_check_lag_seconds_count": 2,
			"pulsewatch_check_lag_seconds_sum":   0,
			"pulsewatch_workers":                 4,
			"pulsewatch_workers_busy":            0,
		}

		for _, c := range claims {
			go func() {
				assert.NoError(t, runner.Run(t.Context(), c))
			}()
		}
		synctest.Wait()

		assert.Equal(t, running, samples(t, reg))
		close(release)
		synctest.Wait()
		assert.Equal(t, finished, samples(t, reg))
	})
}

func TestInstrumentRunnerObservesLag(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := prometheus.NewRegistry()
		c := newClaim(t, time.Now().Add(-3*time.Second))
		errCheck := errors.New("storage is down")
		next := NewMockRunner(gomock.NewController(t))
		next.EXPECT().Run(gomock.Any(), c).Return(errCheck)
		runner := observability.InstrumentRunner(next, 1, reg)
		want := map[string]float64{
			"pulsewatch_check_lag_seconds_count": 1,
			"pulsewatch_check_lag_seconds_sum":   3,
			"pulsewatch_workers":                 1,
			"pulsewatch_workers_busy":            0,
		}

		err := runner.Run(t.Context(), c)

		require.ErrorIs(t, err, errCheck)
		assert.Equal(t, want, samples(t, reg))
	})
}

func TestInstrumentRunnerFreesWorkerAfterPanic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := prometheus.NewRegistry()
		c := newClaim(t, time.Now())
		next := NewMockRunner(gomock.NewController(t))
		next.EXPECT().Run(gomock.Any(), c).DoAndReturn(
			func(context.Context, domain.Claim) error {
				panic("broken check")
			},
		)
		runner := observability.InstrumentRunner(next, 1, reg)
		want := map[string]float64{
			"pulsewatch_check_lag_seconds_count": 1,
			"pulsewatch_check_lag_seconds_sum":   0,
			"pulsewatch_workers":                 1,
			"pulsewatch_workers_busy":            0,
		}

		assert.Panics(t, func() {
			_ = runner.Run(t.Context(), c)
		})

		assert.Equal(t, want, samples(t, reg))
	})
}
