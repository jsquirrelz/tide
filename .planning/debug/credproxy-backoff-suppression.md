---
slug: credproxy-backoff-suppression
status: root_cause_found
trigger: |
  Cascade-8 surfaced after Plan 02.2-09's cascade-7 helper fix closed the "no project found" gate for caps/output/failure namespaces. The credproxy_test.go HARN-03 spec still fails: the task controller runs EXACTLY ONE reconcile attempt for credproxy-task at t=0 (hitting a ResourceVersion conflict because PlanReconciler.stampTaskLabels is concurrently updating the same Task), then controller-runtime exponential-backoff suppresses re-reconciliation for ~127s. The 120s `Eventually` pod-wait expires before backoff releases. Three timeout bumps (60s→120s→[proposed 240s]) are tactically accommodating this race rather than fixing it. Decide between Option α (tactical: bump pod-wait 120s→240s in Plan 02.2-10) vs Option β (durable: production-side debounce/coalesce in plan_controller.go or task_controller.go) BEFORE committing to Plan 02.2-10's scope.
created: 2026-05-14
updated: 2026-05-14
phase_context: 02.2-layer-b-kind-test-timing-fixes
related_artifact: .planning/phases/02.2-layer-b-kind-test-timing-fixes-bump-kindtesttimeout-from-4mi/02.2-09-VERIFICATION.md
prior_debug_session: .planning/debug/credproxy-dispatch-guard.md  # cascade-5 investigation; same test spec, different gate
cascade_classification: spec-flake-with-production-root-cause  # REVISED: misclassification — true class is production-wiring-gap
---

# Debug: credproxy-task exponential-backoff suppression (cascade-8)

## Symptoms

**Expected behavior:** After Plan 02.2-09's cascade-7 fix landed (createProjectHierarchy applied to all 4 fixtures), the credproxy HARN-03 spec should reach Pod creation within its 120s `Eventually` pod-wait budget, then proceed to PASS. All 7 Layer B specs should PASS clean + rerun in ≤ 600s.

**Actual behavior:** Layer A 18/18 PASS (27.18s). Layer B BeforeSuite PASS (66s). HARN-03 spec 1 (credproxy "Pod should be created") FAILS at 146.761s (120s `Eventually` exhausted). Ginkgo `-timeout=5m` panic fires before remaining 6 specs run. Total wall 303s, exit code 1.

**Error messages:**
- `[FAILED] A Pod should be created for the credproxy-task Job` — `Timed out after 120.000s. Expected an error to have occurred. Got: <nil>: nil`
- `panic: test timed out after 5m0s` (Ginkgo inner cap)
- Manager log at 17:58:49Z (spec start): `"Operation cannot be fulfilled on plans.tideproject.k8s \"credproxy-plan\": the object has been modified"` AND `"Operation cannot be fulfilled on tasks.tideproject.k8s \"credproxy-task\": the object has been modified"` (both ResourceVersion conflicts simultaneously)
- Manager log between 17:58:49Z and 18:00:56Z (127 seconds): **ZERO additional task controller reconcile entries for credproxy-task**. The exponential-backoff cycle holds it off the work queue for the entire interval. **[REVISED — see Root Cause]: this 127s of silence is not backoff suppression; it is "no reconcile work to do" because the dispatch path is gated off in production.**

**Timeline:**
- Plan 02.2-07 (cascade-5 fix) — credproxy pod-wait was 60s; failed.
- Plan 02.2-08 (cascade-6 fix) — bumped to 120s; still failed (this was the original "spec-flake" classification, but the 120s observation = full budget already hinted at a deeper issue).
- Plan 02.2-09 — credproxy still consumes the full 120s budget. Manager log evidence isolates the cause to ResourceVersion conflict → exponential-backoff. T-02.2-21 marginal flag fires (third consecutive full-budget consumption).

**Reproduction:** `make test-int` (single command). The credproxy specs deterministically trip the backoff every run because the PlanReconciler.stampTaskLabels and TaskReconciler.status-update fire near-simultaneously at plan creation time.

