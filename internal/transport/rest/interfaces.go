package rest

//go:generate mockgen -source=interfaces.go -destination=mocks_test.go -package=rest_test

import (
	"context"
	"uuid"

	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/service"
)

type MonitorService interface {
	Create(ctx context.Context, settings domain.CheckSettings) (*domain.Monitor, error)
	Get(ctx context.Context, id uuid.UUID) (*domain.Monitor, error)
	List(ctx context.Context, after uuid.UUID, limit int) (service.MonitorPage, error)
	Delete(ctx context.Context, id uuid.UUID) error
	Update(
		ctx context.Context,
		id uuid.UUID,
		changes domain.MonitorChanges,
	) (*domain.Monitor, error)
}
