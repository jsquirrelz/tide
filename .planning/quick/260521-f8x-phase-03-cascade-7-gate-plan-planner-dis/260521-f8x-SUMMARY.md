---
id: 260521-f8x-phase-03-cascade-7-gate-plan-planner-dis
title: "Phase 03 Cascade 7: gate Plan-planner dispatch on resolveProjectForPlan != nil"
type: quick
status: complete
wave: 1
created: 2026-05-21
completed: 2026-05-21
phase: quick
plan: "260521-f8x"
tags: [cascade-7, plan-planner, nil-project-guard, dispatch, credproxy, phase-04.1-12-followup-1]
related:
  debug: .planning/debug/chaos-resume-cascade-10.md
  predecessor: .planning/quick/260521-eoz-phase-03-cascade-10-filter-pillar-4-list/260521-eoz-SUMMARY.md
  phase_summary: .planning/phases/04.1-pre-v1-audit-fixes-cross-phase-uat-closeout/04.1-12-SUMMARY.md
dependency_graph:
  requires: [resolveProjectForPlan, podjob.BuildJobSpec]
  provides: [cascade-7-guard, plan-planner-dispatch-gate]
  affects: [internal/controller/plan_controller.go, internal/controller/plan_controller_test.go, internal/controller/plan_planner_test.go]
key-files:
  modified:
    - internal/controller/plan_controller.go
    - internal/controller/plan_controller_test.go
    - internal/controller/plan_planner_test.go
  created: []
commits:
  - hash: 88356ad
    message: "fix(controller): gate Plan-planner dispatch on resolveProjectForPlan != nil (cascade-7)"
    files: [internal/controller/plan_controller.go]
  - hash: 6212147
    message: "test(controller): cover Plan-planner nil-Project guard (cascade-7)"
    files: [internal/controller/plan_controller_test.go, internal/controller/plan_planner_test.go]
requirements_completed: [04.1-12-FOLLOWUP-1]
metrics:
  tasks: 2
  files_modified: 3
  lines_added: 132
  duration_minutes: 8
  envtest_runtime_seconds: 50
---

# Quick Task 260521-f8x: Cascade-7 nil-Project Guard Summary

Gate the Plan-planner Job dispatch in `reconcilePlannerDispatch` on `resolveProjectForPlan(ctx, plan) != nil` — closes Phase 04.1 Plan 12 Outstanding Follow-up #1 ("Cascade 7") at the source level. The two-arm guard prevents the credproxy-CrashLoopBackOff path when the Plan → Phase → Milestone → Project chain has not yet resolved in the informer cache.

## One-Liner

Two-arm guard inserted after `project := r.resolveProjectForPlan(ctx, plan)` (plan_controller.go:244): empty `Spec.PhaseRef` → permanent refuse (`ctrl.Result{}, false, nil`), non-empty PhaseRef with nil Project → transient requeue (`ctrl.Result{RequeueAfter: 1*time.Second}, false, nil`). Both arms return `handled=false` so dispatch is not falsely recorded as committed. Envtest covers the transient cache-miss arm via `phaseRef = "phase-cascade7-missing"` (no Phase created) and the permanent empty-PhaseRef arm via a stack-constructed Plan.

## What Landed

### Task 1 — Production guard (commit `88356ad`)

Inserted a 21-line guard block between lines 244 (`project := r.resolveProjectForPlan(ctx, plan)`) and the existing Phase 04.1 P1.2 dispatch comment. The block:

1. Tests `if project == nil { ... }` immediately after the Project resolution.
2. Splits on `plan.Spec.PhaseRef == ""`:
   - **Empty (configuration error):** `logger.Info("refusing plan-planner dispatch: plan.spec.phaseRef is empty", "cascade", 7)` → returns `ctrl.Result{}, false, nil` (no requeue).
   - **Non-empty (transient cache-miss):** `logger.V(1).Info("deferring plan-planner dispatch: project chain not yet resolvable, requeueing", "cascade", 7)` → returns `ctrl.Result{RequeueAfter: 1*time.Second}, false, nil`.
