---
phase: 04
plan: 05
subsystem: gates
tags: [gates, gate-policy-hook, pause-between-waves, annotation-handshake, w-3]
dependency_graph:
  requires:
    - "04-01 — ConditionWaveOrLevelPaused + ReasonAwaitingApproval / ReasonPausedAtBoundary / ReasonRejectedByUser / ReasonResumedByUser (consumed by all four reconcilers' patch<Level>AwaitingApproval / patchPlanFailed / patchTaskFailed helpers)"
    - "04-04 — internal/gates package (EvaluatePolicy, CheckApprove, CheckWaveApprove, CheckRejected, RejectedReason, ConsumeApprove, ConsumeWaveApprove, PolicyApprove/Pause/Auto, AnnotationApprovePrefix, AnnotationApproveWavePrefix, AnnotationReject)"
  provides:
    - "MilestoneReconciler / PhaseReconciler / PlanReconciler / TaskReconciler gate-policy seam — EvaluatePolicy + CheckApprove + CheckRejected + ConsumeApprove at the post-children-Succeeded seam (Milestone/Phase/Plan) or pre-Job-dispatch seam (Task)"
    - "patchMilestoneAwaitingApproval / patchPhaseAwaitingApproval / patchPlanAwaitingApproval / patchTaskAwaitingApproval helpers"
    - "patchPlanFailed / patchTaskFailed helpers for reject short-circuit"
    - "PlanReconciler.maybePauseForWaveApprove + tideproject.k8s/wave-paused label + tideproject.k8s/wave-approved-<N> persistent label"
    - "AnnotationChangedPredicate self-Watches on all 5 reconcilers (Milestone/Phase/Plan/Task/Wave)"
    - "test/integration/envtest/gates_test.go — TestGateApproveFlow + TestRejectHalts + TestWavePauseBetweenWaves"
  affects:
    - "04-06 (boundary push trigger + W-1 exit-10 split) — builds on the same up-stack reconciler edits to wire BoundaryDetected at the post-Succeeded seam"
    - "Wave 4 cmd/tide approve/reject/resume — operator-CLI users issue annotations that this plan now honors end-to-end"
tech_stack:
  added: []
  patterns:
    - "self-Watches with AnnotationChangedPredicate (re-enqueue on annotation-only changes without filtering Spec/finalizer Update events that a For()-level GenerationChangedPredicate would drop)"
    - "Status.Phase=AwaitingApproval + ConditionWaveOrLevelPaused True (no requeue — T-04-G4 mitigation; annotation write is the only resume trigger)"
    - "label-driven cross-reconciler pause (PlanReconciler stamps tideproject.k8s/wave-paused on wave-N Tasks; TaskReconciler honors it before Job dispatch)"
    - "persistent wave-approved-<N> label on Plan (mid-flight wave-N Tasks are not re-paused after annotation consume)"
key_files:
  created:
    - internal/controller/milestone_gates_test.go
    - internal/controller/phase_gates_test.go
    - internal/controller/plan_gates_test.go
    - internal/controller/task_gates_test.go
    - internal/controller/plan_wavepause_test.go
    - internal/controller/source_grep_helpers_test.go
    - test/integration/envtest/gates_test.go
  modified:
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/task_controller.go
    - internal/controller/wave_controller.go
decisions:
  - "Task gate hook fires AFTER indegree==0 check (only ready-to-dispatch Tasks pause). Pending tasks waiting on predecessors do not pause — they pause on the dependency, not the gate. Rationale: pausing a task whose predecessors are not done is meaningless, and parking it on AwaitingApproval would confuse the operator (the operator cannot 'approve' a task that has not yet had its dependencies satisfied)."
  - "PauseBetweenWaves uses a label-based cross-reconciler block (PlanReconciler stamps wave-paused on wave-N Tasks; TaskReconciler honors the label before Job dispatch). Considered: per-Task Plan-fetch in TaskReconciler to read the approve-wave-N annotation directly. Rejected: too many Plan-fetches on the hot path. Label approach is one stamp by PlanReconciler when the boundary trips, then no extra reads."
  - "Wave-approval persistence via tideproject.k8s/wave-approved-<N> label on the Plan. Without this, a re-reconcile after annotation consume would re-detect the boundary (gamma still Pending) and re-pause the wave. The label is a one-bit 'this wave has been approved' signal that survives until the entire wave completes (when the next reconcile would find no pending boundary). Considered: a Plan.Status field. Rejected: a label avoids the CRD schema change for this plan."
  - "AnnotationChangedPredicate wired via self-Watches Watches(&Self{}, mapper, ACP) rather than the more idiomatic For()-level builder.WithPredicates(predicate.Or(GenerationChangedPredicate, AnnotationChangedPredicate)). The For()-level Or filters the post-AddFinalizer Update event (Generation does not bump on finalizer-add and annotations are unchanged), stalling auto-reconcile in the integration envtest. The self-Watches pattern adds an additional re-enqueue path on annotation changes without disturbing the default permissive event stream — see the Deviations section for the discovery story."
  - "Plan-level gate hook lands in handlePlannerJobCompletion AFTER MaterializeChildCRDs and BEFORE clearing Phase to \"\". On approve: consume annotation, clear Running, let Wave path proceed. Rationale: matches the Milestone/Phase pattern (gate AFTER children but BEFORE the level-success patch)."
  - "Task gate hook seam placement: Reject check AFTER Step 3 (resolveProject); approve/pause check AFTER Step 5 (indegree compute). The reject check is early so even Pending tasks halt; the approve check is late so only Ready tasks gate (see decision 1)."
