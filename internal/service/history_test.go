package service_test

import (
	"errors"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/domain/domaintest"
	"github.com/iamroockie/pulsewatch/internal/service"
)

func newHistory(
	t *testing.T,
) (*service.History, *MockMonitorRepository, *MockHistoryRepository) {
	t.Helper()

	ctrl := gomock.NewController(t)
	monitors := NewMockMonitorRepository(ctrl)
	checks := NewMockHistoryRepository(ctrl)

	return service.NewHistory(monitors, checks, fixedNow), monitors, checks
}

func checkAt(monitorID uuid.UUID, age time.Duration) *domain.Check {
	return &domain.Check{
		MonitorID: monitorID,
		CheckedAt: fixedNow().Add(-age),
		Result:    domain.CheckResult{IsUp: true, StatusCode: 200, Attempts: 1},
	}
}

func TestHistoryChecks(t *testing.T) {
	const limit = 2
	stored := domaintest.NewMonitor(t, fixedNow().Add(-time.Hour))
	before := fixedNow().Add(-time.Minute)
	first := checkAt(stored.ID, 2*time.Minute)
	second := checkAt(stored.ID, 3*time.Minute)
	third := checkAt(stored.ID, 4*time.Minute)
	tests := map[string]struct {
		stored []*domain.Check
		want   service.CheckPage
	}{
		"more than limit": {
			stored: []*domain.Check{first, second, third},
			want: service.CheckPage{
				Items:      []*domain.Check{first, second},
				NextBefore: new(second.CheckedAt),
			},
		},
		"exactly limit": {
			stored: []*domain.Check{first, second},
			want: service.CheckPage{
				Items:      []*domain.Check{first, second},
				NextBefore: nil,
			},
		},
		"less than limit": {
			stored: []*domain.Check{first},
			want: service.CheckPage{
				Items:      []*domain.Check{first},
				NextBefore: nil,
			},
		},
		"empty": {
			stored: nil,
			want: service.CheckPage{
				Items:      nil,
				NextBefore: nil,
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			svc, monitors, checks := newHistory(t)
			monitors.EXPECT().Get(gomock.Any(), stored.ID).Return(stored, nil)
			checks.EXPECT().List(gomock.Any(), stored.ID, before, limit+1).Return(test.stored, nil)

			got, err := svc.Checks(t.Context(), stored.ID, before, limit)

			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestHistoryChecksRejectsNonPositiveLimit(t *testing.T) {
	svc, _, _ := newHistory(t)

	got, err := svc.Checks(t.Context(), uuid.NewV7(), time.Time{}, 0)

	require.Error(t, err)
	assert.Equal(t, service.CheckPage{}, got)
}

func TestHistoryChecksMonitorNotFound(t *testing.T) {
	svc, monitors, _ := newHistory(t)
	id := uuid.NewV7()
	monitors.EXPECT().Get(gomock.Any(), id).Return(nil, domain.ErrMonitorNotFound)

	got, err := svc.Checks(t.Context(), id, time.Time{}, 10)

	require.ErrorIs(t, err, domain.ErrMonitorNotFound)
	assert.Equal(t, service.CheckPage{}, got)
}

func TestHistoryChecksRepositoryError(t *testing.T) {
	svc, monitors, checks := newHistory(t)
	stored := domaintest.NewMonitor(t, fixedNow().Add(-time.Hour))
	errStorage := errors.New("storage is down")
	monitors.EXPECT().Get(gomock.Any(), stored.ID).Return(stored, nil)
	checks.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, errStorage)

	got, err := svc.Checks(t.Context(), stored.ID, time.Time{}, 10)

	require.ErrorIs(t, err, errStorage)
	assert.Equal(t, service.CheckPage{}, got)
}

func TestHistoryUptime(t *testing.T) {
	svc, monitors, checks := newHistory(t)
	stored := domaintest.NewMonitor(t, fixedNow().Add(-time.Hour))
	want := domain.UptimeReport{
		Hour: domain.Uptime{Checks: 60, Up: 57},
		Day:  domain.Uptime{Checks: 1440, Up: 1437},
		Week: domain.Uptime{Checks: 10080, Up: 10077},
	}
	monitors.EXPECT().Get(gomock.Any(), stored.ID).Return(stored, nil)
	checks.EXPECT().Uptime(gomock.Any(), stored.ID, fixedNow()).Return(want, nil)

	got, err := svc.Uptime(t.Context(), stored.ID)

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestHistoryUptimeMonitorNotFound(t *testing.T) {
	svc, monitors, _ := newHistory(t)
	id := uuid.NewV7()
	monitors.EXPECT().Get(gomock.Any(), id).Return(nil, domain.ErrMonitorNotFound)

	got, err := svc.Uptime(t.Context(), id)

	require.ErrorIs(t, err, domain.ErrMonitorNotFound)
	assert.Equal(t, domain.UptimeReport{}, got)
}

func TestHistoryUptimeRepositoryError(t *testing.T) {
	svc, monitors, checks := newHistory(t)
	stored := domaintest.NewMonitor(t, fixedNow().Add(-time.Hour))
	errStorage := errors.New("storage is down")
	monitors.EXPECT().Get(gomock.Any(), stored.ID).Return(stored, nil)
	checks.EXPECT().Uptime(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(domain.UptimeReport{}, errStorage)

	got, err := svc.Uptime(t.Context(), stored.ID)

	require.ErrorIs(t, err, errStorage)
	assert.Equal(t, domain.UptimeReport{}, got)
}