3. The `handled=false` sentinel in both arms ensures the caller (`PlanReconciler.Reconcile` line 162-165) does NOT treat the branch as a committed dispatch and falls through to wave materialization.
4. The existing flow from line 246 onward (Phase 04.1 P1.2 BuildPlannerEnvelope → Sign → BuildJobSpec → Create → status patch) is byte-identical when `project != nil`.

**Diff stat:** `internal/controller/plan_controller.go | 21 +++++++++++++++++++++`. No new imports — `logf` (line 37), `time` (line 23), `ctrl` (line 31) all already in scope.

### Task 2 — Envtest coverage (commit `6212147`)

Two test changes in a single test-only commit:

**A. New `Describe` block in `plan_controller_test.go`** (+78 lines): sibling to `PlanReconciler Wave materialization` at line 200, label `"envtest", "phase2"`. Two nested `Describe`s:

1. **`TestPlanReconciler_PlannerDispatch_RequeuesWhenPhaseNotInCache`** (REQUIRED — transient arm): creates a Plan via `makePlan(planName, "phase-cascade7-missing", "")` with NO Phase created; calls `r.reconcilePlannerDispatch(ctx, &plan)` directly; asserts `err == nil`, `handled == false`, `result.RequeueAfter == 1*time.Second`, AND that `tide-plan-<plan-uid>-1` does NOT exist (via `apierrors.IsNotFound`).

2. **`TestPlanReconciler_PlannerDispatch_PermanentOnEmptyPhaseRef`** (BONUS — permanent arm): stack-constructs a `tideprojectv1alpha1.Plan{}` with empty `Spec.PhaseRef` (no round-trip through `k8sClient.Create`); calls `r.reconcilePlannerDispatch(ctx, &plan)` directly; asserts `err == nil`, `handled == false`, `result.Requeue == false`, `result.RequeueAfter == time.Duration(0)`.

The optional second test (originally flagged as "include if scaffolding cost is low") was cheap — no `k8sClient.Create` round-trip needed, just direct method call on a stack-allocated Plan. Both arms covered.

Imports added: `batchv1 "k8s.io/api/batch/v1"`, `apierrors "k8s.io/apimachinery/pkg/api/errors"` (both already in the module graph via `plan_controller.go`).

**B. Fixed `plan_planner_test.go` Test 5** (+33 lines): see "Deviations" below.

**Diff stats:**
```
internal/controller/plan_controller_test.go | 78 +++++++++++++++++++++++++++++
internal/controller/plan_planner_test.go    | 33 ++++++++++++
```

## Deviations from Plan

### [Rule 1 — Bug] Fixed `plan_planner_test.go` Test 5 — broken by Task 1's new contract

**Found during:** Task 2 verification (initial `go test -run 'PlanReconciler'` run pre-Task-2-fix).

**Issue:** `plan_planner_test.go` lines 49-83 (`Test 5: dispatches planner Job when Plan has no Tasks yet`) created a Plan with `phaseRef = "phase-planner-dispatch"` but never created the prerequisite Phase/Milestone/Project. Pre-cascade-7, `resolveProjectForPlan` returned nil and `BuildJobSpec` proceeded with `opts.Project == nil`, dropping the provider Secret but still creating a Job — the test asserted Job existence and passed by accident. With Task 1's guard, that path now requeues without creating a Job, and Test 5 timed out at `chaos_resume_test.go:82` waiting for the Job.

**Confirmation:** initial post-Task-1 envtest run showed `89 Passed | 1 Failed` with the failure at `plan_planner_test.go:82`.

**Fix:** in `Describe("PlanReconciler — planner dispatch (Phase 3)")`:
1. Added two new constants: `milestoneRefName = "milestone-planner-dispatch"`, `projectRefName = "project-planner-dispatch"`.
2. Modified `AfterEach` to clean up the Phase, Milestone, and Project (clearing finalizers and deleting in order).
3. Modified Test 5's `It` body to create the Project → Milestone → Phase hierarchy BEFORE creating the Plan, using existing helpers (`makeProjectForTask`, raw Create for Milestone/Phase, `waitForCacheSync`).

