---
id: 0009-mcp-inbound-adapter
title: 9. Model Context Protocol as an inbound adapter, not a new service
sidebar_label: 9. MCP inbound adapter
sidebar_position: 9
description: "Expose this bounded context to the AI ecosystem via an MCP server built as a second driving adapter over the existing read use cases -- Streamable HTTP, official Go SDK, static bearer-key auth, curated intent-level READ tools. This context exposes no write tool: DefineStandard is operator-driven (chi HTTP) and RecordTaskPerformance is Kafka-consumer-driven, neither a decision an MCP-calling agent should make."
---

# 9. Model Context Protocol as an inbound adapter, not a new service

## Status

**Accepted.** The reference implementation and pilot for this pattern across
the estate is `fulfillment-execution` (its own ADR-0008); this record is
`labor-performance` adopting that same decision, adapted to a context that is
a pure read-side reporter to the rest of the fleet.

## Context

The platform is being connected to the AI ecosystem (Claude, Cursor, ChatGPT,
agent frameworks) via the **Model Context Protocol (MCP)**: a client discovers
a server's *tools* (model-callable functions), *resources* (read-only
context), and *prompts* (reusable templates), then an LLM decides which to
call.

The forces, specific to this context:

- **There is already a clean read surface.** `GetAssociateScorecard`,
  `GetTaskTypePerformance`, and `GetStandard` are exactly the reads the chi
  HTTP adapter already exposes. An AI client asking "how is this associate
  doing" or "what is the standard for this task type" needs the same answers
  the HTTP client already gets.
- **This context has no write use case an agent should call.**
  `DefineStandard` is driven by an operator over chi HTTP (setting an
  engineered labor standard is a deliberate human decision about work
  measurement, not something to delegate to a model) and
  `RecordTaskPerformance` is driven exclusively by fulfillment-execution's
  `TaskCompleted` Kafka event (it is idempotent, event-sourced state, not a
  command an external caller should issue directly). Neither belongs behind
  an MCP write tool.
- **The coaching flag is a visibility signal, not an automated trigger.** ADR-
  0005 froze this discipline for the HTTP surface: `CoachingFlag` means "this
  may be worth a human conversation," never grounds for an automated action.
  An MCP tool surfacing this same field to an LLM must carry that constraint
  as plainly as the domain does, not silently let a model treat it as license
  to act.
- **The domain must not learn about MCP.** ADR-0001's dependency rule is
  load-bearing: domain depends on nothing, application depends on domain,
  adapters depend inward. A protocol whose shape is set by an external LLM
  ecosystem is precisely the kind of concern that must stay in an adapter.
- **MCP has an idiomatic Go path now.** The official **MCP Go SDK**
  (`github.com/modelcontextprotocol/go-sdk`) is a Tier-1 SDK, version-pinned
  in `go.mod` exactly like the other five contexts already do it.
- **This is an internal, non-user-facing deployment.** Per the fleet's MCP
  governance charter (`docs/docs/mcp/governance-charter.md`), a static bearer
  token is appropriate for this internal use; full OAuth 2.1 is deferred until
  a server faces real end users.

## Decision

**We will expose this bounded context to the AI ecosystem through an MCP
server built as a second driving adapter over the existing read use cases --
leaving the domain and application layers untouched -- and, because this
context has no write use case an MCP-calling agent should invoke, we will
register only READ tools, a scoped resource, and a prompt, and no write
tool.**

### The adapter, mirroring the HTTP one

A new `internal/adapters/inbound/mcp/` sits beside
`internal/adapters/inbound/http/` and `internal/adapters/inbound/kafka/`:

```
internal/adapters/inbound/mcp/
  server.go      MCP Server wiring (Go SDK), capability registration
  tools.go       intent-level READ tool handlers -> call read use cases
  mapping.go     tool I/O DTOs, kept separate from the HTTP adapter's own
                 DTOs even though the JSON shapes happen to match today
  resources.go   the scoped scorecard resource (not a bulk dump)
  prompts.go     the review_associate_performance workflow prompt
  auth.go        bearer-key auth middleware (interface; OAuth-ready seam)
```

It depends inward on `application` exactly as the HTTP adapter does. No MCP
type appears in `internal/domain/**` or `internal/application/**`. The tool
handlers call the **same** read use case structs (`GetAssociateScorecard`,
`GetTaskTypePerformance`, `GetStandard`) the HTTP handlers call -- never a
parallel code path, never the domain directly.

### A separate `cmd/mcp` binary

The MCP server ships as its own composition root, `cmd/mcp/main.go`, reusing
the same repository selection (in-memory vs Postgres) as `cmd/labor`. Three
deployables now exist from one module: the HTTP + Kafka-consumer service
(`cmd/labor`), the MCP server (`cmd/mcp`), and the analytics reader
(`cmd/labor-reports`, ADR-0007) -- each isolating its own blast radius.

### Streamable HTTP only

The single supported transport is **Streamable HTTP**, matching every
sibling context.

