---
id: 0012-remove-rest-auth-layer
title: 12. Remove the REST/MCP identity layer
sidebar_label: 12. Remove REST auth layer
sidebar_position: 12
description: "labor-performance removes the static bearer-key identity layer introduced by ADR 0011 across both the OLTP/reports REST surfaces and the MCP adapter, superseding that record."
---

# 12. Remove the REST/MCP identity layer

## Status

**Accepted.** Supersedes [ADR 0011 — REST identity: fleet-standard static
bearer keys with read/read-write scopes](./0011-rest-auth-static-bearer-scopes.md).

## Context

ADR 0011 added `internal/adapters/inbound/auth`, a static bearer-key
middleware with `read`/`read-write` scopes, mounted on every REST route
(both the OLTP `cmd/labor` router and the `cmd/labor-reports` reader
router) except `/healthz`, and shared with the MCP adapter
(`cmd/mcp`) per ADR 0009/0011. The fleet decision to remove this layer
(tracked at the fleet level, mirroring the decision that originally
introduced it via warehouse-ops-agent ADR 0005) applies here identically
to every other bounded context.

This context has no outbound REST client to another service (it is a
pure Kafka consumer of `fulfillment-execution`'s `TaskCompleted` event,
ADR 0003), so there is no `<PEER>_API_KEY`/bearer-forwarding logic to
remove on the outbound side — only the inbound identity layer.

## Decision

**We will remove the REST/MCP identity layer entirely from this
repository, across both deployables that serve REST (`cmd/labor` and
`cmd/labor-reports`) and the MCP server (`cmd/mcp`).**

Concretely:

- `internal/adapters/inbound/auth/` is deleted.
- The OLTP router (`internal/adapters/inbound/http/server.go`) no longer
  mounts any auth middleware; every route (`POST /standards`,
  `GET /standards/{taskType}`, `GET /associates/{associateId}/scorecard`,
  `GET /task-types/{taskType}/performance`, `GET /healthz`) is open.
- The reports router (`internal/adapters/inbound/http/reports_handler.go`)
  no longer mounts any auth middleware; `GET /reports/performance`,
  `GET /reports/performance/freshness`, and `GET /healthz` are open.
- `cmd/labor/main.go` and `cmd/labor-reports/main.go` no longer construct
  a `StaticKeyAuth`/`Middleware` or parse `AUTH_MODE`.
- The MCP adapter (`internal/adapters/inbound/mcp/`) no longer gates tools
  or the scorecard resource on a scope, and `cmd/mcp/main.go` no longer
  requires `API_READ_KEY`/`API_READWRITE_KEY` (or the `MCP_READ_KEY`/
  `MCP_READWRITE_KEY` fallbacks) to serve requests.
- The Helm chart (`charts/labor-performance/`) drops the `auth:` values
  block, the `API_READ_KEY`/`API_READWRITE_KEY` Secret rendering, and the
  `AUTH_MODE` ConfigMap entry, for both the OLTP and reports deployments.
- `apis/openapi.yaml` and `apis/openapi-reports.yaml` drop
  `components.securitySchemes.bearerAuth` and every `security:` key
  (top-level and per-operation `security: []` override), and their
  `401`/`403` response entries. `apis/asyncapi.yaml` never referenced
  bearer auth (it documents an inbound Kafka consumption, not a REST
  surface), so it is unchanged.
- The README's auth environment-variable rows and the
  "every route requires a bearer key" paragraph are removed.

## Consequences

### Easier

- One fewer moving part in every composition root: no key management, no
  `AUTH_MODE` rollout sequencing (`log` before `enforce`), no Secret
  wiring to get wrong across two deployments.
- The OpenAPI/AsyncAPI contracts describe exactly what the service does
  today: no scheme a caller must satisfy that the server does not
  actually require.
- Handler and router tests lose an entire dimension (401/403 table
  tests) that existed only to prove the now-deleted middleware behaved
  correctly.

### Harder

- Every REST and MCP endpoint in this context is now unauthenticated at
  the application layer. Any access control this fleet still wants for
  labor-performance data (including the HR-adjacent per-associate
  scorecards ADR 0005 originally called out) must come from outside this
  service — network policy, a gateway, or a future re-introduction of
  identity — not from anything in this repository.
- If REST/MCP identity is reintroduced later, it is a new decision, not
  a revert: this record is deliberately not deleting ADR 0011's history,
  only closing it out.
