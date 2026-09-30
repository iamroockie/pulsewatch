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

type dueCase struct {
	modify       func(*domain.Monitor)
	claimedUntil *time.Time
	want         int
}

func dueCases(now time.Time) map[string]dueCase {
	return map[string]dueCase{
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
}

func claimedIDs(claims []domain.Claim) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(claims))
	for _, c := range claims {
		ids = append(ids, c.Monitor.ID)
	}

	return ids
}

func insertCheck(t *testing.T, pool *pgxpool.Pool, c *domain.Check) {
	t.Helper()

	query := `
		INSERT INTO checks (
			monitor_id, checked_at, is_up, status_code, latency_ms, attempts, error
		)
		VALUES ($1, $2, $3, NULLIF($4, 0), $5, $6, NULLIF($7, ''))
	`

	_, err := pool.Exec(t.Context(), query, c.MonitorID, c.CheckedAt, c.Result.IsUp,
		c.Result.StatusCode, c.Result.Latency.Milliseconds(), c.Result.Attempts, c.Result.Error)
	require.NoError(t, err)
}

func checkAt(monitorID uuid.UUID, checkedAt time.Time, isUp bool) *domain.Check {
	return &domain.Check{
		MonitorID: monitorID,
		CheckedAt: checkedAt,
		Result: domain.CheckResult{
			IsUp:       isUp,
			StatusCode: 200,
			Latency:    42 * time.Millisecond,
			Attempts:   1,
		},
	}
}

func storedCheckTimes(t *testing.T, pool *pgxpool.Pool, monitorID uuid.UUID) []time.Time {
	t.Helper()

	checks := storedChecks(t, pool, monitorID)
	times := make([]time.Time, 0, len(checks))
	for _, c := range checks {
		times = append(times, c.CheckedAt)
	}

	return times
}

