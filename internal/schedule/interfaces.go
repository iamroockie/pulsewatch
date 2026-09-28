package schedule

//go:generate mockgen -source=interfaces.go -destination=mocks_test.go -package=schedule_test

import (
	"context"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

type Claimer interface {
	ClaimDue(ctx context.Context, limit int) ([]domain.Claim, error)
}

type Workers interface {
	Free() int
	Freed() <-chan struct{}
	Go(c domain.Claim)
}
