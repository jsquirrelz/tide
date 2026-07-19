---
phase: 51-the-task-loop
plan: 08
subsystem: testing
tags: [ginkgo, kind, layer-b, verifier-dispatch, concurrency-cap, ESC-04]

# Dependency graph
requires:
  - phase: 51-the-task-loop (plan 06)
    provides: verifierInFlightCount, dispatchVerifier, LevelPhaseVerifying, defaultVerifierConcurrencyCap=2
  - phase: 51-the-task-loop (plan 07)
    provides: handleVerifierCompletion (verdict consumption fail-closed to Failed on nil Verdict)
provides:
  - "test/integration/kind/verifier_concurrency_test.go — Layer B Ginkgo spec proving concurrent role=verifier Job dispatch stays under the sized cap and drains to zero, no dispatch-slot leak"
  - "Documented prerequisite gap: TaskReconcilerDeps.VerifierImage unwired in cmd/manager/main.go (blocks both this spec and Task 2's live proof until closed)"
  - "Documented limitation: verifier Jobs carry no estimated-cost reservation-rederivation label (in-process reservation no-leak invariant stays envtest-only, not re-provable from Layer B)"
affects: []

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "File-local mirrors of unexported controller helpers (isJobTerminalVerifier, countNonTerminalVerifierJobs) for Layer B ground-truth Job-count observation, matching chaos_resume_test.go's isJobSucceededShort precedent — kind_integration deliberately never imports internal/controller"

key-files:
  created:
    - test/integration/kind/verifier_concurrency_test.go
  modified: []

key-decisions:
  - "verifierConcurrencyCap is pinned as a local literal (=2), not imported — internal/controller's defaultVerifierConcurrencyCap is unexported, matching 51-06-SUMMARY.md's own note that Plan 08 pins/re-tunes the cap value from outside the package."
  - "The spec is verdict-agnostic: the final assertion only checks a Task leaves Phase=Verifying (Succeeded OR Failed both satisfy it), because no working verifier image is wired yet (see Known Gaps) and Plan 07's handleVerifierCompletion fail-closes an unset Verdict to Failed, not Succeeded — asserting a specific terminal phase would conflate 'the concurrency mechanism works' with 'the verdict was APPROVED', which is a different, already-envtest-covered claim."
  - "The in-process budget.ReservationStore no-leak invariant is NOT re-asserted here — verifier Jobs carry no tideproject.k8s/estimated-cost label (confirmed via grep of dispatchVerifier's BuildOptions literal), so there is no external, Layer-B-observable proxy for it. That invariant is already proven by internal/controller/task_verify_dispatch_test.go's envtest cap-hit spec (TotalReserved()==0). This kind spec proves the externally-observable half of ESC-04 only: live Job-count-under-cap + drain-to-zero + no Task stranded in Verifying."

patterns-established:
  - "Layer B concurrency-cap specs poll Job counts directly via k8sClient.List + a file-local isJobTerminal mirror, treating the reconciler's own cached-client view as untrusted — the assertion is against ground truth, not a copy of the controller's own cap-gate logic."

requirements-completed: []

# Metrics
duration: ~40min
completed: 2026-07-19
---

# Phase 51 Plan 08: Verifier Concurrency Kind Spec (Task 1 of 2) Summary

**Task 1 complete: a new Layer B Ginkgo spec (`test/integration/kind/verifier_concurrency_test.go`) drives 3 contract-bearing Tasks through executor-complete and asserts concurrent `role=verifier` Job dispatch never exceeds the sized cap (2) and drains to zero — builds, vets, and lints clean; NOT run live. Task 2 (the human-verify live Task-loop proof) is NOT executed — this plan halts at that checkpoint, below, with two newly-discovered prerequisite gaps documented for the operator's runbook.**

## Performance

- **Duration:** ~40 min (context-gathering + spec authoring + iterative gap analysis + lint fix)
- **Tasks:** 1/2 completed (Task 2 is a `checkpoint:human-verify`, intentionally not executed — see below)
- **Files modified:** 1 (created)

## Accomplishments

