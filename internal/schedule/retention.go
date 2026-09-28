package schedule

import (
	"context"
	"log/slog"
	"time"
)

type Retention struct {
	purger Purger
	keep   time.Duration
	every  time.Duration
	log    *slog.Logger
}

func NewRetention(p Purger, keep, every time.Duration, log *slog.Logger) *Retention {
	return &Retention{purger: p, keep: keep, every: every, log: log}
}

func (r *Retention) Run(ctx context.Context) {
	ticker := time.NewTicker(r.every)
	defer ticker.Stop()

	for {
		r.purge(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *Retention) purge(ctx context.Context) {
	deleted, err := r.purger.Purge(ctx, r.keep)
	if deleted > 0 {
		r.log.Info("checks purged", "deleted", deleted)
	}
	if err != nil && ctx.Err() == nil {
		r.log.Error("purge checks", "error", err)
	}
}