### Curated, intent-level READ tools -- not one tool per endpoint

- `get_associate_scorecard` (read) -- one associate's scorecard: task count,
  mean efficiency percent, per-task-type breakdown, trend, coaching flag.
- `get_task_type_performance` (read) -- fleet-wide performance for one task
  type: task count, mean efficiency percent, real measured mean duration.
- `get_labor_standard` (read) -- the currently-active engineered labor
  standard (expected seconds) for one task type.

All three return **compact DTOs**, not the raw domain aggregates. The single
resource exposes an associate's scorecard as a **scoped** context contract
(`scorecard://labor/{associateId}`), backed by the same
`GetAssociateScorecard` read model -- never a database dump. The one prompt
(`review_associate_performance`) encodes the operational SOP for reviewing an
associate's performance against the active standard and fleet-wide
comparison, and explicitly instructs the model to treat the coaching flag as
a signal for human follow-up, never an automated conclusion -- carrying
ADR-0005's discipline into the MCP surface in the model-facing text itself,
not just in the code.

### No write tool -- but the scope seam is kept

Because neither of this context's write use cases is a decision an
MCP-calling agent should make on its behalf, **no write tool is registered.**
The read/read-write `Scope` plumbing in `auth.go` is nonetheless kept
identical to the pilot: two key classes, the `scopeAllows` gate, and the
scope-parameterised tool wrapper all exist, and every registered tool
requires `ScopeRead`. This keeps the pattern uniform across the six contexts
and means that if a legitimate write use case is ever designed for MCP here,
exposing it is a single `ScopeReadWrite` registration with no auth rework.

### Static bearer-key auth, behind an OAuth-ready seam

`auth.go` validates a per-client API key (from a Kubernetes Secret) on every
request; missing or invalid key returns `401` with a `WWW-Authenticate`
challenge; the key is never logged. The middleware is an **interface**, so an
OAuth 2.1 resource-server implementation can drop in later without touching
any tool handler.

### Reuse the existing observability

The adapter is instrumented with the same OpenTelemetry setup as the HTTP
boundary: a span per tool call (tool name, required scope, outcome). MCP
calls appear in traces next to HTTP and Kafka-consumer activity.

## Consequences

### Easier

- **The domain and application layers do not change at all.** MCP is purely
  additive; the dependency rule (ADR-0001) is preserved and checked by the
  existing arch-go fitness tests (the generic inbound/outbound isolation
  rules already cover this new adapter package with no changes needed).
- **One read surface, three protocols.** HTTP, MCP, and (for
  `RecordTaskPerformance`) Kafka all reach the same use cases, so behaviour
  is identical regardless of caller.
- **Nothing here can be mutated by an agent.** No write tool is registered,
  so the entire class of "an autonomous agent changed the wrong thing" risk
  does not exist for this server.
- **The coaching-flag discipline travels with the surface**, not just the
  code: the prompt text itself tells a calling model how to treat the signal
  responsibly.
- **It stays in Go, in one quality gate.** Unit-tested (20 tests: auth, scope
  gating, all three tools' read use-case wrapping, governance-charter
  checks, and full transport-level tests over a real Streamable HTTP server),
  linted, and CI-gated like every other package.

### Harder

- **A second deployable to run and secure.** `cmd/mcp` is another binary and
  image. Today it is built and run only by the `e2e-tests` black-box
  harness (matching the other five contexts' own current state) -- it is
  **not yet deployed to the live `warehouse` kind cluster** by
  `warehouse-infra`'s Terraform, and **not yet wired as a client inside
  `warehouse-ops-agent`** (T5): no existing T5 use case
  (`console_reports`, `dailybrief`, `flow_balance_advisory`,
  `order_lifecycle`, `stranded_reservation`) consumes this context's
  scorecard/coaching-flag data, and the charter's own "tools map to a real
  decision" rule means adding a client with nothing calling it would be
  premature. Both the live-cluster deployment and a genuine T5 consuming
  use case are deliberately deferred as a fast-follow once designed, for
  this context and for the other five equally (none of the six MCP servers
  in the fleet is deployed live today).
- **Auth is deliberately minimal.** A static bearer key is appropriate for an
  internal, non-user-facing server, but does **not** cover user-facing,
  multi-tenant use.
- **The MCP spec is a moving target.** The SDK must stay pinned and
  revisited; deprecated features (`roots`/`sampling`) must be avoided.
- **Tool curation is an ongoing discipline, not a one-time choice.** Nothing
  in the compiler stops a future PR from adding a tool per endpoint, or a
  write tool without re-examining whether this context should accept agent
  writes at all. The MCP governance charter and its review gate exist to
  hold that line.
- **LLM-chosen arguments are untrusted input.** Every tool handler validates
  its inputs defensively via `shared.NewTaskType`'s strict validation (reject
  an empty or unknown task type, never default) -- stricter than what the
  HTTP DTO layer assumes, since the caller here is a model, not our own code.
