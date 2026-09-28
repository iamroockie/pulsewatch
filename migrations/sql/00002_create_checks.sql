-- +goose Up
CREATE TABLE checks (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    monitor_id UUID NOT NULL REFERENCES monitors (id) ON DELETE CASCADE,
    checked_at TIMESTAMPTZ NOT NULL,
    is_up BOOLEAN NOT NULL,
    status_code SMALLINT,
    latency_ms INTEGER NOT NULL,
    attempts SMALLINT NOT NULL,
    error TEXT,
    CONSTRAINT checks_latency_check CHECK (latency_ms >= 0),
    CONSTRAINT checks_status_check CHECK (status_code IS NULL OR status_code BETWEEN 100 AND 599)
);

CREATE INDEX checks_monitor_time_idx ON checks (monitor_id, checked_at DESC);

-- +goose Down
DROP TABLE checks;
