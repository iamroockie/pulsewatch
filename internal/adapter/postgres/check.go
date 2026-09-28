package postgres

import (
	"context"
	"fmt"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/iamroockie/pulsewatch/internal/domain"
)

type Checks struct {
	pool *pgxpool.Pool
}

func NewChecks(pool *pgxpool.Pool) *Checks {
	return &Checks{pool: pool}
}

func (r *Checks) ClaimDue(
	ctx context.Context,
	now time.Time,
	retryDelay time.Duration,
	margin time.Duration,
	limit int,
) ([]domain.Claim, error) {
	query := `
		WITH due AS (
			SELECT id AS due_id
			FROM monitors
			WHERE is_active
				AND next_check_at <= @now
				AND (claimed_until IS NULL OR claimed_until <= @now)
			ORDER BY next_check_at
			LIMIT @limit
			FOR UPDATE SKIP LOCKED
		)
		UPDATE monitors
		SET claimed_until = @now
				+ (max_retries + 1) * timeout_ms * INTERVAL '1 millisecond'
				+ max_retries * @retry_delay::INTERVAL
				+ @margin::INTERVAL,
			next_check_at = @now + interval_seconds * INTERVAL '1 second'
		FROM due
		WHERE id = due.due_id
		RETURNING ` + monitorColumns + `, claimed_until
	`

	rows, err := r.pool.Query(ctx, query, pgx.NamedArgs{
		"now":         now,
		"retry_delay": retryDelay,
		"margin":      margin,
		"limit":       limit,
	})
	if err != nil {
		return nil, fmt.Errorf("claim monitors: %w", err)
	}

	claims, err := pgx.CollectRows(rows, scanClaim)
	if err != nil {
		return nil, fmt.Errorf("claim monitors: %w", err)
	}

	return claims, nil
}

func (r *Checks) Record(ctx context.Context, check *domain.Check, until time.Time) error {
	query := `
		WITH finished AS (
			UPDATE monitors
			SET last_check_at = @checked_at, claimed_until = NULL
			WHERE id = @monitor_id AND claimed_until = @claimed_until
			RETURNING id
		)
		INSERT INTO checks (
			monitor_id, checked_at, is_up, status_code, latency_ms, attempts, error
		)
		SELECT id, @checked_at, @is_up, @status_code, @latency_ms, @attempts, @error
		FROM finished
	`

	tag, err := r.pool.Exec(ctx, query, pgx.NamedArgs{
		"monitor_id":    check.MonitorID,
		"claimed_until": until,
		"checked_at":    check.CheckedAt,
		"is_up":         check.Result.IsUp,
		"status_code":   nullIfZero(check.Result.StatusCode),
		"latency_ms":    check.Result.Latency.Milliseconds(),
		"attempts":      check.Result.Attempts,
		"error":         nullIfZero(check.Result.Error),
	})
	if err != nil {
		return fmt.Errorf("record check: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrClaimLost
	}

	return nil
}

func (r *Checks) Release(ctx context.Context, id uuid.UUID, until, retryAt time.Time) error {
	query := `
		UPDATE monitors
		SET claimed_until = NULL, next_check_at = LEAST(next_check_at, @retry_at)
		WHERE id = @id AND claimed_until = @claimed_until
	`

	_, err := r.pool.Exec(ctx, query, pgx.NamedArgs{
		"id":            id,
		"claimed_until": until,
		"retry_at":      retryAt,
	})
	if err != nil {
		return fmt.Errorf("release monitor: %w", err)
	}

	return nil
}

type claimRow struct {
	monitorRow

	ClaimedUntil time.Time `db:"claimed_until"`
}

func scanClaim(row pgx.CollectableRow) (domain.Claim, error) {
	r, err := pgx.RowToStructByName[claimRow](row)
	if err != nil {
		return domain.Claim{}, err
	}

	return domain.Claim{Monitor: r.toDomain(), Until: r.ClaimedUntil}, nil
}

func nullIfZero[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}

	return &v
}
