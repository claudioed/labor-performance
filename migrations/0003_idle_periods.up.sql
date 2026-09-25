CREATE TABLE idle_periods (
    id            BIGSERIAL PRIMARY KEY,
    associate_id  TEXT NOT NULL,
    task_type     TEXT NOT NULL DEFAULT '',
    started_at    TIMESTAMPTZ NOT NULL,
    ended_at      TIMESTAMPTZ NOT NULL,
    seconds       BIGINT NOT NULL,
    capped        BOOLEAN NOT NULL DEFAULT false
);

CREATE INDEX idx_idle_periods_associate_id_ended_at ON idle_periods (associate_id, ended_at);
CREATE INDEX idx_idle_periods_task_type_ended_at ON idle_periods (task_type, ended_at);