## Pre-investigation hypothesis

**The production-side race:**

1. Test fixture creates Plan + Task via `applyYAML` (single multi-doc apply; both CRDs land near-simultaneously)
2. PlanReconciler picks up the Plan, runs `stampTaskLabels`, fetches the Task, updates its labels (`tideproject.k8s/project`, `tideproject.k8s/wave-index`, etc.), calls `client.Update(task)` — this bumps Task's ResourceVersion
3. TaskReconciler ALSO picks up the Task in parallel, runs `resolveProject` (succeeds), proceeds toward status update or annotation
4. The two controllers' Updates race on the SAME Task object; one wins, the other gets `"the object has been modified"` ResourceVersion conflict from the apiserver
5. The loser's reconcile returns the conflict error, which controller-runtime treats as a transient retryable error and schedules a re-queue with exponential-backoff (default: starts at 5ms, doubles up to 1000s; with multiple consecutive failures the backoff grows quickly)
6. If the race repeats on retry (it can — PlanReconciler may still be running its wave materialization), backoff keeps growing
7. Within 120s, the credproxy-task is held off the queue for the entire HARN-03 pod-wait window

**Why this is a DESIGN-level issue, not a test-fixture bug:**
- The hierarchy is correct (Project + Plan + Task all present)
- `resolveProject` succeeds at the one reconcile attempt that runs
- The dispatch decision is sound — only the timing/serialization of CRD updates causes the conflict
- Same race would affect any production workload where a Plan and its Tasks are created in the same batch

**Why timeout bumps tactically work but don't fix the cause:**
- 60s budget → 60s of suppression → fails (Plan 02.2-07)
- 120s budget → 120s of suppression → still fails because backoff went beyond 120s (Plan 02.2-09)
- 240s budget → most likely closes by accommodation, but if cluster is under load or other reconcilers add pressure, backoff could grow further
- This is the classic "test-driven over-accommodation" anti-pattern — production behavior is questionable; test budgets grow to accommodate it

## Fix landscape (to be evaluated)

**Option α (tactical, Plan 02.2-10):** Bump `credproxy_test.go` HARN-03 pod-wait `120*time.Second → 240*time.Second`. Mirror Plan 02.2-08's cascade-6 fix shape exactly. Test-only change; production code untouched.

**Option β (durable, Plan 02.2-10):** Address the production-side race. Several candidate sub-options:
- **β-1: Use Task `/status` subresource.** If Task has a `+kubebuilder:subresource:status` marker (or can be added in this plan), status updates go to a separate endpoint (`/status`) that doesn't conflict with spec-level updates (labels/annotations). The PlanReconciler updates spec; TaskReconciler updates status; no ResourceVersion conflict.
- **β-2: Serialize PlanReconciler.stampTaskLabels via owner-ref-based predicate.** Have TaskReconciler skip reconciliation until the Plan's `Status.LabelsStamped: true` condition fires (PlanReconciler sets this AFTER stampTaskLabels completes). Adds a state field but eliminates the race.
- **β-3: Coalesce/debounce stampTaskLabels.** Have PlanReconciler queue label-stamping into a single batched update per Plan reconcile cycle, ensuring TaskReconciler doesn't see the Task until labels are stable. Requires careful work-queue handling.

**Option γ (hybrid, smaller surface than full β):** Reduce controller-runtime's default backoff for ResourceVersion conflicts via `reconcile.Result{RequeueAfter: 1*time.Second}` on the conflict-error return path. The conflict is genuinely retryable; aggressive 1s requeue (instead of 5ms→exponential) would close cascade-8 with minimal code surface. Test-time fix in production code; preserves the controllers' independence.

## Current Focus

