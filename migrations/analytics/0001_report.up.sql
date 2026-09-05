-- labor-performance "Labor Performance Report" analytics read model
-- (ADR-0007).
--
-- This is the ANALYTICAL database, separate from the OLTP database in
-- /migrations. It is written only by cmd/labor-projector and read
-- (read-only) by cmd/labor-reports. The tables here are projections
-- derived from this service's own domain-event stream on
-- warehouse.labor-performance.analytics, not sources of truth: the whole
-- schema can be dropped and rebuilt by replaying that topic from its
-- earliest offset.

-- Idempotency + freshness: every applied analytics event id is recorded
-- here exactly once. applied_at is wall-clock insert time; occurred_at is
-- the event's own emission time, used to compute the projection's
-- freshness lag.
CREATE TABLE analytics_processed_events (
    event_id    TEXT PRIMARY KEY,
    occurred_at TIMESTAMPTZ NOT NULL,
    applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_analytics_processed_events_occurred_at
    ON analytics_processed_events (occurred_at DESC);

-- Consumer-level dedupe set, used by the inbound analytics consumer's
-- ProcessedEvents gate. It is kept SEPARATE from
-- analytics_processed_events (which the projection UPSERT claims) so the
-- two idempotency layers never race to claim the same event_id: the
-- consumer gate admits the event, the projection then records its effect.
CREATE TABLE analytics_consumed_events (
    event_id     TEXT PRIMARY KEY,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The labor rollup fact table: one row per (task_type, hour_bucket).
--
-- Only RAW counters and sums are stored — never a pre-computed mean — so
-- each projection apply stays a single += UPSERT and every mean is derived
-- at read time. That is what makes "a bucket with no scored tasks has NO
-- efficiency number, not 0.0" a property of one place in the read model
-- (report.mean) rather than something every writer must remember, which is
-- the analytical restatement of ADR-0004's never-fabricate-a-number rule.
--
-- The counter pairs are deliberately not derivable from one another:
--   tasks_recorded  every TaskPerformanceRecorded, including unscorable
--                   ones (no associate, no duration, no standard) — these
--                   are real business facts and must still be counted.
--   tasks_scored    the subset that carried a non-nil EfficiencyPct; the
--                   denominator of mean efficiency.
--   tasks_measured  the subset whose ActualSeconds was > 0; the
--                   denominator of mean actual seconds. A task can be
--                   measured but unscored (a real duration with no
--                   engineered standard to compare it to — the ADR-0006
--                   distinction), so this is NOT the same subset as
--                   tasks_scored.
--
-- task_type is never empty: the projector maps an unclassified TaskType to
-- the explicit 'UNCLASSIFIED' label (report.NormalizeTaskType) so the key
-- column stays meaningful and the bucket is legible as its own bar on a
-- chart.
CREATE TABLE labor_performance_rollup (
    task_type          TEXT NOT NULL,
    hour_bucket        TIMESTAMPTZ NOT NULL,
    tasks_recorded     BIGINT NOT NULL DEFAULT 0,
    tasks_scored       BIGINT NOT NULL DEFAULT 0,
    efficiency_pct_sum DOUBLE PRECISION NOT NULL DEFAULT 0,
    tasks_measured     BIGINT NOT NULL DEFAULT 0,
    actual_seconds_sum BIGINT NOT NULL DEFAULT 0,
    standards_defined  BIGINT NOT NULL DEFAULT 0,
    standards_revised  BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (task_type, hour_bucket)
);

-- The report's primary access pattern is a time-window scan across every
-- task type, so hour_bucket leads its own index independently of the
-- (task_type, hour_bucket) primary key.
CREATE INDEX idx_labor_performance_rollup_hour_bucket
    ON labor_performance_rollup (hour_bucket);
