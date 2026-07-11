---
status: investigating
trigger: "Root-fix two Layer A envtest flakes from PR #9 CI (branch phase-38-small-independents). Policy: no retry-on-red; deterministic root fixes only."
created: 2026-07-11T00:00:00Z
updated: 2026-07-11T00:00:00Z
---

## Current Focus

hypothesis: |
  FAILURE A (CONFIRMED mechanism): newMilestoneReconcilerForGateIT (gates_test.go:723) omits SigningKey.
  reconcilePlannerDispatch Step 5 calls credproxy.Sign(r.SigningKey,...) which hard-errors on empty key
  (token.go:62-63). So the manually-driven reconciler can NEVER create the planner Job; all 5
  driveMSReconcile passes fail (errors swallowed by `_, _ =`). The Job is only ever created by the
  manager's auto MilestoneReconciler (suite_test.go:272, has testSigningKey) on its own async schedule.
  gates_test.go:295 Gets the Job immediately after the synchronous drive -> pure race. Under CI load the
  auto controller's dispatch pass aborts repeatedly on milestone Update 409s (observed storm on gate-it-ms-1)
  with per-item backoff -> Job absent at :295 -> NotFound.
  FAILURE B (investigating): project controller derives/creates Wave CRs. Log shows single reconcile pass
  8c0dd199 pruned waves 0,1 (currentWaveCount=0, post-teardown of prior spec) right as PERSIST-03 started;
  then NO wave creation for 30s despite nc-task-a existing. Need: what triggers project reconcile on Task
  create, and which write paths return raw 409s feeding per-item backoff on the SHARED project key
  (global-wave-test-project reused across all specs in the file -> backoff accumulates across specs).
test: Read project_controller.go SetupWithManager + global wave derivation + prune; task/plan controller 409-returning writes.
expecting: Find the trigger chain Task-create -> Project reconcile, and the raw-409 paths that starve it.
next_action: grep project_controller.go for Watches/Task mapping and read the wave derivation section (~2400-2500).

reasoning_checkpoint:
  hypothesis: "A: gate IT spec races background reconciler because its own driven reconciler cannot dispatch (empty SigningKey)"
  confirming_evidence:
    - "token.go:62: Sign returns error on empty signingKey; gates_test.go:723 constructor sets no SigningKey"
    - "CI log: Job NotFound at :295 + 409 storm on gate-it-ms-1 (auto controller passes aborting pre-dispatch)"
    - "suite auto MilestoneReconciler HAS testSigningKey -> explains why spec normally passes (auto wins race)"
  falsification_test: "Wire SigningKey into constructor + drive-until-Job-exists; if Job still absent, mechanism wrong"
  fix_rationale: "Make the driven reconciler dispatch-capable and wait on the observable precondition (Job exists) instead of 5 blind passes"
  blind_spots: "makeFakeJobTerminalGates Status().Update could still 409 (nothing else writes Job status in envtest, but cache staleness possible) — harden with RetryOnConflict per house idiom"

## Symptoms

expected: Both specs pass deterministically in CI (they pass on main and passed one round earlier on identical tree).
actual: |
  A) run 29157471359 job "TIDE Phase 1 gates": spec "approve-milestone annotation: transitions Running+ApprovedByUser
     then Succeeded (leaf — ChildCount=0)" FAILED at gates_test.go:295.
     Error body (from gh run view --log-failed): StatusError 404 NotFound:
     'Job.batch "tide-milestone-08da2690-661b-47a5-bfa5-b6a3e2e23a2b-1" not found'.
     Log shows milestone 409 storm on gate-it-ms-1 AND "milestone cleanup" + job-deletion propagation warnings near the failure.
  B) run 29157470371 job "Layer A envtest integration tier (no flake retries)": spec
     "asserts Project.Status has no Schedule/Waves[] cached aggregate (PERSIST-03)" FAILED at
     global_wave_derivation_test.go:121 — Wave CR tide-wave-global-wave-test-project-0 not found after 30.001s.
     Sustained task-controller 409 storm across the window.
errors: |
  A: Job.batch "tide-milestone-08da2690-661b-47a5-bfa5-b6a3e2e23a2b-1" not found (404)
  B: Wave CR tide-wave-global-wave-test-project-0 should exist ... not found (Eventually 30s/500ms timeout)
reproduction: Order/timing-dependent; identical tree passed both jobs one round earlier. CI runs full envtest suites with manager + reconcilers live.
started: Pre-existing on main (git log origin/main..HEAD -- <files> empty); surfaced on PR #9 CI round 2.

## Eliminated

