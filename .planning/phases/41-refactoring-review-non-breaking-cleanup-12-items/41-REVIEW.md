---
phase: 41-refactoring-review-non-breaking-cleanup-12-items
reviewed: 2026-07-12T18:43:26Z
depth: standard
files_reviewed: 65
files_reviewed_list:
  - api/v1alpha3/shared_types.go
  - AGENTS.md
  - cmd/dashboard/api/execution_dag.go
  - cmd/dashboard/api/informer_bridge_test.go
  - cmd/dashboard/api/plans.go
  - cmd/dashboard/api/projects.go
  - cmd/dashboard/api/tasks.go
  - cmd/dashboard/api/waves_test.go
  - cmd/dashboard/api/waves.go
  - cmd/manager/main.go
  - cmd/manager/wave_dispatcher_wiring_test.go
  - cmd/manager/wiring_test.go
  - cmd/tide/approve.go
  - cmd/tide/cancel.go
  - cmd/tide/inspect_wave_run.go
  - cmd/tide/resume.go
  - cmd/tide/watch.go
  - internal/controller/artifact_push.go
  - internal/controller/billing_halt_regression_test.go
  - internal/controller/billing_halt.go
  - internal/controller/boundary_push_test.go
  - internal/controller/boundary_push.go
  - internal/controller/budget_blocked_regression_test.go
  - internal/controller/budget_blocked.go
  - internal/controller/dispatch_helpers.go
  - internal/controller/dispatch_image_test.go
  - internal/controller/failure_halt.go
  - internal/controller/file_touch_gate_test.go
  - internal/controller/git_writer_test.go
  - internal/controller/git_writer.go
  - internal/controller/level_status.go
  - internal/controller/milestone_controller_test.go
  - internal/controller/milestone_controller.go
  - internal/controller/milestone_gates_test.go
  - internal/controller/parentref_surface_test.go
  - internal/controller/phase_controller_test.go
  - internal/controller/phase_controller.go
  - internal/controller/phase_gates_test.go
  - internal/controller/plan_controller_test.go
  - internal/controller/plan_controller.go
  - internal/controller/plan_gates_test.go
  - internal/controller/plan_planner_test.go
  - internal/controller/plan_wave_integration_test.go
  - internal/controller/plan_wavepause_test.go
  - internal/controller/planner_job_absent_test.go
  - internal/controller/project_baseref_halt_test.go
  - internal/controller/project_clone_idempotency_test.go
  - internal/controller/project_controller.go
  - internal/controller/project_phase3_test.go
  - internal/controller/push_helpers_test.go
  - internal/controller/push_helpers.go
  - internal/controller/task_controller_extracted_test.go
  - internal/controller/task_controller_test.go
  - internal/controller/task_controller.go
  - internal/controller/task_gates_test.go
  - internal/controller/wave_controller_test.go
  - internal/controller/wave_controller.go
  - internal/dispatch/dispatcher.go
  - internal/dispatch/podjob/backend.go
  - internal/dispatch/podjob/doc.go
  - internal/dispatch/podjob/jobspec.go
  - internal/owner/label.go
  - internal/subagent/anthropic/subagent.go
  - test/integration/envtest/gates_test.go
  - test/integration/envtest/suite_test.go
findings:
  critical: 2
  warning: 2
  info: 6
  total: 10
status: issues_found
---

# Phase 41: Code Review Report

**Reviewed:** 2026-07-12T18:43:26Z
**Depth:** standard
**Files Reviewed:** 65
**Status:** issues_found

## Summary

Adversarial review of the Phase 41 non-breaking refactoring sweep (11 code items + 1 doc item), diffed against base `4889db1`. Every phase commit was diffed and the behavior-invariance contract was checked mechanically where possible.

**Behavior-invariance verification (what was proven, not assumed):**

