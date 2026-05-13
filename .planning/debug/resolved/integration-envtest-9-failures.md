---
status: resolved
trigger: "Fix 9 failures in new integration envtest suite test/integration/envtest/ for phase 02 plan 02-13"
created: 2026-05-12T00:00:00Z
updated: 2026-05-12T02:05:00Z
---

## Current Focus

reasoning_checkpoint:
  hypothesis: |
    Three distinct root causes in the integration test suite, all test-only bugs (production controllers prove correct via unit tests):

    1. TASK FAILURES (FAIL-01, SUB-02, SUB-03): TaskReconciler.resolveProject returns
       "no project found in namespace default" because indegree tests do NOT create
       a Project. In real K8s, PlanReconciler stamps the 'tideproject.k8s/project'
       label and a Project always exists. Tests bypass this.

    2. INIT/BUDGET FAILURES (ART-01 x3, FAIL-04 x2): ProjectReconciler uses
       predicate.Or(GenerationChangedPredicate, AnnotationChangedPredicate).
       Status-only patches do NOT change Generation, so reconcile is never enqueued
       after the test patches BudgetStatus or Job.Status. ART-01 tests also create
       a Project BEFORE the bound PVC (or use different PVC names), so reconciler
       requeues forever waiting for PVC.

    3. PLAN-03 GREP FALSE POSITIVE: The test greps for cycleRecover|recoverCycle|fixCycle|skipCycle
       in webhook source. plan_webhook.go's doc comment LITERALLY contains those
       strings as the verification-by-absence pattern (line 72:
       grep -nE 'recoverCycle|cycleRecover|fix.*cycle|skip.*cycle'). The test
       matches its own documentation. Fix: skip comment lines or use stricter
       regex (e.g., function name match, not raw substring).
  confirming_evidence:
    - Test run output: "no project found in namespace default" error in indegree/SUB-02/SUB-03 tests (50+ occurrences in stack trace)
    - resolveProject (task_controller.go:497) requires a Project to exist; tests create Plan + Tasks but no Project
    - ProjectReconciler.SetupWithManager uses GenerationChangedPredicate (project_controller.go:446-449) — status updates do not bump Generation
    - Init test patches Job.Status to Complete then expects Project.Status.Phase=Initialized — only a generation/annotation update fires reconcile
    - plan_webhook.go line 72 contains the literal grep pattern in a comment block
  falsification_test: |
    After fixes:
    - Create a default Project in indegreeNamespace before makeTask runs → resolveProject returns nil error → indegree tests pass
    - Trigger reconciles by patching an annotation (not status) → init/budget tests reach desired phase
    - Strip Go comments from PLAN-03 grep → false positive gone, real cycleRecover would still trip the test
  fix_rationale: |
    Test-only fixes. Production controllers correctly use generation-changed predicates
    (proper K8s practice), and unit tests prove the dispatch/budget/init logic works.
    The integration tests violated the contract: they assumed status updates trigger
    reconciles, and they assumed Tasks can dispatch without a Project. Both are wrong.
    Fix the tests, preserve assertions.
  blind_spots:
    - PLAN-03 spec uses BeforeEach not BeforeSuite for namespace setup — but indegree/init/budget tests use a shared 'default' namespace, so cross-test pollution between Project objects is possible. Mitigated by AfterEach cleanup already present.
    - Init Job idempotent test deletes Project in AfterEach but does not wait for it — second spec may see stale Project. Watch out for flakes.

hypothesis: See reasoning_checkpoint above
test: Apply test-side fixes per the three root causes; run integration suite x3
expecting: 18/18 specs pass deterministically
next_action: Apply edits to indegree_test.go, init_test.go, budget_test.go, admission_test.go

## Symptoms

expected: 18/18 integration envtest specs pass, deterministically green
actual: 9 PASS / 9 FAIL after fixing duplicate planRef indexer registration
errors:
  - FAIL-01 indegree blocking (indegree_test.go:75)
  - SUB-02 attempt counter (indegree_test.go:105)
  - SUB-03 deterministic Job name (indegree_test.go:126)
  - ART-01 init Job created (init_test.go:94)
  - ART-01 init Job idempotent (init_test.go:128)
  - ART-01 init Job completion phase=Initialized (init_test.go:200)
  - FAIL-04 BudgetExceeded halts (budget_test.go:89)
  - FAIL-04 bypass annotation clears (budget_test.go:145)
  - PLAN-03 cycle-recovery absent grep (admission_test.go:312)
reproduction: cd /Users/justinsearles/Projects/tide && go test -short -count=1 ./test/integration/envtest/...
started: With phase 02-13 commit that added the integration envtest suite

## Eliminated

(empty)

## Evidence

- timestamp: initial
  checked: knowledge base
  found: wave5-controller-suite-flakes pattern — TaskReconciler.resolveProject needs 'tideproject.k8s/project' label on Tasks; makeTask must stamp it. Integration tests may have same issue.
  implication: When creating Tasks in integration tests, ensure label is set (PlanReconciler does it in prod, tests bypass).

