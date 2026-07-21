---
phase: 52-per-level-looppolicy-parameterization
reviewed: 2026-07-20T00:00:00Z
depth: standard
files_reviewed: 20
files_reviewed_list:
  - api/v1alpha3/loop_types.go
  - api/v1alpha3/plan_types.go
  - api/v1alpha3/project_types.go
  - api/v1alpha3/shared_types.go
  - cmd/manager/main.go
  - cmd/tide-push/main.go
  - cmd/tide/approve.go
  - internal/controller/dispatch_helpers.go
  - internal/controller/level_status.go
  - internal/controller/level_verify.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/project_controller.go
  - internal/controller/task_controller.go
  - internal/dispatch/podjob/jobspec.go
  - internal/dispatch/podjob/names.go
  - internal/subagent/common/prompt_templates.go
  - pkg/dispatch/envelope.go
  - pkg/git/worktree.go
findings:
  critical: 0
  warning: 4
  info: 3
  total: 7
status: issues_found
---

# Phase 52: Code Review Report

**Reviewed:** 2026-07-20T00:00:00Z
**Depth:** standard
**Files Reviewed:** 20
**Status:** issues_found

## Summary

Phase 52 generalizes the Phase-51 Task verification loop to every level via one `ResolveLoopPolicy` resolver keyed on loop level. The high-risk machinery holds up well under adversarial tracing:

- **Fail-closed verdict handling is sound.** `ClassifyVerdict` collapses empty/malformed/missing-verdict to `BLOCKED`; both `handleLevelVerifierCompletion` (level_verify.go) and `handlePlanVerifierCompletion` (plan_controller.go) route unreadable envelopes, nil verdicts, marshal failures, `REPAIRABLE`, `BLOCKED`, and deterministic-gate-dominated `APPROVED` through the exhaust/repair paths — never to Succeeded. The role-aware `readVerifierEnvelope` (ReadVerifierOut) is reused, so the Phase-51 verdict-relay bug is not reintroduced.
- **The `exhaustVerifyLoop` requireApproval/escalate split is correctly parked and non-bypassable.** A parked level is held at Step 1a (AwaitingApproval early-return); the post-approval convergence guard (`levelVerifyConverged`) only fires once the operator has approved (Phase→Running via `consumeApproveAndResume`), so an exhausted-then-approved level closes without resurrecting a verifier/executor.
- **The delete-then-recreate re-plan (`dispatchPlanRepair`) has a correct deletion barrier.** The `len(taskList.Items) > 0` early-return in `reconcilePlannerDispatch` blocks the fresh planner until the deleted Tasks are fully gone; the re-plan attempt uses a distinct `LoopStatus.Iteration`-derived Job name (`tide-plan-<uid>-2`), so no name collision; terminating Tasks carry a DeletionTimestamp and never dispatch.
- **Severity-weighted stall detection is off-by-one-clean.** `Iteration >= MaxIterations` with `Iteration` starting at 0 yields exactly `MaxIterations` re-plans; the stall check (`newScore >= prevScore`) requires a strict decrease and is nil-safe on the first verdict.
- **Concurrency/budget rails are shared correctly.** `ReservationStore.Reserve` overwrites by UID (idempotent on dispatch retry — no double-reservation); the manager wires the same store + verifier image into `PlannerReconcilerDeps` (cmd/manager/main.go); cap-before-reserve ordering holds at every new dispatch site.
- **The podjob verifier generalization is nil-safe for non-Task parents** (`JobKindVerifier` reads `ParentObj` with a nil guard), and the worktree-checkout init container carries no credential surface.

The defects below are correctness/robustness degradations, not fail-closed breaches. The most significant is a boundary-push asymmetry (WR-01): a contract-bearing Phase/Milestone that passes verification skips the boundary push the equivalent non-contract path always performs.

## Warnings

### WR-01: Contract-bearing Phase/Milestone skip the boundary push on the clean verify-APPROVED path

**File:** `internal/controller/phase_controller.go:255`, `internal/controller/milestone_controller.go:263`
**Confidence:** high (code asymmetry is directly provable)
**Issue:** In `handleJobCompletion`, the non-contract success path calls `r.maybeTriggerBoundaryPush(...)` *before* `patch{Level}Succeeded` (phase_controller.go:833, milestone_controller.go:903/934). But when a verification contract is present, `handleJobCompletion` dispatches the verifier and returns early (verify-before-push), and the verifier's `APPROVED` verdict is then consumed on a *later* reconcile through the **Verifying route** in `reconcilePlannerDispatch`:

