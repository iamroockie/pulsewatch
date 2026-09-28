package pool

//go:generate mockgen -source=interfaces.go -destination=mocks_test.go -package=pool_test

import (
	"context"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

type Runner interface {
	Run(ctx context.Context, c domain.Claim) error
}
