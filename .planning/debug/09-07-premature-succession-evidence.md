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
