package postgres_test

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/iamroockie/pulsewatch/internal/adapter/postgres"
	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/domain/domaintest"
)

func fixedNow() time.Time {
	return time.Date(2026, 9, 26, 12, 0, 0, 123456000, time.UTC)
}

func createMonitor(t *testing.T, repo *postgres.Monitors) *domain.Monitor {
	t.Helper()

	m := domaintest.NewMonitor(t, fixedNow())
	require.NoError(t, repo.Create(t.Context(), m))

	return m
}

func getMonitor(t *testing.T, repo *postgres.Monitors, id uuid.UUID) *domain.Monitor {
	t.Helper()

	m, err := repo.Get(t.Context(), id)
	require.NoError(t, err)

	return m
}

func TestMonitorsCreateAndGet(t *testing.T) {
	checkedAt := fixedNow().Add(time.Minute)
	timeout := 1500 * time.Millisecond
	tests := map[string]struct {
		modify func(*domain.Monitor)
	}{
		"new monitor":         {modify: func(*domain.Monitor) {}},
		"paused monitor":      {modify: func(m *domain.Monitor) { m.IsActive = false }},
		"checked monitor":     {modify: func(m *domain.Monitor) { m.LastCheckAt = &checkedAt }},
		"millisecond timeout": {modify: func(m *domain.Monitor) { m.Settings.Timeout = timeout }},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			repo := postgres.NewMonitors(newPool(t))
			want := domaintest.NewMonitor(t, fixedNow())
			test.modify(want)
			require.NoError(t, repo.Create(t.Context(), want))

			got, err := repo.Get(t.Context(), want.ID)

			require.NoError(t, err)
			assert.Equal(t, want, got)
		})
	}
}

func TestMonitorsGetNotFound(t *testing.T) {
	repo := postgres.NewMonitors(newPool(t))

	got, err := repo.Get(t.Context(), uuid.NewV7())

	require.ErrorIs(t, err, domain.ErrMonitorNotFound)
	assert.Nil(t, got)
}

func TestMonitorsList(t *testing.T) {
	repo := postgres.NewMonitors(newPool(t))
	stored := []*domain.Monitor{
		domaintest.NewMonitor(t, fixedNow()),
		domaintest.NewMonitor(t, fixedNow()),
		domaintest.NewMonitor(t, fixedNow()),
	}
	slices.SortFunc(stored, func(a, b *domain.Monitor) int { return a.ID.Compare(b.ID) })
	for _, m := range slices.Backward(stored) {
		require.NoError(t, repo.Create(t.Context(), m))
	}
	tests := map[string]struct {
		after uuid.UUID
		limit int
		want  []*domain.Monitor
	}{
		"first page":        {after: uuid.Nil(), limit: 2, want: stored[:2]},
		"next page":         {after: stored[1].ID, limit: 2, want: stored[2:]},
		"after last":        {after: stored[2].ID, limit: 2, want: []*domain.Monitor{}},
		"limit above total": {after: uuid.Nil(), limit: 10, want: stored},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := repo.List(t.Context(), test.after, test.limit)

			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestMonitorsUpdate(t *testing.T) {
	repo := postgres.NewMonitors(newPool(t))
	stored := createMonitor(t, repo)
	updatedAt := fixedNow().Add(time.Hour)
	want := *stored
	want.IsActive = false
	want.Settings = domain.CheckSettings{
		URL:        "https://example.org/ping",
		Interval:   2 * time.Minute,
		Timeout:    1500 * time.Millisecond,
		MaxRetries: 4,
	}
	want.NextCheckAt = updatedAt
	want.UpdatedAt = updatedAt

	got, err := repo.Update(t.Context(), stored.ID, func(m *domain.Monitor) error {
		*m = want

		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, &want, got)
	assert.Equal(t, &want, getMonitor(t, repo, stored.ID))
}

func TestMonitorsUpdateKeepsColumnsItDoesNotOwn(t *testing.T) {
	pool := newPool(t)
	repo := postgres.NewMonitors(pool)
	stored := createMonitor(t, repo)
	checkedAt := fixedNow().Add(time.Minute)
	_, err := pool.Exec(t.Context(),
		"update monitors set last_check_at = $2 where id = $1", stored.ID, checkedAt)
	require.NoError(t, err)
	want := *stored
	want.LastCheckAt = &checkedAt

	got, err := repo.Update(t.Context(), stored.ID, func(m *domain.Monitor) error {
		m.LastCheckAt = nil
		m.CreatedAt = fixedNow().Add(time.Hour)

		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, &want, got)
	assert.Equal(t, &want, getMonitor(t, repo, stored.ID))
}

func TestMonitorsUpdateNotFound(t *testing.T) {
	repo := postgres.NewMonitors(newPool(t))
	called := false

	got, err := repo.Update(t.Context(), uuid.NewV7(), func(*domain.Monitor) error {
		called = true

		return nil
	})

	require.ErrorIs(t, err, domain.ErrMonitorNotFound)
	assert.Nil(t, got)
	assert.False(t, called)
}

func TestMonitorsUpdateKeepsRowOnError(t *testing.T) {
	repo := postgres.NewMonitors(newPool(t))
	stored := createMonitor(t, repo)
	errRejected := errors.New("rejected")

	got, err := repo.Update(t.Context(), stored.ID, func(m *domain.Monitor) error {
		m.Settings.MaxRetries = 5

		return errRejected
	})

	require.ErrorIs(t, err, errRejected)
	assert.Nil(t, got)
	assert.Equal(t, stored, getMonitor(t, repo, stored.ID))
}

func TestMonitorsUpdateDoesNotLoseConcurrentUpdates(t *testing.T) {
	const workers = 10
	repo := postgres.NewMonitors(newPool(t))
	stored := createMonitor(t, repo)
	var wg sync.WaitGroup

	for range workers {
		wg.Go(func() {
			_, err := repo.Update(t.Context(), stored.ID, func(m *domain.Monitor) error {
				time.Sleep(10 * time.Millisecond)
				m.Settings.MaxRetries++

				return nil
			})
			assert.NoError(t, err)
		})
	}
	wg.Wait()

	got := getMonitor(t, repo, stored.ID)
	assert.Equal(t, stored.Settings.MaxRetries+workers, got.Settings.MaxRetries)
}

func TestMonitorsDelete(t *testing.T) {
	repo := postgres.NewMonitors(newPool(t))
	stored := createMonitor(t, repo)

	err := repo.Delete(t.Context(), stored.ID)

	require.NoError(t, err)
	_, err = repo.Get(t.Context(), stored.ID)
	assert.ErrorIs(t, err, domain.ErrMonitorNotFound)
}

func TestMonitorsDeleteNotFound(t *testing.T) {
	repo := postgres.NewMonitors(newPool(t))

	err := repo.Delete(t.Context(), uuid.NewV7())

	assert.ErrorIs(t, err, domain.ErrMonitorNotFound)
}

func TestMonitorsDeleteRemovesChecks(t *testing.T) {
	pool := newPool(t)
	repo := postgres.NewMonitors(pool)
	stored := createMonitor(t, repo)
	_, err := pool.Exec(t.Context(), `insert into checks
		(monitor_id, checked_at, is_up, status_code, latency_ms, attempts)
		values ($1, $2, true, 200, 42, 1)`, stored.ID, fixedNow())
	require.NoError(t, err)

	err = repo.Delete(t.Context(), stored.ID)

	require.NoError(t, err)
	var checks int
	require.NoError(t, pool.QueryRow(t.Context(),
		"select count(*) from checks where monitor_id = $1", stored.ID).Scan(&checks))
	assert.Zero(t, checks)
}
