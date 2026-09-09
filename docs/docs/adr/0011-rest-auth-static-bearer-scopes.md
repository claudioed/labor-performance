---
id: 0011-rest-auth-static-bearer-scopes
title: 11. REST identity — fleet-standard static bearer keys with read/read-write scopes
sidebar_label: 11. REST auth (fleet adoption)
sidebar_position: 11
description: "labor-performance adopts the fleet-wide REST identity decision (warehouse-ops-agent ADR 0005): static bearer keys with read / read-write scopes, no IdP, one auth package per repository shared by the REST routers and the MCP adapter, rolled out through AUTH_MODE=enforce|log|off."
---

# 11. REST identity — fleet-standard static bearer keys with read/read-write scopes

## Status

**Superseded by [ADR 0012 — Remove the REST/MCP identity layer](./0012-remove-rest-auth-layer.md).**
Originally accepted, adopting the fleet-wide decision recorded in
[warehouse-ops-agent ADR 0005 — Fleet REST identity: static bearer keys
with read/read-write scopes, no IdP](https://github.com/claudioed/warehouse-ops-agent/blob/develop/docs/docs/adr/0005-rest-identity-static-bearer-scopes.md).
That record holds the context, the alternatives considered (OIDC now,
gateway-only auth, a shared Go module) and the rollout plan; this one only
records how it lands here.

## Decision (as applied to this context)

`internal/adapters/inbound/auth` is a verbatim copy of the fleet template
(copy-not-share) holding `Scope`, `Authenticator`, `StaticKeyAuth`,
`Mode` and `Middleware`. The MCP adapter
(`internal/adapters/inbound/mcp`), which had carried its own copy of those
types since ADR 0009, now imports this package, so the repository has
exactly one bearer-key implementation serving both surfaces. The OLTP
router (`cmd/labor`) mounts the middleware on every route except
`/healthz` with the fleet policy — `GET`/`HEAD`/`OPTIONS` require `read`,
everything else `read-write`; the reports router (`cmd/labor-reports`)
mounts it on every `/reports/*` route with the required scope pinned to
`read`. Keys come from `API_READ_KEY` / `API_READWRITE_KEY` (falling back
to `MCP_READ_KEY` / `MCP_READWRITE_KEY`, so one Secret can serve REST and
MCP); `AUTH_MODE=enforce|log|off` selects the behaviour, defaulting to
`enforce` when a key is configured and to `off` (with a loud WARN) when
none is — so local runs and the existing handler tests are unaffected.
Rejections are RFC 7807 problem details under this service's own
`https://errors.labor-performance.warehouse-systems.dev/` namespace: 401
with `WWW-Authenticate` for a missing or invalid credential, 403 for an
under-scoped one. The chart exposes `auth.mode`, `auth.readKey`,
`auth.readWriteKey` and `auth.existingSecret`, and both the OLTP and the
reports deployments receive the same Secret refs. This context has no
outbound REST client (it is a pure Kafka consumer of
fulfillment-execution, ADR 0003), so it needs no `<PEER>_API_KEY`.

## Consequences

Every REST call to this service — including the HR-adjacent per-associate
scorecards ADR 0005 singles out — is now attributable to a scope, and
defining a standard needs a distinct credential from reading one. No
handler, use case or domain type changed. Static keys are still not
identities; the `Authenticator` seam is where an OAuth 2.1 resource server
plugs in later without touching either router.
