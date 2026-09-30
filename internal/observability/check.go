package observability

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

type checker struct {
	next Checker
	up   prometheus.Observer
	down prometheus.Observer
}

func InstrumentChecker(next Checker, reg prometheus.Registerer) Checker {
	duration := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "pulsewatch_check_duration_seconds",
		Help:    "Time spent checking a monitor, retries included.",
		Buckets: []float64{.05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60, 120, 300},
	}, []string{"result"})

	return &checker{
		next: next,
		up:   duration.WithLabelValues("up"),
		down: duration.WithLabelValues("down"),
	}
}

func (c *checker) Check(ctx context.Context, s domain.CheckSettings) (domain.CheckResult, error) {
	start := time.Now()
	result, err := c.next.Check(ctx, s)
	if err != nil {
		return result, err
	}

	observer := c.down
	if result.IsUp {
		observer = c.up
	}
	observer.Observe(time.Since(start).Seconds())

	return result, nil
}
