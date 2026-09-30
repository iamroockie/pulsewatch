package observability

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

type hostLimiter struct {
	next     HostLimiter
	acquired prometheus.Counter
	busy     prometheus.Counter
	failed   prometheus.Counter
}

func InstrumentHostLimiter(next HostLimiter, reg prometheus.Registerer) HostLimiter {
	slots := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "pulsewatch_host_slots_total",
		Help: "Attempts to take a per-host check slot by result.",
	}, []string{"result"})

	return &hostLimiter{
		next:     next,
		acquired: slots.WithLabelValues("acquired"),
		busy:     slots.WithLabelValues("busy"),
		failed:   slots.WithLabelValues("error"),
	}
}

func (l *hostLimiter) Acquire(ctx context.Context, c domain.Claim, now time.Time) (bool, error) {
	acquired, err := l.next.Acquire(ctx, c, now)
	switch {
	case err != nil:
		l.failed.Inc()
	case acquired:
		l.acquired.Inc()
	default:
		l.busy.Inc()
	}

	return acquired, err
}

func (l *hostLimiter) Release(ctx context.Context, c domain.Claim) error {
	return l.next.Release(ctx, c)
}
