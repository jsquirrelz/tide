---
phase: 01-foundation-crds-pkg-dag-controller-scaffold
plan: 06
subsystem: infra
tags: [golang, controller-runtime, reconciler, finalizer, owner-ref, status-conditions, namespace-predicate, envtest, ginkgo, ctrl-01, ctrl-02, ctrl-03, ctrl-05, auth-02, pitfall-1, pitfall-21, pitfall-23]

# Dependency graph
requires:
  - phase: 01-foundation-crds-pkg-dag-controller-scaffold
    provides: "Six kubebuilder-scaffolded internal/controller/*_controller.go skeletons from Plan 01-01; internal/owner.EnsureOwnerRef + internal/finalizer.HandleDeletion + internal/pool.Pool + internal/dispatch.Dispatcher from Plan 01-04; shared status-condition vocabulary in api/v1alpha1/shared_types.go from Plan 01-05"
provides:
  - Six internal/controller/*_controller.go reconcilers at Standard depth (D-C1) — fetch, finalizer-on-delete (5-min bounded), finalizer-ensure-on-create, owner-ref-on-children (child Kinds only), status-condition propagation, Status().Update
  - Uniform Reconciler struct fields across all six Kinds — client.Client, *runtime.Scheme, MaxConcurrentReconciles, PlannerPool, ExecutorPool, Dispatcher (nil in P1), WatchNamespace
  - Per-Kind namespace-filter predicate (predicate.NewPredicateFuncs + WithEventFilter) enforcing AUTH-02 at the controller watch boundary (revision Warning 7)
  - finalizerCleanupTimeout constant (5 min) shared across all six reconcilers — bounded-deadline contract for Pitfall 21
  - Makefile verify-no-blocking target — grep-based Pitfall 1 enforcement on internal/controller/*_controller.go
  - envtest suite extension: TestFinalizerLifecycle on Project, TestOwnerRefCascade on Wave (full Project→Milestone→Phase→Plan→Wave chain), CEL-rejection tests for both
  - RBAC marker upgrade — per-Kind verbs + parent-Kind get/list/watch + batch/jobs CRUD + events create/patch on every reconciler (regenerated config/rbac/role.yaml)
affects: [01-07, 01-08, 01-09, 02-*]

# Tech tracking
tech-stack:
  added: []  # no new module deps — all changes hand-edit existing files
  patterns:
    - "Six-step Reconcile body verbatim per RESEARCH.md §Reconciler Stub Anatomy: fetch, deletion-handler-with-bounded-deadline, finalizer-ensure, owner-ref-to-parent-with-Requeue, dispatcher-nil-guard, status-condition + Status().Update"
    - "Owner-ref-with-Requeue pattern on every child reconciler: Get parent; on NotFound return ctrl.Result{Requeue: true}; on other error surface; on success call internal/owner.EnsureOwnerRef + r.Update. Wave/Task race-with-parent-creation is the motivating case"
    - "Uniform finalizer name convention `tideproject.k8s/<kind>-cleanup` declared as a const per file — never hand-typed inline at the call site"
    - "Shared finalizerCleanupTimeout = 5 * time.Minute constant lives in project_controller.go (the alphabetically-first reconciler) and is referenced by all six. Package-private; not exported"
    - "Namespace-filter predicate is a small inline lambda (predicate.NewPredicateFuncs + WatchNamespace == \"\" short-circuit) — empty WatchNamespace means watch-all-namespaces, which is the default cluster-scoped install posture. Plan 08 main.go reads the value from config and injects it"
    - "Dispatcher seam is an `if r.Dispatcher != nil { /* Phase 2 fills */ }` block in step 5 of every Reconcile body — Phase 2 fills inside without refactoring the surrounding scaffold"
    - "envtest-with-no-GC contract documented in TestOwnerRefCascade body: assertion is owner-refs are wired correctly (Controller=true, BlockOwnerDeletion=true) — real-cluster GC then cascades. envtest doesn't run the garbage-collector controller"

key-files:
  modified:
    - internal/controller/project_controller.go — replaced kubebuilder skeleton with Standard-depth body
    - internal/controller/milestone_controller.go — same + owner-ref to Project
    - internal/controller/phase_controller.go — same + owner-ref to Milestone
    - internal/controller/plan_controller.go — same + owner-ref to Phase
    - internal/controller/task_controller.go — same + owner-ref to Plan
    - internal/controller/wave_controller.go — same + owner-ref to Plan
    - internal/controller/project_controller_test.go — replaced scaffold with TestFinalizerLifecycle + 3 sibling cases
    - internal/controller/wave_controller_test.go — replaced scaffold with TestOwnerRefCascade + 2 sibling cases
    - internal/controller/milestone_controller_test.go — minimal fix: provide valid Spec.ProjectRef for CEL
    - internal/controller/phase_controller_test.go — minimal fix: provide valid Spec.MilestoneRef
    - internal/controller/plan_controller_test.go — minimal fix: provide valid Spec.PhaseRef
    - internal/controller/task_controller_test.go — minimal fix: provide valid Spec.PlanRef + FilesTouched
    - Makefile — append verify-no-blocking gate target
    - config/rbac/role.yaml — regenerated via make manifests with new per-Kind RBAC verbs

key-decisions:
  - "TestOwnerRefCascade asserts owner-ref wiring, not actual cascade GC. envtest runs kube-apiserver + etcd but NOT the garbage-collector controller — so deleting a parent inside envtest never asynchronously deletes its children. The cascade *contract* is that child reconcilers wire controller-owner-refs (Controller=true, BlockOwnerDeletion=true). We verify that contract is met down the entire 5-level chain (Project → Milestone → Phase → Plan → Wave); a real cluster's GC then cascades. Documented inline in the test body so a future reader knows why we don't simply assert IsNotFound on children after Project Delete."
  - "Three-pass reconcile loop in TestOwnerRefCascade rather than two. The reconcile order is: pass 1 adds finalizer + returns; pass 2 sets owner-ref + Updates. Three passes gives slack for resource-version conflicts that an in-process test sometimes hits when two reconcilers touch the same parent within microseconds. Costs ~50ms in test runtime, removes flake potential."
  - "Owner-ref-on-Requeue when parent NotFound, not on error. A real cluster may briefly observe a child created before its parent (creation races, etcd lag). Returning ctrl.Result{Requeue: true} on NotFound lets the controller retry without surfacing a transient error — which would otherwise crash-loop the workqueue counter. The behavior matches the spec's tolerance for level-by-level fan-out: a Wave can land before its Plan reconciler has stamped its finalizer."
  - "Project keeps PlannerPool and ExecutorPool fields even though it never uses them. The struct shape is uniform across all six Kinds so Plan 08's main.go can inject the same Reconciler-shape values without per-Kind special-cases. Plan 08 passes `nil, nil` for both pool fields on Project; the analyzer's pool-field-name check still passes because no code reads from those fields."
  - "Auto-fixed pre-existing scaffold test breakage on Milestone/Phase/Plan/Task (Rule 3 - Blocking). The kubebuilder-scaffolded test files created CRDs with empty Specs; the CEL validations and MinLength markers added by Plan 05 reject those creates with HTTP 422. Minimal fix: add the required parent-ref + FilesTouched. Did not rewrite these tests wholesale — Plan 07 webhook tests will likely touch them again, so leaving the scaffolded test shape preserves the Plan 07 review surface."
  - "Did NOT add the `Named()` builder call when fully replacing SetupWithManager. The original scaffolded form had `.Named(\"project\").`; I kept that line in each new SetupWithManager for fidelity to the kubebuilder convention (the .Named call sets the controller's name for logging/metrics — useful even though optional). Plan 08's manager wiring relies on the metrics-friendly names being stable."

patterns-established:
  - "Standard-depth reconciler template (the six steps + the struct shape) is reusable verbatim. Future CRDs in the project should follow the same template; any structural drift from the six-step form is an automatic review-flag"
  - "Programmatic structural-identity grep loop (Warning 8 acceptance) is the regression net for the six near-identical reconciler files. A future contributor who 'fixes' one reconciler in a non-uniform way is caught by the grep counts diverging from the expected (1, 1, 1, 1, 1) tuple. Document this loop in Plan 09's CI when full RBAC + manifest verification lands"
  - "envtest tests for controller behavior live alongside the *_controller.go file they test (Ginkgo Describe per controller). The BeforeSuite in suite_test.go is shared across the entire package — Plan 07 webhook tests will extend the same BeforeSuite per Warning 9"

requirements-completed:
  - CTRL-01
  - CTRL-02
  - CTRL-03
  - CTRL-05
  - AUTH-02

# Metrics
duration: 6min
completed: 2026-05-12
---

# Phase 1 Plan 06: Reconciler Bodies — Standard Depth + Finalizer + Owner-Ref + Namespace Predicate Summary

**Six kubebuilder-scaffolded reconciler skeletons get hand-edited to the canonical six-step Standard depth pattern (fetch, finalizer-on-delete, finalizer-ensure-on-create, owner-ref-on-children, status conditions, Status().Update), each declaring the uniform Reconciler struct (with Dispatcher nil-guarded for Phase 2's body fill), each wiring a namespace-filter predicate that satisfies AUTH-02 at the controller watch boundary, plus envtest assertions proving finalizer lifecycle + the full Project→Wave owner-ref cascade chain, plus a `make verify-no-blocking` grep gate enforcing Pitfall 1 — Phase 2 now fills the dispatch logic without touching the scaffold.**

## Performance

- **Duration:** ~6 min
- **Started:** 2026-05-12T21:00:27Z
- **Completed:** 2026-05-12T21:06:10Z
- **Tasks:** 2 of 2
- **Files modified:** 14 (six *_controller.go, six *_controller_test.go, Makefile, config/rbac/role.yaml)
- **Files created:** 0

## The Six-Step Reconcile Body

Every reconciler follows this exact shape. The only per-Kind variations are: type name, finalizer-string constant, parent Kind (Project: none; Milestone: Project; Phase: Milestone; Plan: Phase; Task: Plan; Wave: Plan), and the per-Kind RBAC marker verbs.

```go
func (r *<Kind>Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    logger := logf.FromContext(ctx)

    // 1. Fetch.
    var obj tideprojectv1alpha1.<Kind>
    if err := r.Get(ctx, req.NamespacedName, &obj); err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // 2. Handle deletion with bounded-deadline cleanup (CTRL-05, Pitfall 21).
    if !obj.DeletionTimestamp.IsZero() {
        return finalizer.HandleDeletion(ctx, r.Client, &obj, <kind>Finalizer,
            func(_ context.Context) error {
                logger.Info("<kind> cleanup (no-op in Phase 1)", "name", obj.Name)
                return nil
            }, finalizerCleanupTimeout)
    }

    // 3. Ensure finalizer is set on create.
    if !controllerutil.ContainsFinalizer(&obj, <kind>Finalizer) {
        controllerutil.AddFinalizer(&obj, <kind>Finalizer)
        if err := r.Update(ctx, &obj); err != nil { return ctrl.Result{}, err }
        return ctrl.Result{}, nil
    }

    // 4. Ensure owner ref to parent (CRD-02, Pitfall 23). Child Kinds only.
    //    Project skips this step.
    if obj.Spec.<ParentRef> != "" {
        var parent tideprojectv1alpha1.<ParentKind>
        if err := r.Get(ctx, client.ObjectKey{...}, &parent); err != nil {
            if client.IgnoreNotFound(err) == nil { return ctrl.Result{Requeue: true}, nil }
            return ctrl.Result{}, err
        }
        if err := owner.EnsureOwnerRef(&obj, &parent, r.Scheme); err != nil {
            return ctrl.Result{}, err
        }
        if err := r.Update(ctx, &obj); err != nil { return ctrl.Result{}, err }
    }

    // 5. Phase 1: dispatcher seam nil-guarded for Phase 2 (REQ-SUB-01).
    if r.Dispatcher != nil {
        // Phase 2 fills.
    }

    // 6. Update status conditions + persist via Status().Update.
    meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{...})
    if err := r.Status().Update(ctx, &obj); err != nil { return ctrl.Result{}, err }

    return ctrl.Result{}, nil
}
```

## Uniform Reconciler Struct (all six Kinds)

```go
type <Kind>Reconciler struct {
    client.Client
    Scheme *runtime.Scheme

    MaxConcurrentReconciles int     // CTRL-04 — tunable per-Kind

    PlannerPool  *pool.Pool          // POOL-01 — populated for Milestone/Phase/Plan
    ExecutorPool *pool.Pool          // POOL-02 — populated for Wave/Task

    Dispatcher dispatch.Dispatcher   // REQ-SUB-01 — nil in Phase 1; Phase 2 injects

    WatchNamespace string            // AUTH-02 — "" = watch-all-namespaces
}
```

All six Kinds carry the same field set so Plan 08's main.go can wire them uniformly. Project doesn't use either pool, but keeps both nil-able fields for shape uniformity.

## Finalizer Name Table

| Kind      | Constant            | Value                              |
| --------- | ------------------- | ---------------------------------- |
| Project   | `projectFinalizer`  | `tideproject.k8s/project-cleanup`  |
| Milestone | `milestoneFinalizer`| `tideproject.k8s/milestone-cleanup`|
| Phase     | `phaseFinalizer`    | `tideproject.k8s/phase-cleanup`    |
| Plan      | `planFinalizer`     | `tideproject.k8s/plan-cleanup`     |
| Task      | `taskFinalizer`     | `tideproject.k8s/task-cleanup`     |
| Wave      | `waveFinalizer`     | `tideproject.k8s/wave-cleanup`     |

All use the made-up `.k8s` TLD per D-A3 (NEVER `tide.io`). Bounded cleanup deadline: `finalizerCleanupTimeout = 5 * time.Minute` (declared once in `project_controller.go`).

## Pool Field Assignment Matrix

| Reconciler | PlannerPool used? | ExecutorPool used? | Phase 2 dispatch path |
| ---------- | ----------------- | ------------------ | --------------------- |
| Project    | no (nil)          | no (nil)           | drives Milestone fan-out (planner)
| Milestone  | yes               | no                 | dispatches planner subagents
| Phase      | yes               | no                 | dispatches planner subagents
| Plan       | yes               | no                 | dispatches planner subagents
| Task       | no                | yes                | dispatches executor Job
| Wave       | no                | yes                | dispatches executor Jobs (sole producer of Tasks per D-B1)

In Phase 1 all six fields are nil. Plan 08's main.go calls `pool.New(cfg.PlannerConcurrency, "planner")` + `pool.New(cfg.ExecutorConcurrency, "executor")` once each and passes the pointers to the Reconcilers it instantiates.

## AUTH-02 Namespace Predicate Wiring

Every `SetupWithManager` declares:

```go
nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
    if r.WatchNamespace == "" {
        return true // watch-all-namespaces mode (default cluster-scoped install)
    }
    return obj.GetNamespace() == r.WatchNamespace
})
return ctrl.NewControllerManagedBy(mgr).
    For(&tideprojectv1alpha1.<Kind>{}).
    Owns(&batchv1.Job{}).            // CTRL-02
    WithEventFilter(nsPred).          // AUTH-02 (revision Warning 7)
    WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).  // CTRL-04
    Named("<kind>").
    Complete(r)
```

Plan 08 main.go reads `WatchNamespace` from CLI flag / config and injects per-Reconciler. The runtime predicate satisfies AUTH-02 in Phase 1 without requiring per-namespace RoleBindings (which land in Phase 5 per Warning 7).

## envtest Setup Notes

- BeforeSuite was already scaffolded by kubebuilder and registers the v1alpha1 scheme + starts envtest from `config/crd/bases`. No edits needed.
- Plan 07 webhook tests will reuse this same BeforeSuite per revision Warning 9 — adding webhook fixtures to the existing `testEnv` rather than spinning up a parallel suite.
- envtest does NOT run the garbage-collector controller. TestOwnerRefCascade asserts owner-ref wiring (Controller=true + BlockOwnerDeletion=true on every child); a real cluster's GC cascades. Documented inline so a future reader doesn't try to assert `IsNotFound` on children after Project Delete.
- Each test that creates resources cleans up by driving the relevant reconciler to remove the finalizer before issuing Delete — envtest's lack of GC means leftover Terminating objects accumulate otherwise. ~5s polling deadlines per Eventually.

## Makefile Gate Added

```makefile
.PHONY: verify-no-blocking
verify-no-blocking: ## Assert no time.Sleep or <-time.After in reconciler bodies (Pitfall 1).
	@echo "verifying no time.Sleep or <-time.After in Reconcile bodies (Pitfall 1)..."
	@MATCHES=$$(grep -nE 'time\.Sleep|<-time\.After' internal/controller/*_controller.go || true); \
	if [ -n "$$MATCHES" ]; then \
		echo "Pitfall 1 violation: blocking I/O in reconcile body:"; \
		echo "$$MATCHES"; \
		exit 1; \
	fi
	@echo "OK: no blocking I/O in reconcile bodies"
```

Greps `internal/controller/*_controller.go` — scoped to production reconciler files only. Test files legitimately use `time.Duration` literals with `Eventually(...)` polling and would false-positive a `*.go` glob.

## envtest Suite Inventory

| Spec | Suite | Purpose |
| --- | --- | --- |
| `accepts a valid Project CRD apply` | Project | TestCRDsAccept — happy-path apply |
| `rejects a Project with an invalid targetRepo (CEL XValidation)` | Project | CRD-03 — CEL admission rejection |
| `sets the finalizer on create (CTRL-05)` | Project | CTRL-05 — finalizer-ensure path |
| `removes finalizer on deletion (TestFinalizerLifecycle, Pitfall 21)` | Project | Pitfall 21 — bounded-deadline cleanup |
| `accepts a valid Wave with PlanRef and non-negative WaveIndex` | Wave | CRD-01, CRD-03 — schema apply |
| `rejects a Wave with WaveIndex=-1 (CEL Minimum=0)` | Wave | CRD-03 — CEL Minimum |
| `Owner-ref cascade: child reconcilers wire controller owner-refs (TestOwnerRefCascade, Pitfall 23)` | Wave | CRD-02 — owner-ref chain |
| Milestone scaffolded happy-path | Milestone | sanity check (scaffold-shape) |
| Phase scaffolded happy-path | Phase | sanity check |
| Plan scaffolded happy-path | Plan | sanity check |
| Task scaffolded happy-path | Task | sanity check |

11 specs total. Wall time ~8s sequential, ~11s with `-race`. Well inside TEST-01's 30s budget.

## Task Commits

| Task | Name | Commit | Files |
| --- | --- | --- | --- |
| 1 | Hand-edit six reconcilers to Standard depth | `2ae2b24` | 11 files (six *_controller.go + four scaffold-test fixes + config/rbac/role.yaml regen) |
| 2 | verify-no-blocking gate + envtest finalizer/cascade tests | `d7a8cc8` | 3 files (Makefile + project_controller_test.go + wave_controller_test.go) |

**Plan metadata commit:** _(after SUMMARY + STATE + ROADMAP update)_

## Phase 2 Hand-off

Every Reconcile body in Plan 06 has step 5 stubbed as:

```go
if r.Dispatcher != nil {
    // Phase 2 fills.
}
```

Phase 2's REQ-SUB-01 plan fills inside that guard with `Subagent.Run()` calls + Job creation via `r.ExecutorPool.Acquire` / `r.PlannerPool.Acquire`. The WaveReconciler is the first call site to actually run the dispatch path because Wave is the operational level of Phase 2's dispatch contract (D-B1: WaveReconciler is the sole producer of Wave objects). Owner refs are already wired by Plan 06 — Phase 2 inherits the cascade behavior. The dispatcher seam being committed in Phase 1 means Phase 2 does not refactor any reconciler struct field; it only fills the body inside the existing nil-guard.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] kubebuilder-scaffolded test files crashed on CEL/MinLength validation**

- **Found during:** Task 1 (first `make test` run)
- **Issue:** The four kubebuilder-scaffolded test files (`milestone_controller_test.go`, `phase_controller_test.go`, `plan_controller_test.go`, `task_controller_test.go`) create CRDs with empty Specs in their BeforeEach. The CEL XValidation rule and MinLength markers added by Plan 05 reject those creates with HTTP 422. None of those tests would compile through the suite before this plan's edits.
- **Fix:** Provide valid Spec values matching the per-Kind required fields (ProjectRef on Milestone; MilestoneRef on Phase; PhaseRef on Plan; PlanRef + FilesTouched on Task). Minimal edit — kept the scaffolded test shape and added only the required spec fields.
- **Files modified:** `internal/controller/{milestone,phase,plan,task}_controller_test.go` (BeforeEach blocks only)
- **Verification:** `KUBEBUILDER_ASSETS=... go test ./internal/controller/... -count=1 -timeout 90s` passes 11/11 specs in ~8s.
- **Committed in:** `2ae2b24` (Task 1)

**2. [Rule 1 - Bug] TestOwnerRefCascade initially asserted IsNotFound on cascaded children**

- **Found during:** Task 2 (first run of the wave_controller_test.go suite)
- **Issue:** The initial test body issued `k8sClient.Delete(ctx, project)` and then `Eventually` polled for `IsNotFound` on Wave (expecting cascade to delete all children). The Eventually timed out at 15s because envtest runs kube-apiserver + etcd but NOT the garbage-collector controller — deletion of a parent in envtest does NOT asynchronously delete its children. The test was asserting a behavior envtest cannot exhibit.
- **Fix:** Reshape the test assertion to verify the owner-ref *contract* down the chain: every child reports a controller-owner-ref to its parent (Controller=true, BlockOwnerDeletion=true). That is the contract the cascade rests on; a real cluster's GC will then cascade. Documented inline so a future reader understands the envtest limitation.
- **Files modified:** `internal/controller/wave_controller_test.go`
- **Verification:** Full suite passes 11/11 in ~8s; cascade test takes ~1.2s.
- **Committed in:** `d7a8cc8` (Task 2)

---

**Total deviations:** 2 auto-fixed (1 Rule 3 - Blocking, 1 Rule 1 - Bug)

**Impact on plan:** Both adjustments are mechanical. The structural-identity grep block from Warning 8 still passes; the AUTH-02 namespace predicate from Warning 7 still wires per Reconciler; the six-step Reconcile pattern is verbatim from RESEARCH.md.

## Authentication Gates

None — Phase 1 introduces no external service dependencies.

## Known Stubs

The cleanup callback in every reconciler's `HandleDeletion` call site is a deliberate no-op in Phase 1:

```go
func(_ context.Context) error {
    logger.Info("<kind> cleanup (no-op in Phase 1)", "name", obj.Name)
    return nil
}
```

- **Why:** Phase 1 doesn't create Jobs — there's nothing to clean up. The bounded-deadline finalizer call site is wired so Phase 2's dispatch logic can replace the callback body with actual Job teardown without restructuring the Reconcile loop.
- **Resolving plan:** Phase 2's REQ-SUB-01 plan replaces the no-op with an idempotent Job cleanup that lists + deletes child Jobs labeled with the Task UID.

The `if r.Dispatcher != nil { /* Phase 2 fills */ }` block is the second deliberate stub — covered in Plan 04's SUMMARY (the `internal/dispatch.Dispatcher` interface is empty in Phase 1).

## Issues Encountered

- **envtest BinaryAssetsDirectory wasn't pre-installed.** First `go test ./internal/controller/...` run failed with `fork/exec /usr/local/kubebuilder/bin/etcd: no such file or directory`. Resolution: `make setup-envtest` downloaded the etcd + kube-apiserver binaries to `bin/k8s/1.36.0-darwin-amd64/`; future runs set `KUBEBUILDER_ASSETS=/Users/justinsearles/Projects/tide/bin/k8s/1.36.0-darwin-amd64`. The `make test` target sets this automatically; the manual incantation only matters when running `go test` directly.
- **The `Owns(&batchv1.Job{})` grep count per file is 2 (not 1).** The doc comment on `SetupWithManager` mentions `Owns(&batchv1.Job{})` inline, and the actual `.Owns(&batchv1.Job{})` builder call is below it. Acceptance criterion is satisfied (each file contains the string at least once); the structural-identity grep loop uses `-l | wc -l` for file-count, not the raw `-c` per-file count.

## User Setup Required

None — all changes are file edits + envtest binaries (downloaded by `make setup-envtest` which is part of `make test`). No external API keys, no cluster, no service accounts.

## Next Phase Readiness

**Ready for Plan 01-07 (webhook bodies):**
- Plan 07 will add validating + conversion webhook scaffolds. The webhook suite reuses this plan's `suite_test.go` `BeforeSuite` per Warning 9 — the testEnv already has CRDs and v1alpha1 registered; Plan 07 adds a `WebhookInstallOptions` block to the same `testEnv` and a fresh webhook server.
- The Plan webhook will use `pkg/dag.ComputeWaves` for cycle detection; the Wave webhook will reject any client-applied Wave (D-B1) by checking the absence of the WaveReconciler-stamped owner-ref.

**Ready for Plan 01-08 (Manager wiring):**
- `cmd/manager/main.go` constructs six Reconciler instances and calls `SetupWithManager` on each.
- For each Reconciler, main.go injects: `MaxConcurrentReconciles` from `cfg.MaxConcurrentReconciles.<Kind>`; the appropriate Pool pointers (Milestone/Phase/Plan get `&plannerPool`, Wave/Task get `&executorPool`, Project gets both nil); `Dispatcher` stays nil in Phase 1; `WatchNamespace` from `--watch-namespace` flag or empty for watch-all-namespaces.
- Two `pool.PreCharge` calls run after cache sync, before `mgr.Start` — selectors `tideproject.k8s/role=planner` and `tideproject.k8s/role=executor` per Plan 04 SUMMARY.

**Ready for Plan 01-09 (RBAC + CI gates):**
- Plan 09 must verify the per-Kind RBAC markers (already landed here via the `+kubebuilder:rbac:` blocks); the regenerated `config/rbac/role.yaml` from `make manifests` captures the union. Plan 09 will add the no-wildcards CI check + the structural-identity grep loop from revision Warning 8.

**Phase 2 hand-off:**
- The `Dispatcher dispatch.Dispatcher` field on every Reconciler struct is the seam Phase 2's REQ-SUB-01 fills with the real Subagent interface. The placeholder lives in `internal/dispatch/doc.go` per Plan 04.
- The `if r.Dispatcher != nil { /* Phase 2 fills */ }` block in step 5 of every Reconcile body is where Phase 2's body fill lands — no struct refactor required.

**Concerns / watch-items:**
- `TestOwnerRefCascade` asserts owner-ref wiring, not actual cascade GC. If Phase 5's distribution pipeline starts running kind-based E2E tests (it should), an analogous E2E test that asserts real GC cascade should land alongside.
- The 5-minute `finalizerCleanupTimeout` is a default; Phase 2's dispatch logic with real Job cleanup may need to tune this per-Kind (a Wave with 100 Tasks might need 10 minutes). Plan 06 commits a single shared constant; per-Kind override is a future surface.
- The `Named()` call in `SetupWithManager` ("project", "milestone", etc.) sets the controller's metric-friendly name. Phase 4's dashboard will key off these names — keep them stable across Phase 1→2 transitions.

## Self-Check: PASSED

- All claimed commits exist:
  - `2ae2b24` Task 1 (six reconcilers + four scaffold-test fixes + role.yaml regen)
  - `d7a8cc8` Task 2 (verify-no-blocking + project/wave envtest)
- All claimed files modified:
  - `internal/controller/{project,milestone,phase,plan,task,wave}_controller.go` ✓
  - `internal/controller/{project,milestone,phase,plan,task,wave}_controller_test.go` ✓
  - `Makefile` (verify-no-blocking target appended) ✓
  - `config/rbac/role.yaml` (regenerated) ✓
- Verification commands all exit 0:
  - `go build ./...` ✓
  - `go vet ./...` ✓
  - `KUBEBUILDER_ASSETS=... go test ./internal/controller/... -count=1 -timeout 90s` (11/11 specs pass, ~8s) ✓
  - `KUBEBUILDER_ASSETS=... go test ./internal/controller/... -count=1 -race -timeout 120s` (~11s) ✓
  - `make verify-no-blocking` ✓
  - `make verify-no-aggregates` ✓ (no regression)
  - `make verify-no-sqlite-dep` ✓
  - `make verify-dag-imports` ✓
  - `make tide-lint` ✓
- Structural-identity grep checks all pass:
  - `grep -l "Owns(&batchv1.Job{})" internal/controller/*_controller.go | wc -l` returns **6** ✓
  - `grep -l "github.com/jsquirrelz/tide/internal/finalizer" internal/controller/*_controller.go | wc -l` returns **6** ✓
  - `grep -l "owner.EnsureOwnerRef" internal/controller/{milestone,phase,plan,task,wave}_controller.go | wc -l` returns **5** ✓
  - `grep -c "owner.EnsureOwnerRef" internal/controller/project_controller.go` returns **0** (Project has no parent) ✓
  - Per-file unique grep counts for the load-bearing constructs are uniform across the six files
- Anti-checks pass:
  - `grep -nE "time\.Sleep|<-time\.After" internal/controller/*_controller.go` returns empty (Pitfall 1) ✓
  - `grep -E 'verbs="?\*"?' internal/controller/*.go` returns empty (no wildcard verbs) ✓
  - `grep -rE "tide\.io|my\.domain" --include="*.go" internal/controller/` returns empty (D-A3) ✓

---
*Phase: 01-foundation-crds-pkg-dag-controller-scaffold*
*Completed: 2026-05-12*
