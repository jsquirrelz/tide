---
status: resolved
trigger: "the phase→milestone→project succession + boundary-push trigger"
created: 2026-06-09
updated: 2026-06-09
resolution: fixed
fix_commit: 546e84e
---

# Debug: phase→milestone→project succession + boundary-push stall

## Symptoms (gathered)

- **Expected:** With the only Plan and all 3 Tasks `Succeeded`, succession should climb
  Phase → Milestone → Project, fire the boundary push, and drive `Project=Complete`.
- **Actual:** Succession STALLS. Plan + all Tasks are `Succeeded`, but Phase, Milestone,
  and Project remain `Running` indefinitely (>76 min and counting). Boundary push never fires.
- **Evidence source:** Live minikube run (context `minikube`, ns `tide-sample-medium`,
  run UID `18bff5a7-35cd-452b-b8c3-2e683cbb2a93`).
- **Timeline:** Surfaced on the current Phase-11 (boundary-push wiring) live medium run.

## Live evidence (captured 2026-06-09 ~21:14Z; cluster time)

### CR tree (descendants done, ancestors stuck)
```
Project    medium-project                     Running     created 19:47:43Z
Milestone  milestone-01-add-formatted-now     Running     created 19:49:09Z
Phase      phase-01-implement-formatted-now   Running     created 19:50:28Z
Plan       plan-01-implement-formatted-now    Succeeded   created 19:52:43Z
Task       task-01-implement-formatted-now    Succeeded
Task       task-02-update-main-function       Succeeded
Task       task-03-create-tests               Succeeded
```

### Plan status (genuinely complete)
- `Succeeded=True (TasksCompleted)` at **20:03:57Z**: "All owned Tasks reached Succeeded; Plan complete"
- `WaveOrLevelPaused=False (ResumedByUser)`
- `integratedThroughWave: 2`, `validationState: Validated`, `phase: Succeeded`

### Phase status (stuck — never advanced)
- Only condition present: `AuthoringPlanner=True (PlannerDispatched)` at 19:50:28Z.
- NO `Succeeded` condition. `phase: Running`.

### Smoking gun — Phase stops reconciling after the Plan succeeds
- Phase controller's LAST log line for this phase: **19:57:45Z** "spawned reporter Job".
- Plan reached `Succeeded` at **20:03:57Z** — AFTER the phase's last activity.
- There is **NO phase reconcile log after 20:03:57Z**. The `Owns(&Plan{})` watch
  either did not enqueue the phase on the Plan's status flip, OR the phase reconciled
  silently (no log, no requeue) and left itself stuck. Phase last reconcile likely
  returned `ctrl.Result{}` (no RequeueAfter) and now relies on a watch event that isn't firing.

### Project controller stuck in a 5-min requeue loop (separate but coupled)
- "created clone Job" (SAME name `tide-clone-18bff5a7…`, the run-bootstrap clone) logged
  every ~303s: 19:57:53, 20:02:57, 20:08:00, 20:13:03 … 21:08:36, 21:13:39.
- Fresh `tide-clone-…` + `tide-init-…` Jobs both `Complete`, age ~17–20s → the project is
  re-creating the bootstrap clone every reconcile instead of progressing to succession.
- Project never reaches the milestone-succession check because milestone is still Running.

### Noise (likely not root cause)
- Recurring optimistic-lock errors across phase/plan/task:
  `Operation cannot be fulfilled … the object has been modified` — normal conflict retries.

## Suspected area (NOT yet confirmed — for the debugger to verify against code)
- Phase-11 boundary-push wiring: `internal/controller/boundary_push.go` (8807 b, edited Jun 9 12:41)
  + `internal/controller/phase_controller.go` (succession gate, reporter/ChildCount logic).
- Recent commits on this path: `58ccdc1` (per-wave integration gate + boundary push wiring),
  `8e57348` (per-wave integration job merge-only), `9a14363` (per-wave gate defers to pause-between-waves).
- Defect-B fix history: succession was meant to gate on `ChildCount` (expected vs observed children),
  replacing the old `justMaterialized/hasChildren/reporterSpawned` guards. Verify the Phase
  succession path actually fires `BoundaryDetected → push+succeed` when observed≥expected & all Succeeded,
  and that the Plan→Phase watch enqueues on the Plan status flip.

## Current Focus
- hypothesis: CONFIRMED — succession is unreachable because the planner Job has been garbage-collected (TTLSecondsAfterFinished=600s). The Running branch does `Get(plannerJob)` → NotFound → `return ctrl.Result{}` (no requeue, no error), and ALL succession/boundary-push/succeed logic lives inside `handleJobCompletion`, which is only reachable while the planner Job still exists and is terminal.
- next_action: apply fix — on planner-Job NotFound while level is still Running (not Succeeded/Failed), proceed to the completion path instead of silently bailing.

## Root Cause (CONFIRMED)

**The planner Job TTL (`TTLSecondsAfterFinished: 600`) garbage-collects the Job before slow children finish, and the reconciler treats a missing planner Job as "nothing to do" instead of "Job already completed — run succession."**

Mechanism (Phase level, identical at Milestone + Project):
1. `reconcilePlannerDispatch` Running branch (`phase_controller.go:183-195`):
   ```go
   if ph.Status.Phase == "Running" {
       var job batchv1.Job
       if err := r.Get(ctx, jobName, &job); err != nil {
           if !apierrors.IsNotFound(err) { return ctrl.Result{}, err }
           return ctrl.Result{}, nil   // <-- planner Job GC'd → silent dead-end
       }
       if isJobTerminal(&job) { return r.handleJobCompletion(ctx, ph, &job) }
       return ctrl.Result{}, nil
   }
   ```
