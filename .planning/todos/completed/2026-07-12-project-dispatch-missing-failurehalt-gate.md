---
name: project-dispatch-missing-failurehalt-gate
description: ProjectReconciler's planner-dispatch hold chain never checks FailureHalt — conservative-profile halt cannot gate Project-level planner dispatch
type: candidate-finding
captured: 2026-07-12
source: Phase 41 plan-check review (gap flagged against RESEARCH.md Pitfall 1's four-site comparison — the fifth dispatch chain was missed)
relates_to:
  - phase-41 plan 41-05 (REFAC-07 — checkDispatchHolds extraction, text-scoped to Milestone/Phase/Plan)
  - .planning/todos/pending/2026-07-12-task-dispatch-gate-order-divergence.md (sibling finding: Task's Import-position divergence; created by 41-05 Task 3)
  - Phase 25 (FailureProfile {strict|conservative} + ConditionFailureHalt, failure_halt.go at 4 execution sites)
---

# ProjectReconciler dispatch chain has no checkFailureHalt gate

## Finding (verified at HEAD, 2026-07-12)

`project_controller.go` ~:1568-1592 (the Phase-13/14/28 hold block ahead of planner-dispatch pool acquire) runs:

```
checkBillingHalt (30s) → checkBudgetBlocked && !IsBypassed (30s) → Import-pending (5s)
```

with **no `checkFailureHalt` call** — `grep -n 'checkFailureHalt' internal/controller/project_controller.go` returns zero hits for the entire file. Every other dispatch chain gates on it:

- Milestone (`milestone_controller.go:343-393`): Reject → Billing → **Failure** → Budget → Import
- Phase (`phase_controller.go:330-386`): ParentApproval → Reject → Billing → **Failure** → Budget → Import
- Plan (`plan_controller.go:343-400`): same as Phase
- Task (`task_controller.go:366-458`): Reject → ParentApproval → Import → Billing → **Failure** → Budget → headroom

## Consequence

Under `FailureProfile: conservative` (Phase 25), a project-wide `ConditionFailureHalt` freezes Milestone/Phase/Plan/Task dispatch — but the Project-level planner Job itself still dispatches. A halted project can keep spending on project-planner runs while every child level is parked. Latent, pre-existing (dates to the Phase 25 conservative-halt landing, which wired failure_halt.go into "4 execution sites" and did not count the Project chain); NOT introduced or worsened by Phase 41.

## Scope note

Phase 41's REFAC-07 (plan 41-05) stays text-scoped to extracting the Milestone/Phase/Plan shared chain — expanding it to add a new gate to Project would be a behavior change in a strictly non-breaking phase. This finding routes to a future phase.

## Fix fork (future phase decision)

1. **Add the gate:** insert `checkFailureHalt(project)` into Project's chain matching the planner-tier order (Billing → Failure → Budget → Import, 30s requeue), with an envtest proving conservative-profile FailureHalt parks project-planner dispatch. Natural companion: migrate Project onto `checkDispatchHolds` (post-41-05 the helper carries exactly this ordering).
2. **Declare intentional:** document that Project-level planning is exempt from FailureHalt (planning is cheap relative to execution; the halt exists to stop cascading child dispatch) — requires a spec/docs note, not code.

Option 1 is the consistent read of Phase 25's project-wide halt semantics; option 2 needs an explicit design argument.

## Verify line (for whichever phase picks this up)

`grep -c 'checkFailureHalt' internal/controller/project_controller.go` ≥ 1 (option 1) OR a documented exemption in the failure-semantics doc (option 2); conservative-profile envtest covering the Project chain either way.