- hypothesis: `internal/controller/plan_controller.go` `stampTaskLabels` and `internal/controller/task_controller.go` reconcile-flow race on the Task object's ResourceVersion. When the conflict surfaces, controller-runtime's default exponential-backoff (likely the `workqueue.DefaultControllerRateLimiter()` shape: 5ms baseline, exponential growth) holds the failing reconcile off the queue for >120s.
- test: Read `plan_controller.go` Reconcile loop end-to-end; locate `stampTaskLabels` — what fields does it update on the Task? Read `task_controller.go` Reconcile loop — does it update Task spec, annotations, or status? Are status updates already using a separate subresource? If so, why is there still a conflict? If not, β-1 is a small fix. Investigate the work-queue rate-limiter wiring in `cmd/manager/main.go` or `internal/controller/setup.go`.
- expecting: Confirmation that (a) stampTaskLabels modifies spec/labels (not status subresource), (b) TaskReconciler's update path also touches spec/labels (else there'd be no conflict on a status-only update), (c) the work-queue uses controller-runtime's default rate-limiter without custom backoff caps. If all 3 are true, β-1 (status subresource) is the smallest durable fix.
- next_action: Investigate the code paths to confirm/refute the hypothesis, then quantify each fix option's actual surface (lines changed, files touched, test impact). Output a Root Cause Report comparing Option α / β-1 / β-2 / β-3 / γ with concrete file:line citations.

## Evidence

- timestamp: 2026-05-14 — Plan 02.2-09 Task 2 clean run produced `02.2-09-VERIFICATION.md` (gate_decision: BLOCKED, cascade-8 classification: spec-flake/production-root-cause). §Section 5 has the full root-cause sketch.
- timestamp: 2026-05-14 — Manager log evidence: at 17:58:49Z, BOTH plan reconciler AND task reconciler hit ResourceVersion conflict simultaneously. Between 17:58:49Z and 18:00:56Z (127s), ZERO task reconciles for credproxy-task. Clean-run log at `/tmp/02.2-09-clean-run.log`.
- timestamp: 2026-05-14 — T-02.2-21 marginal flag fires 3 consecutive runs (02.2-07/08/09 all consumed full credproxy pod-wait budget at 60s, 120s, 120s respectively).
- timestamp: 2026-05-14 — **NEW evidence from cascade-8 investigation:** kind-controller log (`/var/folders/.../tide-controller-manager-544b984b59-ppdvg/manager/0.log`) shows ONLY 2 reconcile events for the credproxy-test namespace during the 127s window: one Plan conflict (17:58:49.178Z) and one Task conflict (17:58:49.181Z). The subsequent 127s of silence is NOT followed by retry log entries (no V(5) success entries either, but more critically, NO further conflict log entries). This is inconsistent with continuous exponential-backoff cycling against a real race.
- timestamp: 2026-05-14 — **NEW evidence:** `clean_run_specs_passed: 0` across plans 02.2-01, -04, -05, -06, -07, -08, -09 for Layer B kind specs. **No Layer B kind spec has ever observed a successful Pod creation in any prior plan.** Only 02.2-03 shows non-zero passes (17/18), but those are **Layer A envtest** passes (mocked apiserver, in-process); zero confirmation of end-to-end Layer B dispatch ever working.

## Eliminated

- hypothesis: cascade-8 is another missing-hierarchy harness-bug — eliminated. Plan 02.2-09 confirmed `resolveProject` succeeds for credproxy-task; the dispatch decision proceeds beyond that gate.
- hypothesis: cascade-8 is a real timeout-too-tight spec-flake (cascade-6 class) — partially eliminated. The 120s budget IS consumed in full, but evidence shows the budget is consumed by SUPPRESSION (zero reconciles for 127s), not by long-running dispatch operations. A pure timeout bump would close the symptom but not the cause.
- hypothesis: cascade-8 is a kind-infra issue (cluster networking, image-pull) — eliminated. Manager pod stays `1/1 Running` during the spec; no infra errors in logs; the failure is internal to the controller's reconcile cycle.
- hypothesis: cascade-8 is a flag-mismatch (cascade-2/3 regression) — eliminated. Manager log shows zero `flag provided but not defined` errors. All 5 chart args still defined in cmd/manager/main.go.
- **hypothesis: cascade-8 is a ResourceVersion-conflict + exponential-backoff suppression race — ELIMINATED by code investigation.** See Resolution section below for the corrected root cause.
- **hypothesis: β-1 (Task /status subresource) is the smallest durable fix — ELIMINATED.** Both Task (`api/v1alpha1/task_types.go:126`) and Plan (`api/v1alpha1/plan_types.go:57`) already have `+kubebuilder:subresource:status` markers. Status subresource is already enabled; β-1 is a no-op.

