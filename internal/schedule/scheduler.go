package schedule

import (
	"context"
	"log/slog"
	"time"
)

type Scheduler struct {
	claimer Claimer
	workers Workers
	tick    time.Duration
	log     *slog.Logger
}

func NewScheduler(c Claimer, w Workers, tick time.Duration, log *slog.Logger) *Scheduler {
	return &Scheduler{claimer: c, workers: w, tick: tick, log: log}
}

func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.tick)
	defer ticker.Stop()

	var freed <-chan struct{}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-freed:
		}

		freed = nil
		if s.dispatch(ctx) {
			freed = s.workers.Freed()
		}
	}
}

func (s *Scheduler) dispatch(ctx context.Context) (backlog bool) {
	if ctx.Err() != nil {
		return false
	}

	free := s.workers.Free()
	if free == 0 {
		return true
	}

	claimCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	claims, err := s.claimer.ClaimDue(claimCtx, free)
	if err != nil {
		s.log.Error("claim due monitors", "error", err)
		return false
	}

	for _, c := range claims {
		s.workers.Go(c)
	}

	return len(claims) == free
}