- `test/integration/kind/verifier_concurrency_test.go` (new): a Ginkgo `Label("kind")` spec mirroring `chaos_resume_test.go`'s real-Job-lifecycle shape — creates a Project/Milestone/Phase/Plan hierarchy via the shared typed fixture builders, applies 3 contract-bearing Tasks (`Verification.Phase="Locked"`, `GateCommand="true"`, `MaxIterations=1`) with no `dependsOn` (all wave-0 siblings), and:
  1. waits for all 3 to reach `Status.Phase == "Verifying"` (proves `dispatchVerifier` was attempted for every Task, independent of whether the verifier Job itself can actually be created)
  2. polls the live `role=verifier` Job count (via `k8sClient.List`, not the controller's cached client) over a 90s window, asserting it never exceeds `verifierConcurrencyCap=2`
  3. waits for the count to drain to zero
  4. confirms no Task remains stranded in `Verifying` once drained (verdict-agnostic — Succeeded or Failed both satisfy it)
- Two genuine prerequisite gaps discovered and documented (not fixed — outside this plan's declared file scope of `test/integration/kind/verifier_concurrency_test.go` only):
  1. **`TaskReconcilerDeps.VerifierImage` is unwired in `cmd/manager/main.go`.** Confirmed via `grep -n VerifierImage cmd/manager/main.go` returning zero hits inside the `TaskReconciler`'s `Deps` struct literal, while every sibling image field (`CredproxyImage`, `ReporterImage`, `TidePushImage`, `ImportImage`) is wired from a flag/env var. Until this is wired, `dispatchVerifier`'s `Job` Create call resolves to an empty container image, which the K8s API server rejects at admission — the reconcile errors and requeues indefinitely via `checkVerifyingState`'s NotFound-retry path. This means: run live today, this spec's cap/drain assertions pass trivially (zero Jobs are ever actually persisted) but the final "no Task stranded in Verifying" assertion will correctly FAIL — a deliberate fail-loud design, not a bug in the spec.
  2. **Verifier Jobs carry no `tideproject.k8s/estimated-cost` label.** Confirmed via `grep -n EstimatedCostCents internal/controller/task_controller.go` — the executor and planner dispatch sites set it on `podjob.BuildOptions`; `dispatchVerifier`'s own `BuildOptions` literal does not. This means `budget.RederiveReservations` (the documented mechanism for reconstructing in-process reservation state from Job labels after a manager restart) can never see verifier reservations, and — relevant to this plan — there is no external, Layer-B-observable proxy for "no orphaned verifier reservation." That invariant stays proven only by the existing envtest (`task_verify_dispatch_test.go`'s `TotalReserved()==0` cap-hit assertion).
- Verified genuinely: `go build ./test/integration/kind/...`, `go vet ./test/integration/kind/...`, `go test -c -o /dev/null ./test/integration/kind/...` (test-binary compile, since `go build` alone skips `_test.go` files), and `./bin/golangci-lint run ./test/integration/kind/...` all clean (one `modernize` finding for a C-style `for i := 0; i < N; i++` loop was fixed in place to `for i := range N`, re-verified clean).
- `make test-int` was deliberately **NOT run** — per this plan's explicit instruction, standing up a kind cluster is Task 2's live-proof step.

## Task Commits

1. **Task 1: kind concurrent-dispatch spec — verifier dispatch stays under the sized cap** - `5dfed19c` (test)

Task 2 was NOT executed (see Checkpoint below) — no commit.

## Files Created/Modified

- `test/integration/kind/verifier_concurrency_test.go` (new) - Layer B Ginkgo spec: cap/drain/no-leak assertions for concurrent verifier dispatch (ESC-04)

## Decisions Made

See `key-decisions` in frontmatter: (1) the cap value is pinned as a local literal since the source constant is unexported; (2) the spec is deliberately verdict-agnostic; (3) the in-process reservation no-leak invariant is left to the existing envtest rather than fabricated as an unobservable external assertion.

## Deviations from Plan

None — Task 1 was executed exactly as specified (file scope: `test/integration/kind/verifier_concurrency_test.go` only). The two gaps above are pre-existing production-code findings from Plan 06/07, discovered during test design; they were deliberately **not** auto-fixed under Rules 1–3 because closing them requires changes to `cmd/manager/main.go` (and likely the Helm chart) which fall outside this plan's declared file scope and outside Task 1's stated deliverable ("write the spec, verify it compiles — do not attempt to run the live kind suite"). They are surfaced here for the Task 2 runbook below and should be closed before Task 2 is attempted.

## Issues Encountered

None beyond the two documented gaps above (discovered via `grep`, not via a failed run — this plan's Task 1 never executed the live suite).

## User Setup Required

None for Task 1. Task 2 (below) requires operator action.

## Next Phase Readiness

- Task 1's spec is ready to run once the `VerifierImage` wiring gap (above) is closed; no further test-file changes are anticipated.
- Task 2 remains open — see Checkpoint below.

---
*Phase: 51-the-task-loop*
*Completed (Task 1 only): 2026-07-19*

## Self-Check: PASSED

`test/integration/kind/verifier_concurrency_test.go` confirmed present on disk; commit `5dfed19c` confirmed in `git log`.

---

## CHECKPOINT REACHED

**Type:** human-verify
**Plan:** 51-08
**Progress:** 1/2 tasks complete

### Completed Tasks

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | kind concurrent-dispatch spec — verifier dispatch stays under the sized cap | `5dfed19c` | `test/integration/kind/verifier_concurrency_test.go` |

### Current Task

**Task 2:** Live Task-loop proof on kind (real gate command, billable)
**Status:** blocked — awaiting operator action (NOT executed; billable live run against the real Anthropic API)
**Blocked by:** requires explicit operator approval to spend real money on a kind cluster; also requires closing the `VerifierImage` wiring gap discovered during Task 1 (see above) before the loop can dispatch a real verifier Job at all.

### Checkpoint Details

**What would be built:** The full Task loop proven live: a contract-bearing Task whose LOCKED gate command fails is checked by the independent read-only langgraph verifier, classified REPAIRABLE, re-attempted with a bounded evidence packet, and (if still failing) halted on `ConditionVerifyHalt` at `maxIterations` — with the verifier counted against the concurrency cap and its cost bounded by `LoopPolicy.BudgetCents`.

**Operator runbook (what to run, once approved):**

0. **Prerequisite — close the `VerifierImage` gap first.** `cmd/manager/main.go`'s `TaskReconciler`'s `Deps` literal does not set `VerifierImage`. Add a flag/env var mirroring the existing `--credproxy-image` convention (e.g. `--verifier-image` / `TIDE_VERIFIER_IMAGE`, `envOrDefault`-style) and wire it into `controller.TaskReconcilerDeps{VerifierImage: ...}`. This is a small, scoped `cmd/manager/main.go` change (and possibly a `--set` override at `helm upgrade --install` time, mirroring `images.stubSubagent.tag=test` in `test/integration/kind/suite_test.go`'s `helmControllerArgs`) — route it through its own GSD task/plan or as an explicit Rule-3 fix at the start of the Task 2 session, since it is a genuine blocker for everything below.
1. **Single-node kind OOM discipline** (CLAUDE.md Operating Notes): on `kind-tide-dogfood` or a throwaway cluster, delete → recreate → prewarm. Do NOT run the acceptance cluster concurrently. Confirm the real key is present at `~/.tide/anthropic.key`.
2. Build + kind-load `tide-langgraph-verifier` and the `manager` image (with the `VerifierImage` wiring from step 0) at dev-head.
3. Apply a Project/Task with `spec.verification` whose `gateCommand` deterministically FAILS on the first attempt (e.g. asserting a file the executor must create), `maxIterations: 1` (or 2), `onExhaustion: requireApproval`.
4. Observe: executor runs and believes complete → `Task.Status.Phase` reaches `Verifying` → a verifier Job (`role=verifier`) dispatches → `kubectl get task -o yaml` shows the verdict → `REPAIRABLE` → a FRESH attempt (`Attempt` incremented) → at `maxIterations` without `APPROVED`, `kubectl get project -o yaml` shows `ConditionVerifyHalt: True` and dispatch parks.
5. Confirm the verifier ran as an independent read-only Job (RO worktree, no git-write creds) and the reservation settled (no orphaned reservation / no `BudgetCents` overrun — this half is only directly observable via manager logs or in-process, per the "Known Limitation" documented above; there is no CRD/label proxy for it).
6. Confirm the green-gate path: fix the Task so the gate passes → verifier returns `APPROVED` → Task `Succeeded`.
7. Record the observed transitions + a proof artifact (`kubectl` yaml / manager-log excerpt) + approximate spend in a follow-up SUMMARY update.
8. Optionally: also actually run `test/integration/kind/verifier_concurrency_test.go` (Task 1's spec, now unblocked by step 0) via `make test-int` and confirm `MAKE_EXIT=0` with no `^--- FAIL|^FAIL\s` lines — this closes the loop on Task 1's own live verification, deferred from this session per its explicit "do not run the live suite" instruction.

### Awaiting

Operator confirmation to proceed with a **billable** live run against the real Anthropic API, plus the `VerifierImage` wiring fix (step 0 above) as a precondition. Resume with: "approved" + the observed loop transitions, or describe what diverged. Until then, Plan 51-08 stays open (Task 2 incomplete) — do not mark ESC-04 as fully satisfied in REQUIREMENTS.md.
