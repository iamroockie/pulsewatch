package domaintest

import (
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/require"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

func ValidSettings() domain.CheckSettings {
	return domain.CheckSettings{
		URL:        "https://example.com/health",
		Interval:   time.Minute,
		Timeout:    5 * time.Second,
		MaxRetries: 2,
	}
}

func NewMonitor(tb testing.TB, now time.Time) *domain.Monitor {
	tb.Helper()

	m, err := domain.NewMonitor(uuid.NewV7(), ValidSettings(), now)
	require.NoError(tb, err)

	return m
}
