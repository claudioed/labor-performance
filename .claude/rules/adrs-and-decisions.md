# ADR index (0001–0012)

Full records live in `docs/docs/adr/` (Nygard format, Docusaurus-rendered
at `/docs/adr`). This is a summary index — read the actual ADR before
relying on a detail not captured here.

| # | Title | Status | What it settles |
| --- | --- | --- | --- |
| 0001 | Hexagonal (ports & adapters) architecture | Accepted | The layering rule enforced by arch-go. |
| 0002 | A new bounded context, not an extension of workforce-management or fulfillment-execution | Accepted | Why this is its own repo (competitor research + both siblings' boundary discipline). |
| 0003 | Kafka choreography consumer of fulfillment-execution, no REST dependency | Accepted | The bounded-context boundary: pure consumer, no write access, no sync calls. |
| 0004 | StandardSecondsAtCompletion is frozen at ingestion time, never recomputed | Accepted | The "resolve as of CompletedAt, not as of now" invariant. |
| 0005 | Associate Trend and CoachingFlag on the Scorecard read model | Accepted | The passive visibility signal computed from the last (up to 10) scored tasks. |
| 0006 | MeanActualSeconds on TaskTypePerformance, independent of any standard | Accepted | Why a real measured rate is tracked separately from EfficiencyPct. |
| 0007 | Analytical data product | Accepted | The `cmd/labor-projector` + `cmd/labor-reports` split, the separate analytics topic/DB, the Labor Performance Report shape. See `.claude/rules/docs-and-api-drift.md` for the resulting two-OpenAPI-spec setup. |
| 0008 | Standard metrics convention across the fleet | Accepted | Fleet-wide naming/shape conventions this service's metrics follow. |
| 0009 | Model Context Protocol as an inbound adapter, not a new service | Accepted | MCP server lives in this repo (`internal/adapters/inbound/mcp/`, `cmd/mcp/`), not a separate service. |
| 0010 | Transactional outbox for the analytics topic | Accepted | Domain event + `processed_events` marker + analytics event commit in one Postgres transaction; an in-process relay drains `outbox_events` onto `warehouse.labor-performance.analytics`. |
| 0011 | REST identity — fleet-standard static bearer keys with read/read-write scopes | **Superseded by 0012** | Do not re-implement this pattern; it was deliberately removed. |
| 0012 | Remove the REST/MCP identity layer | Accepted | Current state: no auth layer on REST or MCP. Read this before assuming any endpoint requires a bearer token. |

## Reading order for a newcomer

1. 0001 (architecture) → 0002 (why this repo exists) → 0003 (the boundary).
2. 0004 (the core scoring invariant) → 0005/0006 (what got added on top of
   the v1 scoring model).
3. 0007 (analytics data product) → 0010 (how writes get to the analytics
   topic reliably) — read together, 0010 depends on 0007's shape existing.
4. 0009 (MCP) if working on the MCP inbound adapter.
5. 0011 then 0012 together — 0011 is superseded, but reading it first
   makes 0012's reasoning ("why we undid this") legible.

## Proposing a new ADR

Copy the Nygard template from `docs/docs/adr/about.md`, next free number,
`Status: Proposed`, add to both the site's `docs/docs/adr/about.md` table
and `docs/sidebars.ts`, PR it, flip to `Accepted` on merge. An accepted
ADR is never edited to change its decision — a reversal gets a new ADR
that supersedes it (see 0011 → 0012 for the pattern).
