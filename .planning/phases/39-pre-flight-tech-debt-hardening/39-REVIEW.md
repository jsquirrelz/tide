---
phase: 39-pre-flight-tech-debt-hardening
reviewed: 2026-06-29T00:00:00Z
depth: standard
files_reviewed: 6
files_reviewed_list:
  - charts/tide/templates/configmap.yaml
  - hack/helm/augment-tide-chart.sh
  - internal/config/config_default_test.go
  - internal/controller/project_controller.go
  - internal/controller/project_rollup_idempotency_test.go
  - test/integration/kind/configmap_planner_concurrency_test.go
findings:
  critical: 0
  warning: 2
  info: 3
  total: 5
status: issues_found
---

# Phase 39: Code Review Report

**Reviewed:** 2026-06-29
**Depth:** standard
**Files Reviewed:** 6
**Status:** issues_found

## Summary

Two changes were reviewed: PREFLIGHT-01 (chart ConfigMap `plannerConcurrency` Helm fallback `16` → `4`, applied in both the generated `charts/tide/templates/configmap.yaml` and its source `hack/helm/augment-tide-chart.sh`) and PREFLIGHT-02 (project-level `PlannerRolledUpUID` rollup hardened to `retry.RetryOnConflict` + `MergeFromWithOptimisticLock`).

Both changes are correct and faithfully implement their stated intent. I verified the production behavior directly rather than trusting the tests:

- `helm template tide charts/tide` renders `plannerConcurrency: 4` (observed).
- `go test ./internal/config -run TestDefaultPlannerConcurrency` passes (observed).
- The new project-level rollup block is a line-for-line mirror of the established milestone/phase/plan WR-02/WR-03 pattern (`milestone_controller.go:610-635`), including the re-fetch, the idempotent short-circuit, and the error-returning-on-exhaustion semantics.

No BLOCKER-class correctness, security, or data-loss defects were found. The findings below are quality/robustness items and one observation that the headline PREFLIGHT-01 fix is defense-in-depth rather than a live-bug fix (the shipped `values.yaml` already pins `plannerConcurrency: 4`, so the stale `| default 16` fallback was never reached on a default install — it was only reachable for an operator override that explicitly removed the key).

## Warnings

### WR-01: Stale in-memory `PlannerRolledUpUID` after the retry block can re-fire `RollUpUsage` if the rollup block is ever re-entered in the same reconcile pass

**File:** `internal/controller/project_controller.go:1382-1395`
**Issue:** The `RetryOnConflict` closure stamps the marker on a freshly-fetched `latest` object and patches it, but never writes the new value back to the in-memory `project` passed into `handleProjectJobCompletion`. The double-count guard at line 1371 reads `project.Status.Budget.PlannerRolledUpUID` (the stale in-memory copy, still empty). Within a single call this is harmless because the rollup block runs exactly once and the function returns shortly after. But the in-memory `project` is the same pointer the caller continues to use after `handleProjectJobCompletion` returns — any future refactor that re-enters the rollup path on that same in-memory object (without a fresh `Get`) would see an empty marker and roll up a second time. `budget.RollUpUsage` already writes its result back (`project.Status.Budget = latest.Status.Budget`), so the marker is the only sub-field left stale, which is an easy inconsistency to miss. This is consistent-by-design with the milestone/phase/plan levels (they have the identical gap), so it is a WARNING, not a BLOCKER — but the inconsistency is real across all four sites.
**Fix:** Mirror `RollUpUsage`'s write-back by stamping the in-memory object once the patch succeeds:
```go
markerPatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
latest.Status.Budget.PlannerRolledUpUID = plannerJobName
if err := r.Status().Patch(ctx, latest, markerPatch); err != nil {
    return err
}
project.Status.Budget.PlannerRolledUpUID = plannerJobName // keep in-memory copy in sync
return nil
```

### WR-02: Narrow double-count window remains when `RollUpUsage` succeeds but the marker patch exhausts its retry budget

