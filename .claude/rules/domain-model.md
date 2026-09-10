# Domain model: ubiquitous language, aggregates, invariants, use cases

## Why this context exists

Competitor research (Manhattan Active Labor Management, Blue Yonder
Workforce & Labor Management) converges on ONE capability neither
`workforce-management` nor `fulfillment-execution` has today: engineered
labor standards (expected time-per-task-type) and actual-vs-standard
performance scoring. Both vendors treat it as a distinct product capability,
not a sub-feature of shift/schedule planning.

`workforce-management` owns "who is on shift, on which PATH, at what rate"
and explicitly stops at the path boundary, never linking an associate to a
task. `fulfillment-execution` owns Task/Station lifecycle and has zero
concept of a "standard" to measure a completion against — adding one there
would be scope creep into a domain neither aggregate has any business
modeling. A new bounded context is the correct home because it inherits
neither sibling's boundary promise. See ADR 0002.

It is a **Supporting** subdomain (not Core, not Generic): valuable and
real, but it measures execution quality against a standard someone else
configures — it does not define the fulfillment work itself.

## Bounded-context boundary (non-negotiable)

- Pure Kafka **consumer** of `fulfillment-execution`'s `TaskCompleted`
  event on `warehouse.fulfillment.events` (shared/fan-out topic,
  `wes-work-planning` also consumes it under its own consumer group). Only
  `event_type == "TaskCompleted"` is acted on; every other type is silently
  skipped, not an error.
- Separate Go module/repo: no Go import from and no write access to
  `fulfillment-execution` or `workforce-management`'s aggregates. No REST
  dependency on either — everything needed (`AssociateId`, `TaskType`,
  `DurationSeconds`) already travels on the Kafka event.
- This context DOES expose its own REST Open Host Service (standards +
  performance reads), symmetric with every other fleet context. CORS is
  wired proactively for the future `labor-mfe` remote.
- No relationship at all to `workforce-management` — different concepts
  entirely, zero Go-import and zero REST dependency.

See ADR 0002, ADR 0003.

## Ubiquitous language

| Term | Meaning |
| --- | --- |
| **LaborStandard** | Aggregate root: expected duration for a `TaskType` (PICK/PACK/SLAM — mirrors `fulfillment-execution`'s `task.Type` exactly), with append-only revision history. `ExpectedSeconds` must be > 0. ONE active standard per TaskType at any time. |
| **TaskPerformance** | Aggregate root: one scored, already-completed task. Immutable once recorded (event-sourced fact from Kafka, no update/delete use case). |
| **Scorecard** | Read model (NOT a stored aggregate) — projection over `TaskPerformance` rows per associate: task count, mean `EfficiencyPct`, per-`TaskType` breakdown, `trend`, `coachingFlag`. |
| **TaskTypePerformance** | Fleet-wide (all-associates) read model per TaskType — the "labor monitoring" view, independent of any one associate. |
| **EfficiencyPct** | `100 * StandardSecondsAtCompletion / ActualSeconds`, nullable, never computed by dividing by zero. |

## LaborStandard invariants

- `ExpectedSeconds` must be > 0.
- **Append-only history.** `DefineStandard` for a TaskType that already has
  an active standard does NOT overwrite in place — it closes the prior
  standard's effective range (`EffectiveTo`) and starts a new one, so
  already-recorded `TaskPerformance` rows' frozen
  `StandardSecondsAtCompletion` stay historically accurate. Mirrors
  `workforce-management`'s "AssignLabor ends the prior assignment rather
  than rejecting" pattern. See ADR 0004.

## TaskPerformance invariants

- **Immutable once recorded.**
- **Idempotent on the Kafka message's `event_id`**, not `TaskId` (which
  could in principle be reused after a very long time). Recording the same
  `event_id` twice is a no-op, mirroring the `ProcessedEvents`
  idempotency-gate pattern every analytics projector in this fleet uses.
- **`EfficiencyPct` never divides by zero.** `ActualSeconds<=0` (e.g. a
  `TaskCompleted` whose `duration_seconds` is 0 because no claim-timestamp
  existed) or `StandardSecondsAtCompletion<=0` (no active standard existed
  for that TaskType at completion time) both yield `EfficiencyPct=nil` —
  a real, expected business fact, never an error and never a fabricated
  number.
- **`StandardSecondsAtCompletion` is frozen at ingestion time** — resolved
  once as "the standard active AS OF `CompletedAt`," not "active right
  now," and never recomputed later from a since-changed standard. This
  means a possibly out-of-order or replayed Kafka message is scored
  against the standard genuinely in force when the task completed.
