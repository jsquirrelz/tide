---
status: live-reverify-pending
trigger: "the phase→milestone→project succession + boundary-push trigger"
created: 2026-06-09
updated: 2026-06-09
resolution: fixed-pending-live-reverify  # original stall FIXED (546e84e); 546e84e's fall-through exposed a coupled second root cause (EnvelopeReadFailed on the GC'd-Job path) now ALSO fixed in code + unit-verified; live Project=Complete + boundary push still pending operator re-verify
fix_commit: 546e84e  # + second-root-cause fix committed separately (see Second Root Cause section)
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


---

## SECOND ROOT CAUSE (exposed by live re-verify of 546e84e) — FIXED in code + unit-verified; LIVE Complete still PENDING

**Live re-verify of 546e84e (rebuilt :1.0.0 controller, ns `tide-sample-medium`):**
the original succession stall is **FIXED** — the previously-frozen Phase and
Milestone immediately drove out of the NotFound dead-end. But the fall-through
exposed a **coupled next-layer defect**:

- Phase `phase-01-implement-formatted-now` → **Failed**, condition
  `reason=EnvelopeReadFailed, status=True`:
  `read envelope out ".../envelopes/2173dd7b-.../out.json": no such file or directory`
- Milestone also **Failed** (same class). Project still `Running`.
- Plan + all 3 Tasks remain `Succeeded` — the work genuinely completed.

### Root cause (CONFIRMED against code — Observe First, not on hypothesis alone)

On 546e84e's planner-Job-NotFound-while-Running fall-through path, the completion
handler re-reads the planner's `out.json` envelope from the per-run PVC. But once
the planner Job is TTL-GC'd, that per-run envelope artifact can be **gone** (the
path is wiped by the Project's clone/init re-creation loop — the coupled secondary
symptom documented at line 108 above). The handlers then HARD-FAILED on the
unreadable envelope:

- `phase_controller.go:335` → `return r.patchPhaseFailed(ctx, ph, "EnvelopeReadFailed", ...)`
- `milestone_controller.go:416` → `return r.patchMilestoneFailed(ctx, ms, "EnvelopeReadFailed", ...)`

This `return` fires BEFORE the reporter-spawn, the budget rollup, and the
children-based succession gate (Phase 412-440 / Milestone 502-530, and their
`envReadOK==false` fallback at Phase 442-459 / Milestone 532-549). So a level whose
children ALL Succeeded was falsely marked **Failed** purely because the planner's
own envelope re-read failed.

**Key confirmation:** at the Phase and Milestone planner levels the envelope read is
used ONLY for budget rollup + `ChildCount` — there is NO `out.ExitCode`-based
subagent-failure classification at these levels (the only failure path from the
envelope was `EnvelopeReadFailed` itself). So making the read non-fatal loses no
genuine-failure detection: a real planner failure surfaces as ABSENT children, which
the boundary gate already handles by deferring/not-succeeding.

**Asymmetry that pointed straight at the fix:** the **Project** level
(`project_controller.go:843-847`) ALREADY treats the read as non-fatal (logs
`non-fatal`, continues with `envReadOK=false`) — which is exactly why the user
observed **Project still Running, not Failed**. Phase and Milestone were the two
outliers that hard-failed.

### Fix (mirror the Project level — non-fatal envelope read, defer to children gate)

In `phase_controller.go` and `milestone_controller.go` `handleJobCompletion`, on
`EnvReader.ReadOut` error: log non-fatal and continue with `envReadOK=false`
(instead of `patchPhaseFailed`/`patchMilestoneFailed`). The terminal outcome is then
derived from CHILDREN state via the existing `envReadOK==false` fallback
(`BoundaryDetected` / `hasChild*`): children Succeeded → push + succeed; pending →
requeue; none → leaf-succeed. Cost rollup is already gated on `envReadOK` (so it is
skipped — tolerated-as-absent — when the envelope is unreadable, never a hard-fail).
The Project level was already correct and is unchanged.

### Tests (prove Job-absent + envelope-absent + children-Succeeded → Succeeds, not Fails)

Extended `internal/controller/planner_job_absent_test.go` with two envtest specs
(Milestone + Phase): planner Job ABSENT + envelope ABSENT (no `SetOut` → `ReadOut`
errors) + one owner-ref'd child (Phase/Plan) already `Succeeded` → assert the level
reaches `Succeeded`, NOT `Failed`. The shared `makeProject` now sets
`Gates{Milestone:"auto", Phase:"auto"}` so the succession path is reached (the
default Approve policy otherwise parks at `AwaitingApproval` before the gate — see
`milestone_controller.go:474`). The three original absent-Job specs (assert reporter
spawns) stay green.

### Verification (observed, not on faith)

- `make build` → exit **0** (controller-gen produced no CRD/chart drift — pure
  controller logic + test change, no API type change).
- `go test ./internal/controller/...` (-short, KUBEBUILDER_ASSETS set) → `ok`
  (29.9s); full run **98 Passed / 0 Failed / 1 Skipped** (the `-short` leader-election skip).
- Focused run of the two new specs → `ok` (9.0s), both pass.
- First test draft caught a real fixture bug (milestone parked at `AwaitingApproval`
  on the default Approve gate) → fixed by setting auto gates; re-run green.
- `make test` (full unit tier) → exit **0**, every package `ok`, zero `--- FAIL`/`FAIL`.
- `gofmt -l` clean; `go vet ./internal/controller/... ./api/... ./cmd/...` exit 0.

### STILL PENDING — live re-verify (operator)

Rebuild + reload + roll the controller, re-apply/observe the medium run, and confirm
**Project reaches `Complete` + the boundary push fires** (per-run
`tide/run-medium-project-*` branch lands on the in-cluster `http://` remote;
`costSpentCents > 0`). Until that live Complete is observed, this session is
`live-reverify-pending`, NOT resolved.
