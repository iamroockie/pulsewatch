-- +goose Up
create table checks (
    id          bigint      generated always as identity primary key,
    monitor_id  uuid        not null references monitors (id) on delete cascade,
    checked_at  timestamptz not null,
    is_up       boolean     not null,
    status_code smallint,
    latency_ms  integer     not null,
    attempts    smallint    not null,
    error       text,
    constraint checks_latency_check check (latency_ms >= 0),
    constraint checks_status_check check (status_code is null or status_code between 100 and 599)
);

create index checks_monitor_time_idx on checks (monitor_id, checked_at desc);

-- +goose Down
drop table checks;
