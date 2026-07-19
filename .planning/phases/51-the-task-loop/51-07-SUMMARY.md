---
phase: 51-the-task-loop
plan: 07
subsystem: verification-loop

# Dependency graph
requires:
  - phase: 51-the-task-loop (plan 03)
    provides: synthesizeEvaluatorSpan (standalone emitter, no call site yet)
  - phase: 51-the-task-loop (plan 05)
    provides: checkVerifyHalt/setVerifyHaltIfNeeded (verify_halt.go)
  - phase: 51-the-task-loop (plan 06)
    provides: LevelPhaseVerifying, checkVerifyingState (forward half), dispatchVerifier, buildVerifierEnvelopeIn, the verifier Job's terminal-branch placeholder
provides:
  - "handleVerifierCompletion — the BACKWARD half of the verifier sub-state-machine: fail-closed ClassifyVerdict consumption wired onto checkVerifyingState's terminal branch"
  - "hasDeterministicFailure — D-06 controller-side re-check: a Finding{severity:blocker,dimension:gate-command} dominates even a top-level APPROVED verdict"
  - "repairOrHalt/escalateSystem/intersectsProtected/protectedPathsFor — TASK-06 structural anti-gaming: RunEvidence.ChangedFiles intersecting the protected evaluator/fixture path set is a system escalation, never a pass"
  - "dispatchRepairAttempt/stageEvidencePacket — TASK-02: a REPAIRABLE verdict mints a fresh quality-iteration attempt (Attempt++, new attemptID) seeded with the ORIGINAL locked spec + a staged evidence packet (VerifyContext.EvidencePacketPath), never the prior agent's full context"
  - "haltVerify/applyLoopStatus — TASK-05: MaxIterations exhaustion (and BLOCKED/unreadable/anti-gaming outcomes) halt via setVerifyHaltIfNeeded; LoopStatus stays current-iteration-only (LOOP-03), re-derivable from Status.Attempt across a restart"
  - "emitEvaluatorSpanForVerifier — OBS-03 call site for synthesizeEvaluatorSpan, sibling-parented to the Task's own AGENT span via the same PlanTraceSpanID"
  - "settleVerifierSpend/finishVerifierTerminal — rolls the verifier's real token spend into Project.Status.budget and settles the BudgetCents reservation dispatchVerifier left outstanding since Plan 06"
affects: [51-08]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Re-derive-through-ClassifyVerdict: even though EnvReader.ReadOut already decodes EnvelopeOut.Verdict into a typed GateDecision, handleVerifierCompletion re-marshals it and calls dispatch.ClassifyVerdict(raw) rather than trusting the decoded Verdict string field directly — json.Unmarshal into a defined string type does not itself enforce the three-value enum, so an unrecognized/malformed value must still collapse through ClassifyVerdict's own fail-closed default"
    - "applyLoopStatus BEFORE Status.Attempt reassignment: dispatchRepairAttempt calls applyLoopStatus(task, out, \"\") using the OLD Status.Attempt (the just-verified attempt) before mutating Status.Attempt to the new value — LastEvaluation/Iteration must summarize what was evaluated, not the fresh attempt about to dispatch"
    - "Deterministic-path-first PVC writes: stageEvidencePacket computes its workspace-relative path via pure string derivation (mirrors task.Spec.PromptPath) and treats the real filesystem write as best-effort/non-blocking — the returned path never depends on the write succeeding, since envtest has no real PVC mount and would otherwise make the TASK-02 packet-path assertion untestable"
    - "settleVerifierSpend/finishVerifierTerminal split: budget rollup + reservation settle happen on EVERY verifier completion (terminal or mid-loop repair), but Task-level Prometheus metrics (TasksCompletedTotal/TasksFailedTotal) fire ONLY on a genuine terminal outcome — a naive single combined bookkeeping call would have double-counted a mid-loop repair dispatch as a false completion"

key-files:
  created:
    - internal/controller/task_verify_loop_test.go
  modified:
    - internal/controller/task_controller.go
    - internal/controller/task_controller_test.go
    - internal/controller/task_controller_extracted_test.go
    - pkg/dispatch/envelope.go

