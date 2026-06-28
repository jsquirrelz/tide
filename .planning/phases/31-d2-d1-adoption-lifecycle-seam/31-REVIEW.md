---
phase: 31-d2-d1-adoption-lifecycle-seam
reviewed: 2026-06-28T00:00:00Z
depth: standard
files_reviewed: 10
files_reviewed_list:
  - api/v1alpha2/shared_types.go
  - api/v1alpha2/milestone_types.go
  - api/v1alpha2/phase_types.go
  - api/v1alpha2/plan_types.go
  - internal/controller/project_controller.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/adoption_lifecycle_test.go
  - internal/controller/child_rollup_idempotency_test.go
findings:
  critical: 0
  warning: 4
  info: 3
  total: 7
status: issues_found
---

# Phase 31: Code Review Report

**Reviewed:** 2026-06-28
**Depth:** standard
**Files Reviewed:** 10
**Status:** issues_found

## Summary

Reviewed the hand-authored Go for D2 (adopted-Project suppression via `ConditionProjectPlannerSuppressed`) and D1 (per-level `*RolledUpUID` budget-rollup idempotency markers). The core mechanisms are sound: the exactly-once accrual logic correctly mirrors the project-level `PlannerRolledUpUID` pattern (stamp marker only on rollup success), the marker is a per-object constant so it is permanently idempotent once stamped, and the suppression short-circuit is correctly positioned before the live `r.List` and `PlannerPool.Acquire` (no slot leak, cold-cache safe). The package builds clean.

The defects found are correctness-at-the-margins and verification-gap issues, not blockers. The most important: a code/comment contradiction on the suppression patch's conflict-retry claim (the patch cannot actually conflict), and the marker-stamp `Status().Patch` operating on a possibly-stale object after `RollUpUsage` re-fetched a *different* object. No security issues, no data-loss risk to the budget tally (the tally itself retries on conflict inside `RollUpUsage`).

## Warnings

### WR-01: Suppression patch comment claims conflict-retry, but the patch can never conflict

**File:** `internal/controller/project_controller.go:1154-1166`
**Issue:** The first-confirmation suppression patch uses a plain `client.MergeFrom(project.DeepCopy())` (no `MergeFromWithOptimisticLock`). A plain MergeFrom does not embed `resourceVersion` in the patch body, so the API server performs a last-write-wins strategic merge that cannot return a Conflict. The inline comment asserts the opposite:

```go
if pErr := r.Status().Patch(ctx, project, patch); pErr != nil {
    // Conflict is retryable; surface as err so controller retries.
    return ctrl.Result{}, pErr
}
```

The behavior is benign for these two fields (Phase + a single condition), but the comment encodes a false invariant. If a concurrent reconcile (or the Step-1 branch-init patch on a re-entrant cycle) writes `Status.Phase` between this object's read and this patch, the suppression patch silently overwrites it with no conflict surfaced — the opposite of what the comment promises. Either drop the misleading comment or use `MergeFromWithOptimisticLock` if conflict-detection is genuinely wanted here.

