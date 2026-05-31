---
slug: gate-flow-envreader-race
status: resolved
trigger: "Layer A envtest gate-flow spec 'approve-milestone annotation handshake transitions Milestone Succeeded' (gates_test.go:133) deterministically RED — manager auto-reconciler with a separate suite-level mapEnvReader races the test's manually-driven reconciler and writes Status.Phase=Failed (EnvelopeReadFailed) before the spec reaches AwaitingApproval."
created: 2026-05-31
updated: 2026-05-31
phase: 07-project-to-milestone-authoring-and-self-bootstrap
---

# Debug: gate-flow-envreader-race

## Symptoms

- **Expected:** `make test-int-fast` (Layer A / envtest) is 29/29. The spec `approve-milestone annotation handshake transitions Milestone Succeeded` (`test/integration/envtest/gates_test.go:92`) drives a `gates.Milestone=approve` Milestone through planner-dispatch + job-completion and asserts (step 4, `gates_test.go:124-133`) that `Status.Phase == "AwaitingApproval"` with `ConditionWaveOrLevelPaused=True` / `Reason=AwaitingApproval`, then after the approve annotation reaches `Succeeded`.
- **Actual:** The spec fails deterministically at `gates_test.go:133` — `Eventually` times out after 5s with `Expected <string>: Failed to equal <AwaitingApproval>`. The Milestone reaches `Status.Phase="Failed"` instead of parking at `AwaitingApproval`. `make test-int-fast` = **28 Passed | 1 Failed** (`Ran 29 of 29`). Reproduced 3/3 (1 full Layer A run + 2 focused isolated runs).
- **Error:** No log line for the failure (`patchMilestoneFailed` only sets a Condition). The Failed status is written by `milestone_controller.go:396` `patchMilestoneFailed(ctx, ms, "EnvelopeReadFailed", err.Error())`. Manager-side log shows the two reconcilers fighting: `ERROR Reconciler error ... milestone ... "the object has been modified; please apply your changes to the latest version"` on `gate-it-ms-1`.
- **Timeline:** Surfaced 2026-05-31 at the start of the Phase 7 env-gated verification. The spec + its harness wiring are **pre-Phase-7** (gates_test.go = commit 493a63c 2026-05-19; manager milestone-reconciler suite registration = commit f30b72a). Commit `fed7556` is titled "fix(04.1): close TestGateApproveFlow flake — milestone AwaitingApproval re-entry" — i.e. this spec has prior flake history. Phase 7's cascade-9/10 edits to `milestone_controller.go` are in the FRESH-dispatch path (the `List(phases)` idempotency guard, lines 239-250) and do not touch the Running→`handleJobCompletion` completion race that writes `Failed`. So this is a pre-existing test-isolation defect, NOT Phase 7 production code.
- **Reproduction:**
  ```
  export KUBEBUILDER_ASSETS="/Users/justinsearles/Projects/tide/bin/k8s/1.33.0-darwin-amd64"
  go test ./test/integration/envtest/... -timeout=3m -ginkgo.v \
    --ginkgo.focus="approve-milestone annotation handshake" -count=1
  ```
  Fails 3/3 even in isolation (only this 1 spec running) on this machine.

## Current Focus

