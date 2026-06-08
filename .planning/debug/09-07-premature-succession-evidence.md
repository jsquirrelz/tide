# Phase 09 live-acceptance evidence — premature-succession race (Option-C async materialization)

Captured: 2026-06-08 (live minikube medium-sample run, fresh Phase-9 images)

## Tree at detection (Project Complete while grandchild Plans Running)
```
KIND        NAME                                        PHASE       CREATED
Project     medium-project                              Complete    2026-06-08T19:19:04Z
Milestone   milestone-01-formattednow-function          Succeeded   2026-06-08T19:23:04Z
Phase       phase-01-implement-formattednow-with-test   Succeeded   2026-06-08T19:24:03Z
Plan        plan-01-implement-formattednow              <none>      2026-06-08T19:25:34Z
Plan        plan-02-add-formattednow-test               <none>      2026-06-08T19:25:34Z
Task        task-01-implement-formattednow              Failed      2026-06-08T19:28:18Z
Task        task-02-update-main-function                Pending     2026-06-08T19:28:18Z
```

## Jobs (planner + reporter per level — reporter mechanism WORKS)
```
NAME                                                    STATUS     COMPLETIONS   DURATION   AGE
tide-init-44afd640-dbeb-4113-823a-6bf9da8c879e          Complete   1/1           3s         59s
tide-milestone-af5b4917-59d1-48ea-8f93-67106877d0b2-1   Complete   1/1           59s        7m6s
tide-phase-c1bdabb6-b68d-4a68-984a-3409fab69e48-1       Complete   1/1           90s        6m7s
tide-plan-236dc81e-9857-44a4-ad5f-4caa51b5605d-1        Failed     0/1           4m36s      4m36s
tide-plan-5b664e7b-2239-4596-916d-3e75bbeeac4c-1        Complete   1/1           2m43s      4m36s
tide-push-44afd640-dbeb-4113-823a-6bf9da8c879e          Failed     0/1           2m14s      2m14s
tide-reporter-236dc81e-9857-44a4-ad5f-4caa51b5605d      Complete   1/1           3s         2m14s
tide-reporter-5b664e7b-2239-4596-916d-3e75bbeeac4c      Complete   1/1           3s         113s
tide-reporter-c1bdabb6-b68d-4a68-984a-3409fab69e48      Complete   1/1           3s         4m37s
tide-task-3244d796-e0cb-438d-84dc-bf34521d20f0-1        Failed     0/1           112s       112s
```

## Project status (Succeeded=True before descendants done; budget empty)
```
phase=Complete conditions=Ready=True(Initialized) AuthoringPlanner=True(PlannerDispatched) Succeeded=True(MilestonesSucceeded)  budget={"windowStart":"2026-06-08T19:28:25Z"}```

## Phase controller log (spawned reporter then fell through to Succeeded before Plans materialized)
```
{"level":"info","ts":"2026-06-08T19:12:10Z","msg":"Starting EventSource","controller":"phase","controllerGroup":"tideproject.k8s","controllerKind":"Phase","source":"kind source: *v1alpha1.Phase"}
{"level":"info","ts":"2026-06-08T19:12:10Z","msg":"Starting EventSource","controller":"phase","controllerGroup":"tideproject.k8s","controllerKind":"Phase","source":"kind source: *v1alpha1.Phase"}
{"level":"info","ts":"2026-06-08T19:12:10Z","msg":"Starting EventSource","controller":"phase","controllerGroup":"tideproject.k8s","controllerKind":"Phase","source":"kind source: *v1.Job"}
{"level":"info","ts":"2026-06-08T19:12:10Z","msg":"Starting EventSource","controller":"phase","controllerGroup":"tideproject.k8s","controllerKind":"Phase","source":"kind source: *v1alpha1.Plan"}
{"level":"info","ts":"2026-06-08T19:12:10Z","msg":"Starting Controller","controller":"phase","controllerGroup":"tideproject.k8s","controllerKind":"Phase"}
{"level":"info","ts":"2026-06-08T19:12:10Z","msg":"Starting workers","controller":"phase","controllerGroup":"tideproject.k8s","controllerKind":"Phase","worker count":2}
{"level":"info","ts":"2026-06-08T19:25:33Z","msg":"spawned reporter Job","controller":"phase","controllerGroup":"tideproject.k8s","controllerKind":"Phase","Phase":{"name":"phase-01-implement-formattednow-with-test","namespace":"tide-sample-medium"},"namespace":"tide-sample-medium","name":"phase-01-implement-formattednow-with-test","reconcileID":"13e7c3ef-597a-4873-a03c-d39325c4502e","job":"tide-reporter-c1bdabb6-b68d-4a68-984a-3409fab69e48","phase":"phase-01-implement-formattednow-with-test"}
{"level":"error","ts":"2026-06-08T19:25:33Z","msg":"Reconciler error","controller":"phase","controllerGroup":"tideproject.k8s","controllerKind":"Phase","Phase":{"name":"phase-01-implement-formattednow-with-test","namespace":"tide-sample-medium"},"namespace":"tide-sample-medium","name":"phase-01-implement-formattednow-with-test","reconcileID":"0c615b47-d81c-45e6-b0f5-0dfd93c68c51","error":"Operation cannot be fulfilled on phases.tideproject.k8s \"phase-01-implement-formattednow-with-test\": the object has been modified; please apply your changes to the latest version and try again","stacktrace":"sigs.k8s.io/controller-runtime/pkg/internal/controller.(*Controller[...]).reconcileHandler\n\t/go/pkg/mod/sigs.k8s.io/controller-runtime@v0.24.1/pkg/internal/controller/controller.go:494\nsigs.k8s.io/controller-runtime/pkg/internal/controller.(*Controller[...]).processNextWorkItem\n\t/go/pkg/mod/sigs.k8s.io/controller-runtime@v0.24.1/pkg/internal/controller/controller.go:437\nsigs.k8s.io/controller-runtime/pkg/internal/controller.(*Controller[...]).Start.func1.1\n\t/go/pkg/mod/sigs.k8s.io/controller-runtime@v0.24.1/pkg/internal/controller/controller.go:312"}
```

