package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

const (
	persistTimeout = 5 * time.Second
	slotTimeout    = time.Second
)

type Checks struct {
	repo       CheckRepository
	checker    Checker
	hosts      HostLimiter
	now        func() time.Time
	retryDelay time.Duration
	log        *slog.Logger
}

func NewChecks(
	repo CheckRepository,
	checker Checker,
	hosts HostLimiter,
	now func() time.Time,
	retryDelay time.Duration,
	log *slog.Logger,
) *Checks {
	return &Checks{
		repo:       repo,
		checker:    checker,
		hosts:      hosts,
		now:        now,
		retryDelay: retryDelay,
		log:        log,
	}
}

func (s *Checks) ClaimDue(ctx context.Context, limit int) ([]domain.Claim, error) {
	margin := persistTimeout + 5*time.Second

	claims, err := s.repo.ClaimDue(ctx, s.now(), s.retryDelay, margin, limit)
	if err != nil {
		return nil, fmt.Errorf("claim due monitors: %w", err)
	}

	return claims, nil
}

func (s *Checks) Backlog(ctx context.Context) (int64, error) {
	due, err := s.repo.CountDue(ctx, s.now())
	if err != nil {
		return 0, fmt.Errorf("count due monitors: %w", err)
	}

	return due, nil
}

func (s *Checks) Run(ctx context.Context, c domain.Claim) error {
	id := c.Monitor.ID

	if !s.acquireSlot(ctx, c) {
		return s.postpone(ctx, c)
	}
	defer s.releaseSlot(ctx, c)

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

func (s *Checks) Purge(ctx context.Context, keep time.Duration) (int64, error) {
	deleted, err := s.repo.DeleteBefore(ctx, s.now().Add(-keep))
	if err != nil {
		return deleted, fmt.Errorf("purge checks: %w", err)
	}

	return deleted, nil
}

func (s *Checks) acquireSlot(ctx context.Context, c domain.Claim) bool {
	ctx, cancel := context.WithTimeout(ctx, slotTimeout)
	defer cancel()

	acquired, err := s.hosts.Acquire(ctx, c, s.now())
	if err != nil {
		s.log.Warn("check without host limit", "monitor_id", c.Monitor.ID, "error", err)
		return true
	}

	return acquired
}

func (s *Checks) releaseSlot(ctx context.Context, c domain.Claim) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), slotTimeout)
	defer cancel()

	if err := s.hosts.Release(ctx, c); err != nil {
		s.log.Warn("release host slot", "monitor_id", c.Monitor.ID, "error", err)
	}
}

func (s *Checks) postpone(ctx context.Context, c domain.Claim) error {
	id := c.Monitor.ID

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
	defer cancel()

	if err := s.repo.Release(ctx, id, c.Until, s.now().Add(time.Second)); err != nil {
		return fmt.Errorf("postpone check of monitor %s: %w", id, err)
	}

	s.log.Debug("check postponed, host is busy", "monitor_id", id)

	return nil
}
