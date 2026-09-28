package service_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/domain/domaintest"
	"github.com/iamroockie/pulsewatch/internal/service"
)

const retryDelay = 5 * time.Second

func discardLog() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func levelAndMessageLog(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if a.Key == slog.LevelKey || a.Key == slog.MessageKey {
				return a
			}

			return slog.Attr{}
		},
	}))
}

func newChecks(
	t *testing.T,
) (*service.Checks, *MockCheckRepository, *MockChecker, *MockHostLimiter) {
	t.Helper()

	return newChecksWith(t, fixedNow, discardLog())
}

func newChecksWith(
	t *testing.T,
	clock func() time.Time,
	log *slog.Logger,
) (*service.Checks, *MockCheckRepository, *MockChecker, *MockHostLimiter) {
	t.Helper()

	ctrl := gomock.NewController(t)
	repo := NewMockCheckRepository(ctrl)
	checker := NewMockChecker(ctrl)
	hosts := NewMockHostLimiter(ctrl)

	return service.NewChecks(repo, checker, hosts, clock, retryDelay, log), repo, checker, hosts
}

func newClaim(t *testing.T) domain.Claim {
	t.Helper()

	return domain.Claim{
		Monitor: domaintest.NewMonitor(t, fixedNow()),
		Until:   fixedNow().Add(time.Minute),
	}
}

func boundedContext() gomock.Matcher {
	return gomock.Cond(func(ctx context.Context) bool {
		_, hasDeadline := ctx.Deadline()

		return hasDeadline
	})
}

func livePersistContext() gomock.Matcher {
	return gomock.Cond(func(ctx context.Context) bool {
		_, hasDeadline := ctx.Deadline()

		return ctx.Err() == nil && hasDeadline
	})
}

func grantSlot(hosts *MockHostLimiter, c domain.Claim) {
	hosts.EXPECT().Acquire(boundedContext(), c, fixedNow()).Return(true, nil)
	hosts.EXPECT().Release(livePersistContext(), c).Return(nil)
}

func TestChecksClaimDue(t *testing.T) {
	svc, repo, _, _ := newChecks(t)
	want := []domain.Claim{newClaim(t)}
	repo.EXPECT().
		ClaimDue(gomock.Any(), fixedNow(), retryDelay, 10*time.Second, 3).
		Return(want, nil)

	got, err := svc.ClaimDue(t.Context(), 3)

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestChecksClaimDueRepositoryError(t *testing.T) {
	svc, repo, _, _ := newChecks(t)
	errStorage := errors.New("storage is down")
	repo.EXPECT().
		ClaimDue(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, errStorage)

	got, err := svc.ClaimDue(t.Context(), 3)

	require.ErrorIs(t, err, errStorage)
	assert.Nil(t, got)
}

func TestChecksRun(t *testing.T) {
	tests := map[string]struct {
		recordErr error
	}{
		"recorded":   {recordErr: nil},
		"claim lost": {recordErr: domain.ErrClaimLost},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			svc, repo, checker, hosts := newChecks(t)
			c := newClaim(t)
			grantSlot(hosts, c)
			result := domain.CheckResult{IsUp: true, StatusCode: 200, Attempts: 1}
			check := &domain.Check{MonitorID: c.Monitor.ID, CheckedAt: fixedNow(), Result: result}
			checker.EXPECT().Check(gomock.Any(), c.Monitor.Settings).Return(result, nil)
			repo.EXPECT().Record(livePersistContext(), check, c.Until).Return(test.recordErr)

			err := svc.Run(t.Context(), c)

			assert.NoError(t, err)
		})
	}
}

func TestChecksRunLimitsCheckToClaim(t *testing.T) {
	svc, repo, checker, hosts := newChecks(t)
	c := newClaim(t)
	grantSlot(hosts, c)
	var deadline time.Time
	checker.EXPECT().Check(gomock.Any(), c.Monitor.Settings).DoAndReturn(
		func(ctx context.Context, _ domain.CheckSettings) (domain.CheckResult, error) {
			deadline, _ = ctx.Deadline()

			return domain.CheckResult{Attempts: 1}, nil
		},
	)
	repo.EXPECT().Record(gomock.Any(), gomock.Any(), c.Until).Return(nil)

	err := svc.Run(t.Context(), c)

	require.NoError(t, err)
	assert.Equal(t, c.Until.Add(-5*time.Second), deadline)
}