- hypothesis: "FAILURE A is a 409 conflict on Job.Status().Update needing retry.RetryOnConflict in makeFakeJobTerminalGates"
  evidence: gh run view 29157471359 --log-failed shows the error body is StatusError 404 NotFound, not 409 Conflict.
  timestamp: 2026-07-11 (investigation start)

- hypothesis: "FAILURE B: task/plan-controller raw 409s -> per-item exponential backoff -> wave-creation reconcile chain exceeds 30s"
  evidence: |
    Full job log (4596 lines): 409s per second cluster at 15:15:00-07 (prior specs) and 15:15:37+ (next spec).
    The failing window 15:15:08-15:15:36 contains ZERO "Operation cannot be fulfilled" lines of any kind and
    ZERO project-controller log lines. Only 2 project 409s exist in the entire job. A backed-off item would
    still log "Reconciler error" on each retry; total silence means reconciles were DROPPED (Get -> NotFound),
    not delayed. Next spec's project create at 15:15:37.8736 -> wave created at .9874 (120ms) proves the
    controller was healthy and fast the whole time.
  timestamp: 2026-07-11

- hypothesis: "FAILURE A: milestone controller cleanup deleted the Job before the test's terminal patch"
  evidence: The "milestone cleanup" + job-deletion propagation warnings appear AFTER the [FAILED] marker (15:17:49.9002 vs .8895) — they are the AfterEach fixture teardown, not a pre-failure actor.
  timestamp: 2026-07-11

## Evidence

- timestamp: 2026-07-11
  checked: gh run view 29157471359 --repo jsquirrelz/tide --log-failed, spec failure block
  found: |
    Error at gates_test.go:295 is 404 NotFound for Job tide-milestone-08da2690-661b-47a5-bfa5-b6a3e2e23a2b-1.
    Immediately after the FAILED marker: milestone controller "milestone cleanup" INFO for gate-it-ms-1 and six
    "child pods are preserved by default when jobs are deleted" warnings (Job deletions).
    Also sustained 409 storm on milestones "gate-it-ms-1" from milestone controller status writes.
  implication: The Job either was deleted by a cleanup path racing the test, or was never created; need test source + controller cleanup source.

## Resolution

root_cause: |
  A) newMilestoneReconcilerForGateIT omitted SigningKey, so the test-driven MilestoneReconciler aborted
     every dispatch pass at credproxy.Sign ("signingKey must not be empty", token.go:62) BEFORE Job
     creation — errors swallowed by driveMSReconcile's `_, _ =`. The planner Job was only ever created by
     the manager's background auto-reconciler; gates_test.go:295 read the Job immediately after the
     synchronous drives — a pure race the spec loses when the auto-reconciler's passes abort on benign
     milestone Update 409s (observed storm on gate-it-ms-1) with per-item backoff.
  B) global_wave_derivation_test.go BeforeEach did Create + IgnoreAlreadyExists while the previous spec's
     AfterEach project deletion was still terminating (finalizer). The AlreadyExists was swallowed, the old
     project finished deleting ~1ms later, and the spec ran with NO Project — ProjectReconciler dropped
     every Task-mapped reconcile at the NotFound fetch, so Wave CRs were never derived. Log proof: "project
     cleanup" at 15:15:07.7908, ValidateCreate at .7918, then ZERO project-controller activity and ZERO 409s
     for the whole 30s window; next spec's create at 15:15:37.87 produced waves in 120ms.
fix: |
  A) test/integration/envtest/gates_test.go:
     1. newMilestoneReconcilerForGateIT now mirrors the suite auto-reconciler config (SigningKey,
        CredproxyImage, HelmProviderDefaults) so the driven reconciler is dispatch-capable.
     2. Approve-flow spec drives reconcile until the planner Job is observable (Eventually 15s/100ms)
        instead of 5 blind passes before the terminal patch.
     3. makeFakeJobTerminalGates wrapped in retry.RetryOnConflict with refetch inside the closure
        (house optimistic-lock idiom).
  B) test/integration/envtest/global_wave_derivation_test.go:
     BeforeEach now create-or-waits: on AlreadyExists it checks DeletionTimestamp and retries until a
     live (non-terminating) Project exists (Eventually 20s/100ms, fresh object per attempt).
verification: |
  - go vet ./test/integration/envtest/ exit 0; gofmt clean
  - Focused run of both specs: Ran 2 of 55 Specs — 2 Passed, 0 Failed, go test exit 0
  - Full suite (go test ./test/integration/envtest/... -ginkgo.label-filter=envtest, same as CI Layer A tier)
    3 consecutive runs: RUN 1 EXIT=0, RUN 2 EXIT=0, RUN 3 EXIT=0; three "ok" package lines; zero '--- FAIL' lines
files_changed:
  - test/integration/envtest/gates_test.go
  - test/integration/envtest/global_wave_derivation_test.go
