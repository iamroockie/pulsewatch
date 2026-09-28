-- +goose Up
CREATE TABLE checks (
    monitor_id UUID NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
    checked_at TIMESTAMPTZ NOT NULL,
    is_up BOOLEAN NOT NULL,
    status_code SMALLINT,
    latency_ms INTEGER NOT NULL,
    attempts SMALLINT NOT NULL,
    error TEXT,
    PRIMARY KEY (monitor_id, checked_at) INCLUDE (is_up),
    CONSTRAINT checks_latency_check CHECK (latency_ms >= 0),
    CONSTRAINT checks_status_check CHECK (status_code IS NULL OR status_code BETWEEN 100 AND 599)
) WITH (
    autovacuum_vacuum_insert_scale_factor = 0,
    autovacuum_vacuum_insert_threshold = 100000
);

-- +goose Down
DROP TABLE checks;
