---
id: 0006-mean-actual-seconds-independent-of-standard
slug: /adr/0006-mean-actual-seconds-independent-of-standard
title: 0006. MeanActualSeconds on TaskTypePerformance, independent of any standard
sidebar_label: 0006. MeanActualSeconds
description: ADR 0006 — GetTaskTypePerformance now surfaces a real measured mean duration per TaskType that does not require an active LaborStandard to exist, closing the loop for downstream consumers (workforce-management's ProposePathPlan) that need an actual observed rate rather than an efficiency ratio.
---

# 0006. MeanActualSeconds on TaskTypePerformance, independent of any standard

## Status

Accepted.

## Context

`workforce-management`'s `ProposePathPlan` use case computes proposed
headcount as `ceil(charge / plannedRate)` — but `plannedRate` has always
been a caller-supplied number with no connection to reality: a human
guesses "30 units/hour" and the system trusts it verbatim. Verified
against the actual code before starting (`internal/domain/shiftplan/
shift_plan.go`'s `ProposedHeads`, `internal/application/usecases/
propose_path_plan.go`): there was no path, anywhere in the fleet, from
"here is what fulfillment-execution's Task history actually shows this
task type takes" to a labor-planning decision. This context
(`labor-performance`) exists specifically to answer "how long does a
task type actually take" — closing this loop is the natural next step
after ADR 0002 (why this context exists) and ADR 0005 (the associate-
level visibility signal).

Two shapes were considered for exposing the real number:

1. **A new, dedicated endpoint** (e.g. `GET /task-types/{taskType}/mean-rate`).
2. **Extend the existing `GetTaskTypePerformance` read model** with a new
   field.

Option 2 was chosen for the same reason ADR 0005 chose to extend
`Scorecard` rather than add a sibling endpoint: `MeanActualSeconds` and
`MeanEfficiencyPct` are two views of the same underlying rows for the
same `TaskType`, and a caller wanting "how is PICK doing" naturally wants
both in one read.

**The harder design question was independence from `LaborStandard`.**
`MeanEfficiencyPct` requires an active standard to have existed at each
task's completion time (it is `null` for every row recorded before any
`DefineStandard` call for that `TaskType` — see ADR 0004). A naive
"just expose `MeanEfficiencyPct`" approach would be useless for exactly
the use case this ADR targets: a fleet operator who has NEVER defined a
standard yet, and wants labor-performance to tell them what the real rate
IS so they can define one intelligently, gets nothing back — the very
data needed to bootstrap a first standard is gated behind already having
one. `MeanActualSeconds` breaks that circularity: it is computed
directly from `ActualSeconds` on every recorded row (excluding only
genuinely unmeasurable rows, `ActualSeconds<=0` — the same exclusion
`EfficiencyPct` already applies), with zero dependency on `LaborStandard`
ever having existed.

## Decision

**`ports.TaskTypePerformance` gains `MeanActualSeconds *float64`**,
computed as the mean `ActualSeconds` across every `TaskPerformance` row
for a `TaskType` where `ActualSeconds > 0` — independent of
`MeanEfficiencyPct`, independent of any `LaborStandard` ever existing.
`nil` iff no measurable row has ever been recorded for that `TaskType`,
never a fabricated number.

Both adapters implement it directly:
- `memory.PerformanceRepo`: a second running sum/count alongside the
  existing efficiency aggregation, in the same single pass over rows.
- `postgres.PerformanceRepo`: `AVG(actual_seconds) FILTER (WHERE
  actual_seconds > 0)` alongside the existing `AVG(efficiency_pct)` — one
  query, not two — verified against a real local Postgres instance (not
  just unit-tested against the in-memory adapter) that the `FILTER`
  clause correctly excludes zero/negative rows from the average without
  a separate query or a Go-side post-filter.

`GET /task-types/{taskType}/performance` now returns `meanActualSeconds`
alongside the existing fields. `workforce-management`'s
`ProposePathPlan` (a separate PR, `feature/measured-rate-feed`) is the
first real consumer: it will call this endpoint and, when a caller does
not supply an explicit `plannedRate` override, fall back to this real
measured rate rather than requiring a guess.

## What this explicitly is NOT

Mirroring this context's and the fleet's "visibility, not enforcement"
discipline: this ADR does not make `plannedRate` mandatory-derived from
this field, does not auto-commit a `ShiftPlan`, and does not require
`workforce-management` to call this service synchronously for every
plan proposal — a caller retains the ability to override with a
hand-specified `plannedRate` (see the companion ADR in
`workforce-management` for exactly how the fallback and override
interact).

## Consequences

### Easier

- **Bootstraps the "what SHOULD the standard be" question.** An operator
  defining a `LaborStandard` for the first time for a `TaskType` can now
  query `MeanActualSeconds` first, informing the value they set, rather
  than guessing blind — the exact opposite of the circularity a
  standard-dependent field would have created.
- **One query, both numbers.** A caller wanting "how efficient AND how
  fast (in absolute terms) is this task type" gets both from the single
  existing `GetTaskTypePerformance` call, same pattern ADR 0005 used for
  `Scorecard`.
- **Verified against a real Postgres instance**, not assumed from SQL
  syntax alone — `TestPostgres_PerformanceRoundTrip` was extended and run
  against a live local Postgres 16 container, confirming the `FILTER`
  clause's exclusion behavior matches the in-memory adapter's Go-side
  filtering exactly.

### Harder

- **Two parallel "mean" computations now live in the same method** (one
  gated on `EfficiencyPct != nil`, one on `ActualSeconds > 0`) — a small
  but real increase in `TaskTypePerformanceFor`'s cyclomatic complexity in
  both adapters, accepted because splitting them into two separate
  methods/queries would cost an extra round-trip for callers wanting
  both numbers (the common case).
- **A `TaskType` with plenty of `TaskPerformance` rows but zero measurable
  ones** (every row has `ActualSeconds<=0`, e.g. a fleet that has not yet
  rolled out `fulfillment-execution`'s claim-timestamp migration) still
  returns `meanActualSeconds: null` — an honest "we have no measurable
  data yet," not a workaround.
