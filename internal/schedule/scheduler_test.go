package schedule_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/domain/domaintest"
	"github.com/iamroockie/pulsewatch/internal/schedule"
)

const tick = time.Second

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

func newScheduler(
	t *testing.T, log *slog.Logger,
) (*schedule.Scheduler, *MockClaimer, *MockWorkers) {
	t.Helper()

	ctrl := gomock.NewController(t)
	claimer := NewMockClaimer(ctrl)
	workers := NewMockWorkers(ctrl)

	return schedule.NewScheduler(claimer, workers, tick, log), claimer, workers
}

func newClaim(t *testing.T) domain.Claim {
	t.Helper()

	return domain.Claim{
		Monitor: domaintest.NewMonitor(t, time.Now()),
		Until:   time.Now().Add(time.Minute),
	}
}

func never() <-chan struct{} {
	return make(chan struct{})
}

func runFor(t *testing.T, s *schedule.Scheduler, d time.Duration) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	time.Sleep(d)
	cancel()
	<-done
}

func TestSchedulerWaitsForFirstTick(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _, _ := newScheduler(t, slog.New(slog.DiscardHandler))

		runFor(t, s, tick/2)
	})
}

func TestSchedulerDispatchesClaimedMonitors(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, claimer, workers := newScheduler(t, slog.New(slog.DiscardHandler))
		claims := []domain.Claim{newClaim(t), newClaim(t)}
		workers.EXPECT().Free().Return(len(claims))
		claimer.EXPECT().ClaimDue(gomock.Any(), len(claims)).Return(claims, nil)
		workers.EXPECT().Go(claims[0])
		workers.EXPECT().Go(claims[1])
		workers.EXPECT().Freed().Return(never())

		runFor(t, s, tick+tick/2)
	})
}

func TestSchedulerDoesNotClaimWhenWorkersAreBusy(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, _, workers := newScheduler(t, slog.New(slog.DiscardHandler))
		workers.EXPECT().Free().Return(0).Times(3)
		workers.EXPECT().Freed().Return(never()).Times(3)

		runFor(t, s, 3*tick+tick/2)
	})
}

func TestSchedulerDispatchesAsSoonAsWorkerFrees(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, claimer, workers := newScheduler(t, slog.New(slog.DiscardHandler))
		first, second := newClaim(t), newClaim(t)
		freed := make(chan struct{}, 1)
		gomock.InOrder(
			workers.EXPECT().Free().Return(1),
			claimer.EXPECT().ClaimDue(gomock.Any(), 1).Return([]domain.Claim{first}, nil),
			workers.EXPECT().Go(first).Do(func(domain.Claim) { freed <- struct{}{} }),
			workers.EXPECT().Freed().Return(freed),
			workers.EXPECT().Free().Return(1),
			claimer.EXPECT().ClaimDue(gomock.Any(), 1).Return([]domain.Claim{second}, nil),
			workers.EXPECT().Go(second),
			workers.EXPECT().Freed().Return(freed),
		)

		runFor(t, s, tick+tick/2)
	})
}

func TestSchedulerWaitsForTickWithoutBacklog(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, claimer, workers := newScheduler(t, slog.New(slog.DiscardHandler))
		c := newClaim(t)
		freed := make(chan struct{}, 1)
		workers.EXPECT().Free().Return(2)
		claimer.EXPECT().ClaimDue(gomock.Any(), 2).Return([]domain.Claim{c}, nil)
		workers.EXPECT().Go(c).Do(func(domain.Claim) { freed <- struct{}{} })

		runFor(t, s, tick+tick/2)
	})
}

func TestSchedulerKeepsRunningAfterClaimError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var buf bytes.Buffer
		s, claimer, workers := newScheduler(t, levelAndMessageLog(&buf))
		c := newClaim(t)
		gomock.InOrder(
			workers.EXPECT().Free().Return(1),
			claimer.EXPECT().ClaimDue(gomock.Any(), 1).Return(nil, errors.New("storage is down")),
			workers.EXPECT().Free().Return(1),
			claimer.EXPECT().ClaimDue(gomock.Any(), 1).Return([]domain.Claim{c}, nil),
			workers.EXPECT().Go(c),
			workers.EXPECT().Freed().Return(never()),
		)

		runFor(t, s, 2*tick+tick/2)

		assert.Equal(t, "level=ERROR msg=\"claim due monitors\"\n", buf.String())
	})
}

func TestSchedulerDispatchesClaimFinishedDuringShutdown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s, claimer, workers := newScheduler(t, slog.New(slog.DiscardHandler))
		c := newClaim(t)
		workers.EXPECT().Free().Return(1)
		claimer.EXPECT().ClaimDue(gomock.Any(), 1).DoAndReturn(
			func(ctx context.Context, _ int) ([]domain.Claim, error) {
				time.Sleep(tick)

				return []domain.Claim{c}, ctx.Err()
			},
		)
		workers.EXPECT().Go(c)
		workers.EXPECT().Freed().Return(never()).AnyTimes()

		runFor(t, s, tick+tick/2)
	})
}
