---
name: task-dispatch-gate-order-divergence
description: Task's dispatch-holds chain checks Import SECOND while Milestone/Phase/Plan (via checkDispatchHolds) check it LAST — same Project state fires a different hold message + requeue interval depending on level
type: candidate-finding
captured: 2026-07-12
source: Phase 41 plan 41-05 / 41-RESEARCH.md Pitfall 1
relates_to:
  - phase-41 plan 41-05 (REFAC-07 — checkDispatchHolds extraction, scoped to Milestone/Phase/Plan; Task intentionally excluded)
  - .planning/todos/pending/2026-07-12-project-dispatch-missing-failurehalt-gate.md (sibling finding: Project's dispatch chain skips checkFailureHalt entirely)
  - internal/controller/dispatch_helpers.go (checkDispatchHolds — the planner-tier shared chain Task does not call)
  - internal/controller/task_controller.go (the divergence-documenting comment landed by plan 41-05 Task 3)
---

# Task's dispatch-holds chain diverges from the planner-tier order on Import position

## Finding (verified at HEAD, 2026-07-12)

The four project-scoped dispatch-hold chains do not share one order. Milestone, Phase, and Plan are byte-identical and now share one implementation (`checkDispatchHolds` in `dispatch_helpers.go`, landed by plan 41-05):

```
Reject -> Billing(30s) -> Failure(30s) -> Budget(30s) -> Import(5s, LAST)
```

`task_controller.go`'s inline chain (~:358-455) checks Import in a different position — SECOND, immediately after parent-approval, before Billing/Failure/Budget — and adds a task-only reservation-headroom hold with no planner-tier counterpart:

```
Reject -> ParentApproval(5s) -> Import(5s, SECOND) -> Billing(30s) -> Failure(30s) -> Budget(30s) -> reservation-headroom(30s, task-only)
```

## Consequence

When a Project has BOTH import-pending AND a Billing/Failure/Budget halt active simultaneously, the hold that fires — and therefore the log message and the requeue interval (5s vs 30s) an operator observes — depends on which CRD level is reconciling. Task parks on the 5s import hold in that state; Milestone/Phase/Plan park on whichever of Billing/Failure/Budget hit first (30s), never reaching the Import check on that reconcile. This is a real behavioral difference in observability and re-check cadence, though not a correctness bug (Task's chain still stops dispatch — it never dispatches against an un-imported workspace).

## Scope note

Phase 41's REFAC-07 (plan 41-05) is a non-breaking cleanup phase — normalizing Task's order would change which hold fires first under the co-occurring-holds scenario above, which is a behavior change out of scope for this phase. Task 3 of plan 41-05 added a cross-reference comment at the head of `task_controller.go`'s gate chain (pointing here) instead of migrating it.

## Fix fork (future phase decision)

1. **Normalize Task onto `checkDispatchHolds`:** move Task's Import check to LAST (matching the planner tier), drop the inline duplication, and add an envtest proving the co-occurring-holds ordering is now consistent across all four levels. Requires an explicit, tested, documented order change — not a silent behavior shift.
2. **Declare Task a permanent structural outlier:** document that Task's Import-second ordering is intentional (e.g., an argument that execution-tier dispatch should surface import-pending before spend-related halts) in the failure-semantics doc, and leave the reservation-headroom hold as Task-only by design.

Option 1 unifies observability and closes the last gap in the item-7 extraction; option 2 needs an explicit design argument for why Task's tier should differ.

## Verify line (for whichever phase picks this up)

Either `grep -c 'checkDispatchHolds' internal/controller/task_controller.go` shows a real call (not just the cross-reference comment) with a passing co-occurring-holds envtest (option 1), or a documented exemption in the failure-semantics doc explains the divergence (option 2).