## Root cause
Option-C async materialization (plan 09-06): handleJobCompletion spawns a reporter Job and returns.
The succession guard branches: BoundaryDetected -> push+succeed; else justMaterialized||hasChildren -> requeue; else (no children) -> patchSucceeded.
Under async materialization, justMaterialized is only true on the SAME reconcile that spawns the reporter Job.
On the NEXT reconcile (reporter Job exists, justMaterialized=false), if the reporter has not YET created children,
hasChildren=false and the level WRONGLY falls through to patchSucceeded. The Milestone won the race (children appeared in time);
the Phase lost it (Plans appeared ~1s after the fall-through). Fix: gate the 'no children' branch on the reporter Job being Complete.

## Secondary observations
- Reporter RBAC (plan 09-04) was missing 'get projects' (parent UID resolution) AND 'list' on child kinds (idempotency guard lists children). Both patched live during this run; MUST land in SOT + chart + medium sample.
- status.budget == {} on the real path — defect #6 cost rollup may not surface via the tiny-status termination message; needs investigation.

## Confirmed root causes (3 defects surfaced by 09-07 live acceptance)

### Defect A — reporter RBAC (FIXED, committed c1be68c)
tide-reporter Role (plan 09-04) missing `get projects` + `list` on child kinds.
Fixed in chart SOT + template + medium sample.

### Defect B — premature succession race (Option-C async materialization)
The 4 level controllers are INCONSISTENT in how they wait after spawning the
async reporter Job:
- milestone: one-shot `justMaterialized` (true only on the spawn reconcile) → races on the 2nd reconcile
- phase: NO guard → falls straight through to patchSucceeded (this is the one that lost the race in the run)
- plan: `reporterSpawned` early-return
- project: separate path
Because the reporter creates children asynchronously, "no children visible" is
ambiguous (leaf vs reporter-not-done-yet). Levels succeed before descendants exist.

FIX (race-free, uses data the Manager already reads): add `ChildCount int` to
pkg/dispatch.TerminationStub; NewTerminationStub sets it = len(out.ChildCRDs)
(the shims already call NewTerminationStub → no shim change). Each level
controller reads ChildCount (expected) from the tiny status and gates succession:
  expected == 0            -> succeed (genuine leaf)
  observedChildren < expected -> requeue (reporter still materializing)
  observedChildren >= expected -> BoundaryDetected (all Succeeded?) -> push+succeed else requeue
This removes the timing/cache race entirely and replaces the inconsistent
justMaterialized/hasChild/reporterSpawned guards with one uniform rule.

### Defect C — cost rollup dropped on the Option-C path (budget={})
cmd/manager wires podjob.PodStatusEnvelopeReader (reads tiny status from the
dispatch Job termination message — correct). But the controllers call
`if _, err := r.EnvReader.ReadOut(...)` and DISCARD the returned EnvelopeOut —
they read it only for error classification and never RollUpUsage into
Project.Status.budget.costSpentCents. So defect #6's EstimatedCostCents (now
present in the tiny status Usage) is never surfaced → status.budget == {}.