- **checkDispatchHolds extraction (41-05):** the pre-extraction inline chains in Milestone/Phase/Plan were diffed hunk-by-hunk against the helper — gate order (Billing → Failure → Budget → Import), requeue values (30s/30s/30s/5s), the `budget.IsBypassed` guard on the Budget arm, and nil-project semantics are all byte-equivalent. Log *messages* are unchanged (the `"dispatch held: ..."` grep protocols keep working); the phase/plan hold logs gained a `"project"` k/v pair that wasn't there before (cosmetic).
- **LevelPhase\* sweep (41-04):** verified byte-invariant by normalizing every `LevelPhase*`/`PhasePending` constant back to its literal and diffing pre/post files — zero semantic drift. The two project-vocabulary sites (`watch.go` → `PhasePending`, `task_controller.go:456` → `PhaseBudgetExceeded`) correctly used the Project constants, whose values (`"Pending"`, `"BudgetExceeded"`) match the removed literals.
- **countChildren unification (41-07):** the dropped Kind predicate is safe — every child owner-ref stamping path (`internal/owner.EnsureOwnerRef` → `controllerutil.SetControllerReference` with Controller=true; used by the reconcilers, `internal/reporter/materialize.go:293`, and `import_controller.go` at all four levels) sets Controller=true, and UID match is unique cluster-wide. The new per-object `break` also removes a theoretical double-count on duplicate refs the old loops had.
- **consumeApproveAndResume (41-07):** annotation-consume-before-status-write ordering (T-04-G2) is preserved at all six sites; the helper only ever writes `LevelPhaseRunning` (D-04 never-jump-past-children holds); handleJobCompletion call sites correctly discard the `Requeue` result and fall through to ChildCount-gated succession, matching the pre-extraction inline code.
- **patchLevelStatus (41-07):** all 15 wrappers preserve reason/message text, condition counts, requeue values, and the optimistic-lock split (lock on the three planner-tier AwaitingApproval parks, plain MergeFrom on Task's, none elsewhere) byte-for-byte.
- **Halt helpers (41-01):** `meta.IsStatusConditionTrue` is loop-for-loop equivalent to the removed hand-rolled scans.
- **Retry-driver unification (41-02):** `reconcileWithRetryResult` is shape-identical to the deleted `reconcileN`; `apierrors.IsConflict` unwraps wrapped errors via `errors.As`, so `%w`-wrapped 409s are still retried.
- **Polarity flip (41-08, intentional):** grep confirms zero consumers of `ConditionParentUnresolved` outside the two flipped controllers — no reader is broken; clear-on-resolve tests added for both levels.
- **Dead-code deletion (41-03):** deleted `gateDispatch`/`ensureJob` were `nolint:unused` stubs with no callers; doc references repointed correctly.

**Observed test/lint state:** `go build` clean on all reviewed packages; `go test` green on `internal/controller` (envtest suite, 114s), `internal/owner`, `internal/dispatch/podjob`, `internal/subagent/...`, `cmd/manager`, `cmd/tide`, `cmd/dashboard/...`. **Repo-wide `golangci-lint run` (the `make lint` / `.github/workflows/lint.yml` gate) FAILS with exactly 3 findings, all introduced by this phase** — see CR-01.

Two Critical findings: the red lint gate (CR-01) and an incomplete SharedPVCName plumb that leaves the very config the fix claims to enable in a split-brain state (CR-02).

## Critical Issues

### CR-01: Repo lint gate is red — 3 logcheck findings introduced by checkDispatchHolds (41-05), deferred but never fixed

**Severity:** Critical (ship gate) · **Confidence:** high (reproduced locally with the repo-pinned golangci-lint v2.11.4)
**File:** `internal/controller/dispatch_helpers.go:566,574,581`
**Issue:** `checkDispatchHolds` passes the `level` variable as a positional log KEY (`logf...Info("dispatch held: ...", level, objName, ...)`). logcheck requires inlined constant-string keys. `./bin/golangci-lint run` on the whole repo returns exactly these 3 findings and nothing else — meaning `make lint`, which `.github/workflows/lint.yml` runs, fails on this tree. `deferred-items.md` records the findings as deferred out of 41-06's scope, but no later wave fixed them, so the phase ships with a red lint workflow. (Note: these are NOT pre-existing relative to the phase — commit `96dd23b`, Plan 41-05, introduced them.)
**Fix:** Use a constant key and carry the level as a value — the message strings (which the runtime-gate grep protocols depend on) stay identical; only the k/v shape changes:
```go
if checkBillingHalt(project) {
    logf.FromContext(ctx).V(1).Info("dispatch held: project billing halt",
        "level", level, "name", objName, "project", project.Name)
    return true, ctrl.Result{RequeueAfter: 30 * time.Second}
}
// (same change at the failure-halt, budget, and import arms)
```
If preserving the old per-level key (`"milestone", ms.Name`) is considered load-bearing, the alternative is a `//nolint:logcheck` with justification — but no test or protocol greps the key names (AGENTS.md pins message wording only), so the constant-key fix is safe.

### CR-02: SharedPVCName plumb (41-09) is incomplete — reporter/push/boundary Jobs still hard-code "tide-projects", creating split-brain PVC mounts for the exact config the fix enables

**Severity:** Critical for the intended non-default-PVC config (latent until `TIDE_WORKSPACES_PVC_NAME` is set) · **Confidence:** high on the code path; medium on operational reachability (the chart does not yet set the env var — `cmd/manager/main.go:217` TODO — and no `--workspaces-pvc-name` flag actually exists)
**File:** `internal/controller/dispatch_helpers.go:114`, `internal/controller/plan_controller.go:546`, `internal/controller/boundary_push.go:150`, `internal/controller/boundary_push.go:226`, `internal/controller/artifact_push.go:235`
**Issue:** Commit `0dc6dfd` plumbs `SharedPVCName` into the 5 planner/executor dispatch sites (`podjob.BuildOptions.PVCName: r.sharedPVCName()`) and wires all five reconcilers in main.go, fixing the "latent --workspaces-pvc-name config bug." But five other Job builders in the same write/read pipeline still hard-code `defaultSharedPVCName`:
- `spawnReporterIfNeeded` (dispatch_helpers.go:114) — reporter Jobs for Milestone/Phase completions
- the Plan reconciler's inline reporter spawn (plan_controller.go:546)
- `triggerBoundaryPush` (boundary_push.go:150) and `triggerWaveIntegrationJob` (boundary_push.go:226)
- `triggerArtifactPush` (artifact_push.go:235)

With a non-default PVC name configured, planner Jobs now write `out.json` to the configured PVC while the reporter Job mounts `tide-projects` — if that claim doesn't exist the reporter pod is stuck Pending; if it does, the reporter reads an empty envelope path. Either way child CRDs never materialize and the pipeline stalls at the first planner completion. Push/boundary/wave Jobs likewise stage artifacts from the wrong volume. Pre-fix, the flag was uniformly dead (everything consistently on `tide-projects`, system worked); post-fix, setting the env var produces a *broken* pipeline instead of an *ignored* setting — the partial plumb is strictly worse than the bug for the config it targets. (`ImportReconciler` is correctly wired; only the reporter/git-writer Job family was missed.)
**Fix:** Thread the configured name through the remaining builders, e.g.:
```go
// dispatch_helpers.go — add pvcName param to spawnReporterIfNeeded and pass r.sharedPVCName() from both callers
reporterJob := BuildReporterJob(parent, project, pvcName, string(parent.GetUID()), parentKind, ...)

// plan_controller.go:546
pvcName := r.sharedPVCName()

// boundary_push.go / artifact_push.go — add a pvcName (or sharedPVCName string) param to
// triggerBoundaryPush / triggerWaveIntegrationJob / triggerArtifactPush and pass it from
// each reconciler's r.sharedPVCName(); replace the defaultSharedPVCName literals.
```
Until then, either revert the claim that the config bug is "fixed" or document that only dispatch Jobs honor the setting.

## Warnings

### WR-01: Dead-field sweep (41-03) incomplete — five dead struct fields remain, one of which hides an unenforced executor-concurrency cap

**Severity:** Warning · **Confidence:** high (grep-verified zero reads)
**File:** `internal/controller/milestone_controller.go:74`, `internal/controller/phase_controller.go:67`, `internal/controller/plan_controller.go:81`, `internal/controller/task_controller.go:129-131`, `internal/controller/task_controller.go:97`
**Issue:** Plan 41-03's charter was deleting dead reconciler fields (it removed `SubagentImage` ×5 and WaveReconciler's pools), but left behind:
- `ExecutorPool *pool.Pool` on Milestone/Phase/Plan reconcilers — never wired by main.go, never read.
- `PlannerPool *pool.Pool` on TaskReconciler — never wired, never read.
- `TaskReconcilerDeps.Recorder` — never wired, never read.
- **`TaskReconciler.ExecutorPool` is wired by main.go (`main.go:518`) but read nowhere** — no `Acquire` exists on the executor path (`grep Acquire internal/controller` hits only the four PlannerPool sites). `cfg.ExecutorConcurrency` therefore constructs and pre-charges a pool that gates nothing: the executor concurrency cap is silently unenforced. This is pre-existing at the phase base (the deleted `ensureJob`/`gateDispatch` stubs never acquired it either), but a dead-code-deletion phase touching exactly these structs is where it should have been surfaced.
**Fix:** Delete the four never-read fields (Milestone/Phase/Plan `ExecutorPool`, Task `PlannerPool`, `TaskReconcilerDeps.Recorder`). For `TaskReconciler.ExecutorPool`, decide explicitly: either delete it plus the main.go wiring and the `executorConcurrency` config knob (honest about current behavior), or file/execute the fix that makes the Task dispatch path actually acquire it (POOL-01 says it should). Do not leave it half-wired.

