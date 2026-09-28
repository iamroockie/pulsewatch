package service_test

import (
	"context"
	"errors"
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

func newChecks(t *testing.T) (*service.Checks, *MockCheckRepository, *MockChecker) {
	t.Helper()

	ctrl := gomock.NewController(t)
	repo := NewMockCheckRepository(ctrl)
	checker := NewMockChecker(ctrl)

	return service.NewChecks(repo, checker, fixedNow, retryDelay), repo, checker
}

func newClaim(t *testing.T) domain.Claim {
	t.Helper()

	return domain.Claim{
		Monitor: domaintest.NewMonitor(t, fixedNow()),
		Until:   fixedNow().Add(time.Minute),
	}
}

func livePersistContext() gomock.Matcher {
	return gomock.Cond(func(ctx context.Context) bool {
		_, hasDeadline := ctx.Deadline()

		return ctx.Err() == nil && hasDeadline
	})
}

func TestChecksClaimDue(t *testing.T) {
	svc, repo, _ := newChecks(t)
	want := []domain.Claim{newClaim(t)}
	repo.EXPECT().
		ClaimDue(gomock.Any(), fixedNow(), retryDelay, 10*time.Second, 3).
		Return(want, nil)

	got, err := svc.ClaimDue(t.Context(), 3)

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestChecksClaimDueRepositoryError(t *testing.T) {
	svc, repo, _ := newChecks(t)
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
			svc, repo, checker := newChecks(t)
			c := newClaim(t)
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
	svc, repo, checker := newChecks(t)
	c := newClaim(t)
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
	ctrl := gomock.NewController(t)
	repo := NewMockCheckRepository(ctrl)
	checker := NewMockChecker(ctrl)
	clockReads := 0
	clock := func() time.Time {
		clockReads++

		return fixedNow()
	}
	svc := service.NewChecks(repo, checker, clock, retryDelay)
	c := newClaim(t)
	checker.EXPECT().Check(gomock.Any(), c.Monitor.Settings).DoAndReturn(
		func(context.Context, domain.CheckSettings) (domain.CheckResult, error) {
			assert.Equal(t, 1, clockReads)

			return domain.CheckResult{Attempts: 1}, nil
		},
	)
	repo.EXPECT().Record(gomock.Any(), gomock.Any(), c.Until).Return(nil)

	err := svc.Run(t.Context(), c)

	require.NoError(t, err)
	assert.Equal(t, 1, clockReads)
}

func TestChecksRunRecordsResultAfterCancellation(t *testing.T) {
	svc, repo, checker := newChecks(t)
	c := newClaim(t)
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
	svc, repo, checker := newChecks(t)
	c := newClaim(t)
	errStorage := errors.New("storage is down")
	checker.EXPECT().Check(gomock.Any(), c.Monitor.Settings).Return(domain.CheckResult{}, nil)
	repo.EXPECT().Record(gomock.Any(), gomock.Any(), c.Until).Return(errStorage)
	repo.EXPECT().Release(livePersistContext(), c.Monitor.ID, c.Until, fixedNow()).Return(nil)

	err := svc.Run(t.Context(), c)

	assert.ErrorIs(t, err, errStorage)
}

func TestChecksRunReleasesInterruptedCheck(t *testing.T) {
	svc, repo, checker := newChecks(t)
	c := newClaim(t)
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
	svc, repo, checker := newChecks(t)
	c := newClaim(t)
	errStorage := errors.New("storage is down")
	checker.EXPECT().
		Check(gomock.Any(), c.Monitor.Settings).
		Return(domain.CheckResult{}, context.Canceled)
	repo.EXPECT().Release(gomock.Any(), c.Monitor.ID, c.Until, fixedNow()).Return(errStorage)

	err := svc.Run(t.Context(), c)

	require.ErrorIs(t, err, errStorage)
	assert.NotErrorIs(t, err, context.Canceled)
}
