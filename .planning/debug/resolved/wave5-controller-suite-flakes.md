---
status: awaiting_human_verify
trigger: "Three controller-suite tests pass in isolation but fail in full Ginkgo suite. Regressed at Wave-5 merge of phase 02. Find leaking shared/global state and fix it."
created: 2026-05-13T04:58:50Z
updated: 2026-05-13T05:10:00Z
---

## Current Focus

reasoning_checkpoint:
  hypothesis: "TaskReconciler.resolveProject's namespace-wide List fallback returns the wrong Project when other tests have created Projects in the shared 'default' namespace. The dispatch tests rely on resolving a SPECIFIC project (their own projectName) but get whatever Project is Items[0] from the list. This causes rate-limit gate to be skipped (different project has no ProviderSecretRef) and budget gate to be bypassed (different project lacks Phase=BudgetExceeded)."
  confirming_evidence:
    - "Ginkgo runs in randomized order (Random Seed varies between runs); failures appear when project_controller_test.go specs run before dispatch specs."
    - "makeTask helper (task_controller_test.go:121) never stamps the 'tideproject.k8s/project' label that resolveProject's fast path looks for."
    - "resolveProject's fallback (task_controller.go:507-513) returns projectList.Items[0] — order-dependent."
    - "BudgetExceededHalts failure: task.Status.Phase == 'Running' (got dispatched) — i.e., the wrong project was resolved (one without Phase=BudgetExceeded)."
    - "RateLimit tests failure: RequeueAfter == 0 — i.e., the wrong project was resolved (one without ProviderSecretRef, so Step 6 'if project.Spec.ProviderSecretRef != \"\"' short-circuits)."
    - "All three failing tests have non-trivial Project setup (ProviderSecretRef, Phase=BudgetExceeded) that distinguishes them from generic Projects — these distinguishing properties are lost when a different Project is resolved."
    - "All three failing tests fail with the exact same root mechanism: wrong Project resolved."
  falsification_test: "Stamping makeTask with `tideproject.k8s/project: <projectName>` label should make resolveProject's fast path return the correct Project, eliminating all 3 failures. If the fix doesn't work, the hypothesis is wrong."
  fix_rationale: "Production code (PlanReconciler.stampTaskLabels) DOES stamp this label — the test helper bypasses PlanReconciler so must do the same. The fast path is the production contract; the fallback exists only for graceful degradation when the label hasn't propagated yet. Fixing makeTask matches what production already does."
  blind_spots: "The fallback path itself is arguably fragile in production too (any namespace with multiple Projects breaks). But that's a separate concern — for THIS bug, only the test helper needs updating. Also: the storm test creates 20 tasks; if cache propagation is slow, even the label-fast-path Get might miss — but our markTaskSucceeded helper already handles this pattern with Eventually."

hypothesis: TaskReconciler.resolveProject namespace-wide List fallback returns wrong Project (Items[0]) when other tests have left Projects in 'default' namespace.
test: Stamp `tideproject.k8s/project=<projectName>` on Tasks created by makeTask (production code does this via PlanReconciler.stampTaskLabels but tests bypass PlanReconciler).
expecting: Fix should make 3 failing tests pass deterministically across 3+ consecutive full-suite runs.
next_action: Modify makeTask helper to accept projectName and stamp the label; update all callers in task_controller_test.go to pass their projectName.

## Symptoms

expected: All 56 specs pass deterministically when running `go test -short -count=1 ./internal/controller/...`
actual: 3 specific tests fail in full suite but pass when run in isolation via -ginkgo.focus filter
errors: Test failures in TestTaskReconciler_RateLimitGate_RequeuesWhenBucketExhausted, TestTaskReconciler_RateLimitStormAbsorbed, TestTaskReconciler_BudgetExceededHalts — specific failure outputs unknown until reproduced
reproduction: `go test -short -count=1 ./internal/controller/...` from /Users/justinsearles/Projects/tide
started: Wave-5 merge of phase 02 (Plans 02-10 added project_controller.go init Job + budget gate; 02-11 added plan_webhook.go + strict_mode.go; touched cmd/manager/main.go and internal/controller/suite_test.go)

## Eliminated

## Evidence

- timestamp: 2026-05-13T05:00:00Z
  checked: Reproduced full-suite failure 2/3 consecutive runs
  found: Same 3 tests fail (RateLimitGate_Requeues, RateLimitStormAbsorbed, BudgetExceededHalts). Failure modes: RequeueAfter=0 (rate-limit not triggered), Phase=Running (BudgetExceeded gate bypassed).
  implication: Reproducible flake — not a transient issue.

- timestamp: 2026-05-13T05:05:00Z
  checked: task_controller.go:497-514 resolveProject implementation
  found: Fast path checks task.Labels["tideproject.k8s/project"]; fallback does namespace-wide List and returns projectList.Items[0]. The label is stamped by PlanReconciler.stampTaskLabels in production. Test helper makeTask never sets this label.
  implication: When multiple Projects exist in 'default' namespace from prior tests, the wrong Project resolves — explaining all 3 failure modes.

- timestamp: 2026-05-13T05:08:00Z
  checked: Random seed in Ginkgo output (1778648459)
  found: Ginkgo randomizes spec order each run, which explains why failures are flaky (only fail when project_controller specs run before dispatch specs and leave Projects behind in cache during the racing window).
  implication: Confirms order-dependent leak — exactly what test isolation bugs look like.

- timestamp: 2026-05-13T05:09:00Z
  checked: Focused run with -ginkgo.focus passes 2/2
  found: When only the failing specs run, only ONE Project exists per spec → resolveProject's fallback returns the right one by accident.
  implication: Reproducer matches hypothesis: failures depend on coexistence of multiple Projects in the namespace.

## Resolution

root_cause: TaskReconciler.resolveProject's namespace-wide List fallback returns projectList.Items[0] when the 'tideproject.k8s/project' label isn't on the Task. The test helper makeTask (added in plan 02-09 alongside resolveProject) never stamps this label (PlanReconciler.stampTaskLabels does it in production, but the dispatch tests bypass PlanReconciler). When other tests have created Projects in the shared 'default' namespace, the wrong Project resolves — bypassing the rate-limit gate (no ProviderSecretRef on the wrong Project) and the BudgetExceeded gate (wrong Project doesn't have that Status.Phase). The bug was latent in plan 02-09 but exposed by Wave-5 plans 02-10 and 02-11 which add many more Project objects to the shared envtest namespace.
fix: Modified makeTask helper in internal/controller/task_controller_test.go to accept a variadic projectName parameter and stamp the 'tideproject.k8s/project' label on the Task, matching what PlanReconciler.stampTaskLabels does in production. Updated all 14 call sites to pass their projectName. Pure test-fix; no production code changes.
verification: Ran `go test -short -count=1 ./internal/controller/...` 6 consecutive times — all green (56 of 56 specs passed, 1 skipped intentionally). Random seeds differed each run, confirming Ginkgo's randomized spec order is now harmless.
files_changed:
  - internal/controller/task_controller_test.go