## Resolution

- root_cause: **The `Dispatcher` field on both `PlanReconciler` and `TaskReconciler` is never assigned in `cmd/manager/main.go` (lines 220–228 for Plan; lines 240–256 for Task).** Both controllers' Phase 2 dispatch code paths are gated behind `if r.Dispatcher != nil` checks (`plan_controller.go:121`, `task_controller.go:167`), so in production these gates are always FALSE. Consequence: `PlanReconciler.reconcileWaveMaterialization` never runs (no Waves materialized, no Task labels stamped), and `TaskReconciler.reconcileDispatch` never runs (no Job ever created at step 11 line 393). The credproxy HARN-03 spec at `test/integration/kind/credproxy_test.go:95` waits 120s for a Pod that the controller is structurally incapable of creating. The ResourceVersion conflicts at 17:58:49Z are incidental noise from concurrent finalizer/owner-ref updates between the two TaskReconciler r.Update sites (lines 139 and 160); they are not the cause of the 127s silence — the silence is "no reconcile work to do" because the dispatch paths are switched off.
- fix: **None of Option α / β-1 / β-2 / β-3 / γ as originally framed will close this cascade.** The correct fix is structural: instantiate `internal/dispatch/podjob.PodJobBackend{}` in `cmd/manager/main.go` and assign it to the `Dispatcher` field on the Plan and Task reconciler structs. Surface estimate: ~10–15 lines added to `cmd/manager/main.go`; zero production-controller-logic changes; the existing `reconcileWaveMaterialization` and `reconcileDispatch` bodies become live. Call this **Option δ (Dispatcher wiring)**. See Section "Fix landscape (corrected)" below for full comparison.
- verification: pending Plan 02.2-10 (proposed scope: wire Dispatcher in cmd/manager/main.go + verify HARN-03 reaches Pod creation; do NOT bump pod-wait timeout)
- files_changed: []  # investigation only; no production code changed yet

## Fix landscape (corrected by investigation)

**Option α (timeout bump 120s → 240s):** REJECTED. Will not close cascade-8. The 127s of silence is structural ("no work to do"), not bounded backoff. Bumping the test's pod-wait to 240s, 1000s, or 1 hour cannot succeed — there is no Pod-creation code path running. This option also accommodates a hypothesized production defect that does not exist as described.

**Option β-1 (Task /status subresource):** REJECTED — already implemented. Both Task and Plan CRDs already have `+kubebuilder:subresource:status` markers (`api/v1alpha1/task_types.go:126` and `api/v1alpha1/plan_types.go:57`). Status updates already go through the `/status` subresource. β-1 is a no-op.

**Option β-2 (owner-ref predicate to gate TaskReconciler on Plan.Status.LabelsStamped):** REJECTED — addresses a race that is incidental, not load-bearing. The race produces cosmetic log noise but does NOT cause the test failure. Even with perfect serialization between Plan and Task reconcilers, no Pod would be created because the dispatch path is gated off.

**Option β-3 (debounce stampTaskLabels into a single batched update):** REJECTED — same reason as β-2; addresses incidental noise, not the dispatch-path-not-running root cause.

**Option γ (per-conflict explicit RequeueAfter: 1s):** REJECTED — same reason; the conflicts are incidental.

