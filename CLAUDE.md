# Project: Labor Performance (Supporting Bounded Context)

Engineered labor standards ("a PICK should take 45s") and actual-vs-standard
performance scoring ("this associate's last PICK took 52s — 87% of
standard") for the `warehouse-systems` fleet. The eighth bounded-context Go
service in the fleet (after `order-management`, `inventory-storage`,
`wes-work-planning`, `workforce-management`, `fulfillment-execution`,
`facility-layout`, `warehouse-ops-agent`). Full docs site:
https://claudioed.github.io/labor-performance/

> **Study project.** Educational DDD exercise using real industry patterns
> and terminology (WMS/WES, CloudEvents-like envelopes, RFC 7807, hexagonal
> architecture). Not a production system; not affiliated with Amazon,
> Manhattan Associates, Blue Yonder, or any other company.

## Non-negotiables (read before writing any code)

1. **Pure Kafka consumer of `fulfillment-execution`, nothing else.** This
   service subscribes to `warehouse.fulfillment.events` (topic, shared/
   fan-out with `wes-work-planning`) and reacts only to `TaskCompleted`. It
   is a separate Go module/repo: **no Go import from, and no write access
   to**, `fulfillment-execution` or `workforce-management`. It never calls
   either sibling synchronously — choreography, not orchestration. See
   `.claude/rules/domain-model.md` and ADR 0002/0003.
2. **Hexagonal / ports & adapters, strict inward dependency rule:** domain
   depends on nothing; application depends on domain; adapters depend on
   application/domain. No framework, HTTP, Kafka, or SQL types in the domain
   layer (arch-go enforced, `internal/architecture/architecture_test.go`).
3. **Never fabricate a number.** `EfficiencyPct`, `meanEfficiencyPct`, and
   `meanActualSeconds` are nullable everywhere (domain, REST, analytics
   report) and MUST be `null`, never `0`, when nothing was scorable/
   measurable. See `.claude/rules/domain-model.md`.
4. **This service NEVER accepts a TaskPerformance write over REST.**
   Recording a performance row is exclusively Kafka-consumer-driven
   (`RecordTaskPerformance`, idempotent on the Kafka message's `event_id`).

## Project overview

- Go 1.26, module `github.com/claudioed/labor-performance`.
- Three processes, one writer per database (ADR 0007, ADR 0010):
  - `cmd/labor` — OLTP: REST API + Kafka consumer of `TaskCompleted`.
  - `cmd/labor-projector` — analytics writer; consumes
    `warehouse.labor-performance.analytics`, the ONLY writer of the
    analytical DB.
  - `cmd/labor-reports` — analytics reader; read-only pool; serves
    `GET /reports/performance`.
- Two OpenAPI specs (`apis/openapi.yaml` OLTP, `apis/openapi-reports.yaml`
  reports) and one AsyncAPI spec (`apis/asyncapi.yaml`, documents both what
  this service consumes and what it publishes to its own analytics topic).
- `charts/labor-performance/` — Helm chart, fleet parity.
- `web/` — the `labor-mfe` micro-frontend remote (module federation, React,
  Vite) consuming `@warehouse/ui-kit`.
- `docs/` — Docusaurus site (see `.claude/rules/docs-and-api-drift.md`).

## Architecture

```
cmd/labor/                    main.go — OLTP composition root
cmd/labor-projector/          main.go — analytics WRITER (only writer of the analytical DB)
cmd/labor-reports/            main.go — analytics READER (read-only pool)
internal/
  domain/
    standard/                 LaborStandard aggregate: TaskType -> expected duration
    performance/               TaskPerformance aggregate: one scored completed task
    shared/                    value objects: TaskType, AssociateId, events, errors
  application/
    ports/                     OUT: StandardRepo, PerformanceRepo, ProcessedEvents, EventPublisher, Clock
    usecases/                  DefineStandard, GetStandard, RecordTaskPerformance
                                (Kafka-consumer-driven), GetAssociateScorecard,
                                GetTaskTypePerformance
  analytics/
    report/                    Analytical read model (ADR 0007) — depends on NOTHING;
                                OLTP layers must not import it and vice versa (arch-go enforced)
  adapters/
    inbound/http/               chi handlers, DTOs, RFC7807 error mapping;
                                 reports_handler.go serves the analytics read model
    inbound/kafka/               consumer.go (warehouse.fulfillment.events, TaskCompleted only)
                                  analytics_consumer.go (warehouse.labor-performance.analytics)
    outbound/postgres/          pgxpool repos + golang-migrate; unit_of_work.go,
                                 outbox_publisher.go, outbox_relay.go (transactional outbox, ADR 0010)
    outbound/analyticsstore/    analytical DB: writer projection, read-only reader, in-memory store
    outbound/memory/            in-memory repos for tests/local
    outbound/events/            log publisher (default)
    outbound/kafka/              analytics publisher, relay sink, fan-out (EVENT_PUBLISHER=kafka)
    outbound/telemetry/         OTel traces/metrics/logs
migrations/                    golang-migrate SQL (OLTP schema)
migrations/analytics/          golang-migrate SQL (analytical schema)
apis/openapi.yaml               OLTP REST API (5 endpoints)
apis/openapi-reports.yaml       Reports REST API (ADR 0007)
apis/asyncapi.yaml               Consumed + published event contracts
docs/docs/adr/                  ADRs (Nygard format), Docusaurus-rendered
```

Domain layer is pure Go: no `chi`, no `pgx`, no `kafka-go`, no JSON tags.

See `.claude/rules/domain-model.md` for the full ubiquitous language,
aggregates, invariants, and use cases.

## Key commands

```bash
# Run locally (in-memory adapters, no DB/broker needed)
go run ./cmd/labor

# With Postgres
docker compose up -d postgres                 # :5435
export DATABASE_URL='postgres://labor:***@localhost:5435/labor?sslmode=disable'
go run ./cmd/labor                            # migrations run automatically

# Quality gate (mirrors .github/workflows/ci.yml)
make check       # fmt-check + vet + build + lint + test -race
make check-all   # check + coverage (gate: 90% on domain+application+analytics)

# Individual verification surfaces
go test ./... -run TestFeatures -v                    # BDD (godog/Gherkin)
go test ./internal/architecture/... -v                 # arch-fitness (arch-go)
go test -tags=integration ./... -race -count=1         # Postgres + Kafka (testcontainers) integration
gremlins unleash ./internal/domain                      # mutation testing
spectral lint apis/openapi.yaml --ruleset .spectral.yaml --fail-severity=warn
spectral lint apis/openapi-reports.yaml --ruleset .spectral.yaml --fail-severity=warn
spectral lint apis/asyncapi.yaml --ruleset .spectral.asyncapi.yaml --fail-severity=warn
ct lint --charts charts/labor-performance --validate-maintainers=false --check-version-increment=false

# Docs site (see .claude/rules/docs-and-api-drift.md before regenerating)
cd docs && npm ci && npm run gen-api-docs:all && npm run build
```

`lefthook install` wires `pre-commit` (fmt-check/vet/lint) and `pre-push`
(`make check`) — activate once per clone (hooks are not tracked by git).

## Code standards & testing

- gofmt/go vet clean; every package has a doc comment.
- golangci-lint `v2.13.1` (pinned, matches CI) — copy `.golangci.yml` from
  `inventory-storage` when bootstrapping a new package.
- Table-driven tests: domain + application (in-memory adapters, fake Kafka
  reader — never hit a real broker in unit tests). One httptest per REST
  endpoint (success + error path). Build-tagged Kafka integration test
  using Testcontainers (`internal/adapters/inbound/kafka/consumer_integration_test.go`).
- Coverage gate: 90% on `./internal/domain/...,./internal/application/...,
  ./internal/analytics/...` (`make coverage`).
- RFC 7807 `application/problem+json` for every error response, identical
  shape across both OLTP and reports APIs.
- CI (`.github/workflows/ci.yml`) runs the full fleet-standard matrix: lint,
  test, bdd, integration, mutation-fast (blocking) / mutation (scheduled
  exhaustive), api-lint (Spectral, all 3 specs), vuln (govulncheck),
  helm-lint, arch-test, trivy-scan, docker-publish + release (main-only).
  Plus `codeql.yml` and `scorecard.yml`.

## Git workflow

- GitFlow: `develop` is the default/working branch; `main` is release-only.
- Branch off `develop`, PR back into `develop` (`gh pr create --base
  develop`). Never force-push over pushed history. Do not self-merge —
  leave PRs open for review unless told otherwise.
- **This repo's `github-pages` environment allows deploys from BOTH
  `develop` and `main`**, but `.github/workflows/docs.yml` currently
  triggers only on push to `main` — see `.claude/rules/docs-and-api-drift.md`.

## Reference rules (load when relevant)

- `.claude/rules/domain-model.md` — why this context exists, ubiquitous
  language, aggregates & invariants, domain events, use cases, REST/Kafka
  contracts, v1 scope decisions.
- `.claude/rules/docs-and-api-drift.md` — the two OpenAPI specs, the
  Docusaurus `docusaurus-plugin-openapi-docs` wiring for both, the
  `@faker-js/faker` / `postman-collection` pitfall, and the docs.yml
  trigger-branch gap.
- `.claude/rules/adrs-and-decisions.md` — index of ADRs 0001–0012 and what
  each one settles.
