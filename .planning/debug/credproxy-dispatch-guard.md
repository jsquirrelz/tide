---
slug: credproxy-dispatch-guard
status: root_cause_found
trigger: |
  Cascade-5 surfaced after Plan 02.2-06's Makefile timeout fix (300s→600s) closed cascade-4. Two `credproxy_test.go` HARN-03 specs FAIL with "A Pod should be created for the credproxy-task Job: Timed out after 60.000s". The inner Ginkgo `-timeout=5m` cap then fires (panic), leaving the remaining 5 Layer B specs unrun. Decide between Option A (testMode dispatch shortcut in task controller) vs Option B (full hierarchy in fixture) vs Option C (mock Wave Ready) before authoring Plan 02.2-07.
created: 2026-05-14
updated: 2026-05-14
phase_context: 02.2-layer-b-kind-test-timing-fixes
related_artifact: .planning/phases/02.2-layer-b-kind-test-timing-fixes-bump-kindtesttimeout-from-4mi/02.2-06-VERIFICATION.md
cascade_classification: harness-bug
---

# Debug: credproxy dispatch guard (cascade-5)

## Symptoms

**Expected behavior:** `make test-int` clean run reaches 7/7 Layer B specs PASS in ≤ 600s `go test` wall-time, with `tide-controller-manager` Pod Running 0 restarts. After Plan 02.2-06's outer-timeout fix, the budget is no longer the blocker; the gate is the spec contract.

**Actual behavior:** Layer A 18/18 PASS (27.18s). Layer B BeforeSuite PASS (66s, cert-manager wait + helm install). Layer B spec 1 (credproxy "A Pod should be created for the credproxy-task Job") FAILS at 75s. Spec 2 (credproxy "Container should have the expected env vars") FAILS at 73s. After 149s of credproxy failures, Ginkgo's inner `-timeout=5m` cap fires (panic: test timed out after 5m0s) and specs 3–7 (wave, failure, caps, output) NEVER RUN. Total wall 304s, exit code 1 (FAIL — spec failures, not exit 2 SIGKILL).

**Error messages:**
- `[FAILED] A Pod should be created for the credproxy-task Job` — `Timed out after 60.000s. Expected an error to have occurred. Got: <nil>: nil`
- `panic: test timed out after 5m0s` (Ginkgo inner cap)
- Manager log shows `task cleanup` (finalizer path) at 60s post-Task-create; no `dispatch` or `job create` log lines for `credproxy-task`.

**Timeline:** Surfaced 2026-05-14 during Plan 02.2-06's Task 2 clean-run reproducer. The credproxy specs ran identically (or worse — SIGKILLed before completing) in Plans 02.2-05 / 02.2-04 / 02.2-03 / 02.2-01, but those plans' BLOCKED gates were attributed to upstream cascades (flag mismatches, PVC RWX/RWO, test-budget). With cascades 1–4 closed, cascade-5 (the credproxy harness-bug) is now the surfacing blocker.

**Reproduction:** `make test-int` (single command — builds Docker images, creates kind cluster, helm-installs tide, runs Layer A envtest, then Layer B kind specs against the healthy controller). The credproxy specs deterministically fail every run; this is structural, not flaky.

## Pre-investigation hypothesis (from 02.2-06-VERIFICATION.md §Section 5)

`test/integration/kind/credproxy_test.go` (HARN-03) creates a Plan CRD + Task CRD with `spec.dev.testMode: success` in a standalone `credproxy-test` namespace. The fixture does NOT create the parent Milestone/Phase/Wave CRD hierarchy. The task controller (`internal/controller/task_controller.go`) reconciles the Task but its dispatch path requires the parent Wave to be in Ready state before creating a `batchv1.Job`. Without a parent Wave, the controller takes the `task cleanup` path (finalizer/cleanup) rather than the `dispatch Job` path. No Job is ever created → no Pod appears → the test's 60s `Eventually(...).Should(Succeed())` assertion times out.

**Hypothesis verdict: WRONG GATE IDENTIFIED, RIGHT FAMILY OF BUG.** The dispatch gate is NOT a parent Wave Ready check (no such check exists in `task_controller.go`); it is a `resolveProject` Project-in-namespace lookup that fails when no Project CRD exists. See §Reasoning Checkpoint for the corrected map.

## Fix landscape (to be decided)

