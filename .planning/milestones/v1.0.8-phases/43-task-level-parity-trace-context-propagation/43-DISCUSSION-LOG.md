# Phase 43: Task-Level Parity + Trace-Context Propagation - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-16
**Phase:** 43-Task-Level Parity + Trace-Context Propagation
**Areas discussed:** Scope correction (Phase 42 drift), Parent-child linking mechanism, Durable span-ID field shape/coverage, Traceparent injection scope, Task-level failure/retry parity

**Mode:** `--auto` — fully autonomous. No interactive questions were asked; each area below was resolved by direct codebase verification (grep/read against `internal/controller/span_emission.go`, the four planner controllers, `api/v1alpha3/*_types.go`, `pkg/otelai/tracecontext.go`) rather than by asking the user, and the recommended/verified option was auto-selected and logged.

---

## Scope correction — did Phase 42 already add the `.status.trace` field?

| Option | Description | Selected |
|--------|-------------|----------|
| Trust ARCHITECTURE.md's Suggested Build Order (assumes Phase 42 added `Status.Trace.SpanID`) | Phase 43 would only need to extend synthesis to Task + wire propagation | |
| Verify directly against code before scoping | Grep all five `*_types.go` Status structs and `span_emission.go` for existing Trace/SpanID handling | ✓ |

**Selected:** Verify directly. Direct read of `span_emission.go:20-23` and all five CRD Status structs confirmed Phase 42 shipped spans as **independent roots** ("Option A") — no `.status.trace` field exists anywhere, and no `TraceIDFromUID`/remote-SpanContext call exists in production. This is materially bigger scope than ARCHITECTURE.md's build order implied: Phase 43 must retrofit all four existing span-emission call sites for real parenting, not just add a fifth (Task) call site.
**Notes:** [auto] Logged as D-00 in CONTEXT.md. This is a factual correction from code, not a preference — no alternative was viable once verified.

---

## Parent-child linking mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| ARCHITECTURE.md Pattern 2 (recommended) | Child reads parent's persisted span-ID field, builds remote `trace.SpanContext`, injects into `ctx` before `tracer.Start()` | ✓ |
| Alternative: thread full parent context via envelope/dispatch data instead of CRD status | Would duplicate the durable-artifact philosophy CRD status already provides | |

**Selected:** Pattern 2 — reuses the `client.Get` each handler already performs for parent-ref resolution, zero new I/O.
**Notes:** [auto] Selected: "ARCHITECTURE.md Pattern 2" (recommended default). Logged as D-01/D-02 in CONTEXT.md.

---

## Durable span-ID field — shape and level coverage

| Option | Description | Selected |
|--------|-------------|----------|
| Add to all 5 CRDs including Task | Task is a leaf w.r.t. CRD children, but Phase 46's dashboard deep-link (OBS-04) needs every level's own span ID | ✓ |
| Add only to the 4 non-leaf levels, defer Task's field to Phase 46 | Matches ARCHITECTURE.md's hedge ("Task optional, leaf") | |

**Selected:** All 5 CRDs, including Task — deferring it would just move identical field-addition work into Phase 46 instead of doing it once here.
**Notes:** [auto] Selected: "Add to all 5 CRDs" (recommended default — avoids redundant future work). Logged as D-03/D-04 in CONTEXT.md. Field naming/shape (flat string vs. nested struct) left as Claude's Discretion — see CONTEXT.md.

---

## Traceparent injection scope

| Option | Description | Selected |
|--------|-------------|----------|
| All 5 dispatch Jobs + 4 existing reporter Jobs | Matches PROP-01's literal, level-unscoped requirement text | ✓ |
| Task-only (matches ARCHITECTURE.md's Task-parity framing) | Narrower reading focused only on the level ARCHITECTURE.md's build order named | |

**Selected:** All 5 dispatch Jobs (Project/Milestone/Phase/Plan/Task) + the 4 existing reporter Jobs (Task has no reporter Job until Phase 44).
**Notes:** [auto] Selected: "All 5 + 4 existing reporters" (recommended default — literal requirement text has no level scoping). Logged as D-05/D-06 in CONTEXT.md.

---

## Task-level failure/retry span parity

| Option | Description | Selected |
|--------|-------------|----------|
| Inherit Phase 42's D-01..D-04 policies verbatim | Succeeded+failed coverage, one-span-per-attempt, failure detail, degraded-envelope handling | ✓ |
| Task-specific deviation (e.g. skip failed-Task spans) | No stated rationale surfaced for treating Task differently | |

**Selected:** Verbatim inheritance — no Task-specific deviation.
**Notes:** [auto] Selected: "Inherit verbatim" (recommended default). Logged as D-07 in CONTEXT.md.

---

## Claude's Discretion

- Exact field name/shape for the new durable span-ID carrier (flat `{Level}TraceSpanID string` vs. nested `Status.Trace *TraceStatus{SpanID string}`) — recommend the flat-field style for house-style consistency with the existing `{Level}SpanEmittedUID` fields, but either is acceptable.
- Whether the parenting-aware span synthesis is added as a new parameter to `synthesizePlannerSpan` or a new sibling function — implementation detail for planner.

## Deferred Ideas

- `2026-07-12-project-dispatch-missing-failurehalt-gate.md` and `2026-07-12-task-dispatch-gate-order-divergence.md` (W-2 candidates) — dispatch-gate ordering concerns in the same controllers this phase touches, but a distinct concern from span emission; remain next-milestone candidates.
- `cache-f1-direct-sdk-cross-pod-caching.md` (CACHE-F1) — no overlap, deferred vNext+.
- `2026-07-03-signed-commits-verified-badge.md` (GPG/SIGN-02..04) — keyword false-positive, no overlap.
