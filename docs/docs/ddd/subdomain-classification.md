---
title: Subdomain Classification
sidebar_label: Subdomain Classification
description: Why Labor Performance is a Supporting subdomain, with the aggregates and invariants that follow from it.
---

# Subdomain Classification

## Verdict: **Supporting subdomain**

Labor Performance does not define the fulfillment work itself — it only
measures how well it is executed against a standard someone else configures.
It is genuinely useful (both Manhattan and Blue Yonder ship it as a
first-class capability), but it is not what differentiates a fulfillment
operation the way `fulfillment-execution`'s Pick/Pack/SLAM task lifecycle or
`inventory-storage`'s chaotic-storage inventory truth do.

| Context | Classification | Reasoning |
| --- | --- | --- |
| `fulfillment-execution` | Core | The Pick/Pack/SLAM task lifecycle; throughput and accuracy at scale. |
| `inventory-storage` | Core | Owns inventory truth: bin-accurate location + usable inventory. |
| `wes-work-planning` | Core | The conductor — waveless release and flow balance. |
| `workforce-management` | Supporting | Labor & workforce allocation: important, industry-common, not the differentiator. |
| **`labor-performance`** | **Supporting** | **Measures execution quality against a configured standard — valuable, but not the differentiator itself.** |
| `facility-layout` | Generic | Physical warehouse structure, extracted once rather than duplicated. |

## What "Supporting" buys and obliges

Classifying a subdomain Supporting (not Core, not Generic) means:

- **It is genuinely part of the business**, unlike a Generic subdomain —
  engineered labor standards are a real competitive-adjacent capability
  (Manhattan and Blue Yonder both ship it), not a commodity integration
  surface.
- **It does not carry Core-level investment**, but it is not skipped
  either — this service still carries the full fleet-standard rigor (BDD,
  mutation testing, arch-fitness tests, 90%+ domain/application coverage)
  because "Supporting" is a business-differentiation classification, not a
  quality bar.
- **It is a pure Customer of a Core context's Open Host Service** — it never
  gets write access to `fulfillment-execution`'s Task aggregate, and it
  never influences that context's decisions (no gating, no blocking).

## Aggregates & invariants

### LaborStandard

The aggregate root for "how long a task TYPE should take."

- `ExpectedSeconds` must be > 0 — a standard that says a task should take
  zero or negative seconds is not a business fact.
- **Append-only history.** `DefineStandard` for a TaskType that already has
  an active standard does NOT overwrite it in place — it closes the prior
  standard's effective range (`EffectiveTo`) and starts a new one. This
  keeps already-recorded `TaskPerformance` rows' frozen
  `StandardSecondsAtCompletion` values historically accurate even after a
  later revision. Mirrors `workforce-management`'s own "AssignLabor ends
  the prior assignment rather than rejecting" pattern — a revision closes
  the old record rather than erroring. See
  [ADR 0004](/docs/adr/0004-standard-frozen-at-completion-time-not-recomputed).
- ONE ACTIVE standard per TaskType at any given time.

### TaskPerformance

The aggregate root for one scored, already-completed task — an
event-sourced fact from Kafka, not something a human edits.

- **Immutable once recorded.** No update/delete use case anywhere in the
  application layer.
- **Idempotent on the Kafka message's `event_id`**, not `TaskId` (which
  could in principle be reused after a very long time). Recording the same
  `event_id` twice is a no-op, never a double-count — mirrors the
  `ProcessedEvents` idempotency-gate pattern every analytics projector in
  this fleet already uses.
- **`EfficiencyPct` never divides by zero.** `ActualSeconds<=0` (an
  unmeasurable completion — e.g. a `TaskCompleted` whose `duration_seconds`
  is 0 because no claim-timestamp existed to compute it from) or
  `StandardSecondsAtCompletion<=0` (no active standard existed for that
  TaskType at completion time) both yield `null`, not an error and not a
  fabricated number.
- **`StandardSecondsAtCompletion` is frozen at ingestion time.** Resolved
  once — the standard active "as of `CompletedAt`," not "active right
  now" — and never recomputed later from a since-changed standard.
- **An empty `AssociateId` is legitimate**, not an error — a `TaskCompleted`
  from a station with no checked-in occupant (e.g. a robot station) is
  still recorded and counted in `GetTaskTypePerformance`, just excluded
  from any per-associate scorecard.

### Scorecard (read model, not a stored aggregate)

A projection over `TaskPerformance` rows, per associate: task count, mean
`EfficiencyPct` across tasks that have one, breakdown by `TaskType`. A 404
means "this service has never recorded a row for this associate" — distinct
from "the associate has rows but no numeric score yet" (which is a 200 with
`meanEfficiencyPct: null`).

## Ubiquitous language

| Term | Meaning |
| --- | --- |
| **LaborStandard** | The expected duration for a TaskType, with append-only revision history. |
| **TaskPerformance** | One scored, completed task — frozen against the standard active when it finished. |
| **Scorecard** | A per-associate read-model projection over TaskPerformance rows. |
| **TaskType** | PICK, PACK, or SLAM — mirrors `fulfillment-execution`'s `task.Type` enum exactly; no new values invented here. |
| **EfficiencyPct** | `100 * StandardSecondsAtCompletion / ActualSeconds`, nullable, never computed by dividing by zero. |

## Domain events (past tense)

`LaborStandardDefined`, `LaborStandardRevised`, `TaskPerformanceRecorded` —
published via a log publisher only in v1 (no Kafka publish yet). These are
not currently consumed by any other service in the fleet; they exist for
symmetry with the fleet's convention and to leave an integration seam open.
