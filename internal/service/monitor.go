package service

//go:generate mockgen -source=monitor.go -destination=mock_repository_test.go -package=service_test

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

type MonitorRepository interface {
	Create(ctx context.Context, m *domain.Monitor) error
	Get(ctx context.Context, id uuid.UUID) (*domain.Monitor, error)
	List(ctx context.Context, after uuid.UUID, limit int) ([]*domain.Monitor, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Update(
		ctx context.Context,
		id uuid.UUID,
		fn func(*domain.Monitor) error,
	) (*domain.Monitor, error)
}

type MonitorPage struct {
	Items     []*domain.Monitor
	NextAfter *uuid.UUID
}

type Monitors struct {
	repo  MonitorRepository
	now   func() time.Time
	newID func() uuid.UUID
}

func NewMonitors(
	repo MonitorRepository,
	now func() time.Time,
	newID func() uuid.UUID,
) *Monitors {
	return &Monitors{repo: repo, now: now, newID: newID}
}

func (s *Monitors) Create(
	ctx context.Context,
	settings domain.CheckSettings,
) (*domain.Monitor, error) {
	m, err := domain.NewMonitor(s.newID(), settings, s.now())
	if err != nil {
		return nil, fmt.Errorf("create monitor: %w", err)
	}

	if err := s.repo.Create(ctx, m); err != nil {
		return nil, fmt.Errorf("create monitor: %w", err)
	}

	return m, nil
}

func (s *Monitors) Get(ctx context.Context, id uuid.UUID) (*domain.Monitor, error) {
	m, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get monitor: %w", err)
	}

	return m, nil
}

func (s *Monitors) List(ctx context.Context, after uuid.UUID, limit int) (MonitorPage, error) {
	if limit < 1 {
		return MonitorPage{}, fmt.Errorf("list monitors: limit %d is not positive", limit)
	}

	monitors, err := s.repo.List(ctx, after, limit+1)
	if err != nil {
		return MonitorPage{}, fmt.Errorf("list monitors: %w", err)
	}

	if len(monitors) <= limit {
		return MonitorPage{Items: monitors, NextAfter: nil}, nil
	}

	items := monitors[:limit]

	return MonitorPage{Items: items, NextAfter: new(items[limit-1].ID)}, nil
}

func (s *Monitors) Update(
	ctx context.Context,
	id uuid.UUID,
	changes domain.MonitorChanges,
) (*domain.Monitor, error) {
	m, err := s.repo.Update(ctx, id, func(m *domain.Monitor) error {
		return m.Apply(changes, s.now())
	})
	if err != nil {
		return nil, fmt.Errorf("update monitor: %w", err)
	}

	return m, nil
}

func (s *Monitors) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete monitor: %w", err)
	}

	return nil
}
