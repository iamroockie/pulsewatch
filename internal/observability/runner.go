package observability

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

type runner struct {
	next Runner
	busy prometheus.Gauge
	lag  prometheus.Histogram
}

func InstrumentRunner(next Runner, workers uint, reg prometheus.Registerer) Runner {
	factory := promauto.With(reg)
	factory.NewGauge(prometheus.GaugeOpts{
		Name: "pulsewatch_workers",
		Help: "Workers in the pool.",
	}).Set(float64(workers))

	return &runner{
		next: next,
		busy: factory.NewGauge(prometheus.GaugeOpts{
			Name: "pulsewatch_workers_busy",
			Help: "Workers running a check.",
		}),
		lag: factory.NewHistogram(prometheus.HistogramOpts{
			Name:    "pulsewatch_check_lag_seconds",
			Help:    "Delay between the time a check was due and the time it started.",
			Buckets: []float64{.1, .25, .5, 1, 2.5, 5, 10, 30, 60, 300},
		}),
	}
}

func (r *runner) Run(ctx context.Context, c domain.Claim) error {
	r.lag.Observe(time.Since(c.DueAt).Seconds())
	r.busy.Inc()
	defer r.busy.Dec()

	return r.next.Run(ctx, c)
}