**Option A (testMode dispatch shortcut in task controller):** If `spec.dev.testMode` is set, bypass the parent-Wave-Ready check in `internal/controller/task_controller.go` and dispatch a Job directly. Smallest surface change; keeps the test fixture minimal (which is `testMode: success`'s design intent). Production dispatch path unchanged (testMode is a dev-time signal, not a production code path).

**Option B (full hierarchy in test fixture):** Extend `credproxy_test.go` to create Milestone/Phase/Plan/Wave/Task in the credproxy namespace, mirroring `wave_test.go`'s working setup. Bigger test fixture surface but exercises the production dispatch path end-to-end. Tradeoff: tests verify a richer contract but get more brittle to schema evolution.

**Option C (mock Wave Ready in BeforeSuite):** Pre-create a fake "Ready" Wave in the credproxy namespace's BeforeSuite so the dispatch guard sees a Ready parent. Fragile — depends on dispatch-guard internals; future changes to guard logic break this without warning.

## Current Focus

- hypothesis: (RESOLVED) The dispatch gate is `resolveProject` at `task_controller.go:217` (Step 3 of `reconcileDispatch`). It fails when the credproxy fixture creates a Plan+Task without an accompanying Project CRD in the namespace, because (a) the Task carries no `tideproject.k8s/project` label (the fast-path miss at `task_controller.go:533`), and (b) `ProjectList` for the namespace returns zero items (the fallback failure at `task_controller.go:544-547`).
- test: Code reading of `task_controller.go` Reconcile()+reconcileDispatch() lines 118-421; comparison of `credproxy_test.go` inline YAML (lines 78-100) vs `testdata/three-task-wave.yaml` (full hierarchy with Project+Secret).
- expecting: A single early-return error from `resolveProject` that prevents Steps 4–12 from running. Confirmed exactly.
- next_action: Author Plan 02.2-07 with Option B recommended (full hierarchy + Project in fixture) — see §Reasoning Checkpoint for rationale.

## Evidence

- timestamp: 2026-05-14 — Plan 02.2-06 Task 2 clean run produced `02.2-06-VERIFICATION.md` (gate_decision: BLOCKED, cascade-5 classification: `harness-bug`). Full evidence in §Section 1 (Per-Spec Outcome Table, Layer B Pod state, manager log excerpts) and §Section 5 (Root-Cause Summary with 4 sub-points + Fix landscape).
- timestamp: 2026-05-14 — Manager log excerpt confirms `task cleanup` path was taken (finalizer) at exactly 60s post-Task-create, with NO `dispatch` or `job create` log line for `credproxy-task`. The clean-run log is preserved at `/tmp/02.2-06-clean-run.log` (1025 lines).
- timestamp: 2026-05-14 — Code read of `internal/controller/task_controller.go` (818 lines). Findings:
  - Step 3 of `reconcileDispatch` calls `resolveProject(ctx, task)` at line 217 BEFORE Step 4 budget gate, Step 5 indegree, …, Step 11 Job create.
  - `resolveProject` (lines 531-548) returns `fmt.Errorf("no project found in namespace %s", task.Namespace)` when neither the label fast-path nor the namespace ProjectList fallback yields a Project.
  - Zero references to `Wave`, `Milestone`, or `Phase` CRDs in the entire `task_controller.go` (grep verified). The dispatch path has no parent-hierarchy fetch beyond the Plan owner-ref ensure at Step 4 (line 148), which is best-effort and explicitly does NOT gate dispatch ("dispatch must still proceed", line 147 comment).
- timestamp: 2026-05-14 — Code read of `api/v1alpha1/task_types.go`. `Spec.Dev.TestMode` is a valid enum field (`success | fail-exit-1 | hang | exceed-output-paths`) but is only consumed at line 699-701 of `task_controller.go` to populate `EnvelopeIn.Dev` — it does NOT gate any dispatch path.
- timestamp: 2026-05-14 — Code read of `api/v1alpha1/wave_types.go`. `Wave` CRD exists but is purely observational (`Status.TaskRefs`, `Status.DispatchedAt`). It is materialized by `PlanReconciler.materializeWaves` (plan_controller.go:212-254) as a side effect of plan reconciliation. The `Task` API has no `WaveRef` field — waves are encoded via `Task.Spec.DependsOn` + the `tideproject.k8s/wave-index` label.
- timestamp: 2026-05-14 — Comparison of fixtures: `testdata/three-task-wave.yaml` creates Namespace + Secret + Project + Milestone + Phase + Plan + 3 Tasks (Tasks have BOTH `tideproject.k8s/project` AND `tideproject.k8s/wave-index` labels preset). `credproxy_test.go` inline YAML creates only Plan + Task in `credproxy-test` namespace (Task has only the `tideproject.k8s/wave-index` label; NO Project, NO Secret, NO Milestone, NO Phase, NO `tideproject.k8s/project` label).

## Eliminated

- hypothesis: cascade-5 is another flag-mismatch — eliminated. Manager Pod is `Running 0 restarts` for the full run; no `flag provided but not defined` errors in manager log. 5/5 chart args (--config, --metrics-bind-address, --watch-namespace, --leader-elect, --webhook-cert-path) all defined in `cmd/manager/main.go`.
- hypothesis: cascade-5 is a budget-vs-actual issue (further bump needed) — eliminated. Clean run wall 304s < 600s budget. No SIGKILL. Exit code 1 (FAIL) not exit 2 (timeout 124). Plan 02.2-06's Task 1 fix IS effective; the budget is no longer the blocker.
- hypothesis: cascade-5 is a spec flake (timing/race) — eliminated. The credproxy specs FAIL deterministically: 75s + 73s = 148s spent in two `Eventually(60s)` assertions, with the manager log confirming `task cleanup` ran (deterministic finalizer path) rather than `dispatch` (which would produce a Pod). Re-running the same fixture produces the same failure pattern.
- hypothesis: cascade-5 is a kind-infra issue (cluster networking, image-pull, RBAC) — eliminated. BeforeSuite PASSED (cert-manager Ready in ≤ 90s, helm install --wait completed, Pod Ready). The credproxy namespace exists; the Task CRD is created and reconciled (we see `task cleanup` in the manager log); the failure is internal to the controller's dispatch decision, not external infrastructure.
- hypothesis (pre-investigation): the dispatch gate is a parent Wave Ready check — eliminated. `task_controller.go` has zero Wave fetches and zero `Ready` condition checks on any parent CRD. The Wave CRD is observational only; it is materialized by PlanReconciler as a side effect but never consulted by TaskReconciler for dispatch gating.
- hypothesis (pre-investigation): "task cleanup" log line is evidence that the dispatch path routed to the cleanup branch instead of the Job branch — eliminated. The `task cleanup` log line at line 131 of `task_controller.go` is INSIDE `finalizer.HandleDeletion`, which only runs when `task.DeletionTimestamp != nil` (Step 2, line 128). It fires at 60s because AfterEach calls `deleteNamespace(credproxyNS)` after the test's `Eventually(60s)` times out — it is a SYMPTOM of test cleanup running after the timeout, not the dispatch routing.

## Resolution

- root_cause: **`internal/controller/task_controller.go:217` (`reconcileDispatch` Step 3) calls `resolveProject` which fails with `"no project found in namespace credproxy-test"` because the `credproxy_test.go` inline YAML fixture creates only a Plan+Task without a Project CRD in the same namespace.** The reconciler returns the error before reaching Step 11 (Job create at line 393), so no Job is ever dispatched; the test's 60s `Eventually` waiting for a Pod times out. The manager-log `task cleanup` line is a downstream side effect of AfterEach's `deleteNamespace` triggering the finalizer path — NOT evidence of a dispatch-branch decision.
- fix: **Recommended: Option B (full-hierarchy fixture).** See §Reasoning Checkpoint for the option-by-option analysis. The pre-investigation hypothesis described the bug as a parent-Wave-Ready check, which led the §Section 5 author to recommend Option A — but Option A's premise (a "Wave Ready check to bypass") does not exist in the code. Option A would need to be re-framed as "bypass the `resolveProject` requirement when `Spec.Dev.TestMode` is set", which has higher blast radius than originally argued (it touches the very first gating step of every dispatch, not a late-stage Wave check, and `resolveProject`'s result is consumed by Steps 4, 6, 8, 9, 11 — multiple downstream branches would need defensive defaults if Project becomes optional).
- verification: Pending Plan 02.2-07 implementation. The verification contract is: after the fix lands, `make test-int` produces 7/7 Layer B specs PASS within the 600s outer budget, with `credproxy-task` Job and Pod both created in `credproxy-test` namespace and the manager log showing a `dispatch` line for `credproxy-task` BEFORE any `task cleanup` line.
- files_changed: [] (no fix applied this session — goal was `find_root_cause_only`)

