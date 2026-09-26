-- +goose Up
create table monitors (
    id               uuid        primary key,
    url              text        not null,
    interval_seconds integer     not null,
    timeout_ms       integer     not null,
    max_retries      smallint    not null,
    is_active        boolean     not null,
    next_check_at    timestamptz not null,
    last_check_at    timestamptz,
    created_at       timestamptz not null,
    updated_at       timestamptz not null,
    constraint monitors_url_check check (url ~ '^https?://'),
    constraint monitors_interval_check check (interval_seconds > 0),
    constraint monitors_timeout_check check (timeout_ms > 0),
    constraint monitors_retries_check check (max_retries >= 0)
);

create index monitors_due_idx on monitors (next_check_at) where is_active;

-- +goose Down
drop table monitors;
