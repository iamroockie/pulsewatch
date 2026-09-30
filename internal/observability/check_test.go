package observability_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/domain/domaintest"
	"github.com/iamroockie/pulsewatch/internal/observability"
)

func TestInstrumentCheckerObservesDurationByResult(t *testing.T) {
	tests := map[string]struct {
		result domain.CheckResult
		err    error
		want   map[string]float64
	}{
		"site is up": {
			result: domain.CheckResult{IsUp: true, StatusCode: 200, Attempts: 1},
			err:    nil,
			want: map[string]float64{
				`pulsewatch_check_duration_seconds_count{result="down"}`: 0,
				`pulsewatch_check_duration_seconds_sum{result="down"}`:   0,
				`pulsewatch_check_duration_seconds_count{result="up"}`:   1,
				`pulsewatch_check_duration_seconds_sum{result="up"}`:     2,
			},
		},
		"site is down": {
			result: domain.CheckResult{Attempts: 3, Error: "timeout"},
			err:    nil,
			want: map[string]float64{
				`pulsewatch_check_duration_seconds_count{result="down"}`: 1,
				`pulsewatch_check_duration_seconds_sum{result="down"}`:   2,
				`pulsewatch_check_duration_seconds_count{result="up"}`:   0,
				`pulsewatch_check_duration_seconds_sum{result="up"}`:     0,
			},
		},
		"check interrupted": {
			result: domain.CheckResult{},
			err:    context.Canceled,
			want: map[string]float64{
				`pulsewatch_check_duration_seconds_count{result="down"}`: 0,
				`pulsewatch_check_duration_seconds_sum{result="down"}`:   0,
				`pulsewatch_check_duration_seconds_count{result="up"}`:   0,
				`pulsewatch_check_duration_seconds_sum{result="up"}`:     0,
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				reg := prometheus.NewRegistry()
				settings := domaintest.ValidSettings()
				next := NewMockChecker(gomock.NewController(t))
				next.EXPECT().Check(gomock.Any(), settings).DoAndReturn(
					func(context.Context, domain.CheckSettings) (domain.CheckResult, error) {
						time.Sleep(2 * time.Second)

						return test.result, test.err
					},
				)
				checker := observability.InstrumentChecker(next, reg)

				got, err := checker.Check(t.Context(), settings)

				require.ErrorIs(t, err, test.err)
				assert.Equal(t, test.result, got)
				assert.Equal(t, test.want, samples(t, reg))
			})
		})
	}
}