## Reasoning Checkpoint

### Where dispatch is actually gated (confirmed by code reading)

`internal/controller/task_controller.go` `reconcileDispatch` (lines 188-421) executes 12 steps. The early-return gates BEFORE Job creation (Step 11, line 393) are:

| Step | Line | Gate | Behavior when gate trips |
|------|------|------|--------------------------|
| 1 | 191-194 | Terminal short-circuit (Phase=Succeeded\|Failed) | Return without dispatch |
| 2 | 197-214 | Running + Job terminal → handle completion | Calls handleJobCompletion (a different path) |
| **3** | **217-220** | **`resolveProject` — Project must exist in namespace** | **Return error; reconcile re-queues; never reaches Step 11** |
| 4 | 223-236 | Budget gate (Project.Status.Phase=BudgetExceeded) | Patch BudgetBlocked condition; return |
| 5 | 238-258 | Indegree > 0 (predecessors not Succeeded) | Patch Pending; return |
| 6 | 260-305 | Rate-limit gate (Pattern 1, requires Secret) | RequeueAfter delay; return |
| 7 | 307-330 | Attempt > MaxAttempts | Patch Failed/ExceededAttempts; return |
| 8 | 332-349 | Mint signed token | Error if signing fails |
| 9 | 351-355 | Build EnvelopeIn | Error if marshal fails |
| 10 | 357-367 | Patch Status.Attempt | Error if patch fails |
| 11 | 369-402 | **Build + Create Job** ← dispatch happens here | AlreadyExists is success |
| 12 | 404-418 | Patch Status.Phase=Running | Error if patch fails |