metrics:
  duration_minutes: 66
  completed_date: 2026-05-19
  tasks_completed: 3
  files_created: 7
  files_modified: 5
  commits: 6
---

# Phase 4 Plan 05: Gate-Policy Hook + PauseBetweenWaves Summary

Wire the `internal/gates` package (shipped in 04-04) into all four up-stack reconcilers (Milestone, Phase, Plan, Task) at the post-children-Succeeded seam, add a wave-boundary pause hook to PlanReconciler honoring `Project.Spec.Gates.PauseBetweenWaves`, and re-enqueue every modified reconciler on annotation changes via a self-Watches pattern. The label-based wave-pause block bridges PlanReconciler's "is wave N approved?" decision to TaskReconciler's per-task dispatch decision without a Plan-fetch on every Task reconcile.

## What landed

### Task 1 — gate-policy hook in all four up-stack reconcilers

The same seam shape is inserted into `MilestoneReconciler.handleJobCompletion`, `PhaseReconciler.handleJobCompletion`, `PlanReconciler.handlePlannerJobCompletion`, and `TaskReconciler.reconcileDispatch`:

```go
// (1) reject short-circuit
if gates.CheckRejected(project) {
    return r.patch<Level>Failed(ctx, obj, ReasonRejectedByUser, gates.RejectedReason(project))
}

// (2) per-level gate evaluation
policy := gates.EvaluatePolicy(project.Spec.Gates, "<level>")
if policy == gates.PolicyApprove || policy == gates.PolicyPause {
    if !gates.CheckApprove(obj, "<level>") {
        return r.patch<Level>AwaitingApproval(ctx, obj, policy)
    }
    // approve annotation present: consume one-shot, then fall through to patchSucceeded
    newAnno := gates.ConsumeApprove(obj, "<level>")
    patch := client.MergeFrom(obj.DeepCopy())
    obj.SetAnnotations(newAnno)
    r.Patch(ctx, obj, patch)
}
```

Four new `patch<Level>AwaitingApproval` helpers mirror the existing `patch<Level>Succeeded/Failed` shape (client.MergeFrom + meta.SetStatusCondition + r.Status().Patch). Two new helpers (`patchPlanFailed` + `patchTaskFailed`) handle the reject short-circuit at the Plan and Task levels where no such helper previously existed.

**Task seam difference** (documented inline): the Task gate fires BEFORE Job dispatch (Tasks have no children), specifically after the indegree==0 check so only ready-to-dispatch Tasks pause. The reject short-circuit fires earlier (right after `resolveProject`) so even Pending tasks halt on a Project-level reject.

### Task 2 — PauseBetweenWaves + Wave AnnotationChangedPredicate

`PlanReconciler.maybePauseForWaveApprove` (new helper, called at the tail of `reconcileWaveMaterialization`):

1. No-op if `Project.Spec.Gates.PauseBetweenWaves` is false.
2. Compute the smallest wave index N where wave N-1 is fully Succeeded AND wave N has at least one non-Succeeded Task.
3. If the Plan carries label `tideproject.k8s/wave-approved-<N>`, no-op (mid-flight wave-N Tasks are not re-paused).
4. Else if `gates.CheckWaveApprove(plan, N)` is true: consume the annotation, stamp `wave-approved-<N>` on the Plan in the same Patch, clear `tideproject.k8s/wave-paused` labels from wave-N Tasks, flip the Plan's Condition WaveOrLevelPaused to False (Reason=ResumedByUser).
5. Else: stamp `tideproject.k8s/wave-paused=<N>` on each Task in wave N, patch Plan Condition WaveOrLevelPaused True (Reason=PausedAtBoundary, Message embeds the wave index and the exact annotation key the operator should write).