func TestChecksRunReadsClockBeforeCheck(t *testing.T) {
	reads := 0
	clock := func() time.Time {
		reads++

		return fixedNow().Add(time.Duration(reads) * time.Second)
	}
	svc, repo, checker, hosts := newChecksWith(t, clock, discardLog())
	c := newClaim(t)
	var want, got time.Time
	hosts.EXPECT().Acquire(gomock.Any(), c, gomock.Any()).Return(true, nil)
	hosts.EXPECT().Release(gomock.Any(), c).Return(nil)
	checker.EXPECT().Check(gomock.Any(), c.Monitor.Settings).DoAndReturn(
		func(context.Context, domain.CheckSettings) (domain.CheckResult, error) {
			want = fixedNow().Add(time.Duration(reads) * time.Second)

			return domain.CheckResult{Attempts: 1}, nil
		},
	)
	repo.EXPECT().Record(gomock.Any(), gomock.Any(), c.Until).DoAndReturn(
		func(_ context.Context, check *domain.Check, _ time.Time) error {
			got = check.CheckedAt

			return nil
		},
	)

	err := svc.Run(t.Context(), c)

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestChecksRunRecordsResultAfterCancellation(t *testing.T) {
	svc, repo, checker, hosts := newChecks(t)
	c := newClaim(t)
	grantSlot(hosts, c)
	ctx, cancel := context.WithCancel(t.Context())
	result := domain.CheckResult{IsUp: true, StatusCode: 200, Attempts: 1}
	check := &domain.Check{MonitorID: c.Monitor.ID, CheckedAt: fixedNow(), Result: result}
	checker.EXPECT().Check(gomock.Any(), c.Monitor.Settings).DoAndReturn(
		func(context.Context, domain.CheckSettings) (domain.CheckResult, error) {
			cancel()

			return result, nil
		},
	)
	repo.EXPECT().Record(livePersistContext(), check, c.Until).Return(nil)

	err := svc.Run(ctx, c)

	assert.NoError(t, err)
}

func TestChecksRunReleasesClaimWhenRecordFails(t *testing.T) {
	svc, repo, checker, hosts := newChecks(t)
	c := newClaim(t)
	grantSlot(hosts, c)
	errStorage := errors.New("storage is down")
	checker.EXPECT().Check(gomock.Any(), c.Monitor.Settings).Return(domain.CheckResult{}, nil)
	repo.EXPECT().Record(gomock.Any(), gomock.Any(), c.Until).Return(errStorage)
	repo.EXPECT().Release(livePersistContext(), c.Monitor.ID, c.Until, fixedNow()).Return(nil)

	err := svc.Run(t.Context(), c)

	assert.ErrorIs(t, err, errStorage)
}

func TestChecksRunReleasesInterruptedCheck(t *testing.T) {
	svc, repo, checker, hosts := newChecks(t)
	c := newClaim(t)
	grantSlot(hosts, c)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	checker.EXPECT().
		Check(gomock.Any(), c.Monitor.Settings).
		Return(domain.CheckResult{}, context.Canceled)
	repo.EXPECT().Release(livePersistContext(), c.Monitor.ID, c.Until, fixedNow()).Return(nil)

	err := svc.Run(ctx, c)

	assert.ErrorIs(t, err, context.Canceled)
}

func TestChecksRunReportsFailedRelease(t *testing.T) {
	svc, repo, checker, hosts := newChecks(t)
	c := newClaim(t)
	grantSlot(hosts, c)
	errStorage := errors.New("storage is down")
	checker.EXPECT().
		Check(gomock.Any(), c.Monitor.Settings).
		Return(domain.CheckResult{}, context.Canceled)
	repo.EXPECT().Release(gomock.Any(), c.Monitor.ID, c.Until, fixedNow()).Return(errStorage)

	err := svc.Run(t.Context(), c)

	require.ErrorIs(t, err, errStorage)
	assert.NotErrorIs(t, err, context.Canceled)
}

func TestChecksRunPostponesCheckOnBusyHost(t *testing.T) {
	var buf bytes.Buffer
	svc, repo, _, hosts := newChecksWith(t, fixedNow, levelAndMessageLog(&buf))
	c := newClaim(t)
	retryAt := fixedNow().Add(time.Second)
	hosts.EXPECT().Acquire(boundedContext(), c, fixedNow()).Return(false, nil)
	repo.EXPECT().Release(livePersistContext(), c.Monitor.ID, c.Until, retryAt).Return(nil)

	err := svc.Run(t.Context(), c)

	require.NoError(t, err)
	assert.Equal(t, "level=DEBUG msg=\"check postponed, host is busy\"\n", buf.String())
}

func TestChecksRunPostponesCheckAfterCancellation(t *testing.T) {
	svc, repo, _, hosts := newChecks(t)
	c := newClaim(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	hosts.EXPECT().Acquire(gomock.Any(), c, gomock.Any()).Return(false, nil)
	repo.EXPECT().Release(livePersistContext(), c.Monitor.ID, c.Until, gomock.Any()).Return(nil)

	err := svc.Run(ctx, c)

	assert.NoError(t, err)
}

func TestChecksRunReportsFailedPostpone(t *testing.T) {
	svc, repo, _, hosts := newChecks(t)
	c := newClaim(t)
	errStorage := errors.New("storage is down")
	hosts.EXPECT().Acquire(gomock.Any(), c, gomock.Any()).Return(false, nil)
	repo.EXPECT().Release(gomock.Any(), c.Monitor.ID, c.Until, gomock.Any()).Return(errStorage)

	err := svc.Run(t.Context(), c)

	assert.ErrorIs(t, err, errStorage)
}

func TestChecksRunChecksWithoutLimitWhenLimiterFails(t *testing.T) {
	var buf bytes.Buffer
	svc, repo, checker, hosts := newChecksWith(t, fixedNow, levelAndMessageLog(&buf))
	c := newClaim(t)
	errLimiter := errors.New("limiter is down")
	hosts.EXPECT().Acquire(gomock.Any(), c, gomock.Any()).Return(false, errLimiter)
	hosts.EXPECT().Release(livePersistContext(), c).Return(nil)
	checker.EXPECT().Check(gomock.Any(), c.Monitor.Settings).Return(domain.CheckResult{}, nil)
	repo.EXPECT().Record(gomock.Any(), gomock.Any(), c.Until).Return(nil)

	err := svc.Run(t.Context(), c)

	require.NoError(t, err)
	assert.Equal(t, "level=WARN msg=\"check without host limit\"\n", buf.String())
}

func TestChecksRunRecordsResultWhenSlotReleaseFails(t *testing.T) {
	var buf bytes.Buffer
	svc, repo, checker, hosts := newChecksWith(t, fixedNow, levelAndMessageLog(&buf))
	c := newClaim(t)
	hosts.EXPECT().Acquire(gomock.Any(), c, gomock.Any()).Return(true, nil)
	hosts.EXPECT().Release(gomock.Any(), c).Return(errors.New("limiter is down"))
	checker.EXPECT().Check(gomock.Any(), c.Monitor.Settings).Return(domain.CheckResult{}, nil)
	repo.EXPECT().Record(gomock.Any(), gomock.Any(), c.Until).Return(nil)

	err := svc.Run(t.Context(), c)

	require.NoError(t, err)
	assert.Equal(t, "level=WARN msg=\"release host slot\"\n", buf.String())
}

func TestChecksPurge(t *testing.T) {
	svc, repo, _, _ := newChecks(t)
	keep := 30 * 24 * time.Hour
	repo.EXPECT().DeleteBefore(gomock.Any(), fixedNow().Add(-keep)).Return(int64(7), nil)

	got, err := svc.Purge(t.Context(), keep)

	require.NoError(t, err)
	assert.Equal(t, int64(7), got)
}

func TestChecksPurgeRepositoryError(t *testing.T) {
	svc, repo, _, _ := newChecks(t)
	errStorage := errors.New("storage is down")
	repo.EXPECT().DeleteBefore(gomock.Any(), gomock.Any()).Return(int64(3), errStorage)

	got, err := svc.Purge(t.Context(), time.Hour)

	require.ErrorIs(t, err, errStorage)
	assert.Equal(t, int64(3), got)
}
