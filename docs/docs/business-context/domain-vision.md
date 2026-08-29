---
title: Domain Vision
sidebar_label: Domain Vision
description: Why the Labor Performance bounded context exists, and the business problem it solves.
---

# Domain Vision

## The competitor research

Two real, current WMS/WFM products were surveyed for this domain:

> **Manhattan Active Labor Management** — "Labor Monitoring: manage actual
> performance against standards in real time" plus "Time Tracking."

> **Blue Yonder Workforce & Labor Management** — "align labor to demand with
> real-time operational signals."

Both converge on the same underlying capability: an **engineered labor
standard** (an expected time-per-task-type — "a PICK should take 45s") and
**actual-vs-standard performance scoring** ("this associate's last PICK took
52s — 87% of standard"). Both vendors treat it as a *distinct product
capability*, not a sub-feature bolted onto shift/schedule planning — which is
the strongest signal that it deserves its own bounded context here too,
rather than living inside `workforce-management`'s shift-planning model.

## Why neither existing sibling context is the right home

**`workforce-management`** owns "who is on shift, on which PATH, at what
rate." Its own design explicitly stops at the path boundary and never links
an associate to a specific task — extending it to score individual task
completions would mean crossing a boundary that context's own documentation
treats as load-bearing.

**`fulfillment-execution`** owns Task/Station lifecycle. It now publishes WHO
completed a task and HOW LONG it took as an enrichment on its existing
`TaskCompleted` event (via the sibling `feature/labor-performance-hooks`
change) — but it has zero concept of a "standard" to measure that duration
against. Adding one there would be scope creep into a domain neither Task nor
Station has any business modeling, violating that repo's own "stops at
Task/Station only" design discipline.

A **new** bounded context is the correct home: it inherits neither sibling's
boundary promise, so it is free to model "here is the standard, here is the
actual, here is the score" without either context compromising its own
boundary to host it. See
[ADR 0002](/docs/adr/0002-new-bounded-context-not-extension-of-workforce-or-fulfillment)
for the full decision record.

## The engineered-labor-standards problem

The problem this context solves has two halves:

1. **Define the standard.** Someone (an industrial engineer, a time-study)
   decides a PICK should take 45 seconds. That number needs a home, a
   revision history (standards change as processes improve), and a way to
   answer "what was the standard active when THIS task completed" — not
   "what is the standard right now" — since a possibly out-of-order or
   replayed completion event must be scored against the standard genuinely
   in force at the time, not whatever is active at ingestion time. See
   [ADR 0004](/docs/adr/0004-standard-frozen-at-completion-time-not-recomputed).

2. **Score the actual against it.** Every completed task (consumed from
   `fulfillment-execution`'s `TaskCompleted` event) gets an `EfficiencyPct` —
   `100 * standard / actual` — frozen forever at ingestion time. Never
   recomputed. Never divides by zero: an unmeasurable completion
   (`ActualSeconds<=0`) or a TaskType with no standard yet
   (`StandardSecondsAtCompletion<=0`) both yield `null`, a real business
   fact, not an error.

## What this context explicitly refuses to do

Both Manhattan and Blue Yonder go further than pure visibility — Manhattan's
"Pay for Performance" ties compensation to these scores; both vendors offer
gamification and coaching workflows. This context deliberately stops short:

> This context surfaces the number. A human or another system decides what
> to do with it.

Mirrored directly from `workforce-management`'s own "flags a gap, does not
decide" design philosophy for `PathUnderstaffed` — a below-standard associate
is still allowed to claim tasks. This is visibility, not enforcement, and it
is a scope decision, not an oversight: automatic pay/bonus calculation,
gamification, and coaching workflows are explicitly deferred (see the
README's "Deferred (v1)" section).

## Position in the fleet

| Context | Owns | Relationship to Labor Performance |
| --- | --- | --- |
| `fulfillment-execution` | Task/Station lifecycle | **Supplier** — publishes `TaskCompleted`, this context's only input |
| `workforce-management` | Shift headcount, path assignment | No relationship — different concepts entirely |
| **`labor-performance`** | **Standards + scoring** | **Customer** of fulfillment-execution's Open Host Service, via Kafka only |

There is no REST dependency in either direction between this context and its
one upstream. Everything it needs (`AssociateId`, `TaskType`,
`DurationSeconds`) already travels on the Kafka event — see the
[Context Map](/docs/ecosystem/context-map) for the full wiring.