```go
if ph.Status.Phase == tideprojectv1alpha3.LevelPhaseVerifying {
    project := r.resolveProject(ctx, ph)
    if handled, res, vErr := maybeRunLevelVerify(...); handled {
        return res, vErr
    }
    return r.patchPhaseSucceeded(ctx, ph) // <-- no maybeTriggerBoundaryPush
}
```

`maybeRunLevelVerify` returns `handled=false` on `APPROVED`, so the level goes straight to `patch{Level}Succeeded` and the boundary push is never issued at that level's boundary. This is the *common* clean-pass case. (Notably, the requireApproval→human-approve path does NOT have this gap: it resumes to Running and re-enters `handleJobCompletion`, which does call `maybeTriggerBoundaryPush` — so the behavior is inconsistent between "clean APPROVED" and "approved-after-exhaustion.")

The stated intent ("verify BEFORE the boundary push — an unverified outcome must not publish") implies the push should fire *after* verification passes; instead it never fires at that boundary for a contract-bearing level. Impact is bounded (not data loss): the cumulative parent-level and project-level boundary pushes still land the run branch + cumulative artifact map, so work reaches the remote at project completion. But intermediate publish/observability/bounded-recovery at the Phase/Milestone boundary is silently lost only for contract-bearing levels.

**Fix:** mirror the non-contract path in the Verifying route — call the boundary push before succeeding:

```go
if ph.Status.Phase == tideprojectv1alpha3.LevelPhaseVerifying {
    project := r.resolveProject(ctx, ph)
    if handled, res, vErr := maybeRunLevelVerify(...); handled {
        return res, vErr
    }
    if err := r.maybeTriggerBoundaryPush(ctx, ph, project); err != nil {
        if errors.Is(err, errGitWriterBusy) {
            return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
        }
        return ctrl.Result{}, err
    }
    return r.patchPhaseSucceeded(ctx, ph)
}
```

Apply the equivalent change at milestone_controller.go:263. (The Plan level is unaffected — `markPlanVerifiedApproved` clears Phase to "" and the wave-materialization path owns the plan boundary push; the Project level is unaffected — its push fires from `reconcilePhase3Lifecycle` keyed on `PhaseComplete`, not inline.)

### WR-02: `AddReadOnlyWorktree` idempotency returns a stale checkout when the run branch advanced

**File:** `pkg/git/worktree.go:123-127`
**Confidence:** medium
**Issue:** The idempotency pre-check returns the existing worktree dir whenever `git -C <dir> rev-parse --git-dir` succeeds:

```go
if err := exec.Command("git", "-C", worktreeDir, "rev-parse", "--git-dir").Run(); err == nil {
    return worktreeDir, nil
}
```