**Option δ (Dispatcher wiring in cmd/manager/main.go) — RECOMMENDED:**
- Files touched: `cmd/manager/main.go` (only)
- Lines added: ~10–15 (instantiate `PodJobBackend`, assign to PlanReconciler.Dispatcher and TaskReconciler.Dispatcher)
- Blast radius: production wiring change; activates Phase 2 dispatch code paths that have been gated off since Phase 2 plans landed
- Architectural concerns:
  - **None** with the two-DAG split — the dispatch path was DESIGNED to run; wiring it does not alter the planning vs execution DAG separation
  - **None** with the wave-boundary failure contract — the failure semantics in `task_controller.go` reconcileDispatch (steps 1–12) preserve `failed task → siblings continue; dependents never dispatch` once the path is live
  - **None** with CRD-`.status`-only persistence — no new persistence is added
  - **Mild** with resumability: the dispatch path uses `Status.Phase`, `Status.Attempt`, etc. which are already part of the reconciler's state machine; resumption state (indegree map + completed-task set) stays minimal
- Does it actually fix cascade-8? **YES.** With Dispatcher wired:
  1. `PlanReconciler.reconcileWaveMaterialization` runs once plan.Status.ValidationState == "Validated" (must verify a second gap: is ValidationState ever set to "Validated"? See "Secondary risk" below)
  2. `TaskReconciler.reconcileDispatch` runs on every reconcile of an unfinished Task
  3. Step 11 (line 393) builds the Job spec and creates it
  4. The Pod is created within a few seconds; HARN-03's 120s budget is enormously generous (would PASS in well under 30s)
- Implementation sketch (no work yet, just the shape):
  ```go
  // After line 188 of cmd/manager/main.go (where envReader is constructed):
  dispatcher := &podjob.PodJobBackend{
      Client:         mgr.GetClient(),
      Scheme:         scheme,
      SubagentImage:  subagentImage,
      CredproxyImage: credproxyImage,
      SigningKey:     signingKey,
      EnvReader:      envReader,
      PVCName:        "tide-projects",
  }
  // Then in PlanReconciler block (line 220-228), add:
  //     Dispatcher: dispatcher,
  // And in TaskReconciler block (line 240-256), add:
  //     Dispatcher: dispatcher,
  ```

**Secondary risk surfaced during investigation (NOT cascade-8 itself):** No production code path sets `plan.Status.ValidationState = "Validated"`. The Plan admission webhook (`internal/webhook/v1alpha1/plan_webhook.go`) validates and emits warnings but does NOT update Status. The PlanReconciler's `reconcileWaveMaterialization` short-circuits at line 147 if ValidationState != "Validated". This means even with Option δ wired, the Plan path would still be gated. The workaround visible in the test fixture is that `stampTaskLabels` would only execute when the gate opens — but `TaskReconciler.reconcileDispatch` does NOT depend on `Wave` existence or `ValidationState`; it depends only on `r.Dispatcher != nil` + the Task itself. **So Option δ does close cascade-8 for HARN-03 (Pod creation) even with the ValidationState gap.** The ValidationState gap is a separate cascade-9-class issue best surfaced in a follow-up debug session, not folded into Plan 02.2-10. (Recommended classification when it surfaces: `production-wiring-gap`, same family as cascade-8.)

**Summary verdict:** Plan 02.2-10 should NOT bump credproxy pod-wait timeout. It should wire `PodJobBackend` into `cmd/manager/main.go` and re-run Layer B; the 120s budget is more than enough for an actual dispatch path. If a separate Plan 02.2-11 wants to also close the ValidationState gap proactively, that's a clean follow-up — but cascade-8 itself is closed by Option δ alone.

## Reasoning Checkpoint

Investigation trail (chronological):

1. **Confirmed manager log evidence**: kind controller log `/var/folders/51/h7gq6p5x3592gvrbhrd985q80000gn/T/kind-logs-tide-test/tide-test-control-plane/pods/tide-system_tide-controller-manager-544b984b59-ppdvg_9b633376-6579-4d2f-bf35-3f27f29da359/manager/0.log` has exactly 9 log lines for the credproxy-test namespace spanning 17:58:49Z → 18:00:56Z. Plan/Task conflict at the start; cleanup at the end; nothing in between. 127s of silence is real.

