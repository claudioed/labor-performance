---
id: 0015-optional-travel-component-on-labor-standard
title: 15. Optional travel-time component on a LaborStandard
sidebar_label: 15. Travel component on a standard
sidebar_position: 15
description: "Why LaborStandard gained an optional, caller-supplied TravelComponentSeconds breakdown of ExpectedSeconds -- and why this service still never calls facility-layout or any sibling context to compute or validate it, preserving the v1 'no REST dependency in either direction with any sibling context' boundary."
---

# 15. Optional travel-time component on a LaborStandard

## Status

**Accepted** — implemented in the same change that introduced this
record.

## Context

facility-layout's own Phase B3 follow-on list named this exact
integration as this service's piece of the work: "labor-performance:
travel component of engineered standards from real distance instead of a
constant." Two prior B3 items already closed the equivalent gap
elsewhere in the fleet by adding a live, synchronous
`TravelDistanceLookup` port to the consuming service — wes-work-planning
(ADR 0017 there) computes a `PathPlan`'s travel distance at commit time by
calling facility-layout's `GET /distance` directly, and
fulfillment-execution (ADR 0024 there) validates a `Station`'s
`locationCode` against a live `GET /locations/{locationCode}` read.

That pattern is NOT available here. This service's own v1 scope decision,
stated plainly in `.claude/rules/domain-model.md` and `AGENTS.md`'s
non-negotiables, is a blanket one: **"No REST dependency in either
direction with any sibling context."** Unlike wes-work-planning's or
fulfillment-execution's boundary promises (which name specific siblings
they won't call), this service's promise has no carve-out for
facility-layout or anyone else — it is a pure Kafka **consumer** of
fulfillment-execution's `TaskCompleted` and nothing more, choreography
end to end (ADR 0002, ADR 0003). Reaching for the same
`TravelDistanceLookup`-port design used twice already in this fleet would
be architecturally identical in shape but would directly violate this
service's own founding constraint — the constraint is not incidental, it
is the entire reason this is a Supporting subdomain rather than an
extension of an existing context (see ADR 0002's "why a new bounded
context" reasoning).

The underlying business need is nonetheless real: an engineered labor
standard like "PICK should take 45 seconds" is, in practice, made up of a
travel component (walking to the next pick location) and a task-execution
component (the pick itself). A facilities team re-slotting a zone changes
the travel component without changing how long the actual pick action
takes — visibility into that split is genuinely useful for anyone
reviewing why a standard changed. The question this ADR answers is how to
capture that split without this service ever making a network call to
learn it.

## Decision

**We will add an OPTIONAL `TravelComponentSeconds` field to
`LaborStandard`, supplied entirely by the CALLER at `DefineStandard` time
— never computed, resolved, or validated by this service against any
live distance lookup, in facility-layout or anywhere else.**

1. **Declarative input, not a derived fact.** The caller (a human
   operator, or an external tool/script that itself queries
   facility-layout's `estimate_travel_distance` endpoint) supplies the
   number directly in the `POST /standards` request body. This service's
   only job is to persist it, publish it, and enforce one local
   invariant — it performs zero outbound calls to obtain or check it.
   This is the same shape of "optional, caller-supplied hint this service
   never itself resolves" decision process-path-management's own ADR 0009
   reached for its `DestinationLocationRole` field, for the identical
   reason: a stricter zero-dependency boundary than the rest of the fleet
   forecloses the live-lookup design outright.
2. **One local invariant: `0 <= TravelComponentSeconds <= ExpectedSeconds`.**
   A negative travel component is not a business fact
   (`ErrNegativeTravelComponentSeconds`). A travel component larger than
   the standard's own total duration is also not a business fact — travel
   can be the ENTIRE standard (a water-spider replenishment run is
   arguably all travel) but never more than it
   (`ErrTravelComponentExceedsExpectedSeconds`). Both are enforced purely
   in `internal/domain/standard`, no I/O involved.
3. **Nil means "not broken out" — the default, and the only state that
   existed before this field.** Most standards will never declare one;
   omitting it entirely (not defaulting to 0) on the wire, in both the
   REST response and the published `LaborStandardDefined`/
   `LaborStandardRevised` events, is what lets a consumer distinguish "no
   travel breakdown was ever declared" from "the travel component is
   genuinely zero" — the same nullable-not-zero discipline this service's
   own `EfficiencyPct`/`meanEfficiencyPct`/`meanActualSeconds` already
   apply everywhere (CLAUDE.md's "Never fabricate a number" rule extends
   naturally to this field: an unset travel component is not the same
   fact as a zero-second one).
4. **A revision's travel component is its own value, never inherited.**
   `DefineStandard` revising an already-active standard for a TaskType
   does not carry the CLOSED standard's `TravelComponentSeconds` forward
   — the revision declares its own (possibly absent) breakdown, exactly
   mirroring how `ExpectedSeconds` itself already works on a revision
   (append-only history, ADR 0004).
5. **Not resolved against a live distance lookup even for MCP or the
   analytics data product.** `get_labor_standard`'s MCP tool output and
   the `LaborStandardDefined`/`LaborStandardRevised` events on
   `warehouse.labor-performance.analytics` both surface exactly the
   declared value, with the same nil-when-absent discipline — there is no
   code path anywhere in this service that calls out to check it.

## Consequences

### Easier

- **Closes facility-layout's Phase B3 follow-on** for this service
  without introducing a single new outbound dependency, keeping this
  service's "pure Kafka consumer, zero REST dependency in either
  direction" promise fully intact — the one architectural property this
  bounded context exists to guarantee (ADR 0002).
- **A human reviewing a standard change can now see travel vs. execution
  time split out**, closing a real, previously invisible gap, with zero
  new failure modes: there is nothing to time out, retry, or fail open
  on, because there is no call to make.
- **Consistent with process-path-management's own precedent** (ADR 0009)
  for the same shape of problem under the same "stricter-than-usual
  zero-dependency boundary" constraint — a reader who understands one
  understands the other immediately.

### Harder

- **The declared travel component can silently drift out of sync with
  reality.** If a zone is re-slotted and the true walking distance
  changes, nothing in this service notices — the number is exactly as
  fresh as whoever last called `DefineStandard` bothered to make it. This
  is a deliberate, permanent trade for preserving the zero-dependency
  boundary, not an oversight: any design that kept it fresh automatically
  would require exactly the live call this ADR rules out.
- **No compile-time or contract-test link to facility-layout's own
  distance model at all** — unlike wes-work-planning's or
  fulfillment-execution's live-lookup ports, there is no shared wire
  contract, no `*_MODE=http|permissive` toggle, nothing to keep in sync
  beyond a human choosing to re-run `DefineStandard` with an updated
  number. The entire integration is a documented convention ("populate
  this from a real distance estimate"), not an enforced one.
- **A caller can supply an internally-consistent but factually wrong
  number** (e.g. declaring `TravelComponentSeconds: 5` when the real
  walking distance implies 20 seconds) and this service has no way to
  detect it — the one guard-rail it CAN enforce (the value's arithmetic
  relationship to `ExpectedSeconds`) says nothing about whether the value
  is itself accurate.