key-decisions:
  - "MaxIterations counting includes the original attempt, not just repairs: repairOrHalt halts when task.Status.Attempt >= MaxIterations (not >). With MaxIterations=1, a REPAIRABLE verdict on attempt 1 halts immediately (zero repairs allowed); with MaxIterations=3, attempts 1/2/3 all get to run (two repairs beyond the original). The plan did not specify the exact boundary; this reading is internally consistent and both boundaries are proven by dedicated tests."
  - "EvidencePacketPath transports through the EXISTING VerifyContext type on an executor-role envelope (EnvelopeIn.Verify), not a new field — buildEnvelopeIn gained a trailing evidencePacketPath string parameter that, when non-empty, sets EnvelopeIn.Verify = &VerifyContext{EvidencePacketPath: ...} (every other VerifyContext field stays empty on an executor dispatch). pkg/dispatch/envelope.go's EnvelopeIn.Verify doc comment (stale after this: 'Populated only when Role==\"verifier\"') was corrected in place — Rule 3, since the plan's own interfaces section names VerifyContext.EvidencePacketPath as 'the D-04 packet transport' and no clean alternative transport exists inside this plan's declared file scope."
  - "stageEvidencePacket's PVC write is best-effort and non-blocking: the returned workspace-relative path is pure string derivation (task.UID + Status.Attempt), never gated on the write actually succeeding — mirrors task.Spec.PromptPath's own 'controller sets the reference, executor validates at read time' precedent. Necessary because the Manager's /workspaces PVC mount is not visible under envtest (no real PVC), which would otherwise make TASK-02's fresh-attempt packet-path assertion untestable."
  - "No dedicated {Level}TraceSpanID-equivalent status field exists for the EVALUATOR span this phase (api/v1alpha3 schema changes are outside this plan's declared file scope) — emitEvaluatorSpanForVerifier's return values (span ID/sampled/emitted) are discarded; there is no persistence patch to make. A narrow, accepted double-emission risk exists on a reconcile replay between span emission and the terminal status patch landing (the same class of race {Level}SpanEmittedUID markers exist to close elsewhere), documented rather than closed given the file-scope constraint."
  - "Task 1 (verdict tree/haltVerify/EVALUATOR span) and Task 2 (repairOrHalt/anti-gaming/evidence packet) landed in a single commit: handleVerifierCompletion calls repairOrHalt and repairOrHalt calls haltVerify/settleVerifierSpend — a genuine two-way dependency where neither half compiles or tests meaningfully alone. Mirrors 51-01/51-06's own documented precedent for combining implementation-and-proof when a strict per-task split would produce a non-compiling or vacuously-true intermediate commit."

patterns-established:
  - "Bookkeeping-split terminal vs. mid-loop: any future loop-consuming controller (Phase 52's plan-check/Phase/Milestone/Project loops) should reuse the settleVerifierSpend (always) / finishVerifierTerminal (terminal-only) split rather than a single combined call, to avoid double-counting completion metrics on a mid-loop repair dispatch."

requirements-completed: [TASK-02, TASK-03, TASK-04, TASK-05, TASK-06, OBS-03]

# Metrics
duration: ~65min
completed: 2026-07-19
---

# Phase 51 Plan 07: Verifier Verdict Consumption — The Backward Half of the Task Loop Summary

**A terminal verifier Job's EnvelopeOut.Verdict now drives a fail-closed three-tier Task-loop decision (APPROVED→Succeeded, REPAIRABLE→a fresh evidence-seeded attempt, BLOCKED→project-wide VerifyHalt) with a controller-side D-06 dominance re-check and structural TASK-06 anti-gaming enforcement, closing the verifier sub-state-machine Plan 06 opened.**

## Performance

- **Duration:** ~65 min (context-gathering + implementation + iterative Ginkgo cache-sync-race fix + a real LoopStatus-ordering bug caught during self-review)
- **Tasks:** 2/2 completed (single commit — see Decisions Made)
- **Files modified:** 5 (1 created, 4 modified)

## Accomplishments