### WR-02: TestReconcilerWiringComplete is a tautology — the "wiring lock" 41-06 extended proves nothing about main()'s wiring, and only Dispatcher has a real guard

**Severity:** Warning (test reliability — illusory regression guard) · **Confidence:** high
**File:** `cmd/manager/wiring_test.go:45-126`
**Issue:** Every case constructs a fresh struct literal *in the test* with the field set, then asserts the field is non-nil — e.g. `(&controller.ProjectReconciler{Deps: PlannerReconcilerDeps{Dispatcher: dispatcher}}).Deps.Dispatcher == nil`. This can never fail regardless of what main.go does; if main.go dropped `Dispatcher: dispatcher` (or any of the other 7 `plannerDeps` fields) from its construction, this test stays green. The sibling AST test (`wave_dispatcher_wiring_test.go`) even documents this tautology as the reason debug #16 escaped — yet 41-06 extended the tautological matrix rather than the AST guard. The AST guard covers **only** the `Dispatcher` key; `EnvReader`, `SigningKey`, `CredproxyImage`, `TidePushImage`, `ReporterImage`, `HelmProviderDefaults`, and `PricingOverridesJSON` — the cascade-8 "forgotten field" class the carrier exists to prevent — have no real construction-site guard at all.
**Fix:** Extend `TestMainWiresDispatcherOnGatedReconcilers`'s AST walk (it already resolves the `Deps: plannerDeps` indirection) to assert the full required-field set on the `plannerDeps` composite literal, e.g. a `requiredDepsFields = []string{"Dispatcher", "EnvReader", "SigningKey", "CredproxyImage", "TidePushImage", "ReporterImage", "HelmProviderDefaults", "PricingOverridesJSON"}` loop over `varLiterals["plannerDeps"]`. Then delete or clearly re-scope the tautological cases in `wiring_test.go` (they currently document themselves as the guard they are not).

