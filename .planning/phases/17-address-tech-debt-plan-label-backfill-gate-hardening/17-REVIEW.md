---
phase: 17-address-tech-debt-plan-label-backfill-gate-hardening
reviewed: 2026-06-13T00:00:00Z
depth: standard
files_reviewed: 4
files_reviewed_list:
  - cmd/tide/approve.go
  - internal/controller/plan_controller.go
  - internal/controller/phase_controller.go
  - internal/reporter/materialize.go
findings:
  critical: 0
  warning: 0
  info: 3
  total: 3
status: clean
---

# Phase 17: Code Review Report

**Reviewed:** 2026-06-13
**Depth:** standard (Go / controller-runtime)
**Files Reviewed:** 4 production files (test siblings read as context)
**Status:** clean (no Critical/Warning; 3 Info)

## Summary

Reviewed the four Phase 17 production edits adversarially against the strict-failure-profile contract, the label-stamping authorization path, and the four sibling-template analogs documented in `17-PATTERNS.md`. The changes are faithful to their in-tree templates and the authorization-sensitive paths hold up:

- **approve.go (DEBT-03 / Option A):** The guard narrowing is correct. Discovery now runs FIRST (`findAwaitingMilestone → Phase → Plan → Task`), refusal fires only if the discovered target is itself `Failed`, and `findFailedLevel` is consulted ONLY as a fallback when no AwaitingApproval target exists. An unrelated Failed sibling no longer blocks a healthy gate (verified by `TestApproveUnrelatedFailedLevelDoesNotBlockHealthyPhase`), and the project-wide block is not reintroduced. No path lets an approval through that should be blocked: the only writable target is a discovered AwaitingApproval level, and the `--wave` path writes only the named Plan's `approve-wave-N`. No cross-gate bypass.
- **plan_controller.go (DEBT-01 backfill):** Idempotent and orphan-safe. The `plan.Labels[owner.LabelProject] == ""` absent-guard makes the second reconcile a no-op; `resolveProjectName` returns `ErrParentUnresolved` on orphan and the `err == nil && name != ""` guard skips silently, so orphan Plans stay unlabeled rather than mis-scoped. Reuses the existing resolver (no hand-rolled chain). No cross-project label risk — the resolved name comes from the Plan→Phase→Milestone→Project ownership chain, never from LLM input.
- **plan_controller.go (DEBT-04 non-fatal read):** The `envReadOK`/`envReaderPresent` sentinels gate every envelope-dependent downstream path: budget rollup (`isFirstCompletion && envReadOK`), billing-halt backstop (`envReadOK && out.ExitCode != 0`), `ValidationState=Validated` stamp (`if envReadOK`), and the `out.ChildCount` succession gate (`if envReadOK { ... } else if envReaderPresent && countChildTasks()==0`). A transient read error now requeues / defers to children-based succession instead of wedging terminal `Failed`. No path reads `out.*` while `envReadOK==false`.
- **phase_controller.go (DEBT-02 reorder):** The reject short-circuit (`:458`) is now correctly positioned BEFORE `spawnReporterIfNeeded` (`:491`), so a rejected Project prevents a NEW reporter spawn. It does NOT delete in-flight Jobs (park-not-fail via `patchPhaseRejected`, no Job deletion). The reorder skips no setup the reporter spawn depended on — `project`/`projectUID` are derived above the reject block and the early `return` is the intended halt.
- **materialize.go (15-WR-03 *Project stamp):** The type-switch is correct and nil-safe. `StampProjectLabel` no-ops on empty string and overwrites any LLM-authored value, so a child cannot smuggle a cross-project label past the authoritative `parent.GetName()`.

All reviewed files meet quality standards. The items below are advisory only.

## Info

### IN-01: Dead belt-and-suspenders branch in `approveLevelTarget`

**File:** `cmd/tide/approve.go:219-241`
**Issue:** `approveLevelTarget` re-checks `targetPhase == "Failed"` on the object returned by the `findAwaiting*` discovery functions. But those functions only ever return an object whose `Status.Phase == "AwaitingApproval"` (`approve.go:371, 388, 405, 422`), and the object is NOT re-fetched between discovery and this check — it is the same in-memory struct. So `targetPhase` is always `"AwaitingApproval"` here and the `== "Failed"` branch is unreachable in the current call flow. The comment hypothesizes a "transitioned to Failed after the list" race, but no re-Get exists to observe such a transition.
**Fix:** Either drop the dead check (the fallback `findFailedLevel` already covers the no-AwaitingApproval-target case), or make the guard real by re-Getting the target immediately before the write:
```go
// Re-fetch so a Failed transition that landed after discovery is observed.
if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
    return fmt.Errorf("re-get %s/%s: %w", level, obj.GetName(), err)
}
```
Low priority — the current code is safe (it cannot wrongly approve), just inert.

### IN-02: Reject short-circuit skips budget rollup of already-consumed planner spend

**File:** `internal/controller/phase_controller.go:458-460` (and the shared pattern at `plan_controller.go:490-492`, `milestone_controller.go:515-516`)
**Issue:** When a Project is rejected, the completion handler returns at the reject short-circuit BEFORE `budget.RollUpUsage`. The planner Job has already run and consumed provider tokens, but that spend is never rolled into the Project budget on the reject path. This is an established cross-controller tradeoff (plan and milestone behave identically), not a Phase 17 regression — the reorder only moved phase's reject from after-spawn to before-spawn, preserving the existing skip-rollup-on-reject semantics. Flagging for visibility since DEBT-02 touches this ordering.
**Fix:** If unrecorded post-reject spend matters for budget accuracy, roll up usage before the reject return (requires the envelope read to precede the reject check, which conflicts with the "reject FIRST, regardless of envelope availability" doctrine). Likely accept-as-designed; documenting the tradeoff is sufficient.

### IN-03: `buildFailureDetail` first-condition fallback can surface a stale/non-failure condition

**File:** `cmd/tide/approve.go:313-353`
**Issue:** When no `ConditionWaveOrLevelPaused` condition is present, `buildFailureDetail` falls back to `Status.Conditions[0]` — the first condition in slice order, which is not guaranteed to be the failure cause (it could be an old `Ready`/`Initialized` condition). The resulting "(reason: ...: ...)" hint in the error message may mislead the operator about why the level failed. Cosmetic (error-message quality only; no behavioral impact).
**Fix:** Prefer `ConditionFailed` before the bare `Conditions[0]` fallback, or omit the detail when no recognized failure condition is found:
```go
c := meta.FindStatusCondition(conds, tidev1alpha1.ConditionWaveOrLevelPaused)
if c == nil {
    c = meta.FindStatusCondition(conds, tidev1alpha1.ConditionFailed)
}
// drop the Conditions[0] fallback, or keep it only as a last resort
```

---

_Reviewed: 2026-06-13_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
