package postgres_test

import (
	"slices"
	"sync"
	"testing"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/iamroockie/pulsewatch/internal/adapter/postgres"
	"github.com/iamroockie/pulsewatch/internal/domain"
	"github.com/iamroockie/pulsewatch/internal/domain/domaintest"
)

const (
	retryDelay  = 5 * time.Second
	leaseMargin = 10 * time.Second
)

type storedCheck struct {
	CheckedAt  time.Time
	IsUp       bool
	StatusCode *int16
	LatencyMS  int32
	Attempts   int16
	Error      *string
}

func unchanged(*domain.Monitor) {}

func storeMonitor(t *testing.T, pool *pgxpool.Pool, modify func(*domain.Monitor)) *domain.Monitor {
	t.Helper()

	m := domaintest.NewMonitor(t, fixedNow())
	modify(m)
	require.NoError(t, postgres.NewMonitors(pool).Create(t.Context(), m))

	return m
}

func setClaimedUntil(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, until *time.Time) {
	t.Helper()

	_, err := pool.Exec(t.Context(),
		"UPDATE monitors SET claimed_until = $2 WHERE id = $1", id, until)
	require.NoError(t, err)
}

func claimedUntil(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) *time.Time {
	t.Helper()

	var until *time.Time
	require.NoError(t, pool.QueryRow(t.Context(),
		"SELECT claimed_until FROM monitors WHERE id = $1", id).Scan(&until))

	return until
}

func storedChecks(t *testing.T, pool *pgxpool.Pool, monitorID uuid.UUID) []storedCheck {
	t.Helper()

	query := `
		SELECT checked_at, is_up, status_code, latency_ms, attempts, error
		FROM checks
		WHERE monitor_id = $1
	`

	rows, err := pool.Query(t.Context(), query, monitorID)
	require.NoError(t, err)
	checks, err := pgx.CollectRows(rows, pgx.RowToStructByPos[storedCheck])
	require.NoError(t, err)

	return checks
}

func claimedIDs(claims []domain.Claim) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(claims))
	for _, c := range claims {
		ids = append(ids, c.Monitor.ID)
	}

	return ids
}

func TestChecksClaimDue(t *testing.T) {
	pool := newPool(t)
	checks := postgres.NewChecks(pool)
	stored := storeMonitor(t, pool, unchanged)
	now := fixedNow().Add(time.Hour)
	monitor := *stored
	monitor.NextCheckAt = now.Add(stored.Settings.Interval)
	until := now.Add(35 * time.Second)
	want := []domain.Claim{{Monitor: &monitor, Until: until}}

	got, err := checks.ClaimDue(t.Context(), now, retryDelay, leaseMargin, 10)

	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, &monitor, getMonitor(t, postgres.NewMonitors(pool), stored.ID))
	assert.Equal(t, &until, claimedUntil(t, pool, stored.ID))
}

func TestChecksClaimDueLeaseFollowsSettings(t *testing.T) {
	now := fixedNow().Add(time.Hour)
	tests := map[string]struct {
		modify func(*domain.Monitor)
		want   time.Duration
	}{
		"default settings": {
			modify: unchanged,
			want:   35 * time.Second,
		},
		"no retries": {
			modify: func(m *domain.Monitor) {
				m.Settings.Timeout, m.Settings.MaxRetries = 1500*time.Millisecond, 0
			},
			want: 11500 * time.Millisecond,
		},
		"max retries": {
			modify: func(m *domain.Monitor) {
				m.Settings.Timeout, m.Settings.MaxRetries = 2*time.Second, domain.MaxRetries
			},
			want: 47 * time.Second,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			pool := newPool(t)
			checks := postgres.NewChecks(pool)
			stored := storeMonitor(t, pool, test.modify)
			want := now.Add(test.want)

			got, err := checks.ClaimDue(t.Context(), now, retryDelay, leaseMargin, 10)

			require.NoError(t, err)
			require.Len(t, got, 1)
			assert.Equal(t, want, got[0].Until)
			assert.Equal(t, &want, claimedUntil(t, pool, stored.ID))
		})
	}
}