## Info

### IN-01: Mojibake fix (41-01) incomplete — four `Â§` sequences remain in subagent.go

**Severity:** Info · **Confidence:** high (grep-verified)
**File:** `internal/subagent/anthropic/subagent.go:22,30,63,203`
**Issue:** Commit `24bd3df` ("fix mojibake in comments") replaced the `â`-class sequences with `—`/`→` but left four `Â§` (should be `§`) in the same file it was fixing.
**Fix:** `sed -i '' 's/Â§/§/g' internal/subagent/anthropic/subagent.go`

### IN-02: patchLevelStatus / consumeApproveAndResume silently return success on DeepCopyObject type-assertion failure

**Severity:** Info (unreachable in practice, but a swallow-shape) · **Confidence:** high
**File:** `internal/controller/level_status.go:81-83,139-141`
**Issue:** `base, ok := obj.DeepCopyObject().(client.Object); if !ok { return ctrl.Result{}, nil }` — a failed assertion returns a zero Result and nil error, i.e. "success, don't requeue," with the status write silently skipped. Every current caller passes a typed CRD pointer so the branch is dead, but if it ever fired it would wedge a level with no error, no log, no requeue.
**Fix:** Return an error (`fmt.Errorf("patchLevelStatus: %T does not implement client.Object after DeepCopyObject", obj)`) instead of nil, or drop the guard and assert unconditionally (a panic here is a programmer error worth surfacing).