- **An empty `AssociateId` is legitimate**, not an error — a `TaskCompleted`
  from a station with no checked-in occupant (e.g. a robot station) is
  still recorded and counted in `GetTaskTypePerformance`, just excluded
  from any per-associate scorecard.

## Domain events (past tense)

`LaborStandardDefined`, `LaborStandardRevised`, `TaskPerformanceRecorded`.
Default publisher is a log publisher; `EVENT_PUBLISHER=kafka` fans them
additionally onto `warehouse.labor-performance.analytics` (the dedicated
analytics topic feeding `cmd/labor-projector`, ADR 0007), routed through
the transactional outbox when `DATABASE_URL` is set (ADR 0010). Not
consumed by any other repo in the fleet today — published for symmetry and
to leave an integration seam open.

## Use cases (application layer)

1. `DefineStandard(taskType, expectedSeconds) -> LaborStandard` — closes
   any prior active standard for that TaskType, starts a new one.
2. `GetStandard(taskType) -> LaborStandard | 404`
3. `RecordTaskPerformance(taskId, associateId, taskType, actualSeconds,
   completedAt, kafkaEventId) -> TaskPerformance` — the Kafka-consumer-
   driven use case, called from the inbound Kafka adapter only, never
   from HTTP. Idempotent on `kafkaEventId`.
4. `GetAssociateScorecard(associateId) -> Scorecard | 404` — 404 only when
   this service has NEVER recorded a TaskPerformance row for this
   associate; an associate with 1+ rows but all-nil efficiency returns 200
   with `meanEfficiencyPct: null`.
5. `GetTaskTypePerformance(taskType) -> TaskTypePerformance` — always 200,
   including a zero-count result for a TaskType never recorded.

## REST API (OLTP, `apis/openapi.yaml`)

| Method | Path | Use case |
| --- | --- | --- |
| `POST` | `/standards` | DefineStandard |
| `GET` | `/standards/{taskType}` | GetStandard |
| `GET` | `/associates/{associateId}/scorecard` | GetAssociateScorecard |
| `GET` | `/task-types/{taskType}/performance` | GetTaskTypePerformance |
| `GET` | `/healthz` | Liveness probe |

There is deliberately **no** REST endpoint for `RecordTaskPerformance` — it
is exclusively Kafka-consumer-driven. Every error is RFC 7807
`application/problem+json`, identical shape across every fleet service.

## Inbound Kafka contract (`apis/asyncapi.yaml`, inbound section)

Subscribes to `warehouse.fulfillment.events`. Envelope (CloudEvents-like,
shared across the fleet):

```json
{
  "event_id": "uuid-v4",
  "event_type": "TaskCompleted",
  "occurred_at": "2026-08-29T22:00:00Z",
  "source": "fulfillment-execution",
  "data": {
    "task_id": "...", "station_id": "...", "work_unit_id": "...",
    "associate_id": "...", "duration_seconds": 52
  }
}
```

`associate_id` and `duration_seconds` are OPTIONAL on the wire — an older
payload predating `fulfillment-execution`'s enrichment omits them, and this
service's JSON unmarshaling degrades them to `""`/`0` (the same "no
occupant"/"unmeasurable" business facts already modeled, not an error).

**Known wire-contract gap:** `fulfillment-execution`'s `TaskCompleted`
payload does not yet carry a `task_type` field. This service resolves
`TaskType` as `""` (unclassified) for every consumed event as a result — a
`""`-typed row is still recorded and counted, but never resolves a
`LaborStandard` and never appears under `GetTaskTypePerformance` (which
requires PICK/PACK/SLAM). Reports this as `UNCLASSIFIED` on the analytics
side (ADR 0007).

## v1 scope decisions still in force

- **No automatic pay-for-performance / bonus calculation** (a real
  Manhattan feature, "Pay for Performance") — this context surfaces the
  number, a human/other system decides what to do with it.
- **No gamification or automated coaching workflows.** v1 (ADR 0005) ships
  the passive `Trend`/`CoachingFlag` signal only, a human reads it.
- **No REST dependency in either direction with any sibling context.**
- **No gating/blocking of `fulfillment-execution`** — a below-standard
  associate is still allowed to claim tasks. Visibility, not enforcement.

## Now built (fleet-parity pass, no longer deferred from the original v1)

Helm chart, godog/BDD acceptance tests, Postgres integration tests
(testcontainers), Gremlins mutation-testing gate, arch-go fitness tests,
Spectral linting (all 3 specs), CodeQL/Scorecard/Trivy, Docker publish +
release automation, full Docusaurus site, the analytics data product
(ADR 0007), the transactional outbox (ADR 0010), and (per ADR 0012) the
REST/MCP auth layer was added then explicitly removed again — see
`.claude/rules/adrs-and-decisions.md`.
