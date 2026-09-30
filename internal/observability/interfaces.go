package observability

//go:generate mockgen -source=interfaces.go -destination=mocks_test.go -package=observability_test

import (
	"context"
	"time"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

type Checker interface {
	Check(ctx context.Context, s domain.CheckSettings) (domain.CheckResult, error)
}

type HostLimiter interface {
	Acquire(ctx context.Context, c domain.Claim, now time.Time) (bool, error)
	Release(ctx context.Context, c domain.Claim) error
}

type Queue interface {
	Backlog(ctx context.Context) (int64, error)
}

type Runner interface {
	Run(ctx context.Context, c domain.Claim) error
}