### IN-03: Corrupted/merged doc comment above newStatusPhaseOrDepsChangedPredicate (pre-existing)

**Severity:** Info (pre-existing at phase base — not introduced by Phase 41) · **Confidence:** high
**File:** `internal/controller/task_controller.go:1730-1745,1762-1765`
**Issue:** The comment block starting `// SetupWithManager wires the watch with Owns(&batchv1.Job{}) per CTRL-02, a` sits above `newStatusPhaseOrDepsChangedPredicate` and mid-sentence morphs into that function's doc; the real `SetupWithManager` below has an orphaned fragment (`// namespace-filter predicate per AUTH-02, ...`) as its doc comment. Also claims the predicate constructor is "Exported" while describing it as lowercase.
**Fix:** Split into two correct doc comments — one for the predicate constructor, one complete sentence for `SetupWithManager`.

### IN-04: WaveReconciler.SetupWithManager doc claims Owns(&batchv1.Job{}) that does not exist (pre-existing)

**Severity:** Info (pre-existing at phase base) · **Confidence:** high
**File:** `internal/controller/wave_controller.go:255-261`
**Issue:** The doc comment says the builder wires `Owns(&batchv1.Job{}) per CTRL-02`; the builder chain (lines 269-285) has no `Owns` at all — consistent with the "OBSERVATIONAL ONLY, never creates Jobs" contract, so the comment is stale, not the code.
**Fix:** Drop the `Owns(&batchv1.Job{})` claim from the comment.

### IN-05: AGENTS.md logging amendment's examples contradict its own lowercase-initial rule

**Severity:** Info · **Confidence:** medium (style-doc internal consistency, judgment call)
**File:** `AGENTS.md:215-232`
**Issue:** The 41-01 amendment declares lowercase-initial messages as the repo convention ("deliberate deviation from the upstream K8s SIG style"), then immediately illustrates with upstream-cased examples: `"Deployment could not create Pod"`, `"Could not delete Pod"`, `"Deleted Pod"`. A literal-minded reader (the doc's stated audience is AI agents, which the repo's own guidance notes follow prompts literally) gets conflicting signals — three uppercase examples versus one rule sentence.
**Fix:** Lowercase the example messages (`"deployment could not create Pod"`, `"could not delete Pod"`, `"deleted Pod"`) or annotate them as upstream-style counter-examples.

### IN-06: 41-09's "config bug fix" references a `--workspaces-pvc-name` flag that does not exist, and the chart never sets the env var

**Severity:** Info (documentation/config accuracy; operational impact covered under CR-02) · **Confidence:** high
**File:** `internal/controller/milestone_controller.go:84-86` (and the identical field comments on Phase/Plan/Task/Project reconcilers), `cmd/manager/main.go:212-219`
**Issue:** All five `SharedPVCName` field comments say "Configurable via --workspaces-pvc-name flag on the manager." No such flag is registered in main.go (the only source is the `TIDE_WORKSPACES_PVC_NAME` env var), and main.go:217's TODO records that the chart does not wire that env var from `workspaces.pvc.name` either. So the "fixed" config path is reachable only by hand-editing the Deployment env — and per CR-02, doing so currently breaks the reporter/push Job family.
**Fix:** Either register the flag (mirroring the other Helm→flag tunables) or correct the five field comments to name the env var; complete the chart wiring alongside the CR-02 plumb.

---

_Reviewed: 2026-07-12T18:43:26Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