`TaskReconciler.reconcileDispatch` honors the `wave-paused` label via a check between the indegree==0 step and the per-task gate-policy check: if the label is present, the Task is parked at AwaitingApproval (Reason=PausedAtBoundary) and no Job is dispatched. The label only clears when PlanReconciler consumes the wave-approve annotation.

`WaveReconciler.SetupWithManager` gains the same self-Watches annotation hook as the other four reconcilers.

### Task 3 — three-flow envtest

`test/integration/envtest/gates_test.go` (new, 476 LOC) — three Ginkgo subtests covering the three core gate flows end-to-end against the existing Layer A envtest suite:

| Test | Surface | Assertion shape |
| ---- | ------- | --------------- |
| `TestGateApproveFlow` | Project.Gates.Milestone=approve | Milestone parks at AwaitingApproval → approve-milestone annotation → Succeeded + annotation consumed |
| `TestRejectHalts` | tideproject.k8s/reject on Project | Milestone Failed with Reason=RejectedByUser, Message contains reject reason |
| `TestWavePauseBetweenWaves` | Project.Gates.PauseBetweenWaves=true | wave 0 Succeeded → Plan Condition + wave-1 Tasks wave-paused-labeled → approve-wave-1 annotation → Condition False + labels cleared |

Each subtest creates its own fixture (Project + Milestone + Phase + Plan + Tasks) with uniquely-named resources, drives reconcilers directly via `r.Reconcile(...)` (the existing suite_test.go does not configure Dispatcher on MilestoneReconciler/PhaseReconciler), then asserts via Gomega Eventually. AfterEach drops finalizers and deletes the entire fixture chain to isolate state across tests.

## Reconciler edit map

| Reconciler | Seam location | Helper(s) added | SetupWithManager change |
| ---------- | ------------- | --------------- | ----------------------- |
| Milestone  | handleJobCompletion AFTER MaterializeChildCRDs | patchMilestoneAwaitingApproval | self-Watches with AnnotationChangedPredicate |
| Phase      | handleJobCompletion AFTER MaterializeChildCRDs | patchPhaseAwaitingApproval | self-Watches with AnnotationChangedPredicate |
| Plan       | handlePlannerJobCompletion AFTER MaterializeChildCRDs (gate hook) + reconcileWaveMaterialization tail (wave pause) | patchPlanAwaitingApproval + patchPlanFailed + maybePauseForWaveApprove | self-Watches with AnnotationChangedPredicate |
| Task       | reconcileDispatch — reject AFTER Step 3, wave-paused label check AFTER Step 5, per-task gate AFTER wave-paused | patchTaskAwaitingApproval + patchTaskFailed | self-Watches with AnnotationChangedPredicate |
| Wave       | (no gate body — observation-only per D-B2) | — | self-Watches with AnnotationChangedPredicate |

## Plan verification block satisfied

| Check | Result |
| ----- | ------ |
| `go test ./internal/controller/... -run Gate -race -v` | 14/14 gate specs PASS |
| `go test ./internal/controller/... -ginkgo.label-filter='gates||wavepause' -race -v` | 18/18 specs PASS |
| `go test ./test/integration/envtest/... -run Gate -race -timeout 5m -v` | 3/3 gate-integration specs PASS |
| `grep -c "gates.EvaluatePolicy" internal/controller/{milestone,phase,plan,task}_controller.go` | **4** (≥ 4 required) |
| `grep -c "AnnotationChangedPredicate" internal/controller/{milestone,phase,plan,task,wave}_controller.go` | **14** (≥ 5 required) |
| `grep -c "gates.CheckWaveApprove" internal/controller/plan_controller.go` | **2** (≥ 1 required) |
| `make tide-lint` | clean (no metric-cardinality / provider-firewall violations) |
| `go build ./...` | clean |

## Test coverage

