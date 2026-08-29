---
id: 0004-standard-frozen-at-completion-time-not-recomputed
slug: /adr/0004-standard-frozen-at-completion-time-not-recomputed
title: 0004. StandardSecondsAtCompletion is frozen at ingestion time, never recomputed
sidebar_label: 0004. Frozen standard at completion time
description: ADR 0004 — a TaskPerformance freezes the LaborStandard active as of its CompletedAt timestamp, resolved once and stored redundantly, never recomputed from a later revision.
---

# 0004. StandardSecondsAtCompletion is frozen at ingestion time, never recomputed

## Status

Accepted. Established with the initial implementation of this bounded context.

## Context

`LaborStandard.ExpectedSeconds` can be revised — engineered labor
standards genuinely change over time as processes improve or industrial
engineering re-times a task. `DefineStandard` implements this as an
append-only history: revising a `TaskType`'s standard closes the prior
record's effective range and starts a new one, rather than overwriting
`ExpectedSeconds` in place (mirroring `workforce-management`'s own
"`AssignLabor` ends the prior assignment rather than rejecting" pattern —
a revision closes the old record rather than erroring).

The question this ADR answers: when `TaskPerformance.EfficiencyPct` is
computed, WHICH standard's `ExpectedSeconds` does it compare against — the
one active when the task completed, or whatever happens to be active
later, at some arbitrary read/report time?

Two designs were possible:

1. **Store only a reference (the `TaskType`) and compute `EfficiencyPct`
   at READ time**, by looking up whatever `LaborStandard` is active *right
   now* for that `TaskType`.
2. **Resolve the active standard ONCE, at ingestion time** (as of the
   task's `CompletedAt`), freeze its `ExpectedSeconds` value redundantly
   onto the `TaskPerformance` row as `StandardSecondsAtCompletion`, and
   compute `EfficiencyPct` from that frozen value — never recomputed
   later.

Option 1 fails a concrete, realistic scenario: a `PICK` standard is 45s
throughout August; industrial engineering re-times it to 40s starting
September 1st. On September 5th, someone reads a `TaskPerformance` row for
a PICK task that completed on August 20th. Under option 1, that historical
row's `EfficiencyPct` would silently change to reflect the 40s standard —
rewriting history that was accurate under the standard genuinely in force
in August. Worse, Kafka delivery is at-least-once and can reorder or
replay: a `TaskCompleted` for an August task could in principle be
consumed (or re-consumed after a redelivery) AFTER the September revision
already landed. Under option 1 there is no way to tell "this task
completed in August, before the revision" from "this task completed in
September, after it" — both would resolve against whatever is active at
read/consume time, silently wrong for the former.

## Decision

**Resolve the active `LaborStandard` for a `TaskCompleted` event's
`TaskType` AS OF its `CompletedAt` timestamp — not "active right now" —
exactly once, at `RecordTaskPerformance` ingestion time.** The resolved
`ExpectedSeconds` (or `0` if no standard was active at that instant) is
stored redundantly on the `TaskPerformance` aggregate as
`StandardSecondsAtCompletion`, and `EfficiencyPct` is computed from that
frozen value at construction time. Neither field is ever recomputed later
— `TaskPerformance` is immutable once recorded (it is an event-sourced
fact from Kafka, not something a human edits), so there is no update path
that could even attempt a recompute.

Concretely, `StandardRepo.FindActiveAsOf(taskType, t)` answers "which
record's effective range — `[EffectiveFrom, EffectiveTo)` — covers instant
`t`", distinct from `FindCurrentlyActive(taskType)` (used by
`DefineStandard`/`GetStandard`, which legitimately care about "right
now"). `RecordTaskPerformance` always calls the former, passing the
event's `CompletedAt`, never the latter.

This means a possibly out-of-order or replayed `TaskCompleted` for an
August task, consumed in September after a revision, still correctly
freezes the AUGUST standard's value — because `FindActiveAsOf` resolves
against the August timestamp, not against September's "currently active"
state.

## Consequences

### Easier

- **A `TaskPerformance` row's `EfficiencyPct` is a permanent, auditable
  historical fact.** Reading it in a year gives the same answer it gave
  the day it was recorded, regardless of how many times the standard has
  been revised since.
- **Kafka replay/backfill is safe by construction.** `RecordTaskPerformance`
  gives the correct answer whether a message arrives in order, late, or is
  redelivered — the resolution is anchored to the event's own timestamp,
  never to consumption time.
- **The invariant is directly, deterministically testable.** `TestDefineStandard_Revision`
  in `internal/application/usecases/usecases_test.go` defines a standard,
  revises it a day later, then records one `TaskPerformance` completed
  BEFORE the revision and one completed AFTER — asserting the former
  freezes the OLD value and the latter the NEW value, with a `FixedClock`
  making both timestamps exact rather than tolerance-based (see
  [ADR 0001](./0001-hexagonal-ports-and-adapters.md)'s `Clock`-as-a-port
  decision).

### Harder

- **Storage is denormalized on purpose.** `StandardSecondsAtCompletion`
  duplicates data that, at insert time, also exists in `labor_standards`.
  This is a deliberate tradeoff for correctness under time-travel/replay,
  not an oversight — the alternative (a live JOIN at read time) is exactly
  the design this ADR rejects.
- **A standard defined AFTER a task already completed can never be applied
  retroactively to that task**, even if an operator wishes it could be —
  `FindActiveAsOf` looks for a standard whose effective range already
  covered the completion instant; a standard that did not exist yet by
  definition was not "active" then. This is consistent with the whole
  model's philosophy (frozen history, not a live recomputation), not a
  bug.