**Fix:** If optimistic concurrency is desired (matches `RollUpUsage`'s own pattern):
```go
patch := client.MergeFromWithOptions(project.DeepCopy(), client.MergeFromWithOptimisticLock{})
```
Otherwise, correct the comment to state the patch is a server-side merge that will not conflict, and that error paths here are I/O/transport errors only.

### WR-02: Marker `Status().Patch` runs against a possibly-stale object after `RollUpUsage`

**File:** `internal/controller/milestone_controller.go:600-604` (and `phase_controller.go:529-533`, `plan_controller.go:605-609`)
**Issue:** `RollUpUsage` re-fetches and patches the *Project* with optimistic locking, but the level object (`ms`/`ph`/`plan`) is never re-fetched. The marker stamp then does `markerPatch := client.MergeFrom(ms.DeepCopy()); ms.Status.MilestoneRolledUpUID = jobName; r.Status().Patch(...)` against the level object captured at the top of Reconcile. In the live controller this is currently safe because controller-runtime serializes reconciles per object key and the marker patch is the first status write in the completion path, so `ms.ResourceVersion` is current. But the safety is incidental, not enforced: any future code that issues a status patch on `ms` earlier in `handleJobCompletion` (e.g. a new condition write before the rollup block) would leave `ms` stale here, and the plain `MergeFrom` would either clobber that earlier write or fail silently (non-fatal, logged). The marker write is the single durable idempotency guard for exactly-once accrual — it deserves the same optimistic-lock + re-fetch-on-conflict treatment `RollUpUsage` itself uses, rather than a best-effort non-fatal patch on an assumed-fresh object.

**Fix:** Wrap the marker stamp in `retry.RetryOnConflict` with a re-fetch, mirroring `budget.RollUpUsage`:
```go
if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
    latest := &tideprojectv1alpha2.Milestone{}
    if err := r.Get(ctx, client.ObjectKeyFromObject(ms), latest); err != nil { return err }
    if latest.Status.MilestoneRolledUpUID == milestoneJobName { return nil }
    patch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
    latest.Status.MilestoneRolledUpUID = milestoneJobName
    return r.Status().Patch(ctx, latest, patch)
}); err != nil {
    logger.Error(err, "patch MilestoneRolledUpUID failed (non-fatal)", "milestone", ms.Name)
}
```

### WR-03: Non-fatal marker-patch failure can drop the exactly-once guarantee across a crash

**File:** `internal/controller/milestone_controller.go:595-605` (and phase/plan equivalents)
**Issue:** The ordering is correct (`RollUpUsage` first, then stamp the marker), and a marker-patch failure is logged and the reconcile continues — the comment correctly notes "leaving the marker unset on error lets the next reconcile retry." However, the retry only re-rolls-up if `isFirstCompletion` is still true. `isFirstCompletion` is `true` only while the reporter Job is absent (newly spawned or `ReporterImage==""`). In the normal (non-empty `ReporterImage`) path, the reporter Job was just spawned in `spawnReporterIfNeeded` immediately above. On the *next* reconcile the reporter Job exists, so `isFirstCompletion=false`, so the rollup block is skipped entirely — and because the marker patch failed, `MilestoneRolledUpUID` is still empty. Net effect: the rollup happened once (cost counted) but the marker never persisted. That is benign here (cost was counted once, and the guard's purpose is only to prevent a *re-count* after TTL-GC). But the window where the guard is "unset yet already rolled up" means if the reporter Job *does* TTL-GC and a later reconcile sees `isFirstCompletion=true` again, the unset marker will permit a second rollup — exactly the double-count this phase set out to prevent. The probability is low (marker patch must fail AND reporter must GC AND a reconcile must re-fire), but the design intent is "guaranteed exactly-once," and this path is not guaranteed.

**Fix:** Adopting WR-02's `RetryOnConflict` re-fetch loop materially shrinks this window. For full closure, make the marker stamp blocking on its retry budget being exhausted (return the error to requeue the reconcile) rather than swallowing it, so the marker is durably set before the reporter Job can GC.

### WR-04: D-07 "single patch" invariant is asserted in comments but never verified by a test

**File:** `internal/controller/adoption_lifecycle_test.go:232-261`
**Issue:** The code comments (project_controller.go:1149-1153) and the test's package doc (adoption_lifecycle_test.go:20-21) repeatedly claim the Phase=Running advance and the suppression condition are written in "ONE Status().Patch ... never two sequential patches" (D-07). The ADOPT-01 test only asserts the end state (Phase==Running AND condition present AND zero Jobs) — it does not assert the single-patch invariant. A regression that split this into two sequential patches (the exact anti-pattern D-07 warns against, which would transiently expose Running-without-suppression and risk a re-dispatch race) would pass every existing assertion. Since the single-patch atomicity is the load-bearing claim of D-02/D-07, it should be covered.

**Fix:** Add an assertion that, after the first reconcile, the Project's `Status.Conditions` contains the suppression condition with the *same* `observedGeneration`/`resourceVersion` transition as the Phase change, or instrument the test reconciler's client with a patch counter and assert exactly one status patch was issued during the adoption advance. At minimum, assert that there is never an observable Running-with-no-suppression-condition intermediate state.

## Info

### IN-01: Child-level rollups lack the `ImportSource` skip that the project level has — confirm this is intentional

**File:** `internal/controller/milestone_controller.go:593` / `phase_controller.go:524` / `plan_controller.go:599`
**Issue:** The project-level rollup explicitly skips entirely when `project.Spec.ImportSource != nil` (project_controller.go:1348, "the prior run already counted the planning cost; rolling up here would double-count"). The three child-level rollups have no such guard. This is correct by design — child planners are *held* until `ImportComplete=True` and, once unheld, represent genuinely new post-adoption planning work (new UIDs → new marker values), not imported cost — but the asymmetry is non-obvious and undocumented at the child sites. A future reader may "fix" the inconsistency and wrongly suppress legitimate child-planner accrual on adopted projects.

**Fix:** Add a one-line comment at each child rollup site noting that, unlike the project planner, child planners on adopted projects do real new work post-import and therefore must accrue (no `ImportSource` skip).

### IN-02: `LastTransitionTime` set manually on a `meta.SetStatusCondition` call is redundant

**File:** `internal/controller/project_controller.go:1161`
**Issue:** `meta.SetStatusCondition` derives `LastTransitionTime` itself (it preserves the prior timestamp when status is unchanged and stamps `metav1.Now()` on a true transition). Passing `LastTransitionTime: metav1.Now()` in the struct is redundant and, on a no-op re-set, the manually-supplied value is ignored anyway. Harmless but inconsistent with the helper's contract; the gate-policy condition writes in the same file pass it too, so this matches local convention — flagged only for awareness.

**Fix:** Optional — drop the explicit `LastTransitionTime` field; `meta.SetStatusCondition` manages it.

### IN-03: Marker `jobName` string is reconstructed in three places — extract a helper

**File:** `internal/controller/milestone_controller.go:592`, `phase_controller.go:523`, `plan_controller.go:598` (and the project mirror at project_controller.go:1345)
**Issue:** `fmt.Sprintf("tide-<level>-%s-1", obj.UID)` is duplicated at four sites and also at the dispatch sites (milestone_controller.go:284, phase_controller.go:278, plan_controller.go:298). The `-1` suffix and `tide-<level>-` prefix are a construction-site invariant; if the planner Job naming ever changes, the marker comparison silently diverges from the real Job name at four scattered call sites. A shared `plannerJobName(kind string, uid types.UID) string` helper would keep the marker and the dispatch name provably in lockstep.

**Fix:** Extract a single `plannerJobName(level, uid)` helper used by both the dispatch and rollup-marker sites in each controller.

---

_Reviewed: 2026-06-28_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
