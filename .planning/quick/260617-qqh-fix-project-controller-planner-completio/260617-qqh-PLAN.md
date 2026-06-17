---
phase: quick-260617-qqh
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - internal/controller/project_planner_completion_test.go
  - internal/controller/project_controller.go
autonomous: true
requirements: [QQH-01]
must_haves:
  truths:
    - "When a project planner Job is Complete AND still exists (no TTL GC), reconciling the Project spawns tide-reporter-<project.UID>."
    - "The completed planner Job's Usage is rolled up into Project.Status.Budget.CostSpentCents on the same reconcile."
    - "A still-Running planner Job causes reconcileProjectPlannerDispatch to return without spawning a reporter or re-dispatching."
  artifacts:
    - path: "internal/controller/project_planner_completion_test.go"
      provides: "RED-first envtest proving terminal-Job-with-existing-Job spawns reporter + rolls up cost"
      contains: "tide-reporter-"
    - path: "internal/controller/project_controller.go"
      provides: "reconcileProjectPlannerDispatch with terminal-state check before idempotency early-return"
      contains: "reconcileProjectPlannerDispatch"
  key_links:
    - from: "reconcileProjectPlannerDispatch"
      to: "handleProjectJobCompletion"
      via: "Running-branch terminal check reached while planner Job still exists"
      pattern: "isJobTerminal\\(&job\\)"
---