| File | Subtests | Pass with `-race` |
| ---- | -------- | ----------------- |
| `internal/controller/milestone_gates_test.go` | 4 (approve, resume, reject, auto) | ✅ |
| `internal/controller/phase_gates_test.go` | 4 | ✅ |
| `internal/controller/plan_gates_test.go` | 3 | ✅ |
| `internal/controller/task_gates_test.go` | 3 | ✅ |
| `internal/controller/plan_wavepause_test.go` | 4 (pause, resume, no-pause, grep) | ✅ |
| `test/integration/envtest/gates_test.go` | 3 (approve / reject / wave-pause) | ✅ |

Total: 21 new test specs across unit + integration layers.

## TDD Gate Compliance

Tasks 1 and 2 followed strict RED → GREEN cycles. Task 3 was authored as a single envtest commit (no separate RED gate — the contract is the existence of the integration test, not behavior-add). Commit ledger:

| Task | Phase | Commit | Type | Subject |
| ---- | ----- | ------ | ---- | ------- |
| 1 | RED   | `4fd545d` | test | RED — gate-policy hook tests for all four up-stack reconcilers |
| 1 | GREEN | `8487e39` | feat | GREEN — gate-policy hook in all four up-stack reconcilers |
| 2 | RED   | `68bca28` | test | RED — PauseBetweenWaves + Wave AnnotationChangedPredicate tests |
| 2 | GREEN | `8567f1b` | feat | GREEN — PauseBetweenWaves + Wave AnnotationChangedPredicate |
| 1+2 fix | -- | `51fe27e` | fix | wire AnnotationChangedPredicate via self-Watches instead of For() |
| 3 | --    | `493a63c` | test | integration envtest for the three gate flows |

Every RED commit was verified to fail (1 Failed / 3 Failed) before its GREEN landed.

## Deviations from Plan

### Auto-fixed issues

**1. [Rule 1 — Bug] AnnotationChangedPredicate at For() level filters post-finalizer-add Update events**

