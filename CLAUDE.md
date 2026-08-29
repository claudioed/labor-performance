# Project: Labor Performance (Supporting Bounded Context)

> This is the **eighth** bounded-context Go service in the warehouse-systems
> fleet (after order-management, inventory-storage, wes-work-planning,
> workforce-management, fulfillment-execution, facility-layout,
> warehouse-ops-agent). This CLAUDE.md is the build spec that drove this
> service's v1 implementation (PR #1, merged 2026-08-29).

## Why this context exists (read before writing any code)

Competitor research (Manhattan Active Labor Management, Blue Yonder
Workforce & Labor Management — both real, current products, verified via
their public marketing/feature pages this session) converges on ONE
capability neither `workforce-management` nor `fulfillment-execution` has
today: **engineered labor standards** (an expected time-per-task-type,
e.g. "a PICK should take 45s") and **actual-vs-standard performance
scoring** ("this associate's last PICK took 52s — 87% of standard").
Manhattan calls this "Labor Monitoring — manage actual performance
against standards in real time" + "Time Tracking"; Blue Yonder calls it
"align labor to demand with real-time operational signals." Both treat it
as a distinct product capability, not a sub-feature of shift/schedule
planning.

**This is new domain logic, not a rename of anything that exists.**
`workforce-management` owns "who is on shift, on which PATH, at what
rate" — it explicitly stops at the path boundary and never links an
associate to a task (see its own CLAUDE.md/README "why this context stops
at the path boundary"). `fulfillment-execution` owns Task/Station
lifecycle and now (via a sibling PR, `feature/labor-performance-hooks`)
publishes WHO completed a task and HOW LONG it took as an enrichment on
its existing `TaskCompleted` event — but it has zero concept of a
"standard" to measure that duration against, and adding one there would
be scope creep into a domain neither Task nor Station has any business
modeling (per that repo's own "stops at X boundary" design discipline
that every context in this fleet follows).

A **new** bounded context is the correct home: it does not inherit
`workforce-management`'s "stops at the path boundary" promise or
`fulfillment-execution`'s "Task/Station only" promise, so it is free to
model "here is the standard, here is the actual, here is the score"
without either sibling context having to compromise its own boundary to
host it. It is Supporting (not Core) — it does not define the fulfillment
work itself, only measures how well it is executed against a standard
someone else configures.

## Bounded-context boundary (NON-NEGOTIABLE — read this before writing any code)

This service is a **pure Kafka consumer** of `fulfillment-execution`'s
already-published `TaskCompleted` integration event (topic
`warehouse.fulfillment.events`) — it is a **separate Go module in a
separate repository**, MUST NOT import any Go package from
`fulfillment-execution` or `workforce-management`, and gets **no write
access whatsoever** to either service's aggregates. It never calls into
`fulfillment-execution` or `workforce-management` synchronously — this is
choreography, not orchestration, matching the existing
`wes-work-planning` ← `fulfillment-execution`/`order-management`
consumer pattern already proven in this fleet (see
`wes-work-planning/internal/adapters/inbound/kafka/consumer.go` for the
exact reference shape: a private struct mirroring the producer's JSON,
wired to an existing use case).

`fulfillment-execution` is the **Supplier / Open Host Service** here;
this context is the **Customer**. It has ZERO REST dependency on
`fulfillment-execution` or `workforce-management` — everything it needs
(`AssociateId`, `TaskType`, `DurationSeconds`) is already carried on the
Kafka event.

This context DOES expose its own REST API (to configure standards and to
read performance scores — including for the fleet console's future
`labor-mfe` remote), but that is this context's OWN Open Host Service,
symmetric with every other context in the fleet.

## Architecture (NON-NEGOTIABLE — identical shape to the other six services)

Hexagonal / Ports & Adapters. Strict dependency rule: **domain depends on
nothing; application depends on domain; adapters depend on
application/domain.** No framework, HTTP, Kafka, or SQL types in the
domain layer.

```
cmd/labor/                    main.go — composition root
internal/
  domain/
    standard/                 LaborStandard aggregate: TaskType -> expected duration
    performance/               TaskPerformance aggregate: one scored completed task
    shared/                    value objects: TaskType, AssociateId, events, errors
  application/
    ports/                     OUT: StandardRepo, PerformanceRepo, Clock
                                IN (consumed fact): TaskCompletedFact (see below)
    usecases/                  DefineStandard, RecordTaskPerformance (consumer-driven),
                                GetPerformance, GetAssociateScorecard, GetStandard
  adapters/
    inbound/http/               chi handlers, DTOs, RFC7807 error mapping
    inbound/kafka/               consumes warehouse.fulfillment.events (TaskCompleted only;
                                  ignore/skip every other event type on that topic, same
                                  pattern wes-work-planning already uses for a shared topic)
    outbound/postgres/          pgxpool repo + migrations
    outbound/memory/            in-memory repo for tests
migrations/                   golang-migrate SQL files
apis/openapi.yaml
apis/asyncapi.yaml             (consumer-only: documents what this service SUBSCRIBES to,
                                mirror the shape of an existing repo's consumer-side asyncapi
                                doc if one exists, else document TaskCompleted as an inbound
                                message with this repo as the consumer, source
                                fulfillment-execution)
```

## Ubiquitous Language (use these exact names)

- **LaborStandard** — the aggregate root for "how long a task TYPE should
  take." Fields: `TaskType` (one of fulfillment-execution's existing task
  types — PICK, PACK, SLAM; do not invent new ones, mirror
  `fulfillment-execution`'s `task.Type` enum exactly), `ExpectedSeconds`
  (int64, > 0), `EffectiveFrom` (time.Time — a standard can be revised;
  keep history, do not overwrite in place — see invariant below). ONE
  ACTIVE standard per TaskType at any given time.
- **TaskPerformance** — the aggregate root for one scored, already-
  completed task. Fields: `TaskId` (fulfillment-execution's, treated as an
  opaque foreign reference — this context does not own or validate it),
  `AssociateId` (optional — fulfillment-execution's `TaskCompleted`
  enrichment supplies empty string when the completing station had no
  checked-in occupant, e.g. a robot station; a `TaskPerformance` with no
  `AssociateId` is legitimate and MUST still be recorded and counted in
  aggregate/task-type reporting, just excluded from any per-associate
  scorecard), `TaskType`, `ActualSeconds` (from the event's
  `DurationSeconds`; a `TaskCompleted` with `DurationSeconds` of 0 — e.g.
  emitted for a task claimed before the fulfillment-execution migration
  that added claim-timestamp tracking, so no duration could be computed —
  is a real, expected case: record `ActualSeconds=0`, `EfficiencyPct=nil`
  rather than a computed-but-nonsensical score; this is a business fact
  (unmeasurable), not an error), `StandardSecondsAtCompletion` (the
  `LaborStandard.ExpectedSeconds` that was ACTIVE at the moment this task
  completed — resolved once, at ingestion time, and stored redundantly on
  this aggregate; do NOT recompute it later from a since-changed
  standard, since that would rewrite history), `EfficiencyPct` (nullable
  float: `100 * StandardSecondsAtCompletion / ActualSeconds` when both are
  positive, else null), `CompletedAt`.
- **Scorecard** — a read model (NOT a stored aggregate — a projection over
  `TaskPerformance` rows), per associate: task count, mean `EfficiencyPct`
  across tasks that have one, breakdown by `TaskType`.
- What this context explicitly does NOT do: it does not decide anything
  (no automatic coaching, no automatic pay/bonus calculation despite that
  being a real competitor feature — Manhattan's "Pay for Performance" —
  that is explicitly OUT OF SCOPE for this v1; this context only makes
  the actual-vs-standard picture legible, mirroring `workforce-management`'s
  own "flags a gap, does not decide" design philosophy for
  `PathUnderstaffed`). It does not talk to payroll, HR, or scheduling
  systems. It does not gate or block anything in `fulfillment-execution`
  (a below-standard associate is still allowed to claim tasks — this is
  visibility, not enforcement).

## Aggregates & invariants (enforce in domain, unit-tested — each needs a failing-path test)

- **LaborStandard**: `ExpectedSeconds` must be > 0. `DefineStandard` for a
  TaskType that already has an active standard does NOT overwrite it in
  place — it closes the prior standard's effective range and starts a new
  one (append-only history), so past `TaskPerformance` rows' frozen
  `StandardSecondsAtCompletion` values remain historically accurate even
  after a standard is revised. (Mirrors `workforce-management`'s own
  "AssignLabor ends the prior assignment rather than rejecting" pattern —
  a revision closes the old record rather than erroring.)
- **TaskPerformance**: immutable once recorded (this is an event-sourced
  fact from Kafka, not something a human edits) — no update/delete use
  case. Recording the same `TaskId` twice (e.g. a redelivered/duplicate
  Kafka message) MUST be idempotent — reject/no-op the second attempt
  rather than double-counting (mirror the `ProcessedEvents`
  idempotency-gate pattern already used by every analytics projector in
  this fleet, e.g. `workforce-management`'s
  `internal/application/ports` `ProcessedEvents`, keyed on the Kafka
  message's `event_id` this time since `TaskId` could theoretically be
  reused after a very long time — use `event_id` as the dedup key, not
  `TaskId`).
- **EfficiencyPct computation**: NEVER divide by zero. `ActualSeconds<=0`
  or `StandardSecondsAtCompletion<=0` (no active standard existed for
  that TaskType at completion time) both yield `EfficiencyPct=nil`, not
  an error and not a fabricated number — a `TaskPerformance` for a
  TaskType with no defined standard yet is a normal, expected v1 case
  (this service does not require every TaskType to have a standard
  before consuming events for it).

## Domain events (past tense — this service PUBLISHES its own; log publisher only in v1, no Kafka publish required)

LaborStandardDefined, LaborStandardRevised, TaskPerformanceRecorded.
(These are NOT currently consumed by any other service — publish them for
symmetry with the fleet's convention and to leave an integration seam
open, but no other repo's CLAUDE.md references them yet; do not build a
consumer-side contract for them beyond documenting the shape in
`apis/asyncapi.yaml`.)

## Use cases (application layer)

1. DefineStandard(taskType, expectedSeconds) -> LaborStandard (closes any
   prior active standard for that TaskType, starts a new one)
2. GetStandard(taskType) -> currently-active LaborStandard, or 404
3. RecordTaskPerformance(taskId, associateId, taskType, actualSeconds,
   completedAt, kafkaEventId) -> TaskPerformance. THIS IS THE KAFKA-
   CONSUMER-DRIVEN use case — called from the inbound Kafka adapter, not
   from HTTP. Idempotent on kafkaEventId (see invariant above). Looks up
   the currently-active LaborStandard for taskType (may be none) to
   freeze `StandardSecondsAtCompletion` and compute `EfficiencyPct`.
4. GetAssociateScorecard(associateId) -> Scorecard read model (task
   count, mean efficiency, per-TaskType breakdown) — 404 if the associate
   has zero recorded TaskPerformance rows (do not return an empty-but-200
   scorecard for an associate this service has literally never heard of;
   an associate with 1+ rows but all-nil `EfficiencyPct` returns 200 with
   `meanEfficiencyPct: null`, not 404 — the distinction is "have we ever
   seen this associate" not "do we have a numeric score for them").
5. GetTaskTypePerformance(taskType) -> aggregate read model across ALL
   associates for one TaskType (task count, mean efficiency) — this is
   the fleet-wide view competitors call "labor monitoring," independent
   of any one associate.

## REST API (inbound adapter)

- POST /standards                          -> DefineStandard
- GET  /standards/{taskType}               -> GetStandard
- GET  /associates/{associateId}/scorecard -> GetAssociateScorecard
- GET  /task-types/{taskType}/performance  -> GetTaskTypePerformance
- GET  /healthz

JSON DTOs live in the http adapter; never leak domain structs. Follow the
SAME REST maturity level (Richardson Level 2) and RFC 7807
(`application/problem+json`) error format every other service in this
fleet already uses — copy the `problemDetails` struct and error-mapping
pattern from `inventory-storage`'s http adapter exactly, do not reinvent
it.

CORS middleware (`go-chi/cors`) is enabled on every route, allowing
`CORS_ALLOWED_ORIGINS` (env, default
`http://localhost:5173,http://localhost:5187` — the `warehouse-console`
shell and this service's own future `labor-mfe` remote, next port after
facility-layout's `:5186` in the fleet's `5181`-`5186`+ MFE port range).
No `web/` micro-frontend remote is being built in THIS round — CORS is
added now, proactively, matching the fleet's established convention that
CORS ships alongside a service's first console-facing REST surface, but
the actual `labor-mfe` screen is out of scope for this PR (note it as a
deferred v1 item explicitly, do not build it silently-partial).

## Inbound Kafka contract (EXACT — verified against fulfillment-execution's actual publisher this session, do not guess a different shape)

Subscribes to **`warehouse.fulfillment.events`** (the SAME shared topic
`wes-work-planning` already consumes from — this is a fan-out topic with
multiple independent consumer groups, use your own consumer group id,
e.g. `labor-performance`). The envelope (identical CloudEvents-like shape
across every warehouse-systems publisher):

```json
{
  "event_id": "uuid-v4",
  "event_type": "TaskCompleted",
  "occurred_at": "2026-08-29T22:00:00Z",
  "source": "fulfillment-execution",
  "data": {
    "task_id": "...",
    "station_id": "...",
    "work_unit_id": "...",
    "associate_id": "...",
    "duration_seconds": 52
  }
}
```

`associate_id` and `duration_seconds` are the two fields added by the
sibling `feature/labor-performance-hooks` PR in `fulfillment-execution`
this session — verify they exist in that repo's
`internal/adapters/outbound/kafka/publisher.go` `TaskCompletedData`
struct before wiring this consumer; if that PR has not yet merged to
`fulfillment-execution`'s `develop`, treat those two fields as OPTIONAL/
possibly-absent in your JSON unmarshaling (default to `""`/`0`) rather
than blocking this service's own build on that PR's merge timing —
this context must degrade gracefully (record `AssociateId=""`,
`ActualSeconds=0`) against an OLDER `fulfillment-execution` payload that
predates the enrichment, exactly as documented in the "Aggregates &
invariants" section above.

Only `event_type == "TaskCompleted"` is acted on — every other event type
on this shared topic (there are none from fulfillment-execution today,
but the topic is shared/fan-out by convention) is silently skipped, NOT
an error, mirroring `wes-work-planning`'s own consumer's
skip-unrecognized-event-type behavior.

## Tech & standards

- Go 1.26, modules. Module path: `github.com/claudioed/labor-performance`.
- chi (github.com/go-chi/chi/v5), pgx/v5 + pgxpool, golang-migrate SQL migrations.
- segmentio/kafka-go for the inbound consumer (same library every other
  repo in this fleet already uses).
- Config via env (DATABASE_URL, HTTP_ADDR, KAFKA_BROKERS,
  KAFKA_CONSUMER_GROUP default `labor-performance`,
  CORS_ALLOWED_ORIGINS). docker-compose.yml for Postgres 16.
- Typed domain errors mapped to HTTP status in the adapter, RFC 7807
  problem+json for every error response (identical shape to the other six
  services — copy their `problemDetails` struct and error-mapping pattern
  exactly).
- Table-driven tests: domain + application (in-memory adapter + a fake
  Kafka reader for the consumer — never hit a real broker in unit tests,
  mirror `wes-work-planning`'s `consumer_test.go` fake-reader pattern);
  one httptest per REST endpoint covering at least one success and one
  error path each; a build-tagged Kafka integration test
  (`consumer_integration_test.go`, skipped without a real broker) mirroring
  `wes-work-planning`'s own, since real end-to-end Kafka wiring has been a
  real source of bugs elsewhere in this fleet and deserves a real test,
  not just a unit-level fake.
- gofmt/go vet clean; every package has a doc comment.
- golangci-lint: copy `.golangci.yml` verbatim from
  `/Users/claudioed/warehouse-systems/inventory-storage/.golangci.yml`.
- OTel traces/metrics/logs matching every sibling service's shape exactly
  (OTLP/gRPC, non-blocking exporters, `service.name=labor-performance`) —
  copy `workforce-management`'s `internal/adapters/outbound/telemetry/`
  package verbatim and adjust the service name only.

## v1 scope — explicitly deferred (do NOT build these; document them, don't skip silently)

- `labor-mfe` micro-frontend remote (CORS is added now; the actual
  screen is a separate, later PR).
- Automatic pay-for-performance / bonus calculation (a real competitor
  feature, explicitly out of scope — this context surfaces the number,
  a human/other system decides what to do with it).
- Gamification, coaching workflows, real-time digital communication to
  associates (real Manhattan/Blue Yonder features — explicitly out of
  scope for v1; this is the foundational data model only).
- Publishing `LaborStandardDefined`/`LaborStandardRevised`/
  `TaskPerformanceRecorded` to Kafka for other services to consume (log
  publisher only, no integration contract yet — no other repo needs
  these events today).
- Helm chart / warehouse-infra kind-cluster wiring, gremlins mutation
  testing gate, godog/BDD acceptance tests, MCP inbound adapter,
  per-service analytics data-mesh — all present in OTHER repos in this
  fleet but explicitly out of scope for this FIRST v1 pass on a brand
  new service; add them in a fast-follow once the core domain is proven,
  same bootstrapping order `order-management` followed.
- Any change to `fulfillment-execution` beyond what
  `feature/labor-performance-hooks` already does — this build must be
  100% additive from that repo's point of view.

Write a short "Deferred (v1)" section in the README listing these
explicitly, so a reader never mistakes an absence for an oversight.

## Local quality gate (mirror order-management's minimal v1 Makefile/lefthook shape)

Targets: `build` (go build ./...), `vet`, `fmt-check` (gofmt -l . empty),
`lint` (golangci-lint run ./...), `test` (go test ./... -race), `coverage`
(gate: 90% on `./internal/domain/...,./internal/application/...`), `check`
(fmt-check + vet + build + lint + test), `check-all` (check + coverage).
NO `integration`/`mutation`/`mutation-full`/`bdd` targets in v1 (matching
`order-management`'s own v1 scope-cut) — but DO include a Kafka
integration test target (`make integration-kafka` or similar, build-tagged,
skipped without a broker) since the consumer is this service's core
integration surface and deserves real verification, unlike
`order-management` v1 which had no async integration at all.

lefthook.yml: pre-commit runs fmt-check/vet/lint; pre-push runs `check`.
Copy the structural shape from
`/Users/claudioed/warehouse-systems/order-management/lefthook.yml`.

GitHub Actions CI (`.github/workflows/ci.yml`): `lint` and `test` jobs
minimum (mirror `order-management`'s v1 CI exactly — same
golangci-lint version pin `v2.13.1`, same Go setup action version). Add a
`vuln` job (govulncheck) since this is new code with fresh dependencies
(kafka-go, pgx) worth scanning from day one, even though `order-management`
v1 skipped it.

## Git workflow

- Work on a branch named `feature/labor-performance-v1` off `develop`.
- `develop` branch already exists (created via `git checkout -b develop`
  before this spec was written) as this repo's default branch, matching
  every other repo in the fleet — do NOT create a new default branch.
- Commit frequently with clear messages.
- Push and open a PR into `develop` via `gh pr create --base develop`.
- Do NOT merge the PR yourself — leave it open for independent review.
- Do NOT force-push over history once pushed.

## Definition of done

- `go build ./...`, `go vet ./...`, `go test ./... -race` all green.
- `golangci-lint run ./...` reports 0 issues.
- Coverage on `internal/domain/...,internal/application/...` >= 90%.
- Every named invariant in "Aggregates & invariants" above has a
  dedicated failing-path test, INCLUDING: duplicate `event_id` consumed
  twice is idempotent (no double-count); `TaskCompleted` with no active
  standard for its TaskType yields `EfficiencyPct=nil` not an error;
  `TaskCompleted` with `duration_seconds=0` yields `ActualSeconds=0`,
  `EfficiencyPct=nil`; `TaskCompleted` with empty `associate_id` is
  recorded and counted in `GetTaskTypePerformance` but excluded from any
  scorecard; `DefineStandard` called twice for the same TaskType closes
  the first and a subsequently-recorded `TaskPerformance` freezes the
  NEW standard's value, while a `TaskPerformance` recorded BEFORE the
  revision (backfilled/replayed) keeps the OLD standard's frozen value if
  its `CompletedAt` predates the revision (i.e., resolve "active as of
  CompletedAt", not "active right now", when consuming a possibly-
  out-of-order or replayed Kafka message).
- Every REST endpoint has at least one httptest success case and one
  error case.
- README.md: run steps (compose up, migrate, go run, a Kafka consumer
  smoke test against the shared broker), curl example per endpoint, a
  hexagonal-layering note, and the "Deferred (v1)" section.
- `apis/openapi.yaml` covers all 5 endpoints plus the RFC 7807 error
  schema. `apis/asyncapi.yaml` documents the consumed `TaskCompleted`
  message (this service as consumer, `fulfillment-execution` as source).
- At least 4 ADRs under `docs/docs/adr/` (Docusaurus-style frontmatter —
  copy the EXACT shape from
  `/Users/claudioed/warehouse-systems/order-management/docs/docs/adr/0001-hexagonal-ports-and-adapters.md`):
  1. `0001-hexagonal-ports-and-adapters.md`
  2. `0002-new-bounded-context-not-extension-of-workforce-or-fulfillment.md`
     — the "why this context exists" reasoning above, in ADR form,
     explicitly citing the competitor research (Manhattan/Blue Yonder)
     and both sibling contexts' own boundary-discipline as the forces.
  3. `0003-kafka-choreography-consumer-of-fulfillment-execution.md` — the
     bounded-context-boundary decision above (pure consumer, no REST
     dependency, no write access) in ADR form.
  4. `0004-standard-frozen-at-completion-time-not-recomputed.md` — the
     `StandardSecondsAtCompletion` frozen-history invariant, in ADR form.
- Do NOT attempt a full Docusaurus site build in v1 unless time permits
  trivially, same scope-cut as `order-management` v1 — the ADR markdown
  files existing with correct content matters more than a working
  `npm run build` for this first pass. Note the Docusaurus site's status
  honestly in your final summary either way.
