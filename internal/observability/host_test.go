package observability_test

import (
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/iamroockie/pulsewatch/internal/observability"
)

func TestInstrumentHostLimiterCountsAcquireResults(t *testing.T) {
	tests := map[string]struct {
		acquired bool
		err      error
		want     map[string]float64
	}{
		"slot acquired": {
			acquired: true,
			err:      nil,
			want: map[string]float64{
				`pulsewatch_host_slots_total{result="acquired"}`: 1,
				`pulsewatch_host_slots_total{result="busy"}`:     0,
				`pulsewatch_host_slots_total{result="error"}`:    0,
			},
		},
		"host is busy": {
			acquired: false,
			err:      nil,
			want: map[string]float64{
				`pulsewatch_host_slots_total{result="acquired"}`: 0,
				`pulsewatch_host_slots_total{result="busy"}`:     1,
				`pulsewatch_host_slots_total{result="error"}`:    0,
			},
		},
		"limiter is unavailable": {
			acquired: false,
			err:      errors.New("redis is down"),
			want: map[string]float64{
				`pulsewatch_host_slots_total{result="acquired"}`: 0,
				`pulsewatch_host_slots_total{result="busy"}`:     0,
				`pulsewatch_host_slots_total{result="error"}`:    1,
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			reg := prometheus.NewRegistry()
			now := time.Now()
			c := newClaim(t, now)
			next := NewMockHostLimiter(gomock.NewController(t))
			next.EXPECT().Acquire(gomock.Any(), c, now).Return(test.acquired, test.err)
			limiter := observability.InstrumentHostLimiter(next, reg)

			got, err := limiter.Acquire(t.Context(), c, now)

			require.ErrorIs(t, err, test.err)
			assert.Equal(t, test.acquired, got)
			assert.Equal(t, test.want, samples(t, reg))
		})
	}
}

func TestInstrumentHostLimiterPassesReleaseThrough(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := newClaim(t, time.Now())
	errRelease := errors.New("redis is down")
	next := NewMockHostLimiter(gomock.NewController(t))
	next.EXPECT().Release(gomock.Any(), c).Return(errRelease)
	limiter := observability.InstrumentHostLimiter(next, reg)
	want := map[string]float64{
		`pulsewatch_host_slots_total{result="acquired"}`: 0,
		`pulsewatch_host_slots_total{result="busy"}`:     0,
		`pulsewatch_host_slots_total{result="error"}`:    0,
	}

	err := limiter.Release(t.Context(), c)

	require.ErrorIs(t, err, errRelease)
	assert.Equal(t, want, samples(t, reg))
}