- **Found during:** Task 3 envtest authoring + running the full integration suite
- **Issue:** The Task 1 GREEN wired `For(&Type{}, builder.WithPredicates(predicate.Or(GenerationChangedPredicate{}, AnnotationChangedPredicate{})))`. Both predicates returned false for the post-`controllerutil.AddFinalizer` Update event (Generation doesn't bump on finalizer-add — finalizers live in metadata — and annotations are unchanged), so the reconciler stalled after the finalizer-add step. Layer A integration tests (which depend on manager auto-reconcile to drive the lifecycle) timed out waiting for `Status.Attempt >= 1` and owner-ref stamping (SUB-02 / SUB-03 / PERSIST-03 indegree tests; gate-flow TestGateApproveFlow).
- **Fix:** Re-wire AnnotationChangedPredicate as a self-Watches handler — `Watches(&Self{}, EnqueueRequestsFromMapFunc(self), builder.WithPredicates(AnnotationChangedPredicate{}))`. The default For() event stream now keeps all events (Spec, finalizer, owner-ref Updates) while a separate Watches re-enqueues the object on annotation-only changes. T-04-G4 mitigation preserved.
- **Files modified:** milestone_controller.go, phase_controller.go, plan_controller.go, task_controller.go, wave_controller.go
- **Commit:** `51fe27e`

**2. [Rule 2 — Missing functionality] PlanReconciler did not have a patchPlanFailed helper**

- **Found during:** Task 1 GREEN authoring (needed by reject short-circuit at Plan level)
- **Fix:** Added `patchPlanFailed(ctx, plan, reason, message)` mirroring the existing patchMilestoneFailed/patchPhaseFailed shape. Inline with the gate-policy seam edits in plan_controller.go.
- **Commit:** `8487e39`

**3. [Rule 2 — Missing functionality] TaskReconciler did not have a patchTaskFailed helper**

- **Found during:** Task 1 GREEN authoring (needed by reject short-circuit at Task level)
- **Fix:** Added `patchTaskFailed(ctx, task, reason, message)`. Inline with the gate-policy seam edits in task_controller.go.
- **Commit:** `8487e39`

**4. [Rule 2 — Missing functionality] Wave-approval persistence across reconciles**

- **Found during:** Task 2 wavepause Test 2 RED → first GREEN failed (condition flipped back to True after annotation consume)
- **Issue:** Without a persistence signal, the reconcile pass that consumed the approve-wave-N annotation cleared the wave-paused labels + flipped the Plan condition False. But the very next reconcile (triggered by the Task status patch or by the manager's re-enqueue loop) re-detected the boundary (gamma still Pending in wave 1) and re-paused. The annotation was already consumed (one-shot, T-04-G2), so `CheckWaveApprove` returned false and the boundary re-stamped.
- **Fix:** Added `tideproject.k8s/wave-approved-<N>` label stamped on the Plan in the same Patch that consumes the annotation. The pause detection short-circuits when this label is present — the wave is "mid-flight, already approved", and pausing again would be meaningless.
- **Commit:** `8567f1b`

### Pre-existing flakes observed but NOT caused by this plan

While running the controller suite during Task 3 authoring, `phase_controller_test.go:Test 4: dispatches planner Job tide-phase-<uid>-1` flaked 2-of-3 runs. Bisected against base commit `016d5c7` (3 runs at base also failed, with the same shape and runtime ≥ 100s). This is a pre-existing flake in the planner-dispatch path of PhaseReconciler unrelated to plan 04-05. Logged as deferred for a follow-up debug session.

## Known Stubs

None. The label-based wave-pause mechanism is a complete cross-reconciler block (PlanReconciler stamps, TaskReconciler honors, PlanReconciler clears on approve). All four up-stack reconcilers consult `gates.EvaluatePolicy` at their respective seams. All five reconcilers' SetupWithManager wire AnnotationChangedPredicate.

## Threat Flags

None. The plan's `<threat_model>` is fully mitigated:

| Threat | Mitigation |
| ------ | ---------- |
| T-04-G1 (spoofing / privilege escalation — bypassing the gate seam) | All four reconcilers call `gates.EvaluatePolicy(project.Spec.Gates, level)` at the same seam. Grep contract `grep -c gates.EvaluatePolicy ...` = 4. The CEL constraint on api/v1alpha1.GatePolicy (`Enum=auto;approve;pause`) restricts wire values at admission. |
| T-04-G2 (tampering / replay-approval) | `gates.ConsumeApprove` and `gates.ConsumeWaveApprove` return a NEW annotation map with the consumed key removed; the reconciler patches once. Re-triggering requires a fresh annotation write — and the next level's / wave's gate check would block independently (wave-approved-N label is per-integer). |
| T-04-G3 (wave-skip) | `gates.CheckWaveApprove(plan, N)` keys on integer wave index; approve-wave-3 does not match approve-wave-4. The wave-approved-<N> label is similarly per-integer. PlanReconciler's `maybePauseForWaveApprove` finds the smallest pending wave and only approves that wave. |
| T-04-G4 (DoS via gate-pause infinite loop) | All `patch<Level>AwaitingApproval` helpers return `ctrl.Result{}, nil` (no requeue, no requeue-after). The only path forward is an operator-driven annotation write, which fires through the self-Watches AnnotationChangedPredicate handler. No polling loop. |

## Self-Check: PASSED

Files exist:
- ✅ `internal/controller/milestone_gates_test.go` (288 LOC)
- ✅ `internal/controller/phase_gates_test.go` (273 LOC)
- ✅ `internal/controller/plan_gates_test.go` (236 LOC)
- ✅ `internal/controller/task_gates_test.go` (175 LOC)
- ✅ `internal/controller/plan_wavepause_test.go` (245 LOC)
- ✅ `internal/controller/source_grep_helpers_test.go` (23 LOC — grep-contract assertion helper)
- ✅ `test/integration/envtest/gates_test.go` (476 LOC)

Commits exist on worktree branch (`git log --all --oneline | grep 04-05`):
- ✅ `4fd545d` test(04-05): RED — gate-policy hook tests
- ✅ `8487e39` feat(04-05): GREEN — gate-policy hook in all four up-stack reconcilers
- ✅ `68bca28` test(04-05): RED — PauseBetweenWaves + Wave AnnotationChangedPredicate
- ✅ `8567f1b` feat(04-05): GREEN — PauseBetweenWaves + Wave AnnotationChangedPredicate
- ✅ `51fe27e` fix(04-05): wire AnnotationChangedPredicate via self-Watches instead of For()
- ✅ `493a63c` test(04-05): integration envtest for the three gate flows

Tests pass with `-race`:
- ✅ Controller unit gates+wavepause: 18 specs in 17.7s
- ✅ Layer A integration envtest gate-integration: 3 specs in 10.4s

Plan verification block satisfied: 4 EvaluatePolicy / 14 AnnotationChangedPredicate / 2 CheckWaveApprove grep counts. `make tide-lint` clean. `go build ./...` clean.

STATE.md / ROADMAP.md NOT touched — orchestrator owns those writes after all worktree agents in the wave complete.
