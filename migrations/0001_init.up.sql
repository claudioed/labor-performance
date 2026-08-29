CREATE TABLE labor_standards (
    id               TEXT PRIMARY KEY,
    task_type        TEXT NOT NULL,
    expected_seconds BIGINT NOT NULL,
    effective_from   TIMESTAMPTZ NOT NULL,
    effective_to     TIMESTAMPTZ
);

CREATE INDEX idx_labor_standards_task_type ON labor_standards (task_type);

CREATE TABLE task_performances (
    event_id                        TEXT PRIMARY KEY,
    task_id                         TEXT NOT NULL,
    associate_id                    TEXT NOT NULL DEFAULT '',
    task_type                       TEXT NOT NULL DEFAULT '',
    actual_seconds                  BIGINT NOT NULL,
    standard_seconds_at_completion  BIGINT NOT NULL,
    efficiency_pct                  DOUBLE PRECISION,
    completed_at                    TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_task_performances_associate_id ON task_performances (associate_id);
CREATE INDEX idx_task_performances_task_type ON task_performances (task_type);

CREATE TABLE processed_events (
    event_id     TEXT PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
