package service_test

import (
	"context"
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

func fixedNow() time.Time {
	return time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
}

func fixedID() uuid.UUID {
	return uuid.MustParse("01998a2e-6f00-7000-8000-000000000001")
}

func newMonitors(t *testing.T) (*service.Monitors, *MockMonitorRepository) {
	t.Helper()

	repo := NewMockMonitorRepository(gomock.NewController(t))

	return service.NewMonitors(repo, fixedNow, fixedID), repo
}

func updateStored(
	stored *domain.Monitor,
) func(context.Context, uuid.UUID, func(*domain.Monitor) error) (*domain.Monitor, error) {
	return func(
		_ context.Context, _ uuid.UUID, fn func(*domain.Monitor) error,
	) (*domain.Monitor, error) {
		m := *stored
		if err := fn(&m); err != nil {
			return nil, err
		}

		return &m, nil
	}
}

func TestMonitorsCreate(t *testing.T) {
	svc, repo := newMonitors(t)
	settings := domaintest.ValidSettings()
	want := &domain.Monitor{
		ID:          fixedID(),
		IsActive:    true,
		Settings:    settings,
		NextCheckAt: fixedNow(),
		LastCheckAt: nil,
		CreatedAt:   fixedNow(),
		UpdatedAt:   fixedNow(),
	}
	repo.EXPECT().Create(gomock.Any(), want).Return(nil)

	got, err := svc.Create(t.Context(), settings)

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestMonitorsCreateRejectsInvalidSettings(t *testing.T) {
	svc, _ := newMonitors(t)
	settings := domaintest.ValidSettings()
	settings.Interval = time.Second

	got, err := svc.Create(t.Context(), settings)

	require.ErrorIs(t, err, domain.ErrIntervalOutOfRange)
	assert.Nil(t, got)
}

func TestMonitorsCreateRepositoryError(t *testing.T) {
	svc, repo := newMonitors(t)
	errStorage := errors.New("storage is down")
	repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(errStorage)

	got, err := svc.Create(t.Context(), domaintest.ValidSettings())

	require.ErrorIs(t, err, errStorage)
	assert.Nil(t, got)
}

func TestMonitorsGet(t *testing.T) {
	svc, repo := newMonitors(t)
	want := domaintest.NewMonitor(t, fixedNow().Add(-time.Hour))
	repo.EXPECT().Get(gomock.Any(), want.ID).Return(want, nil)

	got, err := svc.Get(t.Context(), want.ID)

	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestMonitorsGetNotFound(t *testing.T) {
	svc, repo := newMonitors(t)
	id := uuid.NewV7()
	repo.EXPECT().Get(gomock.Any(), id).Return(nil, domain.ErrMonitorNotFound)

	got, err := svc.Get(t.Context(), id)

	require.ErrorIs(t, err, domain.ErrMonitorNotFound)
	assert.Nil(t, got)
}

func TestMonitorsList(t *testing.T) {
	const limit = 2
	after := uuid.NewV7()
	created := fixedNow().Add(-time.Hour)
	first := domaintest.NewMonitor(t, created)
	second := domaintest.NewMonitor(t, created)
	third := domaintest.NewMonitor(t, created)
	tests := map[string]struct {
		stored []*domain.Monitor
		want   service.MonitorPage
	}{
		"more than limit": {
			stored: []*domain.Monitor{first, second, third},
			want: service.MonitorPage{
				Items:     []*domain.Monitor{first, second},
				NextAfter: new(second.ID),
			},
		},
		"exactly limit": {
			stored: []*domain.Monitor{first, second},
			want: service.MonitorPage{
				Items:     []*domain.Monitor{first, second},
				NextAfter: nil,
			},
		},
		"less than limit": {
			stored: []*domain.Monitor{first},
			want: service.MonitorPage{
				Items:     []*domain.Monitor{first},
				NextAfter: nil,
			},
		},
		"empty": {
			stored: nil,
			want: service.MonitorPage{
				Items:     nil,
				NextAfter: nil,
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			svc, repo := newMonitors(t)
			repo.EXPECT().List(gomock.Any(), after, limit+1).Return(test.stored, nil)

			got, err := svc.List(t.Context(), after, limit)

			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestMonitorsListRejectsNonPositiveLimit(t *testing.T) {
	svc, _ := newMonitors(t)

	got, err := svc.List(t.Context(), uuid.Nil(), 0)

	require.Error(t, err)
	assert.Equal(t, service.MonitorPage{}, got)
}

func TestMonitorsListRepositoryError(t *testing.T) {
	svc, repo := newMonitors(t)
	errStorage := errors.New("storage is down")
	repo.EXPECT().List(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errStorage)

	got, err := svc.List(t.Context(), uuid.Nil(), 10)

	require.ErrorIs(t, err, errStorage)
	assert.Equal(t, service.MonitorPage{}, got)
}

func TestMonitorsUpdate(t *testing.T) {
	svc, repo := newMonitors(t)
	stored := domaintest.NewMonitor(t, fixedNow().Add(-time.Hour))
	want := *stored
	want.Settings.MaxRetries = 4
	want.UpdatedAt = fixedNow()
	repo.EXPECT().Update(gomock.Any(), stored.ID, gomock.Any()).DoAndReturn(updateStored(stored))

	got, err := svc.Update(t.Context(), stored.ID, domain.MonitorChanges{MaxRetries: new(int32(4))})

	require.NoError(t, err)
	assert.Equal(t, &want, got)
}

func TestMonitorsUpdateRejectsInvalidChanges(t *testing.T) {
	svc, repo := newMonitors(t)
	stored := domaintest.NewMonitor(t, fixedNow().Add(-time.Hour))
	repo.EXPECT().Update(gomock.Any(), stored.ID, gomock.Any()).DoAndReturn(updateStored(stored))

	got, err := svc.Update(t.Context(), stored.ID, domain.MonitorChanges{Timeout: new(time.Minute)})

	require.ErrorIs(t, err, domain.ErrTimeoutExceedsInterval)
	assert.Nil(t, got)
}

func TestMonitorsUpdateNotFound(t *testing.T) {
	svc, repo := newMonitors(t)
	id := uuid.NewV7()
	repo.EXPECT().Update(gomock.Any(), id, gomock.Any()).Return(nil, domain.ErrMonitorNotFound)

	got, err := svc.Update(t.Context(), id, domain.MonitorChanges{IsActive: new(false)})

	require.ErrorIs(t, err, domain.ErrMonitorNotFound)
	assert.Nil(t, got)
}

func TestMonitorsUpdateReadsClockInsideRepositoryUpdate(t *testing.T) {
	repo := NewMockMonitorRepository(gomock.NewController(t))
	clockReads := 0
	clock := func() time.Time {
		clockReads++

		return fixedNow()
	}
	svc := service.NewMonitors(repo, clock, fixedID)
	stored := domaintest.NewMonitor(t, fixedNow().Add(-time.Hour))
	repo.EXPECT().Update(gomock.Any(), stored.ID, gomock.Any()).DoAndReturn(func(
		ctx context.Context, id uuid.UUID, fn func(*domain.Monitor) error,
	) (*domain.Monitor, error) {
		assert.Zero(t, clockReads)

		return updateStored(stored)(ctx, id, fn)
	})

	_, err := svc.Update(t.Context(), stored.ID, domain.MonitorChanges{IsActive: new(false)})

	require.NoError(t, err)
	assert.Equal(t, 1, clockReads)
}

func TestMonitorsDelete(t *testing.T) {
	svc, repo := newMonitors(t)
	id := uuid.NewV7()
	repo.EXPECT().Delete(gomock.Any(), id).Return(nil)

	err := svc.Delete(t.Context(), id)

	assert.NoError(t, err)
}

func TestMonitorsDeleteNotFound(t *testing.T) {
	svc, repo := newMonitors(t)
	id := uuid.NewV7()
	repo.EXPECT().Delete(gomock.Any(), id).Return(domain.ErrMonitorNotFound)

	err := svc.Delete(t.Context(), id)

	assert.ErrorIs(t, err, domain.ErrMonitorNotFound)
}
