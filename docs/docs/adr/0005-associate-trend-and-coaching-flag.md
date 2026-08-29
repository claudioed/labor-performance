---
id: 0005-associate-trend-and-coaching-flag
slug: /adr/0005-associate-trend-and-coaching-flag
title: 0005. Associate Trend and CoachingFlag on the Scorecard read model
sidebar_label: 0005. Trend & CoachingFlag
description: ADR 0005 — GetAssociateScorecard now surfaces a bounded-recent-window Trend classification and a below-standard CoachingFlag, closing a real gap against Manhattan Active Labor Management's "employee report cards" and Blue Yonder's "monitor performance trends over time... systematically coach preferred methods."
---

# 0005. Associate Trend and CoachingFlag on the Scorecard read model

## Status

Accepted.

## Context

Competitor research (verified via live browser navigation to Blue
Yonder's own Warehouse Labor Management solution page, since
`web_search`/`web_extract` were unavailable this session) lists five
named capabilities for its Warehouse Labor product: Labor planning,
Flexible labor standards, Continuous Improvement, Granular visibility,
Incentive management. This context, before this ADR, covered only
"Flexible labor standards" (`LaborStandard`) and a flat, point-in-time
"Granular visibility" (`GetAssociateScorecard`'s `MeanEfficiencyPct`).

The specific gap: Blue Yonder's own copy for "Continuous Improvement"
reads *"Evaluate and improve quality, safety, and productivity with
robust labor observation capabilities. **Monitor performance trends over
time** and utilize **employee report cards** to align people to
corporate goals and **systematically coach preferred methods**."*
Manhattan Active Labor Management uses the same language ("Labor
Monitoring — manage actual performance against standards in real time").
Verified against this context's actual code before starting: `Scorecard`
carried only an all-time flat mean with no notion of "recently" or "is
this associate on a bad streak right now" — a supervisor reading it could
not distinguish an associate who was struggling last month but has
recovered from one who is struggling today.

Two designs were considered for closing this gap:

1. **A separate new endpoint** (e.g. `GET /associates/{id}/trend`) that a
   caller calls alongside `GET /associates/{id}/scorecard`.
2. **Extend the existing `Scorecard` read model** with `Trend` and
   `CoachingFlag` fields, computed as part of the same
   `GetAssociateScorecard` call.

Option 2 was chosen: a supervisor's real workflow is "look at this
associate's picture," not "look at their mean, then separately check
their trend" — splitting it into two round-trips (and two REST calls the
future `labor-mfe` screen would have to compose) serves no one. The
two new fields are computed alongside the existing read model in a
single query pass, not via a second network hop.

## Decision

**`GetAssociateScorecard` now additionally computes, from the same
associate's most recent (up to 10) `TaskPerformance` rows:**

1. **`Trend`** (`IMPROVING` \| `DECLINING` \| `STABLE` \|
   `INSUFFICIENT_DATA`) — the recent-window mean `EfficiencyPct` compared
   against the associate's all-time baseline mean (the existing
   `MeanEfficiencyPct`), via the new pure domain function
   `performance.ClassifyTrend`. A ±5 percentage-point band around the
   baseline counts as `STABLE` (natural task-to-task variance, not a real
   trend); fewer than 3 *scored* recent tasks always yields
   `INSUFFICIENT_DATA`, never a fabricated direction from a thin sample.
2. **`CoachingFlag`** (`bool`) — `true` iff the associate's 3 most recent
   *scored* tasks are ALL below an 85% efficiency floor, via the new pure
   domain function `performance.DetectCoachingFlag`. Unscored rows (nil
   `EfficiencyPct` — no active standard existed, or `duration_seconds`
   was 0) are skipped entirely when building this window; they carry no
   signal either way and are never treated as a below-standard task.

Both are pure functions in `internal/domain/performance/trend.go`,
operating on plain `*float64`/`[]float64` — no repository, no I/O — so
every threshold/boundary case is directly unit-testable without standing
up any adapter. `internal/application/usecases/get_associate_scorecard.go`
composes them: it calls a new port method,
`PerformanceRepo.RecentByAssociateID(associateId, limit)` (newest-first,
capped at 10), reorders to oldest-first, extracts only the scored values,
and passes them to both domain functions.

The 85% coaching floor and the 3-consecutive-task threshold are
documented named constants (`coachingEfficiencyFloor`,
`coachingConsecutiveThreshold` in `trend.go`), not magic numbers — chosen
to mirror the everyday floor-supervisor heuristic ("three in a row, time
for a word") and to treat routine variance under a perfectly-tuned 100%
standard as normal, not coaching-worthy. Both are revisitable with real
fleet data; nothing about the design requires them to be fixed forever.

**`RecentByAssociateID` is capped at 10, not unbounded.** An associate
could accumulate thousands of scored tasks over a long tenure; the
trend/coaching signal only ever needs a small recent slice, so this stays
a cheap, bounded query (`ORDER BY completed_at DESC LIMIT $2` in
Postgres) regardless of tenure length — consistent with this context's
existing performance discipline (`ScorecardFor`/`TaskTypePerformanceFor`
are both single-pass aggregate queries, never full-table scans returned
to the caller).

## What this explicitly is NOT

Mirroring this context's existing "visibility, not enforcement"
discipline (see ADR 0002's framing of `PathUnderstaffed` in
`workforce-management`, and this context's own README "What this
context explicitly does NOT do"): `CoachingFlag` is a signal a human
reads, never an automated action. It does not:

- Gate or block a below-standard associate's ability to claim tasks in
  `fulfillment-execution`.
- Trigger any notification, automated coaching workflow, or write to any
  other system.
- Feed automatic pay/bonus calculation (Manhattan's "Pay for
  Performance" / Blue Yonder's "Incentive management" — a separate, still
  out-of-scope competitor gap, tracked as a distinct future feature, not
  conflated with this one).

## Consequences

### Easier

- **A supervisor reading one `GET /associates/{id}/scorecard` call gets
  both "how has this person done overall" and "is this a live concern
  right now"**, matching how Manhattan/Blue Yonder's own "report card"
  language frames the capability — one read, not a stitched-together
  picture across two endpoints.
- **Every threshold/boundary case is directly, deterministically unit
  tested** at the pure-function level (`trend_test.go`: 9 cases for
  `ClassifyTrend`, 9 for `DetectCoachingFlag`, including exact-boundary
  cases at the 5-point trend threshold and the 85% coaching floor) AND
  at the real end-to-end wiring level (`usecases_test.go`,
  `server_test.go`) — proving the real `memory`/Postgres repos and HTTP
  JSON round-trip correctly, not just the isolated math.
- **Unscored rows never corrupt the signal.** A robot-station completion
  (empty `AssociateId`, excluded upstream) or a pre-migration task with
  no `ClaimedAt` (nil `EfficiencyPct`) is silently skipped when building
  the recent window, rather than counted as a zero or triggering a false
  coaching flag.

### Harder

- **`RecentByAssociateID` is a second query per `GetAssociateScorecard`
  call** (alongside the existing `ScorecardFor` aggregate query) — an
  accepted, small cost for a bounded (`LIMIT 10`) query, not a
  full-table scan.
- **The 5-point trend band and 85% coaching floor are judgment calls**,
  not derived from real fleet data (this context has no real production
  traffic yet) — documented explicitly as revisitable constants rather
  than treated as settled science.