**File:** `internal/controller/project_controller.go:1372-1395`
**Issue:** The ordering is: `RollUpUsage` (increments `CostSpentCents`/`TokensSpent`) runs first and succeeds; then the marker patch runs under `RetryOnConflict`. If the marker patch fails for a *non-conflict* reason and exhausts `DefaultRetry` (e.g. a transient API-server 500, a `Forbidden`, or context deadline), the code returns the error and requeues — but the budget was already incremented and the marker was never durably set, so the next reconcile re-enters `RollUpUsage` and double-counts. The change closes the *conflict* window (the common case) but not the *non-conflict-exhaustion* window. The comment at line 1379-1381 ("the marker must be durably set before … rollup") asserts a guarantee the code does not actually provide on retry-budget exhaustion. This matches the milestone/phase/plan levels, so it is a pre-existing accepted tradeoff rather than a regression — flagging for coverage since the comment overstates the guarantee.
**Fix:** No clean fix without making rollup+marker a single atomic patch (the two are distinct status writes by construction). At minimum, soften the comment to state the *conflict* window is closed and the non-conflict-exhaustion path may re-roll-up on requeue, so a future reader does not assume exactly-once under all failure modes. If exactly-once under API failure is required, fold the marker into the same `RollUpUsage` patch (stamp `PlannerRolledUpUID` inside the `RollUpUsage` closure) so cost increment and marker land atomically.

## Info

### IN-01: PREFLIGHT-01 is defense-in-depth, not a live-bug fix — `values.yaml` already pinned `plannerConcurrency: 4`

**File:** `charts/tide/templates/configmap.yaml:22`, `hack/helm/augment-tide-chart.sh:90`
**Issue:** Both `charts/tide/values.yaml:89` and `hack/helm/tide-values.yaml:89` already set `plannerConcurrency: 4`, so on a default `helm install` the `| default 16` fallback was unreachable and the rendered ConfigMap already produced `4`. The stale `16` was only reachable for an operator override that explicitly *removed* the key (or set it to null). The fix is still correct and worthwhile (it removes a footgun for override authors), but the phase framing ("a fresh default deploy … cannot dispatch a 16-wide burst and OOM the node") is slightly stronger than reality — a fresh default deploy was already safe.
**Fix:** None required; consider noting in the summary/SUMMARY.md that this hardens the override path, not the default path, so the risk model is accurately recorded.

### IN-02: `extractConfigMapSection` matches the first `kind: ConfigMap` and can excerpt the wrong document in failure messages

**File:** `test/integration/kind/configmap_planner_concurrency_test.go:68-79`
**Issue:** The helper does `strings.Index(rendered, "kind: ConfigMap")` and slices to the next `\n---`. If the chart ever renders more than one ConfigMap before `tide-config`, the failure excerpt would show the wrong document. This is cosmetic — it only affects the `t.Errorf` diagnostic, not pass/fail — but a misleading excerpt slows debugging. The assertion itself (`strings.Contains(outStr, "plannerConcurrency: 4")`) scans the whole render and is robust.
**Fix:** Anchor the section search on the `tide-config` ConfigMap specifically, e.g. find `name: tide-config` and walk backward to the preceding `kind: ConfigMap`, or scan forward from `kind: ConfigMap` only when the following lines contain `name: tide-config`.

### IN-03: PREFLIGHT-01 absence check scans the entire render, which is fine today but could mask a real regression if a future field renders `plannerConcurrency: 16` outside the ConfigMap

**File:** `test/integration/kind/configmap_planner_concurrency_test.go:59-63`
**Issue:** The negative assertion `!strings.Contains(outStr, "plannerConcurrency: 16")` runs against the full multi-document render rather than the ConfigMap section. Today no other template emits that literal, so the test is correct. The positive check has the same whole-render scope. The risk is purely theoretical (a future template would have to emit the exact `plannerConcurrency: 16` string elsewhere), but scoping both assertions to the `tide-config` ConfigMap section would make the test express its real intent ("the ConfigMap value is 4, not 16") and survive unrelated chart growth.
**Fix:** Run both `Contains` checks against `extractConfigMapSection(outStr)` (once IN-02 is fixed to target `tide-config`) rather than the full `outStr`.

---

_Reviewed: 2026-06-29_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