It never verifies the existing detached checkout points at the *current* `runBranch` tip. Because worktree dirs live on the shared PVC and are keyed by the level UID, a plan-check re-plan (attempt 2+ reuses the same `plan.UID`) or any re-verify of a level whose run branch has since advanced would reuse a checkout pinned to the earlier tip, silently verifying stale content. In the current flows this is benign (plan-check runs pre-execution, so the run branch does not move between a plan's re-plan attempts, and phase/milestone/project verify exactly once via `MaxIterations:0`), but it is a latent correctness trap the moment a level re-verifies at a moved tip.
**Fix:** when the worktree already exists, reset it to the requested tip before returning (e.g. `git -C <worktreeDir> checkout --detach <runBranch>` / `git -C <repoPath> worktree repair`), or fail-closed by verifying `rev-parse HEAD` matches the resolved `runBranch` tip and re-adding otherwise.

### WR-03: Re-plan window runs wave materialization over terminating child Tasks

**File:** `internal/controller/plan_controller.go:1727-1730` (dispatchPlanRepair), `internal/controller/plan_controller.go:2180` (reconcileWaveMaterialization gate)
**Confidence:** medium
**Issue:** `dispatchPlanRepair` deletes the rejected attempt's child Tasks and patches `Status.Phase = ""` but leaves `Status.ValidationState == "Validated"` untouched. On the next reconcile, while the deleted Tasks are still terminating (finalizer pending, `len(taskList.Items) > 0`), `reconcilePlannerDispatch` returns `dispatched=false` and control falls into `reconcileWaveMaterialization`, whose Step-1 gate (`ValidationState == "Validated"`) still passes. It then derives waves and materializes Wave CRs over Tasks that are about to vanish. This is transient churn (the terminating Tasks are not Succeeded, so no premature succession or executor dispatch occurs, and the state self-heals once deletion completes), but it produces spurious Wave-CR create/update traffic and briefly desynchronizes the wave view from the true (empty, re-planning) Task set.
**Fix:** in `dispatchPlanRepair`, reset `plan.Status.ValidationState = ""` (or `"Pending"`) alongside the `Phase = ""` patch so wave materialization is disarmed until the fresh planner re-stamps `Validated`; alternatively, gate `reconcileWaveMaterialization`'s wave-derivation on the absence of any Task carrying a DeletionTimestamp.

### WR-04: `LoopPolicy.Autonomy` is never consulted by the resolver or the exhaustion branch

**File:** `internal/controller/dispatch_helpers.go:478-483` (ResolveLoopPolicy), `internal/controller/level_status.go:206` (exhaustVerifyLoop)
**Confidence:** high (behavioral, low impact)
**Issue:** `ResolveLoopPolicy` constructs the returned `LoopPolicy` with `Level`, `MaxIterations`, `EscalationPolicy`, and `EvaluatorRef` — but never sets `Autonomy`, and `exhaustVerifyLoop` branches solely on `EscalationPolicy`. The `AutonomyLevel` field (`autonomous`/`supervised`, defined and documented in loop_types.go as "how much of the bounded exit/escalation path runs without a human") is therefore dead on this dispatch surface: an operator authoring `autonomy: autonomous` on a level's verification contract gets no behavioral effect — the escalate/requireApproval decision is driven exclusively by the hardcoded per-level `EscalationPolicy` defaults. This is a config field that silently no-ops.
**Fix:** either resolve `Autonomy` into the returned `LoopPolicy` and consume it (e.g. `autonomous` overrides `requireApproval`→`escalate`, or short-circuits the human gate), or drop the field until a consumer exists (per the D-06 "never ship a speculative superset ahead of a real consumer" rule the same file cites). At minimum document that Autonomy is not yet wired so operators don't rely on it.

## Info

### IN-01: D-03 plan-check hold relies on wave-gating during the Running→Verifying window

**File:** `internal/controller/dispatch_helpers.go:754-761` (checkParentApproval), `internal/controller/plan_controller.go:2206` (Verifying transition)
**Confidence:** low
**Issue:** `checkParentApproval` holds child Task dispatch only when the parent Plan's Phase is `Verifying` or `AwaitingApproval`. When the reporter Job materializes child Tasks, they exist momentarily with `Plan.Status.Phase == Running` before the reconcile transitions the Plan to `Verifying` (plan_controller.go:2206, which returns before wave derivation). A Task's own reconcile firing in that window sees `held=false` from `checkParentApproval`. Premature dispatch is prevented only because wave CRs (which the Task needs for dispatch) are not created until after the Verifying transition. The hold is effective given current wave-gating, but it is a structural rather than an explicit guard — worth a targeted test asserting no executor Job is created for a contract-bearing Plan while it is Running-with-materialized-children.
**Fix (optional hardening):** have `checkParentApproval` also treat `Plan.Status.Phase == Running` with an active-but-unresolved plan-check `LoopStatus` as held, so the hold does not depend on wave-CR timing.

### IN-02: `truncateReplanString` cuts on a byte boundary, not a rune boundary

**File:** `internal/controller/plan_controller.go:1532-1537`
**Confidence:** high (low impact)
**Issue:** `truncateReplanString` does `s[:n]`, which can split a multi-byte UTF-8 rune mid-sequence. The resulting bytes are later `json.Marshal`ed (which substitutes U+FFFD, so the stored annotation stays valid UTF-8 and the apiserver accepts it), and the text is diagnostic-only and never re-parsed — so this is not a correctness bug, but the truncation can emit a replacement character at the boundary. The doc comment already acknowledges this. No action required unless clean truncation is desired.
**Fix:** if clean boundaries matter, truncate on a rune boundary (walk back to the last valid rune start before `n`).

### IN-03: `handleLevelVerifierCompletion` swallows a DeepCopyObject cast failure into a silent no-op

**File:** `internal/controller/level_verify.go:488-490`
**Confidence:** high (defensive, effectively unreachable)
**Issue:** On the APPROVED branch, `base, ok := target.Obj.DeepCopyObject().(client.Object); if !ok { return true, ctrl.Result{}, nil }` returns `handled=true` with no error and no requeue if the cast ever fails — which would leave the level wedged in Verifying with no LoopStatus patched and no succession. `DeepCopyObject` on any real `client.Object` cannot fail this cast, so this is defensive-only, but the failure mode (stuck, no error surfaced) is worse than a requeue. `handleLevelVerifierCompletion` at level_verify.go:488 and the sibling cast in `patchLevelStatus`/`consumeApproveAndResume` share this shape.
**Fix:** return an error (or a requeue) instead of a silent handled-true no-op so the condition is at least observable and retried.

---

_Reviewed: 2026-07-20T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