FIX: capture the ReadOut result and roll Usage up to Project.Status.budget
(costSpentCents) on the dispatch-completion path, same as the pre-09-06 inline path did.

## Net
The cross-namespace reporter MECHANISM works (children materialize via the K8s
API, same-namespace out.json read, tiny status rides the termination message).
The remaining work is succession-gating (B) + cost-rollup wiring (C). Both block
a LEGITIMATE Complete (DoD: all descendants Succeeded + costSpentCents > 0).
v1.0.0 retag stays blocked until 09-07 records a legitimate Complete.

## 09-08 re-run (2026-06-08): Phase-9 fixes VALIDATED; next layer surfaced

Phase-9 deliverables PROVEN working end-to-end on the live medium run:
- Succession gate (Defect B): Project/Milestone/Phase/Plan all stayed Running while
  descendants pending — NO premature succession. Cascade drove correctly to Task level
  + derived Waves. The exact 09-07 bug (Project=Complete while Plans Running) did NOT recur.
- Cost rollup (Defect C): status.budget populated and climbed across planner levels
  (8c→24c→45c; tokensSpent 25760→104933). costSpentCents > 0 confirmed.
- Reporter mechanism + RBAC (Defect A): all reporter Jobs Complete, no RBAC errors.

NEXT-LAYER blocker (orthogonal to Phase 9 — task-executor/git layer, pre-existing,
first exposed now that a run legitimately reaches task execution):
- task-01 Failed: `EnsureWorktree: add worktree ... branch=: git worktree: empty branch`.
  The claude-subagent reads its branch from branch.txt beside in.json
  (cmd/claude-subagent/main.go:readBranch); the run branch is project.Status.Git.BranchName
  (tide/run-<project>-<unix>). The task-dispatch envelope-write path did not populate
  branch.txt for the task → empty branch → worktree add fails.
- clone error: `clone failed: ... repository already exists` — clone not idempotent on the
  shared tide-projects PVC (likely stale workspace state from prior runs; a clean DoD run
  may need a fresh/cleaned workspace PVC).

These are NOT Phase-9 (cross-namespace envelope return) scope. Recommend a separate
gap/phase for task-executor branch propagation (branch.txt) + clone idempotency +
clean-workspace handling. Phase-9's mechanism is validated; the v1.0.0 retag DoD
(full legitimate Complete) remains blocked on this task-layer work.

## 09-09 re-run (2026-06-08): branch fix correct, but task-execution layer is a defect CASCADE

The 09-09 EnvelopeIn.Branch fix is merged + unit-tested (buildEnvelopeIn stamps Branch;
claude-subagent passes env.Branch). But the live re-run could NOT reach a task executor —
it failed earlier with a cascade of task-execution-layer defects (all orthogonal to
Phase 9; a separate phase's worth of work):

1. Clone non-idempotent: `tide-push: clone failed: ... repository already exists` — even on
   a FRESHLY wiped tide-projects PVC (the clone Job retries compound; bare repo never lands
   cleanly). pkg/git.Clone (PlainCloneContext, bare) errors when destDir exists; needs
   skip/fetch-if-exists idempotency + Job-retry handling.
2. Fresh-PVC workspace permissions: `tide-push: mkdir /workspace/envelopes/push: permission
   denied`. Recreating the PVC dropped workspace dir perms the prior init had set; the
   workspace-perms setup is not self-healing per-run (init job assumption).
3. Real-Claude malformed child JSON: the Plan planner FAILED parsing its OWN authored child:
   `read child CRDs: parse child file "task-03.json": invalid character 'W' after object
   key:value pair` (cost 27c, 22726 output tokens). LLM-output-robustness / child-CRD parse
   hardening gap — the planner must tolerate or repair malformed model JSON, or constrain output.

NET: Phase 9 (cross-namespace return) + 09-08 (succession + cost rollup) + 09-09 (branch) are
all CORRECT and merged. The medium DoD (legitimate Complete + push) is blocked by the
task-execution/git/clone/LLM-output layer — recommend a dedicated Phase 10 (task-execution
reliability: clone idempotency + workspace-perms + push + child-CRD parse robustness), then
re-run the DoD. v1.0.0 retag stays blocked. NOTE: wiping the PVC introduced issue #2; a clean
run needs proper per-run workspace init, not a manual PVC wipe.