<objective>
Fix `reconcileProjectPlannerDispatch` in `internal/controller/project_controller.go` so the planner-Job terminal-state check runs BEFORE the blanket "Job exists -> return" idempotency early-return. Today the early-return (Step 1b) fires whenever the planner Job is present, making the Step 2 terminal branch unreachable. Net effect (dogfood run #2): planner completes with childCount=3 but no `tide-reporter-<uid>` Job spawns, 0 Milestones materialize, and `status.budget` stays empty — self-healing only after the ~10-min `ttlSecondsAfterFinished=600` GC drops the Job and the line-983 absent-Job fallback fires.

Root cause is ALREADY diagnosed — do not re-investigate. The correct ordering is demonstrated one level down in `milestone_controller.go:reconcilePlannerDispatch` (Step 2 Running/terminal check at ~286-301 BEFORE Step 2b idempotency guard at ~304). Mirror it exactly.

Purpose: reporter Job spawns immediately on planner completion; planner-level cost ($0.36 observed) is attributed without a 10-min stall.
Output: a RED-first envtest + a minimal reorder of the dispatch state machine.
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/STATE.md
@./CLAUDE.md

<interfaces>
<!-- Extracted from the codebase. Use directly — no exploration needed. -->

ProjectReconciler config fields (internal/controller/project_controller.go):
  - Client, Scheme, Dispatcher, MaxConcurrentReconciles
  - SigningKey string        // MUST be non-empty (testSigningKey) or reconcileProjectPlannerDispatch is a no-op (line 944)
  - CredproxyImage string    // testCredproxyImage
  - HelmProviderDefaults ProviderDefaults{ Image: testSubagentImage }
  - EnvReader podjob.EnvelopeReader   // set to a *mapEnvReader (newMapEnvReader())
  - ReporterImage string     // MUST be non-empty or reporter spawn is skipped (line 1139)
  - SharedPVCName string

Planner Job name (line 955):  fmt.Sprintf("tide-project-%s-1", project.UID)
Reporter Job name (line 1145): fmt.Sprintf("tide-reporter-%s", project.UID)

handleProjectJobCompletion(ctx, project, completedJob *batchv1.Job) (line 1110):
  - reads EnvelopeOut via r.EnvReader.ReadOut(ctx, string(project.UID), string(project.UID))
  - when ReporterImage != "" and reporter Job absent: Creates tide-reporter-<uid> and sets isFirstCompletion=true
  - when isFirstCompletion AND envReadOK: budget.RollUpUsage(...) -> Project.Status.Budget.CostSpentCents

Shared test helpers (already in package controller):
  - newTestProjectReconciler() *ProjectReconciler           (project_controller_test.go:42 — omits SigningKey/EnvReader/ReporterImage; the new test builds its own reconciler)
  - newMapEnvReader() *mapEnvReader  + (m).SetOut(taskUID string, out pkgdispatch.EnvelopeOut)  (suite_test.go:84-96)
  - makeFakeJobTerminal(ctx, c client.Client, name, namespace string, succeeded bool) error    (milestone_controller_test.go:64 — sets SuccessCriteriaMet+Complete, leaves Job present)
  - newPlannerPoolForTest() *pool.Pool   (milestone_controller_test.go:38)
  - makeTestBoundPVC / ensurePVC(ctx, name, ns)             (project_phase3_test.go:34, project_controller_test.go:52)
  - testSigningKey, testCredproxyImage, testSubagentImage   (package-level test consts)
  - k8sClient (direct), mgrClient (cache-backed) — milestone Test 5 uses mgrClient + Eventually for status reads
</interfaces>

REFERENCE PATTERN — milestone_controller.go:reconcilePlannerDispatch (~239-326). Read the Step 2 (~286-301) / Step 2b (~304-326) ordering; project_controller.go must mirror it.

REGRESSION GUARD MODEL — milestone_controller_test.go Test 5 (~212-301): create Project, drive planner, SetOut a non-zero-cost EnvelopeOut, makeFakeJobTerminal, reconcile, assert Project.Status.Budget.CostSpentCents >= cost via Eventually.
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: RED test — terminal planner Job that still exists must spawn reporter + roll up cost</name>
  <files>internal/controller/project_planner_completion_test.go</files>
  <behavior>
    - A Project at PhaseRunning with planner Job tide-project-UID-1 present and Complete (NOT GC'd): reconciling the Project creates Job tide-reporter-UID in the project namespace.
    - Same reconcile: Project.Status.Budget.CostSpentCents is greater-than-or-equal-to the planner EnvelopeOut.Usage.EstimatedCostCents.
    - Control: a still-Running (non-terminal) planner Job leaves no reporter Job and does not advance budget.
  </behavior>
  <action>
    Create a new Ginkgo envtest file internal/controller/project_planner_completion_test.go, package controller, Label("envtest"). Mirror milestone_controller_test.go Test 5 and billing_halt_regression_test.go for the inject-terminal-Job + envReader + Eventually idioms.

    Use a UNIQUE project name (e.g. "test-proj-qqh-completion") and a UNIQUE PVC name; create the bound PVC via makeTestBoundPVC/ensurePVC; DeferCleanup the Project (clear finalizers, Delete) and any Jobs in the namespace.

    Build the reconciler INLINE (do NOT reuse newTestProjectReconciler — it omits the dispatch wiring). Set: Client mgrClient (so cache-backed Eventually reads converge), Scheme k8sClient.Scheme(), Dispatcher &stubDispatcher{}, PlannerPool newPlannerPoolForTest(), EnvReader a captured *mapEnvReader from newMapEnvReader(), SigningKey testSigningKey, CredproxyImage testCredproxyImage, ReporterImage the non-empty literal "ghcr.io/jsquirrelz/tide-reporter:test", SharedPVCName the unique PVC, HelmProviderDefaults ProviderDefaults{Image: testSubagentImage}.

    Drive to the bug-manifesting state by calling r.reconcileProjectPlannerDispatch(ctx, project) DIRECTLY (it is in-package). First Status().Patch the Project to Phase = tidev1alpha2.PhaseRunning (the Running branch is where the unreachable terminal check lives), re-Get it, then call reconcileProjectPlannerDispatch once to create the planner Job. Re-Get the project to capture its UID. Assert the planner Job tide-project-UID-1 now exists (sanity: dispatch fired).

    Seed cost: const plannerCostCents = int64(36); envReader.SetOut(string(project.UID), pkgdispatch.EnvelopeOut{TaskUID: string(project.UID), ExitCode: 0, ChildCount: 0, Usage: pkgdispatch.Usage{InputTokens: 1000, OutputTokens: 200, EstimatedCostCents: plannerCostCents}}). ChildCount 0 keeps the succession gate from requeuing on missing children.

    Make the planner Job terminal WHILE IT STILL EXISTS: makeFakeJobTerminal(ctx, mgrClient, fmt.Sprintf("tide-project-%s-1", project.UID), ns, true). Do NOT delete the Job — the bug is specifically that the still-present Job triggers the early-return.

    Re-Get the project (for resourceVersion), then call r.reconcileProjectPlannerDispatch(ctx, project) again. Assert via Eventually(mgrClient, 5s, 100ms):
      (a) Get Job tide-reporter-UID in ns Succeeds (reporter spawned).
      (b) project.Status.Budget.CostSpentCents is greater-than-or-equal-to plannerCostCents.

    Add a second It() control spec: identical setup but leave the planner Job NON-terminal (skip makeFakeJobTerminal); after the second reconcile, assert tide-reporter-UID is NotFound and budget remains 0. This proves the fix does not over-trigger.

    Do NOT modify project_controller.go in this task. Do NOT touch handleProjectJobCompletion, the reporter logic, milestone_controller.go, or values.yaml.
  </action>
  <verify>
    <automated>go test ./internal/controller/... -count=1 -args -ginkgo.focus='terminal planner Job' 2>&amp;1 | tail -40</automated>
  </verify>
  <done>The new test file compiles and the primary spec FAILS (RED) on current project_controller.go — reporter Job absent and/or budget 0 because Step 1b returns before the terminal check. The control (still-Running) spec PASSES. Confirm RED by reading the Ginkgo failure summary; do not claim it fails without seeing the failing assertion.</done>
</task>

<task type="auto">
  <name>Task 2: Fix — move terminal-state check ahead of the idempotency early-return</name>
  <files>internal/controller/project_controller.go</files>
  <action>
    In reconcileProjectPlannerDispatch (~941-1095), restructure so the Job-terminal-state handling precedes the blanket "Job exists -> return" early-return, mirroring milestone_controller.go:reconcilePlannerDispatch (Step 2 ~286-301 BEFORE Step 2b ~304).

    REMOVE the standalone Step 1b block (~957-971) that does `if planner Job exists return ctrl.Result{}, nil` unconditionally. MOVE the Running-branch terminal handling (current Step 2, ~973-989) so it runs first, with these mutually-exclusive arms (mirror milestone ordering exactly):
      - project.Status.Phase == PhaseRunning:
          * planner Job present AND isJobTerminal -> return handleProjectJobCompletion(ctx, project, &job)
          * planner Job present AND NOT terminal -> return ctrl.Result{}, nil   (in-flight; do nothing)
          * planner Job absent (NotFound) -> return handleProjectJobCompletion(ctx, project, nil)  (TTL/GC fallback, preserved)
          * Get error other than NotFound -> return the error
      - NOT Running (planner not yet dispatched): preserve the idempotency intent — if a planner Job already exists, return nil (do not create a second one); if absent, fall through to the dispatch path (pool acquire, envelope build, Job create, Status.Phase=Running patch) UNCHANGED.

    Keep everything from the BillingHalt/BudgetBlocked holds (~997-1010) through the Status patch (~1080-1092) byte-for-byte. Keep the SigningKey no-op guard (~944) and the terminal short-circuit (~948-953) at the top, unchanged. Update the Step comment numbering/prose to match the new order and cite milestone_controller.go reconcilePlannerDispatch Step 2/2b as the mirror, matching surrounding comment density.

    Do NOT modify handleProjectJobCompletion's body, BuildReporterJob, BuildPlannerEnvelope, the milestone controller, or values.yaml.
  </action>
  <verify>
    <automated>make test 2>&amp;1 | tee /tmp/qqh-fix-test.log | tail -30; echo "MAKE_EXIT=${PIPESTATUS[0]}"; grep -nE '^--- FAIL|^FAIL[[:space:]]' /tmp/qqh-fix-test.log || echo NO_GO_TEST_FAIL</automated>
  </verify>
  <done>The Task 1 primary spec now PASSES (GREEN) and the control spec still PASSES. `make test` exits 0 (MAKE_EXIT=0) with NO_GO_TEST_FAIL — read both the Ginkgo summary AND the MAKE_EXIT + grep, per CLAUDE.md (Ginkgo green alone is insufficient). No other controller spec regressed.</done>
</task>

</tasks>

<verification>
- `make test` exits 0 (unit/envtest tier; integration tier excluded — runs via test-int-fast). Confirm MAKE_EXIT=0 AND no `--- FAIL`/`FAIL ` line in the log.
- New spec project_planner_completion_test.go: primary case green, control case green.
- `git diff internal/controller/project_controller.go` touches ONLY reconcileProjectPlannerDispatch; handleProjectJobCompletion body unchanged.
- No changes to milestone_controller.go or charts/tide/values.yaml.
</verification>

<success_criteria>
- A terminal-but-still-present planner Job spawns tide-reporter-UID on the next reconcile (no 10-min TTL stall).
- Planner Usage rolls up into Project.Status.Budget.CostSpentCents on that reconcile.
- A still-running planner Job neither spawns a reporter nor advances budget (no over-trigger).
- Full unit/envtest suite green; reporter spawn + budget rollup logic and the FIXED values.yaml contract untouched.
</success_criteria>

<output>
Create `.planning/quick/260617-qqh-fix-project-controller-planner-completio/260617-qqh-SUMMARY.md` when done.
</output>
