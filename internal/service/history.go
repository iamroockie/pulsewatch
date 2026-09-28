package service

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

type CheckPage struct {
	Items      []*domain.Check
	NextBefore *time.Time
}

type History struct {
	monitors MonitorRepository
	checks   HistoryRepository
	now      func() time.Time
}

func NewHistory(m MonitorRepository, c HistoryRepository, now func() time.Time) *History {
	return &History{monitors: m, checks: c, now: now}
}

func (s *History) Checks(
	ctx context.Context,
	monitorID uuid.UUID,
	before time.Time,
	limit int,
) (CheckPage, error) {
	if limit < 1 {
		return CheckPage{}, fmt.Errorf("list checks: limit %d is not positive", limit)
	}

	if _, err := s.monitors.Get(ctx, monitorID); err != nil {
		return CheckPage{}, fmt.Errorf("list checks: %w", err)
	}

	checks, err := s.checks.List(ctx, monitorID, before, limit+1)
	if err != nil {
		return CheckPage{}, fmt.Errorf("list checks: %w", err)
	}

	if len(checks) <= limit {
		return CheckPage{Items: checks, NextBefore: nil}, nil
	}

	items := checks[:limit]

	return CheckPage{Items: items, NextBefore: new(items[limit-1].CheckedAt)}, nil
}

func (s *History) Uptime(ctx context.Context, monitorID uuid.UUID) (domain.UptimeReport, error) {
	if _, err := s.monitors.Get(ctx, monitorID); err != nil {
		return domain.UptimeReport{}, fmt.Errorf("get uptime: %w", err)
	}

	report, err := s.checks.Uptime(ctx, monitorID, s.now())
	if err != nil {
		return domain.UptimeReport{}, fmt.Errorf("get uptime: %w", err)
	}

	return report, nil
}
