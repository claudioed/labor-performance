---
id: 0002-new-bounded-context-not-extension-of-workforce-or-fulfillment
slug: /adr/0002-new-bounded-context-not-extension-of-workforce-or-fulfillment
title: 0002. A new bounded context, not an extension of workforce-management or fulfillment-execution
sidebar_label: 0002. Why a new bounded context
description: ADR 0002 — why engineered labor standards and performance scoring live in a new Supporting context rather than being bolted onto an existing sibling.
---

# 0002. A new bounded context, not an extension of workforce-management or fulfillment-execution

## Status

Accepted. Established with the initial implementation of this bounded context.

## Context

Competitor research this session (Manhattan Active Labor Management, Blue
Yonder Workforce & Labor Management — both real, current products,
verified via their public marketing/feature pages) converges on one
capability neither existing sibling context in this fleet has today:
**engineered labor standards** (an expected time-per-task-type, e.g. "a
PICK should take 45s") paired with **actual-vs-standard performance
scoring** ("this associate's last PICK took 52s — 87% of standard").
Manhattan brands this "Labor Monitoring — manage actual performance
against standards in real time" plus "Time Tracking"; Blue Yonder brands
it "align labor to demand with real-time operational signals." Both
vendors treat it as a distinct product capability, not a sub-feature of
shift/schedule planning — which is the same shape of evidence this fleet's
other DDD decisions (e.g. `inventory-storage`'s `ProductClassification`)
have used to justify a dedicated model rather than bolting a new concept
onto an adjacent one.

Two existing contexts are the obvious places this *could* have been
bolted on, and both have their own documented reasons it should not be:

- **`workforce-management`** owns "who is on shift, on which process path,
  at what rate; direct vs indirect hours." Its own CLAUDE.md is explicit
  that it **stops at the path boundary**: "it never links an associate to
  a specific task — dispatch of individual tasks to a claiming station
  belongs to Fulfillment Execution." Scoring an associate's actual PICK
  duration against a standard requires exactly that link — associate to
  task — which this context's own design discipline forbids it from
  making. Weakening that boundary to host labor-performance scoring would
  undo the very separation ("dispatch policy can change without touching
  workforce planning, and vice versa, because they change at completely
  different cadences") that boundary exists to protect.
- **`fulfillment-execution`** owns the Task/Station/Package lifecycle and,
  via the sibling `feature/labor-performance-hooks` change, now publishes
  WHO completed a task and HOW LONG it took as an enrichment on its
  existing `TaskCompleted` event. But it has zero concept of a
  "standard" to measure that duration against, and it explicitly follows
  a "stops at Task/Station only" design discipline (see its own CLAUDE.md,
  e.g. "Fragile... does not gate claiming" and "hazmat... already worked
  via the pre-existing generic capability-matching mechanism — no
  structural change was needed"). Adding a standards concept there would
  be scope creep into a domain neither Task nor Station has any business
  modeling — a Task cares about being claimed and completed, not about
  whether its own duration was efficient.

## Decision

**A new Supporting-subdomain bounded context, `labor-performance`, owns
`LaborStandard` and `TaskPerformance`.** It does not inherit either
sibling's boundary promise, so it is free to model "here is the standard,
here is the actual, here is the score" without either sibling context
having to compromise its own boundary to host it.

It is explicitly **Supporting, not Core**: it does not define the
fulfillment work itself (that remains `fulfillment-execution`'s job), it
only measures how well that work was executed against a standard someone
else configures via this service's own REST API. It never gates or blocks
anything in `fulfillment-execution` — a below-standard associate is still
allowed to claim tasks (see
[ADR 0003](./0003-kafka-choreography-consumer-of-fulfillment-execution.md)
for the mechanics of how it observes that work without touching it) — this
is visibility, not enforcement, mirroring `workforce-management`'s own
"flags a gap, does not decide" design philosophy for `PathUnderstaffed`.

This context explicitly does **not** attempt automatic coaching or
automatic pay/bonus calculation, despite "Pay for Performance" being a
real Manhattan feature — that is out of scope for this v1 (see the
README's "Deferred (v1)" section). It does not talk to payroll, HR, or
scheduling systems.

## Consequences

### Easier

- **Neither sibling context has to compromise its own documented
  boundary.** `workforce-management`'s "stops at the path boundary" and
  `fulfillment-execution`'s "Task/Station only" promises both remain
  exactly as strict as they were before this context existed.
- **The domain model is honest about what it does and does not know.**
  `TaskPerformance.AssociateId` can be legitimately empty (a robot station
  completion); `TaskType` can be legitimately unclassified (the
  wire-contract gap documented in
  [ADR 0003](./0003-kafka-choreography-consumer-of-fulfillment-execution.md)).
  A context bolted onto an existing aggregate would have had to force
  these into that aggregate's existing assumptions about what a "valid"
  record looks like.
- **The two aggregates can evolve on this context's own cadence.**
  Revising how a standard's history works, or adding new read models over
  `TaskPerformance`, never requires touching `workforce-management`'s or
  `fulfillment-execution`'s release cycle.

### Harder

- **A seventh service in the fleet is a seventh thing to run, monitor, and
  deploy.** This is the standard cost of a new bounded context; it is
  accepted here because the competitor-validated capability is real and
  distinct enough to earn its own home, matching this fleet's established
  bar (e.g. `facility-layout` was onboarded on the same reasoning for zone
  topology).
- **Cross-context correlation ("show me this associate's shift AND their
  efficiency") requires a caller (e.g. a future console BFF) to join
  `workforce-management`'s and this context's REST APIs itself.** Neither
  service is aware of the other. This is a deliberate, accepted cost of
  keeping the two contexts' write models independent — the same tradeoff
  `order-management`'s console screens already make for order lifecycle
  correlation across `wes-work-planning` and `fulfillment-execution`.
