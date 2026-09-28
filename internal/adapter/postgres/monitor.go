package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

const monitorColumns = `id, url, interval_seconds, timeout_ms, max_retries, is_active,
	next_check_at, last_check_at, created_at, updated_at`

type Monitors struct {
	pool *pgxpool.Pool
}

func NewMonitors(pool *pgxpool.Pool) *Monitors {
	return &Monitors{pool: pool}
}

func (r *Monitors) Create(ctx context.Context, m *domain.Monitor) error {
	query := `
		INSERT INTO monitors (` + monitorColumns + `)
		VALUES (
			@id, @url, @interval_seconds, @timeout_ms, @max_retries, @is_active,
			@next_check_at, @last_check_at, @created_at, @updated_at
		)
	`

	_, err := r.pool.Exec(ctx, query, pgx.StrictStructArgs(rowFromDomain(m)))
	if err != nil {
		return fmt.Errorf("insert monitor: %w", err)
	}

	return nil
}

func (r *Monitors) Get(ctx context.Context, id uuid.UUID) (*domain.Monitor, error) {
	query := `
		SELECT ` + monitorColumns + `
		FROM monitors
		WHERE id = $1
	`

	return queryMonitor(ctx, r.pool, query, id)
}

func (r *Monitors) List(
	ctx context.Context, after uuid.UUID, limit int,
) ([]*domain.Monitor, error) {
	query := `
		SELECT ` + monitorColumns + `
		FROM monitors
		WHERE id > $1
		ORDER BY id
		LIMIT $2
	`

	rows, err := r.pool.Query(ctx, query, after, limit)
	if err != nil {
		return nil, fmt.Errorf("select monitors: %w", err)
	}

	monitors, err := pgx.CollectRows(rows, scanMonitor)
	if err != nil {
		return nil, fmt.Errorf("select monitors: %w", err)
	}

	return monitors, nil
}

func (r *Monitors) Update(
	ctx context.Context, id uuid.UUID, fn func(*domain.Monitor) error,
) (*domain.Monitor, error) {
	lockQuery := `
		SELECT ` + monitorColumns + `
		FROM monitors
		WHERE id = $1
		FOR UPDATE
	`
	updateQuery := `
		UPDATE monitors
		SET url = @url,
			interval_seconds = @interval_seconds,
			timeout_ms = @timeout_ms,
			max_retries = @max_retries,
			is_active = @is_active,
			next_check_at = @next_check_at,
			updated_at = @updated_at
		WHERE id = @id
		RETURNING ` + monitorColumns

	var updated *domain.Monitor

	err := pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		m, err := queryMonitor(ctx, tx, lockQuery, id)
		if err != nil {
			return err
		}

		if err := fn(m); err != nil {
			return err
		}

		updated, err = queryMonitor(ctx, tx, updateQuery, pgx.StructArgs(rowFromDomain(m)))

		return err
	})
	if err != nil {
		return nil, err
	}

	return updated, nil
}

func (r *Monitors) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM monitors WHERE id = $1`

	tag, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete monitor: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrMonitorNotFound
	}

	return nil
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func queryMonitor(
	ctx context.Context, q querier, sql string, args ...any,
) (*domain.Monitor, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query monitor: %w", err)
	}

	m, err := pgx.CollectExactlyOneRow(rows, scanMonitor)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrMonitorNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query monitor: %w", err)
	}

	return m, nil
}

type monitorRow struct {
	ID              uuid.UUID  `db:"id"`
	URL             string     `db:"url"`
	IntervalSeconds int64      `db:"interval_seconds"`
	TimeoutMS       int64      `db:"timeout_ms"`
	MaxRetries      int32      `db:"max_retries"`
	IsActive        bool       `db:"is_active"`
	NextCheckAt     time.Time  `db:"next_check_at"`
	LastCheckAt     *time.Time `db:"last_check_at"`
	CreatedAt       time.Time  `db:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at"`
}

func scanMonitor(row pgx.CollectableRow) (*domain.Monitor, error) {
	r, err := pgx.RowToStructByName[monitorRow](row)
	if err != nil {
		return nil, err
	}

	return r.toDomain(), nil
}

func rowFromDomain(m *domain.Monitor) monitorRow {
	return monitorRow{
		ID:              m.ID,
		URL:             m.Settings.URL,
		IntervalSeconds: int64(m.Settings.Interval / time.Second),
		TimeoutMS:       int64(m.Settings.Timeout / time.Millisecond),
		MaxRetries:      m.Settings.MaxRetries,
		IsActive:        m.IsActive,
		NextCheckAt:     m.NextCheckAt,
		LastCheckAt:     m.LastCheckAt,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

func (r monitorRow) toDomain() *domain.Monitor {
	return &domain.Monitor{
		ID:       r.ID,
		IsActive: r.IsActive,
		Settings: domain.CheckSettings{
			URL:        r.URL,
			Interval:   time.Duration(r.IntervalSeconds) * time.Second,
			Timeout:    time.Duration(r.TimeoutMS) * time.Millisecond,
			MaxRetries: r.MaxRetries,
		},
		NextCheckAt: r.NextCheckAt,
		LastCheckAt: r.LastCheckAt,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}