- `checkVerifyingState`'s terminal branch now calls `handleVerifierCompletion` instead of halting bare — the placeholder Plan 06 left is fully wired
- `handleVerifierCompletion`: reads the verifier's `out.json` via `EnvReader.ReadOut`; an unreadable envelope or nil `Verdict` halts fail-closed (BLOCKED, never APPROVED) using `synthesizeNoEnvelopeOut` to preserve `LoopRunID`/`AttemptID` span identity through the degraded envelope; a readable envelope re-marshals `out.Verdict` and classifies through `dispatch.ClassifyVerdict` (not the raw decoded string) before switching on APPROVED/REPAIRABLE/BLOCKED
- `hasDeterministicFailure` (D-06): an APPROVED verdict carrying a `Finding{severity:"blocker", dimension:"gate-command"}` is forced into `repairOrHalt` instead of `markVerifiedSucceeded` — a red gate on any authored pass-criterion command can never be silently approved, even by a buggy/compromised verifier's top-level verdict
- `repairOrHalt` (TASK-02/TASK-06): checks `intersectsProtected(out.RunEvidence.ChangedFiles, protectedPathsFor(task))` first — an attempt touching `internal/eval/`, `evals/`, `cmd/tide-langgraph-verifier/`, or `internal/subagent/common/templates/task_verifier.tmpl` is a system escalation (`escalateSystem`) that never mints a fresh attempt and never passes; otherwise checks `Status.Attempt >= Verification.MaxIterations` (halts via `haltVerify` + `ExitIterationsExhausted` if exhausted); otherwise dispatches a fresh quality-iteration attempt
- `dispatchRepairAttempt`/`stageEvidencePacket` (TASK-02): mints `Attempt++` (via the existing `nextAttempt` Job-label scan), stages a bounded evidence packet (verdict summary + up to 20 findings + `RunEvidence.Bounded()`'s changed-files/commands) at a deterministic PVC path, and dispatches a fresh executor Job whose envelope carries the ORIGINAL `task.Spec.PromptPath` (untouched) plus `EnvelopeIn.Verify.EvidencePacketPath` — never the prior agent's full context. The pre-existing eviction/infra-retry path (`checkRunningState`, same attemptID) is untouched and has no code route to this function
- `haltVerify`/`markVerifiedSucceeded`/`applyLoopStatus`: `haltVerify` stamps `Task.Status.Phase=Failed` + calls `setVerifyHaltIfNeeded` (ESC-02/ESC-03, project-wide, CR-02 time-fenced by Plan 05's own helper); `applyLoopStatus` keeps `LoopStatus` current-iteration-only (Iteration, ParentRunID, LastEvaluation, ExitReason) — no accumulating history, matching LOOP-03
- `emitEvaluatorSpanForVerifier` (OBS-03): wires `synthesizeEvaluatorSpan`'s call site, resolving the SAME `PlanTraceSpanID` parent the Task's own AGENT span uses so the EVALUATOR span is a true sibling, not a child
- `settleVerifierSpend`/`finishVerifierTerminal`: rolls the verifier's real token spend into `Project.Status.budget` and settles the `BudgetCents` reservation `dispatchVerifier` (Plan 06) left outstanding; terminal-only metrics (`TasksCompletedTotal`/`TasksFailedTotal`) are split out so a mid-loop repair dispatch never double-counts as a completion
- `buildEnvelopeIn` gained a trailing `evidencePacketPath` parameter (all 6 existing test call sites + `prepareDispatch`'s own call site updated to pass `""`); `pkg/dispatch/envelope.go`'s `EnvelopeIn.Verify` doc comment corrected to reflect it is now populated on an executor-role envelope too
- `internal/controller/task_verify_loop_test.go` (new, 10 Ginkgo `It`s across 4 `Describe` blocks + 3 plain-`testing.T` funcs): APPROVED→Succeeded, APPROVED-over-red-gate cannot pass (D-06), unreadable-envelope BLOCKED, BLOCKED→Failed+VerifyHalt, REPAIRABLE→fresh-attempt-with-packet, REPAIRABLE-at-MaxIterations→halt, anti-gaming true-positive + true-negative, infra-retry structural separation, and a simulated-controller-restart resume proof (fresh reconciler + fresh in-memory `ReservationStore`, zero carryover, correct outcome from CRD state alone)
- Full `internal/controller` package suite (255 specs) green across multiple random Ginkgo seeds; `go vet`/`go build`/`golangci-lint` (custom-gcl, gosec-equivalent linters this repo actually enables) all clean

## Task Commits

Both tasks landed in a single commit — see "Decisions Made" for why a strict per-task split was not meaningful here (a genuine two-way function-call dependency between `handleVerifierCompletion` and `repairOrHalt`):

1. **Task 1 (verdict tree/haltVerify/EVALUATOR span) + Task 2 (repairOrHalt/anti-gaming/evidence packet)** - `b0bd534a` (feat)

## Files Created/Modified

- `internal/controller/task_verify_loop_test.go` (new) - the BACKWARD-half envtest suite: 10 Ginkgo `It`s (VerifyLoop/AntiGaming/InfraRetry/Resume-labeled) + 3 plain-Go unit tests for `hasDeterministicFailure`/`intersectsProtected`/`applyLoopStatus`
- `internal/controller/task_controller.go` - `checkVerifyingState` wired onto `handleVerifierCompletion`; `hasDeterministicFailure`, `applyLoopStatus`, `emitEvaluatorSpanForVerifier`, `settleVerifierSpend`, `finishVerifierTerminal`, `haltVerify`, `markVerifiedSucceeded`, `escalateSystem`, `repairOrHalt`, `intersectsProtected`, `protectedPathsFor`, `evidencePacket`, `stageEvidencePacket`, `dispatchRepairAttempt`, `handleVerifierCompletion` added; `buildEnvelopeIn` gained the `evidencePacketPath` parameter; `os`/`path`/`path/filepath` imports added
- `internal/controller/task_controller_test.go` / `task_controller_extracted_test.go` - the 6 pre-existing `buildEnvelopeIn` call sites updated for the new trailing parameter
- `pkg/dispatch/envelope.go` - `EnvelopeIn.Verify`'s doc comment corrected (no field/schema change)

## Decisions Made

- **MaxIterations counting includes the original attempt.** `repairOrHalt` halts once `task.Status.Attempt >= Verification.MaxIterations` — with `MaxIterations=1` a REPAIRABLE verdict on the very first attempt halts immediately (zero repairs), with `MaxIterations=3` attempts 1/2/3 all get to run. The plan text didn't pin the exact boundary condition; this reading is internally consistent and both edges are covered by dedicated tests ("REPAIRABLE at MaxIterations halts instead of repairing further" and the multi-cycle Resume spec).
- **EvidencePacketPath rides the existing `VerifyContext` type on an executor-role envelope**, not a new schema field — `buildEnvelopeIn` sets `EnvelopeIn.Verify = &VerifyContext{EvidencePacketPath: ...}` when non-empty (every other `VerifyContext` field stays empty for `Role=="executor"`). `pkg/dispatch/envelope.go`'s stale `EnvelopeIn.Verify` doc comment ("Populated only when Role=='verifier'") was corrected — a Rule 3 blocking-issue deviation, since no alternative transport exists inside this plan's declared `files_modified` (`task_controller.go`/`task_verify_loop_test.go` only) and the plan's own interfaces section names `VerifyContext.EvidencePacketPath` as "the D-04 packet transport."
- **`stageEvidencePacket`'s PVC write is best-effort, never blocking.** The returned workspace-relative path (`envelopes/<taskUID>/evidence/attempt-<N>.json`) is pure string derivation, computed and returned regardless of whether the real filesystem write succeeds — mirroring `task.Spec.PromptPath`'s own "controller sets the reference, executor validates at read time" precedent. Necessary because envtest has no real `/workspaces` PVC mount (`os.MkdirAll` on a non-existent top-level directory fails with permission-denied, not the tolerable `fs.ErrNotExist` pattern `validateControllerOutputPaths` already handles) — without this design the TASK-02 fresh-attempt packet-path assertion would be untestable in this suite.
- **No dedicated status field persists the EVALUATOR span's ID this phase.** `emitEvaluatorSpanForVerifier`'s return values are discarded (best-effort observability, matching `spawnTaskTraceReporterIfNeeded`'s non-fatal posture) — adding a `{Level}TraceSpanID`-equivalent field is an `api/v1alpha3` schema change outside this plan's declared file scope. A narrow, accepted double-emission risk exists on a reconcile replay between span emission and the terminal status patch landing; documented, not closed.
- **Task 1 and Task 2 landed in one commit.** `handleVerifierCompletion` (Task 1's own decision tree) calls `repairOrHalt` (Task 2), and `repairOrHalt`/`escalateSystem`/`dispatchRepairAttempt` call back into `haltVerify`/`settleVerifierSpend`/`applyLoopStatus` (Task 1's own helpers) — a genuine two-way dependency neither half compiles or tests meaningfully in isolation. Mirrors 51-01/51-06's own documented precedent ("the implementation and its proof were built and verified together rather than a strict split... because X and Y needed to exist simultaneously for any one spec to be meaningful").

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `LoopStatus.Iteration`/`LastEvaluation` computed from the NEW attempt instead of the just-verified one**
- **Found during:** Task 2 self-review, before the initial commit (caught via hyper-critical diff review, not by the test suite — my own tests didn't originally assert `Iteration`'s value in the mid-loop-repair case)
- **Issue:** `dispatchRepairAttempt` originally called `task.Status.Attempt = attempt` (the fresh attempt number) BEFORE calling `applyLoopStatus(task, out, "")`. Since `applyLoopStatus` reads `task.Status.Attempt` to set `LoopStatus.Iteration`, this meant `Iteration`/`LastEvaluation` ended up summarizing the NEW (not-yet-dispatched) attempt instead of the attempt that was actually just verified — directly contradicting the function's own doc comment ("Iteration mirrors Status.Attempt, the attempt just evaluated").
- **Fix:** Reordered `dispatchRepairAttempt` to call `applyLoopStatus(task, out, "")` BEFORE reassigning `task.Status.Attempt`, with an inline comment explaining why the order is load-bearing.
- **Files modified:** `internal/controller/task_controller.go`
- **Verification:** Added a new regression-guard assertion to the "REPAIRABLE mints a fresh attempt" Ginkgo spec (`Expect(task.Status.LoopStatus.Iteration).To(Equal(int32(attempt)))`, asserting the OLD attempt number) — confirmed it fails against the pre-fix ordering and passes after the fix; full `internal/controller` suite re-verified green across 3 random Ginkgo seeds.
- **Committed in:** `b0bd534a` (same commit — caught before the initial commit was made)

**2. [Rule 3 - Blocking] Ginkgo cache-sync race between direct-client Job status patches and the reconciler's cached-client reads**
- **Found during:** Writing/verifying `task_verify_loop_test.go`, first `--ginkgo.focus` run
- **Issue:** `completeVerifierJob`/`completeExecutorJob` patch a Job's terminal status via `k8sClient` (the DIRECT test client), while `checkVerifyingState`/`checkRunningState` read the same Job via `r.Client` (`mgrClient`, the reconciler's CACHED/informer client). Without an explicit wait, the completion `Reconcile()` call can race the informer cache and observe the Job as still non-terminal — a flaky false-negative identical in class to 51-06-SUMMARY.md's own documented cap-hit-test fix ("dispatchVerifier's cap check reads via the reconciler's cached client... without an explicit Eventually-based cache-sync wait, the check races the informer cache").
- **Fix:** Added `waitForJobTerminalInCache(ctx, jobName)` (polls `mgrClient.Get` + `isJobTerminal` via `Eventually`) and called it from `completeVerifierJob` and after both `completeExecutorJob` call sites, before triggering the completion `Reconcile()`.
- **Files modified:** `internal/controller/task_verify_loop_test.go`
- **Verification:** Reproduced the flake deterministically via `--ginkgo.seed`; confirmed 0 failures across 8+ distinct seeds after the fix (previously failed ~1-in-3 runs).
- **Committed in:** `b0bd534a` (same commit — caught and fixed before the initial commit was made)

---

**Total deviations:** 2 auto-fixed (1 Rule 1 — a genuine LoopStatus-ordering bug caught during self-review before committing; 1 Rule 3 — a test-infrastructure cache-sync race, same class already documented and fixed once in this phase).
**Impact on plan:** No scope creep — both fixes are narrowly required for the plan's own stated LoopStatus/anti-gaming behavior to be correct and for its own verification commands to run genuinely (not flakily).

## Issues Encountered

Both issues above were caught and resolved during self-review/test-hardening BEFORE the commit landed — see Deviations. No unresolved issues.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The verifier sub-state-machine is now fully closed end-to-end: dispatch (Plan 06) → verdict consumption → Succeeded/repair/halt (this plan). Plan 08 (kind concurrent-dispatch test, per 51-06-SUMMARY.md's own forward note) can now exercise the full loop against a real cluster rather than envtest fixtures.
- `defaultVerifierConcurrencyCap=2` remains unvalidated against a live run (51-06's own carried-forward note) — still flagged for Plan 08.
- Scope note: `make test-int`'s kind-based Layer B suite (`test/integration/kind/`) was NOT re-run — zero files in that directory are touched by this plan's diff, and Layer A (envtest, this package) is fully green across multiple random seeds, mirroring the accepted precedent from prior phases ("Layer A envtest N/N; Layer B kind ... zero commits touch test/integration/kind/").
- No blockers. `go build ./...`, `go vet ./...`, `golangci-lint` (custom-gcl build, 0 issues), and the full unfiltered `internal/controller`/`pkg/dispatch` suites are all clean.

---
*Phase: 51-the-task-loop*
*Completed: 2026-07-19*