func TestChecksClaimDue(t *testing.T) {
	pool := newPool(t)
	checks := postgres.NewChecks(pool)
	stored := storeMonitor(t, pool, unchanged)
	now := fixedNow().Add(time.Hour)
	monitor := *stored
	monitor.NextCheckAt = now.Add(stored.Settings.Interval)
	until := now.Add(35 * time.Second)
	want := []domain.Claim{{Monitor: &monitor, DueAt: stored.NextCheckAt, Until: until}}

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

	for name, test := range dueCases(now) {
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

func TestChecksCountDue(t *testing.T) {
	now := fixedNow().Add(time.Hour)

	for name, test := range dueCases(now) {
		t.Run(name, func(t *testing.T) {
			pool := newPool(t)
			stored := storeMonitor(t, pool, test.modify)
			setClaimedUntil(t, pool, stored.ID, test.claimedUntil)

			got, err := postgres.NewChecks(pool).CountDue(t.Context(), now)

			require.NoError(t, err)
			assert.Equal(t, int64(test.want), got)
		})
	}
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
	retryAt := fixedNow().Add(time.Hour)
	until := retryAt.Add(time.Minute)
	tests := map[string]struct {
		nextCheckAt time.Time
		want        time.Time
	}{
		"claimed schedule is pulled back to retry time": {
			nextCheckAt: retryAt.Add(time.Minute),
			want:        retryAt,
		},
		"earlier schedule is kept": {
			nextCheckAt: retryAt.Add(-time.Minute),
			want:        retryAt.Add(-time.Minute),
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

			err := postgres.NewChecks(pool).Release(t.Context(), stored.ID, until, retryAt)

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

func TestChecksList(t *testing.T) {
	pool := newPool(t)
	checks := postgres.NewChecks(pool)
	stored := storeMonitor(t, pool, unchanged)
	foreign := storeMonitor(t, pool, unchanged)
	newest := checkAt(stored.ID, fixedNow().Add(-time.Minute), true)
	failed := &domain.Check{
		MonitorID: stored.ID,
		CheckedAt: fixedNow().Add(-2 * time.Minute),
		Result:    domain.CheckResult{Latency: 5 * time.Second, Attempts: 3, Error: "timeout"},
	}
	oldest := checkAt(stored.ID, fixedNow().Add(-3*time.Minute), false)
	for _, c := range []*domain.Check{oldest, newest, failed} {
		insertCheck(t, pool, c)
	}
	insertCheck(t, pool, checkAt(foreign.ID, fixedNow().Add(-90*time.Second), true))
	tests := map[string]struct {
		before time.Time
		limit  int
		want   []*domain.Check
	}{
		"newest first":        {limit: 10, want: []*domain.Check{newest, failed, oldest}},
		"limited":             {limit: 2, want: []*domain.Check{newest, failed}},
		"before is exclusive": {before: failed.CheckedAt, limit: 10, want: []*domain.Check{oldest}},
		"before oldest":       {before: oldest.CheckedAt, limit: 10, want: []*domain.Check{}},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := checks.List(t.Context(), stored.ID, test.before, test.limit)

			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestChecksUptime(t *testing.T) {
	now := fixedNow().Add(time.Hour)
	type ageCheck struct {
		age  time.Duration
		isUp bool
	}
	tests := map[string]struct {
		checks []ageCheck
		want   domain.UptimeReport
	}{
		"no checks": {
			checks: nil,
			want:   domain.UptimeReport{},
		},
		"window boundaries": {
			checks: []ageCheck{
				{age: 30 * time.Minute, isUp: true},
				{age: time.Hour - time.Microsecond, isUp: false},
				{age: time.Hour, isUp: true},
				{age: 24*time.Hour - time.Microsecond, isUp: false},
				{age: 24 * time.Hour, isUp: true},
				{age: 7*24*time.Hour - time.Microsecond, isUp: true},
				{age: 7 * 24 * time.Hour, isUp: false},
			},
			want: domain.UptimeReport{
				Hour: domain.Uptime{Checks: 2, Up: 1},
				Day:  domain.Uptime{Checks: 4, Up: 2},
				Week: domain.Uptime{Checks: 6, Up: 4},
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			pool := newPool(t)
			stored := storeMonitor(t, pool, unchanged)
			foreign := storeMonitor(t, pool, unchanged)
			for _, c := range test.checks {
				insertCheck(t, pool, checkAt(stored.ID, now.Add(-c.age), c.isUp))
			}
			insertCheck(t, pool, checkAt(foreign.ID, now.Add(-time.Minute), false))

			got, err := postgres.NewChecks(pool).Uptime(t.Context(), stored.ID, now)

			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestChecksDeleteBefore(t *testing.T) {
	pool := newPool(t)
	cutoff := fixedNow()
	active := storeMonitor(t, pool, unchanged)
	paused := storeMonitor(t, pool, func(m *domain.Monitor) {
		m.IsActive = false
	})
	for _, m := range []*domain.Monitor{active, paused} {
		insertCheck(t, pool, checkAt(m.ID, cutoff.Add(-time.Microsecond), true))
		insertCheck(t, pool, checkAt(m.ID, cutoff, true))
		insertCheck(t, pool, checkAt(m.ID, cutoff.Add(time.Minute), true))
	}
	want := []time.Time{cutoff, cutoff.Add(time.Minute)}

	got, err := postgres.NewChecks(pool).DeleteBefore(t.Context(), cutoff)

	require.NoError(t, err)
	assert.Equal(t, int64(2), got)
	assert.ElementsMatch(t, want, storedCheckTimes(t, pool, active.ID))
	assert.ElementsMatch(t, want, storedCheckTimes(t, pool, paused.ID))
}

func TestChecksDeleteBeforeConcurrentCallsDeleteOnce(t *testing.T) {
	const monitors = 10
	pool := newPool(t)
	cutoff := fixedNow()
	for range monitors {
		m := storeMonitor(t, pool, unchanged)
		insertCheck(t, pool, checkAt(m.ID, cutoff.Add(-time.Minute), true))
		insertCheck(t, pool, checkAt(m.ID, cutoff.Add(-2*time.Minute), true))
	}
	deleted := make([]int64, 4)
	var wg sync.WaitGroup

	for i := range deleted {
		wg.Go(func() {
			var err error
			deleted[i], err = postgres.NewChecks(pool).DeleteBefore(t.Context(), cutoff)
			assert.NoError(t, err)
		})
	}
	wg.Wait()

	var total int64
	for _, n := range deleted {
		total += n
	}
	assert.Equal(t, int64(2*monitors), total)
}
