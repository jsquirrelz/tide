# Phase 12: Gate Semantics + Reject/Resume - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-11
**Phase:** 12-Gate Semantics + Reject/Resume
**Areas discussed:** Approval semantics, Approved-state shape, Reject/resume contract, Planner-retry trigger

---

## Approval semantics (gate seat)

| Option | Description | Selected |
|--------|-------------|----------|
| Gate at descent | AwaitingApproval blocks child dispatch; approve releases descent; level auto-succeeds when children complete. Matches docs' review-before-descent promise; folds run-1 finding 1 in as GATE-04 | ✓ |
| Gate at completion only | Children dispatch immediately (today's timing); approval only unlocks the final Succeeded flip | |
| Two gates (descent + completion) | Approve to release descent AND approve to accept completion | |

**User's choice:** Gate at descent (recommended option)
**Notes:** Scope addition acknowledged — GATE-04 added to REQUIREMENTS.md and Phase 12 traceability.

### Follow-up: brake point

| Option | Description | Selected |
|--------|-------------|----------|
| Materialize children, hold dispatch | Child CRs created immediately (DAG visible while reviewing); child reconcilers refuse Job dispatch while parent unapproved | ✓ |
| Hold materialization | Reporter doesn't create child CRs until approval | |
| You decide | Claude's discretion | |

**User's choice:** Materialize children, hold dispatch (recommended option)

---

## Approved-state shape

| Option | Description | Selected |
|--------|-------------|----------|
| Running + ApprovedByUser condition | No CRD enum change; condition mirrors existing ResumedByUser pattern | ✓ |
| Distinct Approved phase value | Matches gates.md:99 sketch; churns every phase-switch surface | |
| You decide | Claude's discretion | |

**User's choice:** Running + ApprovedByUser condition (recommended option)

---

## Reject/resume contract

| Option | Description | Selected |
|--------|-------------|----------|
| Reject parks, doesn't fail | Rejected condition + dispatch halt; no Failed writes; resume lifts the park; "preserves all state" becomes true | ✓ |
| Keep fail-marking; resume resets | Reject keeps writing Failed; resume implements the kubectl reset recipe | |
| You decide | Claude's discretion | |

**User's choice:** Reject parks, doesn't fail (recommended option)

### Follow-up: resume scope

| Option | Description | Selected |
|--------|-------------|----------|
| Yes — resume retries Failed too | `tide resume --retry-failed` implements the run-1 kubectl recipe as a sanctioned verb; pairs with HALT-01 recovery | ✓ |
| No — resume only lifts parks | Failed recovery stays a manual documented recipe | |
| You decide | Claude's discretion | |

**User's choice:** Yes — resume retries Failed too (recommended option)

---

## Planner-retry trigger

| Option | Description | Selected |
|--------|-------------|----------|
| Approve errors, points to resume | `tide approve` on a Failed level prints reason + directs to resume --retry-failed; one recovery verb; GATE-03 reworded | ✓ |
| Approve implicitly retries | Approval doubles as retry trigger; risks re-firing into empty credit balance | |
| You decide | Claude's discretion | |

**User's choice:** Approve errors, points to resume (recommended option)
**Notes:** Gate-at-descent structurally eliminates finding 5's original scenario (children can't have failed planner attempts while parent is parked).

---

## Claude's Discretion

- Regression-test vehicle split (envtest vs kind Layer B per scenario)
- Condition type/reason naming (follow existing api/v1alpha1 conventions)
- Migration handling for run-1 CRs already fail-marked in the live kind cluster
- Whether reject cancels in-flight Jobs or lets them drain

## Deferred Ideas

- Dashboard approval UX beyond copy-to-clipboard (dashboard stays read-only per PROJECT.md)
- Per-level gate timeout / auto-approve-after-N-hours (new capability, no v1.0.1 requirement)