hypothesis: The manager's auto-registered MilestoneReconciler (suite_test.go:242) uses a SEPARATE suite-level mapEnvReader that is never SetOut for the spec's Milestone UID. It Owns(&Job{}), so the test's `makeFakeJobTerminalGates` Job re-enqueues it; with the Milestone in Running + Job terminal it runs `handleJobCompletion` → `EnvReader.ReadOut` MISS → error → `patchMilestoneFailed("EnvelopeReadFailed")` → `Status.Phase=Failed`. This wins the race against the test's own manually-driven reconciler (which has the populated local `envReader` and would reach AwaitingApproval). Once `Failed` is written, the terminal short-circuit (`milestone_controller.go:186`) locks it.
next_action: Confirm the root cause by reading the gate-flow specs + suite reconciler wiring, then apply the minimal deterministic fix (share the manager's suite-level envReader so the spec SetOuts on the same reader the manager reads — OR otherwise prevent the manager auto-reconciler from racing these specs with stale envelope data). Verify focused green, then full `make test-int-fast` = 29/29. Apply symmetrically to the sibling gate specs (TestRejectHalts etc.) if they share the pattern.

## Evidence

- timestamp: 2026-05-31 — `ReadOut` (suite_test.go:117-125) returns `fmt.Errorf("no envelope out for task UID %q")` on a UID miss — confirms a miss is an ERROR, which routes to `patchMilestoneFailed("EnvelopeReadFailed")` at milestone_controller.go:396.
- timestamp: 2026-05-31 — Two distinct `mapEnvReader` instances: manager's at suite_test.go:240 (`envReader := newMapEnvReader()`), test's at gates_test.go:113 (separate `newMapEnvReader()` passed to `newMilestoneReconcilerForGateIT`). The spec `SetOut`s only on its local reader.
- timestamp: 2026-05-31 — `handleJobCompletion` Failed paths are only line 396 (EnvelopeReadFailed) and 405 (ChildCRDMaterializationFailed, gated on `len(ChildCRDs)>0`); the spec's envelope has empty ChildCRDs, so the Failed must come from 396 via the manager reader's miss.
- timestamp: 2026-05-31 — CAUSE CONFIRMED at source: suite_test.go:240 `envReader := newMapEnvReader()` is local to `newPhase2ReconcilersForTest` and shared only between the manager's MilestoneReconciler (line 245) and TaskReconciler (line 286); gates_test.go:113 + :202 each create a private `newMapEnvReader()`. The spec SetOut (gates_test.go:120) lands on the private reader; the manager reconciler, re-enqueued by Owns(&Job{}) when `makeFakeJobTerminalGates` marks the Job terminal, reads its own empty reader → miss → Failed. milestone_controller.go production logic (terminal short-circuit :186, AwaitingApproval pause :198) is correct — defect is purely test isolation (two readers).
- timestamp: 2026-05-31 — BASELINE CONTROL: with the fix stashed, `make test-int-fast` = 28 Passed | 1 Failed with the failing spec being exactly `TestGateApproveFlow approve-milestone annotation handshake` (Ran 29 of 29 in 31.5s). Confirms the fix is the variable that flips this spec green.

## Eliminated

- hypothesis: "Phase 7 cascade-9/10 guard caused the regression" — ELIMINATED: the guard (milestone_controller.go:239-250) is in the fresh-dispatch path and runs an extra `List(phases)` but does not change the Running→completion path; the gate spec + manager wiring are pre-Phase-7 (493a63c / f30b72a).
- hypothesis: "CPU-contention flake under full Layer A suite" — ELIMINATED: fails 3/3 INCLUDING focused isolated runs (1 spec only), so it is deterministic on this machine, not a contention flake.

## Resolution

root_cause: Two separate `mapEnvReader` instances in the Layer A envtest harness. The manager's auto-registered MilestoneReconciler used a private suite-local reader (suite_test.go:240) that was never `SetOut` for the gate spec's Milestone UID; because that reconciler `Owns(&Job{})`, the spec's fake-terminal Job re-enqueued it, and `handleJobCompletion` → `ReadOut` missed → `patchMilestoneFailed("EnvelopeReadFailed")` → `Status.Phase=Failed`, beating the spec's own manually-driven reconciler (which held a populated private reader). The terminal short-circuit (milestone_controller.go:186) then locked `Failed`, so the spec never reached `AwaitingApproval`. Production reconciler logic was correct — this was a test-isolation defect only.

fix: Test-harness only, confined to `test/integration/envtest/`. Promoted the suite-level reader to a package-scoped `suiteEnvReader` (suite_test.go) and injected that single instance into both the manager's MilestoneReconciler and TaskReconciler; changed the gate-flow specs (`TestGateApproveFlow` and `TestRejectHalts`, gates_test.go) to `SetOut` on the shared `suiteEnvReader` instead of a private reader, so the background manager reconciler reads the same populated envelope as the manually-driven reconciler. Added a `sync.RWMutex` to `mapEnvReader` so concurrent `SetOut` (test goroutine) / `ReadOut` (manager goroutine) on the now-shared instance is race-free. `TestWavePauseBetweenWaves` uses the PlanReconciler with no envReader and was left unchanged. No production controller, chart, or script edits.

verification:
- Focused: `go test ./test/integration/envtest/... --ginkgo.focus="approve-milestone annotation handshake" -count=1` → `Ran 1 of 29 Specs ... 1 Passed | 0 Failed | 0 Pending | 28 Skipped`.
- Full Layer A: `make test-int-fast` → `Ran 29 of 29 Specs in 26.4s ... 29 Passed | 0 Failed | 0 Pending | 0 Skipped` (two consecutive stable runs at 26.4s / 26.8s; one earlier run hit a transient 20s-timeout flake on the unrelated budget spec under a 46s machine-load spike — the budget spec passes in isolation and in both steady-state runs).
- Race detector: `go test ./test/integration/envtest/... -race --ginkgo.focus="gate-flow envtest"` → `3 Passed | 0 Failed`, 0 DATA RACE — confirms the shared-reader RWMutex is correct.
- Baseline control: fix stashed → the gate-flow spec is RED (28/29), confirming the fix is what closes the race.
