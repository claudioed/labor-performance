# Labor Performance

> **⚠️ Study project.** This repository is an educational exercise in
> Domain-Driven Design applied to warehouse management/execution systems. It
> follows real industry-standard patterns and terminology (WMS/WES/WCS,
> CloudEvents-like envelopes, RFC 7807, hexagonal architecture) but is
> **not a production system** and is **not affiliated with, endorsed by, or
> representative of Amazon, Manhattan Associates, Blue Yonder, or any other
> company**.

Engineered labor standards ("a PICK should take 45s") and
actual-vs-standard performance scoring ("this associate's last PICK took
52s — 87% of standard") for the `warehouse-systems` fleet — the eighth
bounded-context Go service in this fleet, after `order-management`,
`inventory-storage`, `wes-work-planning`, `workforce-management`,
`fulfillment-execution`, `facility-layout`, and `warehouse-ops-agent`.

📚 **Full documentation site:** https://claudioed.github.io/labor-performance/

## Why this context exists

Competitor research (Manhattan Active Labor Management, Blue Yonder
Workforce & Labor Management) converges on this as a distinct product
capability, not a sub-feature of shift/schedule planning. Neither
`workforce-management` (which explicitly "stops at the path boundary" and
never links an associate to a task) nor `fulfillment-execution` (which
follows a strict "Task/Station only" design discipline) is the right home
for it. See
[ADR 0002](docs/docs/adr/0002-new-bounded-context-not-extension-of-workforce-or-fulfillment.md)
for the full reasoning.

## Bounded-context boundary (read this first)

This service is a **pure Kafka consumer** of `fulfillment-execution`'s
already-published `TaskCompleted` integration event (topic
`warehouse.fulfillment.events`). It is a **separate Go module in a
separate repository** — it imports no Go code from `fulfillment-execution`
or `workforce-management` and has no write access to either service's
aggregates. It never calls either sibling synchronously: this is
choreography, not orchestration, matching the proven
`wes-work-planning` ← `fulfillment-execution`/`order-management` consumer
pattern already in this fleet. See
[ADR 0003](docs/docs/adr/0003-kafka-choreography-consumer-of-fulfillment-execution.md).

This build is **100% additive** from `fulfillment-execution`'s point of
view — the sibling `feature/labor-performance-hooks` PR in that repo
already carries everything this service needs
(`associate_id`, `duration_seconds`) on the existing `TaskCompleted`
event; nothing else in that repository is touched.

This context DOES expose its own REST API (to configure standards and
read performance scores) — that is this context's OWN Open Host Service,
symmetric with every other context in the fleet.

## Architecture

Hexagonal (ports & adapters), with a strict inward-only dependency rule —
**domain depends on nothing; application depends on domain; adapters
depend on application/domain** — identical in shape to the other six
services in the fleet. See
[ADR 0001](docs/docs/adr/0001-hexagonal-ports-and-adapters.md).

```
cmd/labor/                        main.go — the OLTP composition root
cmd/labor-projector/              main.go — analytics WRITER (the only writer of
                                   the analytical DB); consumes the analytics
                                   topic from FirstOffset, runs its migrations
cmd/labor-reports/                main.go — analytics READER (read-only pool);
                                   serves GET /reports/performance
internal/
  domain/
    standard/                     LaborStandard aggregate: TaskType -> expected duration
    performance/                  TaskPerformance aggregate: one scored completed task
    shared/                       TaskType, AssociateId, domain events, errors
  application/
    ports/                        OUT: StandardRepo, PerformanceRepo, ProcessedEvents,
                                   EventPublisher, Clock
    usecases/                     DefineStandard, GetStandard, RecordTaskPerformance
                                   (Kafka-consumer-driven), GetAssociateScorecard,
                                   GetTaskTypePerformance
  analytics/
    report/                       ANALYTICAL read model (ADR 0007) — depends on
                                   NOTHING; the OLTP layers must not import it
                                   and it must not import them (arch-go enforced)
  adapters/
    inbound/http/                 chi handlers, DTOs, RFC 7807 error mapping;
                                   reports_handler.go serves the read model
    inbound/kafka/                consumer.go        — warehouse.fulfillment.events
                                                       (TaskCompleted only, OLTP)
                                   analytics_consumer.go — warehouse.labor-performance
                                                       .analytics (projector)
    outbound/postgres/            pgxpool repos + golang-migrate runner (OLTP DB)
    outbound/analyticsstore/      analytical DB: writer projection, read-only
                                   reader, in-memory store for tests
    outbound/memory/              in-memory repos for tests/local
    outbound/events/              log publisher (the default)
    outbound/kafka/               analytics publisher + fan-out (EVENT_PUBLISHER=kafka)
    outbound/telemetry/           OTel traces/metrics/logs (copied from workforce-management)
migrations/                       golang-migrate SQL files (OLTP schema)
migrations/analytics/             golang-migrate SQL files (analytical schema)
apis/openapi.yaml                 This service's OWN OLTP REST API (5 endpoints)
apis/openapi-reports.yaml         The read-only reports API (ADR 0007)
apis/asyncapi.yaml                What this service SUBSCRIBES TO and PUBLISHES
docker-compose.yml                Local Postgres 16 (OLTP :5435, analytics :5436)
docs/docs/adr/                    Architecture Decision Records
```

**Three processes, one writer.** The analytical data product added in
[ADR 0007](docs/docs/adr/0007-analytical-data-product.md) splits into
`cmd/labor` (OLTP), `cmd/labor-projector` (the only writer of the
analytical database) and `cmd/labor-reports` (read-only). They share no
database connection: report query load can never contend with the
transactional path that ingests `TaskCompleted`.

The domain layer is pure Go: no `chi`, no `pgx`, no `kafka-go`. No JSON
struct tags in the domain packages.

## Business rules worth knowing before you read the code

- **LaborStandard is append-only history.** `DefineStandard` for a
  `TaskType` that already has an active standard closes the prior
  standard's effective range and starts a new one — it never overwrites
  `ExpectedSeconds` in place. See
  [ADR 0004](docs/docs/adr/0004-standard-frozen-at-completion-time-not-recomputed.md).
- **A `TaskPerformance`'s `StandardSecondsAtCompletion` is resolved
  once, "active AS OF `CompletedAt`", and frozen forever.** A revision
  never rewrites a past row's efficiency, and a possibly out-of-order or
  replayed Kafka message is scored against the standard genuinely in
  force when the task completed, not whatever is active right now. See
  [ADR 0004](docs/docs/adr/0004-standard-frozen-at-completion-time-not-recomputed.md).
- **`EfficiencyPct` never divides by zero.** `ActualSeconds<=0` (an
  unmeasurable completion) or `StandardSecondsAtCompletion<=0` (no
  standard was active) both yield `null`, not an error and not a
  fabricated number.
- **An empty `AssociateId` is legitimate, not an error.** A `TaskCompleted`
  from a station with no checked-in occupant (e.g. a robot station) is
  still recorded and counted in `GetTaskTypePerformance`, just excluded
  from any per-associate scorecard.
- **Idempotent on the Kafka message's `event_id`.** Recording the same
  `event_id` twice is a no-op, never a double-count. Keyed on `event_id`,
  not `TaskId`, since a task id could in principle be reused after a long
  time.

## Running locally

### 1. Without a database or a broker (fastest)

With no `DATABASE_URL`, the service starts on the in-memory adapters and is
fully functional over REST (the Kafka consumer still needs a broker to
actually receive messages, but the process starts and serves regardless):

```bash
go run ./cmd/labor
# {"level":"INFO","msg":"database url not configured; using in-memory adapters"}
# {"level":"INFO","msg":"http server listening","addr":":8080"}
# {"level":"INFO","msg":"kafka consumer starting", ...}
```

### 2. With Postgres

```bash
docker compose up -d postgres          # Postgres 16 on localhost:5435

export DATABASE_URL='postgres://labor:***@localhost:5435/labor?sslmode=disable'
go run ./cmd/labor                     # migrations run automatically at startup
```

### 3. With Docker

A working `Dockerfile` builds a scratch-minimal, non-root image:

```bash
docker build -t labor-performance:local .
docker run --rm -p 8080:8080 \
  -e DATABASE_URL='postgres://labor:***@host.docker.internal:5435/labor?sslmode=disable' \
  labor-performance:local
curl -s localhost:8080/healthz
```

### 4. With Helm (kind / any Kubernetes cluster)

```bash
helm install labor-performance charts/labor-performance \
  --set database.url='postgres://labor:***@postgres:5432/labor?sslmode=disable'
kubectl port-forward svc/labor-performance 8080:80
curl -s localhost:8080/healthz
```

See `charts/labor-performance/values.yaml` for the full configuration
surface (`kafka.enabled`, `otel.enabled`, ingress, autoscaling, an
`existingSecret` pattern for `DATABASE_URL`).

### 5. Kafka consumer smoke test against the shared broker

```bash
# From the workspace root, start the fleet's shared Kafka broker:
docker compose -f ../docker-compose.kafka.yml up -d

export KAFKA_BROKERS=localhost:9092
export KAFKA_CONSUMER_GROUP=labor-performance
go run ./cmd/labor

# In another terminal, publish a TaskCompleted-shaped message using any
# Kafka producer CLI, e.g. kcat:
echo '{"event_id":"'$(uuidgen)'","event_type":"TaskCompleted","occurred_at":"2026-08-29T22:00:00Z","source":"fulfillment-execution","data":{"task_id":"task-1","station_id":"station-1","work_unit_id":"wu-1","associate_id":"assoc-1","duration_seconds":52}}' \
  | kcat -P -b localhost:9092 -t warehouse.fulfillment.events

# Then verify it was recorded:
curl -s localhost:8080/associates/assoc-1/scorecard
```

### Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | Listen address. |
| `DATABASE_URL` | *(unset)* | Postgres DSN. Unset ⇒ in-memory adapters. |
| `MIGRATIONS_PATH` | `migrations` | golang-migrate source directory. |
| `KAFKA_BROKERS` | `localhost:9092` | Comma-separated broker addresses. |
| `KAFKA_CONSUMER_GROUP` | `labor-performance` | Consumer group id on `warehouse.fulfillment.events`. |
| `CORS_ALLOWED_ORIGINS` | `http://localhost:5173,http://localhost:5187` | Comma-separated allowed origins. |
| `OTEL_SERVICE_NAME` | `labor-performance` | OTel `service.name` resource attribute. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | OTLP/gRPC Collector endpoint. |
| `LOG_LEVEL` | `info` | `debug` \| `info` \| `warn` \| `error`. |

## API

Four endpoints plus a liveness probe. The full contract, including the RFC
7807 error schema, is in [`apis/openapi.yaml`](apis/openapi.yaml).

| Method | Path | Use case |
| --- | --- | --- |
| `POST` | `/standards` | DefineStandard |
| `GET` | `/standards/{taskType}` | GetStandard |
| `GET` | `/associates/{associateId}/scorecard` | GetAssociateScorecard |
| `GET` | `/task-types/{taskType}/performance` | GetTaskTypePerformance |
| `GET` | `/healthz` | Liveness probe |

There is deliberately **no** REST endpoint for `RecordTaskPerformance` —
it is exclusively Kafka-consumer-driven (see
[ADR 0003](docs/docs/adr/0003-kafka-choreography-consumer-of-fulfillment-execution.md)).

Every error response is `application/problem+json` (RFC 7807), the same
shape every other service in this fleet emits.

### Curl walkthrough

**Health:**

```bash
curl -s localhost:8080/healthz
# {"status":"ok"}
```

**DefineStandard:**

```bash
curl -s -X POST localhost:8080/standards \
  -H 'Content-Type: application/json' \
  -d '{"taskType":"PICK","expectedSeconds":45}'
# 201 Created
# {"taskType":"PICK","expectedSeconds":45,"effectiveFrom":"2026-08-29T12:00:00Z"}
```

Revising it closes the prior record and starts a new one:

```bash
curl -s -X POST localhost:8080/standards \
  -H 'Content-Type: application/json' \
  -d '{"taskType":"PICK","expectedSeconds":40}'
# 201 Created
# {"taskType":"PICK","expectedSeconds":40,"effectiveFrom":"2026-08-30T09:00:00Z"}
```

**GetStandard:**

```bash
curl -s localhost:8080/standards/PICK
# 200 OK, or 404 application/problem+json if none is active
```

**GetAssociateScorecard** (populated only by the Kafka consumer):

```bash
curl -s localhost:8080/associates/assoc-1/scorecard
# 200 OK:
# {"associateId":"assoc-1","taskCount":3,"meanEfficiencyPct":91.2,
#  "byTaskType":{"PICK":{"taskCount":3,"meanEfficiencyPct":91.2}},
#  "trend":"STABLE","coachingFlag":false}
# 404 application/problem+json if this service has never seen assoc-1
```

`trend` (`IMPROVING`/`DECLINING`/`STABLE`/`INSUFFICIENT_DATA`) and
`coachingFlag` are computed from the associate's most recent (up to 10)
scored tasks — see [ADR 0005](docs/docs/adr/0005-associate-trend-and-coaching-flag.md)
for the full design. Visibility only: this service never automates
coaching, pay, or task-claim decisions from either field.

**GetTaskTypePerformance** (fleet-wide, all associates):

```bash
curl -s localhost:8080/task-types/PICK/performance
# 200 OK — always 200, including a zero-count result for a never-seen type:
# {"taskType":"PICK","taskCount":42,"meanEfficiencyPct":88.7,"meanActualSeconds":39.4}
```

`meanActualSeconds` is a real measured rate, independent of
`meanEfficiencyPct` — populated even when no `LaborStandard` has ever
been defined for this TaskType. See
[ADR 0006](docs/docs/adr/0006-mean-actual-seconds-independent-of-standard.md).

## Quality gate

Every `make` target mirrors a step in `.github/workflows/ci.yml`, so the
same feedback CI gives you post-push is available locally, pre-commit:

```bash
make check       # fmt-check + vet + build + lint + test -race
make check-all   # check + coverage (gate: 90% on domain + application)
```

| Target | What it runs |
| --- | --- |
| `build` | `go build ./...` |
| `vet` | `go vet ./...` |
| `fmt` / `fmt-check` | `gofmt -w .` / fail if `gofmt -l .` is non-empty |
| `lint` | `golangci-lint run ./...` (CI pins `v2.13.1`) |
| `test` | `go test ./... -race` |
| `coverage` | coverage profile + the 90% gate |
| `integration-kafka` | Build-tagged consumer test against a real broker (`KAFKA_BROKERS` required) |

Additional verification surfaces, each with its own CI job:

```bash
go test ./... -run TestFeatures -v                  # BDD (godog/Gherkin)
go test ./internal/architecture/... -v               # arch-fitness (arch-go)
go test -tags=integration ./... -race -count=1       # Postgres + Kafka integration
gremlins unleash ./internal/domain                    # mutation testing (see .gremlins.yaml)
ct lint --charts charts/labor-performance \
  --validate-maintainers=false --check-version-increment=false
spectral lint apis/openapi.yaml --ruleset .spectral.yaml --fail-severity=warn
spectral lint apis/asyncapi.yaml --ruleset .spectral.asyncapi.yaml --fail-severity=warn
```

Git hooks are wired through [lefthook](https://github.com/evilmartians/lefthook)
— `pre-commit` runs fmt-check/vet/lint, `pre-push` runs `make check`. Hooks
are not tracked by git, so activate them once per clone:

```bash
brew install lefthook   # or: go install github.com/evilmartians/lefthook@latest
lefthook install
```

CI (`.github/workflows/ci.yml`) runs the full fleet-standard matrix:
**`lint`**, **`test`**, **`bdd`**, **`integration`** (Postgres service
container), **`mutation-fast`** (blocking, `./internal/domain/performance`)
and **`mutation`** (exhaustive, scheduled/manual, `./internal/domain`),
**`api-lint`** (Spectral against both `apis/openapi.yaml` and
`apis/asyncapi.yaml`), **`vuln`** (govulncheck), **`helm-lint`**
(`ct lint`), **`arch-test`** (arch-go fitness tests), **`trivy-scan`**
(container CVE gate on every push/PR), **`docker-publish`** (main-only,
cosign keyless signing + SPDX SBOM attestation), and **`release`**
(main-only, auto-tagged GitHub release + published Helm chart). Plus
`.github/workflows/codeql.yml` (security-extended CodeQL analysis) and
`.github/workflows/scorecard.yml` (OpenSSF Scorecard).

## Known gaps

- **`fulfillment-execution`'s `TaskCompleted` payload does not carry a
  `task_type` field today**, verified against its actual
  `feature/labor-performance-hooks` publisher this session. This service
  resolves `TaskType` as `""` (unclassified) for every consumed event as a
  result. A `""`-typed row is still recorded and counted, but never
  resolves a `LaborStandard` and never appears under
  `GetTaskTypePerformance` (which requires PICK/PACK/SLAM). Adding
  `task_type` to that payload on the fulfillment-execution side is a
  natural, additive fast-follow that would let this service resolve it
  directly — see
  [ADR 0003](docs/docs/adr/0003-kafka-choreography-consumer-of-fulfillment-execution.md).

## Deferred (v1)

The following are **deliberately out of scope**. They are listed so an
absence is never mistaken for an oversight — each is a decision, not a gap
someone forgot about.

- **`labor-mfe` micro-frontend remote.** CORS is added now (proactively,
  matching the fleet's convention that CORS ships alongside a service's
  first console-facing REST surface); the actual screen is a separate,
  later PR.
- **Automatic pay-for-performance / bonus calculation.** A real Manhattan
  competitor feature, explicitly out of scope — this context surfaces the
  number, a human/other system decides what to do with it.
- **Gamification, automated coaching workflows, real-time digital
  communication to associates.** Real Manhattan/Blue Yonder features,
  explicitly out of scope for v1 — v1 (this PR, ADR 0005) ships the
  passive visibility signal only (`Trend`/`CoachingFlag` on the
  Scorecard read model, a human reads it); an active workflow that
  automatically messages, schedules, or nudges an associate based on
  that signal remains deferred.
- **Publishing `LaborStandardDefined`/`LaborStandardRevised`/
  `TaskPerformanceRecorded` to Kafka for other services to consume.** Log
  publisher only, no integration contract yet — no other repo needs these
  events today. `apis/asyncapi.yaml` documents only what this service
  CONSUMES, not what it would publish.
- **MCP inbound adapter.** HTTP and Kafka are the only inbound adapters.
- **Per-service analytics data-mesh** (a separate analytics topic,
  analytical Postgres, and projector/reports binaries, as several sibling
  services now have). This service's OLTP write path already IS the
  analytics-relevant signal (`TaskPerformance` rows); a dedicated
  analytics side-projection is a natural but deferred fast-follow.
- **Any change to `fulfillment-execution` beyond what
  `feature/labor-performance-hooks` already does.** This build is 100%
  additive from that repo's point of view.

### Now built (fleet-parity pass) — no longer deferred

The following were listed as deferred in this service's original v1 build
and have since been added, bringing this service to full fleet parity with
`inventory-storage` / `wes-work-planning`:

- **Helm chart** (`charts/labor-performance/`) — `helm install
  labor-performance charts/labor-performance`.
- **godog/BDD acceptance tests** (`features/*.feature` +
  `features_test.go`) — covers the REST-visible contract (define/revise/get
  a standard, get-scorecard 404, get-task-type-performance always-200).
- **Postgres integration tests**
  (`internal/adapters/outbound/postgres/*_integration_test.go`) — real
  round-trips for `StandardRepo`, `PerformanceRepo`, `ProcessedEventRepo`.
- **Gremlins mutation-testing gate** (`.gremlins.yaml`) — see the CI section
  above for the measured baseline.
- **arch-go fitness tests** (`internal/architecture/architecture_test.go`)
  — enforces the hexagonal dependency rule.
- **Spectral API linting** (`.spectral.yaml`, `.spectral.asyncapi.yaml`) —
  against both `apis/openapi.yaml` and `apis/asyncapi.yaml`.
- **CodeQL, OpenSSF Scorecard, Trivy container scanning.**
- **Docker image publishing + release automation** (`docker-publish`,
  `release` CI jobs) — cosign keyless signing, SPDX SBOM attestation,
  auto-tagged GitHub releases, Helm chart published to
  `oci://ghcr.io/claudioed`.
- **Full Docusaurus documentation site**, live at
  https://claudioed.github.io/labor-performance/.

## Architecture Decision Records

1. [0001 — Hexagonal (ports & adapters) architecture](docs/docs/adr/0001-hexagonal-ports-and-adapters.md)
2. [0002 — A new bounded context, not an extension of workforce-management or fulfillment-execution](docs/docs/adr/0002-new-bounded-context-not-extension-of-workforce-or-fulfillment.md)
3. [0003 — Kafka choreography consumer of fulfillment-execution, no REST dependency](docs/docs/adr/0003-kafka-choreography-consumer-of-fulfillment-execution.md)
4. [0004 — StandardSecondsAtCompletion is frozen at ingestion time, never recomputed](docs/docs/adr/0004-standard-frozen-at-completion-time-not-recomputed.md)

## License

MIT (or match the other repos' licensing — TBD).