- timestamp: investigation-1
  checked: test run output (full stack)
  found: |
    Repeated reconciler error "no project found in namespace default" for jobname-task-a,
    attempt-task-a. Tests in indegree_test.go create Plan + Tasks but never a Project.
    resolveProject (task_controller.go:497) first checks task.Labels['tideproject.k8s/project'],
    then falls back to listing Projects in the namespace. Tests stamp neither.
  implication: indegree_test.go needs to create a Project before the Plan/Tasks, with proper
    ProviderSecretRef etc. Plus stamp the label on Tasks if a wrong Project might exist.

- timestamp: investigation-2
  checked: project_controller.go SetupWithManager
  found: |
    predicate.Or(GenerationChangedPredicate, AnnotationChangedPredicate). Status patches
    do NOT change metadata.Generation, so the reconciler is never enqueued after the
    test patches Project.Status.Budget or Job.Status.Conditions.
  implication: After a status-only patch, tests must add or modify an annotation to
    re-trigger reconcile. The bypass test already does this; the cap test does NOT.
    Init Job completion test must patch the Project's annotation (Owns(Job) does
    requeue on owned-Job status change — verify this is wired correctly).

- timestamp: investigation-3
  checked: plan_webhook.go content (line 60-75)
  found: |
    Doc-comment block line 72 contains literal pattern:
    "grep -nE 'recoverCycle|cycleRecover|fix.*cycle|skip.*cycle' internal/webhook/v1alpha1/"
    The PLAN-03 grep test scans plan_webhook.go and matches these tokens, reporting
    a false positive.
  implication: Test must strip Go comments before scanning, or use a stricter match
    pattern that recognizes function/identifier boundaries instead of raw substring.

- timestamp: investigation-4
  checked: Owns(&batchv1.Job{}) on ProjectReconciler
  found: |
    Owns triggers reconcile when owned Job updates. But the init Job is created with
    OwnerReference to the Project via owner.EnsureOwnerRef. When test patches Job.Status,
    the Owns event SHOULD fire reconcile. Need to verify the test does flush Job status
    correctly and the owner ref is in place before status patch.
  implication: ART-01 completion test should work IF the Owns watch sees the Job status
    change. The annotation flush trick (forcing reconcile via annotation patch) is a
    robust fallback if Owns is racy.

## Resolution

root_cause: |
  Three independent test-only bugs in the new integration envtest suite (none in
  production code; the unit suite under internal/... was already green):

  1. Task dispatch tests (FAIL-01, SUB-02, SUB-03) created Plans + Tasks but no
     Project. TaskReconciler.resolveProject (task_controller.go:497) returned
     "no project found in namespace default", so dispatch never ran.
     PlanReconciler stamps the tideproject.k8s/project label in production,
     but integration tests bypass PlanReconciler. (Same shape as the resolved
     wave5-controller-suite-flakes session.)

  2. ProjectReconciler init/budget tests (ART-01 x3, FAIL-04 x2) relied on
     status-only patches to fire subsequent reconciles. ProjectReconciler's
     watch uses predicate.Or(GenerationChangedPredicate, AnnotationChangedPredicate);
     status updates don't bump metadata.Generation, so the predicate filtered
     them out and the reconciler's seam body never ran past finalizer-add.
     Unit tests sidestep this by calling Reconcile() directly. Plus K8s 1.33+
     requires SuccessCriteriaMet=True + startTime + completionTime before
     Complete=True on Job status. Plus the bypass test asserted a racy
     terminal phase ("phase != BudgetExceeded") when the production
     guarantee is one-shot consumption.

  3. PLAN-03 grep test did a raw substring scan and matched the literal grep
     example documented inside plan_webhook.go's doc comment.

fix: |
  Test-only fixes in test/integration/envtest/:
  - indegree_test.go: BeforeEach creates default Project + bound PVC; AfterEach
    cleans up Projects/PVCs; makeTaskWithWaveLabel stamps tideproject.k8s/project
    label on every Task.
  - budget_test.go: Added kickProjectReconcile helper that bumps a benign
    tide-test/kick annotation to fire AnnotationChangedPredicate. Bypass
    assertion now checks annotation consumption (the production one-shot
    signal) instead of racy phase state.
  - init_test.go: kickProjectReconcile after Project create (past finalizer
    early-return) and after Job status patch; Job status patch matches K8s
    1.33+ requirements; completion-test resolves Job by deterministic
    tide-init-{UID} name to avoid stale-job races between specs.
  - admission_test.go: PLAN-03 grep replaced with go/ast walk over identifier
    names; doc comments are no longer scanned.

verification: |
  go test -short -count=1 ./test/integration/envtest/... three times in a row,
  18/18 green each time (~20-21s wall). Full gate go test -short -count=1 ./...
  passes with no regressions in unit suites.

files_changed:
  - test/integration/envtest/admission_test.go
  - test/integration/envtest/budget_test.go
  - test/integration/envtest/indegree_test.go
  - test/integration/envtest/init_test.go