2. **Located `stampTaskLabels`**: `internal/controller/plan_controller.go:262-296`. Critical observation: this function uses `r.Patch(ctx, t, patch)` at line 291, NOT `r.Update`. It also early-returns (line 279-282) when Task labels already match — and the test fixture pre-sets the correct labels (`suite_test.go:622`), so this function is a no-op in this test path. **The hypothesized "stampTaskLabels patches Task labels races with TaskReconciler Update" cannot be the conflict source — stampTaskLabels never patches in this test path.**

3. **Located TaskReconciler conflict sources**: `internal/controller/task_controller.go` has two `r.Update(ctx, &task)` calls at line 139 (finalizer add) and line 160 (owner ref set). These are full-object updates that DO carry ResourceVersion. Two consecutive reconcile attempts on a fresh Task: reconcile #1 adds finalizer (RV bumps); reconcile #2 sets owner ref. If reconcile #2's `r.Get` returns a cached stale RV, the `r.Update` at line 160 conflicts. **This is the actual source of the Task conflict at 17:58:49Z** — TaskReconciler racing against its own previous reconcile via cache lag, not against PlanReconciler.

4. **Located status subresource markers**: Both `api/v1alpha1/task_types.go:126` and `api/v1alpha1/plan_types.go:57` already have `+kubebuilder:subresource:status`. **Option β-1 (add status subresource) is a no-op — already done.**

5. **Located work-queue wiring**: `cmd/manager/main.go` lines 220-228 and 240-256 instantiate PlanReconciler and TaskReconciler. Each uses `controller.Options{MaxConcurrentReconciles: ...}` only — no custom RateLimiter, no custom Backoff. Default rate-limiter (controller-runtime v0.24.1, default `UsePriorityQueue=true`) is `NewTypedItemExponentialFailureRateLimiter(5ms, 1000s)`. **A single failure produces a 5ms next-requeue, not 127s of silence.** The observed silence cannot be exponential backoff after a single conflict.

6. **Located the smoking gun — `Dispatcher` is never assigned**: `grep "Dispatcher:" cmd/manager/main.go` returns ZERO matches. Comparison: `grep "Dispatcher:" internal/controller/*_test.go` returns 6 matches (all in test code). **`Dispatcher` is the Phase 2 dispatch-path feature flag; in production it's always nil; both `reconcileWaveMaterialization` and `reconcileDispatch` are gated off in production.**

7. **Verified PodJobBackend exists and implements Dispatcher**: `internal/dispatch/podjob/backend.go:58-79`. Constructor surface: `{Client, Scheme, SubagentImage, CredproxyImage, SigningKey, EnvReader, PVCName}`. All seven fields are already constructed elsewhere in `cmd/manager/main.go` lines 174-188. **Wiring requires only one PodJobBackend literal + two struct-field assignments.**

8. **Verified the "no Layer B kind spec has ever passed pod-creation" claim**: `grep clean_run_specs_passed: *-VERIFICATION.md`. All Layer B kind verifications since 02.2-04 show `clean_run_specs_passed: 0`. The cascade-8 investigation in Plan 02.2-09 inferred "ResourceVersion conflict → exponential backoff suppression" from a single data point (the manager log silence) — but the same silence pattern would equally well be explained by "dispatch path gated off, nothing else to reconcile."

**The prior diagnosis was a plausible-sounding but factually incorrect inference.** A single piece of evidence (127s of manager-log silence around a Task object) supported BOTH "race + backoff" AND "no work to do" hypotheses. The code investigation distinguishes between them definitively: there is no race-driven backoff because the conflict-producing path is itself never the dispatch path.

## TDD Checkpoint

(not applicable — TDD mode is off)
