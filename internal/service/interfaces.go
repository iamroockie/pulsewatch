package service

//go:generate mockgen -source=interfaces.go -destination=mocks_test.go -package=service_test

import (
	"context"
	"time"
	"uuid"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

type CheckRepository interface {
	ClaimDue(
		ctx context.Context,
		now time.Time,
		retryDelay time.Duration,
		margin time.Duration,
		limit int,
	) ([]domain.Claim, error)
	Record(ctx context.Context, check *domain.Check, until time.Time) error
	Release(ctx context.Context, id uuid.UUID, until, now time.Time) error
}

type Checker interface {
	Check(ctx context.Context, s domain.CheckSettings) (domain.CheckResult, error)
}

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
