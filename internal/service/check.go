package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

const persistTimeout = 5 * time.Second

type Checks struct {
	repo       CheckRepository
	checker    Checker
	now        func() time.Time
	retryDelay time.Duration
}

func NewChecks(
	repo CheckRepository,
	checker Checker,
	now func() time.Time,
	retryDelay time.Duration,
) *Checks {
	return &Checks{repo: repo, checker: checker, now: now, retryDelay: retryDelay}
}

func (s *Checks) ClaimDue(ctx context.Context, limit int) ([]domain.Claim, error) {
	margin := persistTimeout + 5*time.Second

	claims, err := s.repo.ClaimDue(ctx, s.now(), s.retryDelay, margin, limit)
	if err != nil {
		return nil, fmt.Errorf("claim due monitors: %w", err)
	}

	return claims, nil
}

func (s *Checks) Run(ctx context.Context, c domain.Claim) error {
	id := c.Monitor.ID

	checkCtx, cancelCheck := context.WithDeadline(ctx, c.Until.Add(-persistTimeout))
	defer cancelCheck()

	checkedAt := s.now()
	result, err := s.checker.Check(checkCtx, c.Monitor.Settings)

	persistCtx, cancelPersist := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
	defer cancelPersist()

	if err != nil {
		if releaseErr := s.repo.Release(persistCtx, id, c.Until, s.now()); releaseErr != nil {
			return fmt.Errorf("release interrupted check of monitor %s: %w", id, releaseErr)
		}

		return fmt.Errorf("check monitor %s: %w", id, err)
	}

	check := &domain.Check{MonitorID: id, CheckedAt: checkedAt, Result: result}
	err = s.repo.Record(persistCtx, check, c.Until)
	if err == nil || errors.Is(err, domain.ErrClaimLost) {
		return nil
	}

	releaseErr := s.repo.Release(persistCtx, id, c.Until, s.now())

	return fmt.Errorf("record check of monitor %s: %w", id, errors.Join(err, releaseErr))
}