The credproxy fixture trips Step 3 specifically. Subsequent steps (4-12) never execute.

### Why the fast-path label miss in resolveProject

`resolveProject` (lines 531-548) tries two paths:

1. **Fast path (line 533):** `task.Labels["tideproject.k8s/project"]` — present? Get Project by that name.
2. **Fallback (lines 540-547):** `r.List(ctx, &projectList, client.InNamespace(task.Namespace))` — if list is empty, error.

The credproxy fixture's Task has only `tideproject.k8s/wave-index: "0"` and no `tideproject.k8s/project` label (compare to `testdata/three-task-wave.yaml:73-74` which preset both labels). PlanReconciler's `stampTaskLabels` (plan_controller.go:262-296) DOES backfill the project label — but only if `projectName != ""` (line 280, 288). `resolveProjectName` (plan_controller.go:301+) returns `""` when no Project label on Plan AND no Projects in namespace ListAll. Both conditions hold for the credproxy fixture, so the label is never stamped, and the Task remains label-less. The fallback ProjectList also returns empty. Error is the only outcome.

### Option-by-option assessment with corrected gate

**Option A (testMode dispatch shortcut in task controller — REQUIRES RE-FRAMING):**

- *As stated in §Section 5:* "bypass the parent-Wave-Ready check"
- *Actual edit needed:* Add a branch BEFORE Step 3 (`resolveProject`) that, when `task.Spec.Dev.TestMode != ""`, constructs a synthetic Project (or skips Project resolution entirely) and routes to a minimal Job-create path.
- *Blast radius:* The Project is consumed in Steps 4 (budget gate uses `project.Status.Phase`), 6 (rate-limit uses `project.Spec.ProviderSecretRef` → Secret UID), 8 implicitly (token validity window via `task.Spec.Caps`, no Project dependency), 9 (envelope build does not use Project directly), 11 (`opts.Project = project` and `opts.ProjectUID = string(project.UID)` — used in PVC mount path `/workspaces/<projectUID>/workspace`).
- *Hidden coupling:* `BuildOptions.ProjectUID` flows into `taskWorkspaceRoot` (task_controller.go:450) used by `harness.Validate`. A synthetic Project with a fixed UID may work for the success path but creates a tight coupling between the testMode bypass and the harness validator's path conventions. Future changes to PVC layout (e.g., per-project subdirectories with stricter ownership) would need parallel changes to the synthetic-Project construction.
- *Production-path purity argument:* "testMode is a dev-time signal; production dispatch path unchanged" — this is TRUE only if the bypass branch is fully isolated. As stated above, the bypass touches `BuildOptions.ProjectUID` which is consumed by production code (`opts.ProjectUID` is not gated by testMode in `BuildJobSpec`). The bypass is therefore not as surgically scoped as §Section 5 argued.
- *Unintended-consequence vector w.r.t. §Section 6 threat model:* §Section 6 emphasizes carry-forward T-02.2-01 through T-02.2-13 — none of those directly concern the testMode dispatch shortcut. However, a testMode bypass that constructs a synthetic Project introduces a NEW threat vector: a misconfigured production Task with `dev.testMode` accidentally set could bypass the budget-cap gate (Step 4) AND the rate-limit gate (Step 6), both of which consume the Project. This is a real security regression unless the bypass branch keeps Steps 4 and 6 active (which means it can't fully avoid Project resolution — it just needs a relaxed "synthesize a default Project when none exists" fallback). At that point the bypass effectively becomes "ProjectList fallback uses a synthetic default" which is structurally Option D, not Option A.

**Option B (full hierarchy in test fixture — RECOMMENDED):**

- *Actual edit needed:* Extend `credproxy_test.go` to apply a YAML fragment mirroring `testdata/three-task-wave.yaml` (Namespace + Secret + Project + Milestone + Phase + Plan + Task with both labels preset) OR factor the boilerplate into a shared `applyHierarchy(ns, planName, taskName)` test helper in `suite_test.go`.
- *Blast radius:* Test-only. Zero production-code change.
- *Test-fixture brittleness argument:* §Section 5 raises "tests verify a richer contract but get more brittle to schema evolution." This is true but is the correct tradeoff for the cascade-5 fix because: (a) the wave_test.go path ALREADY uses the full hierarchy and serves as the working reference, so the schema-evolution cost is already paid for one test and amortizing it for credproxy is marginal; (b) credproxy_test.go's stated purpose is HARN-03 (verify sidecar topology + startup log), which is fundamentally about the dispatch Pod's container shape — exercising that requires the production dispatch path, so the test is more truthful when it uses the production fixture.
- *Schema-evolution mitigation:* If brittleness is a real concern, the shared helper pattern (`applyHierarchy(ns, planName, taskName)` in suite_test.go) makes future schema changes a single-site edit.
- *Production-path verification bonus:* This option exercises the live production dispatch path including resolveProject, rate-limit gate, attempt counter, token mint, PVC mount — increasing the cascade-5-related regression surface that the credproxy test actively defends against.

**Option C (mock Wave Ready in BeforeSuite — REMOVABLE FROM CONSIDERATION):**

- *Premise refutation:* There is no "Wave Ready check" to mock. The Wave CRD has a `Status.Phase` and `Status.Conditions` field but no code path in `task_controller.go` reads them. Mocking a Ready Wave would have zero effect on dispatch.
- *Even if reinterpreted as "pre-create a Project CRD in BeforeSuite":* This is just Option B with the hierarchy split between BeforeSuite and It blocks, which is strictly worse (test setup state is split across two locations, AfterEach's `deleteNamespace` must skip the BeforeSuite namespace, and the test reader has to traverse two files to see the full fixture).
- *Verdict:* Drop Option C from consideration. The original §Section 5 author included it as a third option for taxonomic completeness, but the premise it rests on is structurally absent.

### Recommendation

**Recommended fix: Option B (full-hierarchy fixture).**

Rationale:
1. The originally-recommended Option A's premise ("bypass the Wave Ready check") does not match the actual gate (`resolveProject`). Re-framing Option A correctly reveals it is not as minimally-scoped as argued — it requires either a synthetic Project (with new threat-model considerations for accidental production-Task bypass of budget+rate-limit gates) or a "default Project when none exists" fallback (which is a permanent production-code change disguised as a dev-time signal).
2. Option B is purely test-code, zero production-code change, and exercises the live dispatch path that production code already runs in `wave_test.go`. Schema-evolution brittleness is real but bounded; a shared helper (`applyHierarchy`) addresses it.
3. Option C's premise is structurally absent.

**Runner-up: Option A re-framed as "synthesize default Project when ProjectList is empty AND task.Spec.Dev.TestMode is set" (added as new Option D).**

This re-framing keeps the production path untouched when `Dev.TestMode == ""` and adds a single defensive branch in `resolveProject` (not `reconcileDispatch`) when both conditions hold. If the team prefers minimal test-fixture surface and is willing to accept a small production-code change scoped to `resolveProject`, this version of Option A is preferable to the originally-stated Option A because it confines the bypass to a single function (`resolveProject`) rather than threading testMode awareness through Steps 3-11.

### Specialist hint for follow-up

`go-controller-runtime` — the investigation is squarely about reconcile-loop gating in a controller-runtime-based controller. The fix-direction analysis touches reconcile early-return semantics, finalizer/deletion paths, and the watch/requeue contract — all controller-runtime-idiomatic concerns.

## TDD Checkpoint

(not applicable — TDD mode is off; will skip unless `workflow.tdd_mode: true` is set later)