This is a Rule 1 deviation (existing test exercised now-broken behavior under the new contract). Scope is bounded: only the test setup was extended; the assertion (Job exists with `Status.Phase=Running`) is unchanged. Test 6 (line 85) was unaffected — it uses `makePlan + alphaThroughThetaFixture` which creates Tasks first, so the planner-dispatch path short-circuits before reaching the guard.

**Files modified:** `internal/controller/plan_planner_test.go` (+33 lines).
**Bundled into:** Task 2 commit `6212147` (the deviation is itself test-only and directly caused by Task 1's contract; the production commit `88356ad` stays a single-file commit per the plan).

### [Process — Cwd-drift] Initial `cd /Users/justinsearles/Projects/tide && grep ...` resolved to main repo, not worktree

**Found during:** Task 1 verify step.

**Issue:** the plan's `<automated>` verify command used `cd /Users/justinsearles/Projects/tide && go vet ... && grep ...`. The worktree is at `/Users/justinsearles/Projects/tide/.claude/worktrees/agent-a263395e13b52ca27`; the literal `cd /Users/justinsearles/Projects/tide` moves OUT of the worktree into the main repo, where `plan_controller.go` did NOT yet have my edit. Grep returned 0, falsely suggesting Task 1 didn't land. The Read tool and `git diff` (which use `.git/worktrees/<id>` for repository operations) correctly saw the edit; only Bash commands that `cd` to the literal main repo path were fooled.

**Fix:** all subsequent verification commands prefixed with `WT=/Users/justinsearles/Projects/tide/.claude/worktrees/agent-a263395e13b52ca27 && cd "$WT" && ...` (or grep with an absolute path under `$WT`). Both Task 1 and Task 2 final verifies pass under that protocol.

This is not a code bug — it's a verification-script bug in the plan's `<automated>` block. Future quick tasks executed inside a worktree should use a worktree-relative `cd` (or no `cd` at all) in their verify commands.

## Auto-fix attempt budget

1 / 3 used (the plan_planner_test.go fix). Budget intact.

## Mechanism Reminder

`BuildJobSpec` (`internal/dispatch/podjob/jobspec.go:259-273`) correctly omits the provider Secret from credproxy's `EnvFrom` when `opts.Project == nil`. The nil-Project guard at jobspec.go:266-272 is INTENTIONAL defense-in-depth and remains untouched — it prevents a nil-deref when constructing the EnvFrom list. The cascade-7 fix prevents reaching that code path with `opts.Project == nil` in the first place. Removing the defense-in-depth guard is only safe AFTER all three planner controllers (Milestone, Phase, Plan) gate dispatch on Project resolution; today only Plan is gated.

## Verification

| Step | Command | Exit | Result |
|------|---------|------|--------|
| Task 1 vet | `go vet ./internal/controller/...` | 0 | PASS |
| Task 1 build | `go build ./internal/controller/...` | 0 | PASS |
| Task 1 grep: `"cascade", 7` | `grep -c '"cascade", 7' plan_controller.go` | — | 2 (expect ≥ 2) |
| Task 1 grep: refusing | `grep -c 'refusing plan-planner dispatch: plan.spec.phaseRef is empty' plan_controller.go` | — | 1 (expect 1) |
| Task 1 grep: deferring | `grep -c 'deferring plan-planner dispatch: project chain not yet resolvable, requeueing' plan_controller.go` | — | 1 (expect 1) |
| Task 1 grep: RequeueAfter | `grep -c 'RequeueAfter: 1 \* time.Second' plan_controller.go` | — | 1 (expect ≥ 1) |
| Task 2 vet | `go vet ./internal/controller/...` | 0 | PASS |
| Task 2 build | `go build ./internal/controller/...` | 0 | PASS |
| Task 2 envtest | `go test -run 'PlanReconciler' ./internal/controller/ -count=1 -timeout 120s` | 0 | PASS (90 specs, 44.0s, 0 failed) |
| Task 2 grep: Describe block | `grep -c 'PlanReconciler nil-Project dispatch guard (cascade-7)' plan_controller_test.go` | — | 1 (expect 1) |
| Task 2 grep: direct call | `grep -c 'reconcilePlannerDispatch(ctx, &plan)' plan_controller_test.go` | — | 2 (expect ≥ 1) |

All gates green. Note: Task 2 envtest is the canonical Layer A test for the new guard; the user's runtime gate (`make test-int GINKGO_LABEL_FILTER='kind && D-D4'`) is out of scope here per the plan's constraint.

## Test-Debt

None — both transient and permanent guard arms have direct envtest coverage. The OPTIONAL Test 2 (empty-PhaseRef arm) was included; no follow-up envtest needed for this cascade.

## Self-Check

Verifying claims in this SUMMARY:

- Commit `88356ad` on branch: confirmed via `git log --oneline -1 -- internal/controller/plan_controller.go` → matches.
- Commit `6212147` on branch: confirmed via `git log --oneline -1 -- internal/controller/plan_controller_test.go` → matches.
- Both commits touch the documented files only: confirmed via `git show --stat <hash>`.
- No accidental deletions: `git diff --diff-filter=D --name-only HEAD~2 HEAD` → empty.
- Working tree clean post-commit: `git status --short` → empty.
- Envtest passes after both commits: `go test -run 'PlanReconciler' ./internal/controller/ -count=1 -timeout 120s` → `ok` in 44.0s (all 90 specs).

## Self-Check: PASSED

## Out of Scope (documented follow-ups)

1. **Cascade 7-bis** — `internal/controller/phase_controller.go` lines 195-200 has the symmetric nil-Project race with a 2-hop walk (`resolveProject` instead of `resolveProjectForPlan`). Same fix shape applies; tracked as a separate quick-task follow-up.

2. **Cascade 7-ter (defensive)** — `internal/controller/milestone_controller.go` lines 240-244 has an even more latent nil-deref pattern (different shape — defaults `project` to nil and then accesses `project.Spec.ProviderSecretRef`, which would panic). Tracked as a defensive follow-up.

3. **Remove `jobspec.go:266-272` defense-in-depth guard** — only safe AFTER all three planner controllers (Milestone, Phase, Plan) gate dispatch on Project resolution. Today only Plan is gated; defense-in-depth must remain until 7-bis and 7-ter land.

4. **Refactor `resolveProjectForPlan` to return `(*Project, error)`** — would touch 4 call sites and force callers to thread error handling through the resolver. Out of scope for cascade-7; the `nil`-sentinel contract is preserved at the dispatch call site instead.

## Expected Post-Fix Runtime Shape

(For the user's separate runtime gate `make test-int GINKGO_LABEL_FILTER='kind && D-D4'`, which is NOT executed by this quick task per constraints.)

1. Plan planner Job `tide-plan-<plan-uid>-1` in `chaos-resume-test` namespace lands with `status.succeeded=1` (was: `status.failed=1` due to credproxy ANTHROPIC_API_KEY missing). Total succeeded Job count in namespace: 6 → 7 out of 8.

2. Manager log shows at most a brief flurry of `"deferring plan-planner dispatch: project chain not yet resolvable, requeueing"` V(1) lines during the initial reconcile burst before the Phase informer cache catches up (visible only with `--v=1` or higher). Should be ZERO `"refusing plan-planner dispatch: plan.spec.phaseRef is empty"` Info lines — that branch only fires on configuration errors absent from the fully-populated chaos_resume fixture.

3. Pillar 4's filtered List assertion (cascade-10 fix already landed in `aa65c8e`) is unaffected — it filters by `tideproject.k8s/role=executor` so it sees exactly 3 executor Jobs regardless of planner Job outcome.

4. If a new cascade surfaces (e.g., Phase planner's symmetric race fires on a different fixture path), that is "Cascade 7-bis" — a follow-up, not a regression of Cascade 7.

## Pointer to Debug Session

Root-cause analysis for Cascade 7's mechanism (BuildJobSpec drops provider Secret when `opts.Project == nil` → credproxy starts without `ANTHROPIC_API_KEY` → CrashLoopBackOff → Job `status.failed=1`) is documented in `.planning/debug/chaos-resume-cascade-10.md` lines 221-240 (Resolution section). The cascade-10 debug investigation (Pillar 4 list filter) refuted the duplicate-dispatch framing and surfaced Cascade 7 as a separate follow-up, leading to this quick task.
