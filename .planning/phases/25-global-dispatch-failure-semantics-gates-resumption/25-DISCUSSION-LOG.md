# Phase 25: Global Dispatch, Failure Semantics, Gates & Resumption - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-16
**Phase:** 25-global-dispatch-failure-semantics-gates-resumption
**Areas discussed:** Global readiness locus, Conservative failure profile, Gate-as-hold composition, Resumption boundaries

---

## Global readiness locus (DISP-01)

| Option | Description | Selected |
|--------|-------------|----------|
| A/C — TaskReconciler re-derives | Task resolves its own global DependsOn against the completed-set each reconcile via a shared fan-out helper; nothing derived persisted. RESUME-01 free, single dispatch authority. | ✓ |
| B — ProjectReconciler stamps signal | Centralize compute; stamp a per-task dispatchable/blocked-by label TaskReconciler reads. Computes once per project reconcile but reintroduces a derived persisted signal + two-writer staleness hop. | |
| D — wave-gated (rejected) | Dispatch wave N once all of wave <N Succeeded. Breaks strict failure contract (blocks independent later-wave work behind unrelated earlier failure). | |
| E — persisted indegree map (rejected) | `IndegreeMap` in `.status` — forbidden by `verify-no-aggregates`. | |

**User's choice:** A/C — TaskReconciler re-derives.
**Notes:** User asked for full pros/cons + ranking before deciding. Ranking presented: A/C ≫ B ≫ D ≫ E, anchored on RESUME-01 + PERSIST-03 both pointing to "re-derive, persist nothing." Readiness test must be per-dependency (not per-wave-completion) — load-bearing for strict profile. List-all vs per-dep-scoped resolution left to planner.

---

## Conservative failure profile (DISP-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Project enum + BillingHalt-style halt | `Project.Spec.FailureProfile {strict\|conservative}`, default strict. Strict free from indegree. Conservative → project-wide `ConditionFailureHalt` (mirrors BillingHalt), in-flight finishes, cleared by `tide resume --retry-failed`. | ✓ |
| Project enum + per-scope halt | Conservative freezes only the failed task's milestone/phase. More granular than spec requires; more code. | |
| (rejected) pure per-task halt | Each task computes "anything failed AND am I a non-dependent" — stateful, messy, no reuse. | |

**User's choice:** Project enum + BillingHalt-style halt.
**Notes:** Key realization surfaced — strict profile is almost free given the locked A/C indegree model (dependents blocked + siblings continue both fall out). Conservative reuses the proven BillingHalt project-wide-halt rails.

---

## Gate-as-hold composition (DISP-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Option 2 — keep task gate | M/P/P gates = planning-DAG holds (no execution re-check). Task gate = sole execution hold composed with global indegree; AwaitingApproval task blocks dependents free (via indegree). Keeps fully-supervised expressible. | ✓ |
| Option 1 — drop task gate | Gates only at M/P/P; execution = indegree + halts. Simpler but removes fully-supervised expressiveness, narrows DISP-03's explicit "task approve." | |
| (superseded) per-task ancestry gate-closure at execution | My initial framing — re-check every ancestor gate at dispatch. Refuted by user: approve-at-descent means execution can't outrun a milestone/phase/plan gate (un-approved scope ⇒ tasks never authored). | |
| (declined) + optional execution-boundary gate | Project-level "approve the assembled global DAG before execution spends" slack-tide checkpoint. User declined the extra touch. | |

**User's choice:** Option 2 — keep task gate.
**Notes:** This area took the most iteration. The user asked three sharpening questions: (1) requested pros/cons + ranking; (2) "if a Milestone needs approval before continuing planning, how could execution even begin against a global DAG?" — which correctly refuted the per-task ancestry-closure framing and established M/P/P gates as planning-DAG-only; (3) "I still do not understand what a Task-level gate is" — clarified the task gate is the unique *execution* hold (a leaf has no children to author). The user then proposed the final framing themselves (limit gates to M/P/P, OR keep task gate + block dependents-of-AwaitingApproval), and correctly intuited that dependent-blocking is free via the indegree `Succeeded` test.

---

## Resumption boundaries (RESUME-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Confirm — falls out of A/C | Completed-set = Task Succeeded status; indegree re-derived never persisted; in-flight Running re-adopted via deterministic Job name; halts persist as Project status, cleared by tide resume. Phase 25 adds a mid-run-restart regression test. | ✓ |
| Discuss a resume nuance | Work through a restart edge case (completed-but-unreported, attempt counter, halt staleness). | |

**User's choice:** Confirm — falls out of A/C.
**Notes:** Resumption is a consequence of the A/C readiness model, not new work. Clarified that RESUME-01's "no other persisted execution state" targets the derived schedule/indegree, not per-object status conditions or the authored DAG (those are persisted inputs).

---

## Claude's Discretion

- D-01 resolution mechanic (list-all-and-filter vs per-dependency-scoped label-select) and the shared-helper package boundary.
- Watch/field-index wiring so a completing/held Task re-enqueues its global dependents.
- `FailureHalt` condition type/reason strings (mirror `BillingHalt`), `FailureProfile` CEL enum markers/printer columns.
- Keeping `verify-no-aggregates` / `verify-no-sqlite-dep` / `verify-dag-imports` green; metric label set unchanged.

## Deferred Ideas

- Multi-milestone drive + cross-milestone shared waves + per-milestone gate policy + README conformance test — Phase 26 (MS-01..03, SPEC-01).
- Optional project-level execution-boundary gate ("slack tide") — declined for this phase; composes cleanly later if revived.
- Per-scope conservative-halt granularity — rejected for Phase 25; natural Phase 26 extension once per-milestone policy exists.
- Direct-SDK cross-pod prompt caching (CACHE-F1) — unrelated Ebb-Tide follow-up.