func TestChecksClaimDueSelectsDueMonitors(t *testing.T) {
	now := fixedNow().Add(time.Hour)
	tests := map[string]struct {
		modify       func(*domain.Monitor)
		claimedUntil *time.Time
		want         int
	}{
		"overdue": {
			modify: unchanged,
			want:   1,
		},
		"due right now": {
			modify: func(m *domain.Monitor) {
				m.NextCheckAt = now
			},
			want: 1,
		},
		"not due yet": {
			modify: func(m *domain.Monitor) {
				m.NextCheckAt = now.Add(time.Microsecond)
			},
			want: 0,
		},
		"paused": {
			modify: func(m *domain.Monitor) {
				m.IsActive = false
			},
			want: 0,
		},
		"claimed by another checker": {
			modify:       unchanged,
			claimedUntil: new(now.Add(time.Microsecond)),
			want:         0,
		},
		"claim expired": {
			modify:       unchanged,
			claimedUntil: new(now),
			want:         1,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			pool := newPool(t)
			checks := postgres.NewChecks(pool)
			stored := storeMonitor(t, pool, test.modify)
			setClaimedUntil(t, pool, stored.ID, test.claimedUntil)

			got, err := checks.ClaimDue(t.Context(), now, retryDelay, leaseMargin, 10)

			require.NoError(t, err)
			assert.Len(t, got, test.want)
		})
	}
}

func TestChecksClaimDueTakesOldestFirst(t *testing.T) {
	pool := newPool(t)
	checks := postgres.NewChecks(pool)
	oldest := storeMonitor(t, pool, func(m *domain.Monitor) {
		m.NextCheckAt = fixedNow().Add(-3 * time.Minute)
	})
	older := storeMonitor(t, pool, func(m *domain.Monitor) {
		m.NextCheckAt = fixedNow().Add(-2 * time.Minute)
	})
	storeMonitor(t, pool, func(m *domain.Monitor) {
		m.NextCheckAt = fixedNow().Add(-time.Minute)
	})
	want := []uuid.UUID{oldest.ID, older.ID}

	got, err := checks.ClaimDue(t.Context(), fixedNow(), retryDelay, leaseMargin, len(want))

	require.NoError(t, err)
	assert.ElementsMatch(t, want, claimedIDs(got))
}

func TestChecksClaimDueConcurrentClaimsDoNotOverlap(t *testing.T) {
	const monitors = 20
	pool := newPool(t)
	checks := postgres.NewChecks(pool)
	want := make([]uuid.UUID, 0, monitors)
	for range monitors {
		want = append(want, storeMonitor(t, pool, unchanged).ID)
	}
	now := fixedNow().Add(time.Hour)
	claimed := make([][]domain.Claim, 10)
	var wg sync.WaitGroup

	for i := range claimed {
		wg.Go(func() {
			var err error
			claimed[i], err = checks.ClaimDue(t.Context(), now, retryDelay, leaseMargin, 3)
			assert.NoError(t, err)
		})
	}
	wg.Wait()

	assert.ElementsMatch(t, want, claimedIDs(slices.Concat(claimed...)))
}

func TestChecksRecord(t *testing.T) {
	checkedAt := fixedNow().Add(time.Hour)
	until := checkedAt.Add(time.Minute)
	tests := map[string]struct {
		result domain.CheckResult
		want   storedCheck
	}{
		"response received": {
			result: domain.CheckResult{
				IsUp:       true,
				StatusCode: 200,
				Latency:    1500 * time.Microsecond,
				Attempts:   1,
			},
			want: storedCheck{
				IsUp:       true,
				StatusCode: new(int16(200)),
				LatencyMS:  1,
				Attempts:   1,
			},
		},
		"no response": {
			result: domain.CheckResult{
				Latency:  5 * time.Second,
				Attempts: 3,
				Error:    "timeout",
			},
			want: storedCheck{
				LatencyMS: 5000,
				Attempts:  3,
				Error:     new("timeout"),
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			pool := newPool(t)
			stored := storeMonitor(t, pool, unchanged)
			setClaimedUntil(t, pool, stored.ID, &until)
			check := &domain.Check{MonitorID: stored.ID, CheckedAt: checkedAt, Result: test.result}
			wantCheck := test.want
			wantCheck.CheckedAt = checkedAt
			wantMonitor := *stored
			wantMonitor.LastCheckAt = &checkedAt

			err := postgres.NewChecks(pool).Record(t.Context(), check, until)

			require.NoError(t, err)
			assert.Equal(t, []storedCheck{wantCheck}, storedChecks(t, pool, stored.ID))
			assert.Equal(t, &wantMonitor, getMonitor(t, postgres.NewMonitors(pool), stored.ID))
			assert.Nil(t, claimedUntil(t, pool, stored.ID))
		})
	}
}