2. ALL succession logic (reporter spawn, ChildCount gate, `BoundaryDetected`, `maybeTriggerBoundaryPush`, `patchPhaseSucceeded`) lives inside `handleJobCompletion` — reachable ONLY while the planner Job exists and is terminal.
3. Planner Job TTL = `DefaultTTLSecondsAfterFinished = 600` (`internal/dispatch/podjob/jobspec.go:72,445`). Job finished ~19:57:45Z (reporter spawned) → GC'd ~20:07. The Plan didn't reach Succeeded until 20:03:57Z and the Phase needed a reconcile AFTER that to succeed — but by then the planner Job was gone, so every reconcile hits the NotFound dead-end.
4. `handleJobCompletion`'s Job parameter is `_` (unused) — the Job body is never read, only its terminal-ness gated entry. So the fix is safe: the completion path does not need the Job object.

**Live confirmation:**
- Planner Job `tide-phase-2173dd7b-…-1`: **NotFound** (GC'd).
- Poke probe: `kubectl annotate phase … debug/poke=<ts>` forced a guaranteed reconcile (annotation-changed watch) → Phase stayed `Running`, no succession log. This rules out "watch not firing" and proves the succession logic is **unreachable**.

**Blast radius:** identical pattern at all three planner-dispatch levels:
- `phase_controller.go:185-189`
- `milestone_controller.go:223-227`
- `project_controller.go:710-714`
Any level whose children take >~10 min to fully Succeed loses its succession trigger permanently. (The Defect-B ChildCount gate and the Plan→Phase `Owns(&Plan{})` watch are both correct and NOT the cause — the watch does enqueue, but the reconcile dead-ends before reaching them.)

**Coupled secondary symptom (same root cause, downstream):** Project re-creates the bootstrap clone Job every ~303s because it never advances past its own Running branch — same NotFound dead-end at `project_controller.go:710-714` once its planner Job is GC'd.

specialist_hint: go

## Suggested Fix Direction

In each of the three Running branches, when `Get(plannerJob)` returns NotFound AND the level is still `Running` (i.e. not yet Succeeded/Failed), do NOT `return ctrl.Result{}, nil`. Since `Status.Phase==Running` means the planner Job was already dispatched, a now-missing Job means it completed and was GC'd — so call the completion handler (`handleJobCompletion` / `handleProjectJobCompletion`), which takes an unused `_ *batchv1.Job` and is therefore safe to call with a synthesized/nil Job. The completion handler is already idempotent (reporter-spawn guarded by `IsNotFound`/`AlreadyExists`, ChildCount gate requeues until children are ready).

Idiomatic options, in order of preference:
- **(A) Preferred — fall through to completion on NotFound.** Replace the `return ctrl.Result{}, nil` in the NotFound branch with the completion call. Guards inside `handleJobCompletion` already make re-entry safe.
- **(B) Belt-and-suspenders — also raise/disable the planner-Job TTL.** TTL=600s is too short relative to child-materialization + child-succession latency. But (A) is the real fix; TTL tuning alone just widens the race window.

Recommend (A) as the root-cause fix, with a unit test that drives the Running branch with the planner Job absent and asserts succession fires. Mirror the change across phase + milestone + project controllers.


## Resolution (FIXED — commit 546e84e, fix-shape A)

**Fix (option A — fall through to completion on NotFound-while-Running):** in all
three planner-dispatch Running branches, on `Get(plannerJob)` → NotFound while the
level is still `Running` (not Succeeded/Failed), replaced `return ctrl.Result{}, nil`
with a call to the completion handler:
- `phase_controller.go` → `return r.handleJobCompletion(ctx, ph, nil)`
- `milestone_controller.go` → `return r.handleJobCompletion(ctx, ms, nil)`
- `project_controller.go` → `return r.handleProjectJobCompletion(ctx, project, nil)`

The `*batchv1.Job` param is `_` (unused) in every handler — the envelope is read
from the namespace PVC keyed by the level UID, not the Job — so passing `nil` is
safe. The completion path is idempotent (reporter-spawn guarded by
IsNotFound/AlreadyExists; ChildCount gate requeues until children are ready), so
re-entry after a GC'd Job is harmless. TTL tuning (option B) was NOT applied — (A)
is the root-cause fix; TTL alone only widens the race window.

**Test:** added `internal/controller/planner_job_absent_test.go` — one envtest spec
per level driving the Running branch with the planner Job ABSENT and asserting
succession fires (reporter Job `tide-reporter-<uid>` spawns). Reverting any of the
three production edits turns all three specs RED (verified), proving the tests
target the bug.

**Verification (observed, not on faith):**
- `make build` → BUILD_EXIT=0.
- `go test ./internal/controller/...` (full package, -short) → `ok` (27.8s).
- Bug-catch proof: with all three fixes reverted, the three new specs go
  0 Passed / 3 Failed; restored → 3 Passed.
- `make test` (full unit tier) → MAKE_EXIT=0, zero `--- FAIL`/`FAIL` lines.
- `gofmt -l` clean; `go vet ./internal/controller/...` clean.

Live re-verification on the parked minikube run (UID 18bff5a7) is left for the next
medium run with the rebuilt controller image — the unit layer proves the
succession path is now reachable when the planner Job is absent at every level.
