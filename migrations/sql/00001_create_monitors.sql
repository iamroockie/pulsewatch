-- +goose Up
CREATE TABLE monitors (
    id UUID PRIMARY KEY,
    url TEXT NOT NULL,
    interval_seconds INTEGER NOT NULL,
    timeout_ms INTEGER NOT NULL,
    max_retries SMALLINT NOT NULL,
    is_active BOOLEAN NOT NULL,
    next_check_at TIMESTAMPTZ NOT NULL,
    last_check_at TIMESTAMPTZ,
    claimed_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT monitors_url_check CHECK (url ~ '^https?://'),
    CONSTRAINT monitors_interval_check CHECK (interval_seconds > 0),
    CONSTRAINT monitors_timeout_check CHECK (timeout_ms > 0),
    CONSTRAINT monitors_retries_check CHECK (max_retries >= 0)
);

CREATE INDEX monitors_due_idx ON monitors (next_check_at) WHERE is_active;

-- +goose Down
DROP TABLE monitors;