func TestChecksRecordRejectsLostClaim(t *testing.T) {
	until := fixedNow().Add(time.Minute)
	tests := map[string]struct {
		claimedUntil *time.Time
	}{
		"claim taken over": {claimedUntil: new(until.Add(time.Microsecond))},
		"claim released":   {claimedUntil: nil},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			pool := newPool(t)
			stored := storeMonitor(t, pool, unchanged)
			setClaimedUntil(t, pool, stored.ID, test.claimedUntil)
			check := &domain.Check{
				MonitorID: stored.ID,
				CheckedAt: fixedNow(),
				Result:    domain.CheckResult{Attempts: 1},
			}

			err := postgres.NewChecks(pool).Record(t.Context(), check, until)

			require.ErrorIs(t, err, domain.ErrClaimLost)
			assert.Empty(t, storedChecks(t, pool, stored.ID))
			assert.Equal(t, stored, getMonitor(t, postgres.NewMonitors(pool), stored.ID))
			assert.Equal(t, test.claimedUntil, claimedUntil(t, pool, stored.ID))
		})
	}
}

func TestChecksRecordMonitorNotFound(t *testing.T) {
	pool := newPool(t)
	check := &domain.Check{
		MonitorID: uuid.NewV7(),
		CheckedAt: fixedNow(),
		Result:    domain.CheckResult{Attempts: 1},
	}

	err := postgres.NewChecks(pool).Record(t.Context(), check, fixedNow())

	require.ErrorIs(t, err, domain.ErrClaimLost)
	assert.Empty(t, storedChecks(t, pool, check.MonitorID))
}

func TestChecksRelease(t *testing.T) {
	now := fixedNow().Add(time.Hour)
	until := now.Add(time.Minute)
	tests := map[string]struct {
		nextCheckAt time.Time
		want        time.Time
	}{
		"claimed schedule is pulled back to now": {
			nextCheckAt: now.Add(time.Minute),
			want:        now,
		},
		"earlier schedule is kept": {
			nextCheckAt: now.Add(-time.Minute),
			want:        now.Add(-time.Minute),
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			pool := newPool(t)
			stored := storeMonitor(t, pool, func(m *domain.Monitor) {
				m.NextCheckAt = test.nextCheckAt
			})
			setClaimedUntil(t, pool, stored.ID, &until)
			want := *stored
			want.NextCheckAt = test.want

			err := postgres.NewChecks(pool).Release(t.Context(), stored.ID, until, now)

			require.NoError(t, err)
			assert.Equal(t, &want, getMonitor(t, postgres.NewMonitors(pool), stored.ID))
			assert.Nil(t, claimedUntil(t, pool, stored.ID))
		})
	}
}

func TestChecksReleaseKeepsForeignClaim(t *testing.T) {
	pool := newPool(t)
	stored := storeMonitor(t, pool, func(m *domain.Monitor) {
		m.NextCheckAt = fixedNow().Add(time.Minute)
	})
	foreign := fixedNow().Add(time.Hour)
	setClaimedUntil(t, pool, stored.ID, &foreign)

	err := postgres.NewChecks(pool).Release(t.Context(), stored.ID, fixedNow(), fixedNow())

	require.NoError(t, err)
	assert.Equal(t, stored, getMonitor(t, postgres.NewMonitors(pool), stored.ID))
	assert.Equal(t, &foreign, claimedUntil(t, pool, stored.ID))
}

func TestChecksReleaseMonitorNotFound(t *testing.T) {
	pool := newPool(t)

	err := postgres.NewChecks(pool).Release(t.Context(), uuid.NewV7(), fixedNow(), fixedNow())

	assert.NoError(t, err)
}
