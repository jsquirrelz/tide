---
phase: 51-the-task-loop
reviewed: 2026-07-19T00:00:00Z
depth: deep
files_reviewed: 19
files_reviewed_list:
  - internal/controller/task_controller.go
  - internal/controller/verify_halt.go
  - internal/controller/dispatch_helpers.go
  - internal/controller/project_controller.go
  - internal/controller/span_emission.go
  - api/v1alpha3/task_types.go
  - api/v1alpha3/shared_types.go
  - pkg/dispatch/envelope.go
  - pkg/dispatch/provider.go
  - pkg/dispatch/vendor_capabilities.go
  - pkg/dispatch/verdict.go
  - pkg/otelai/attrs.go
  - internal/dispatch/podjob/caps.go
  - internal/dispatch/podjob/jobspec.go
  - internal/dispatch/podjob/names.go
  - internal/subagent/common/prompt_templates.go
  - internal/subagent/common/templates/task_verifier.tmpl
  - cmd/manager/main.go
  - cmd/tide-langgraph-verifier/verifier/__main__.py
findings:
  blocker: 1
  high: 1
  medium: 3
  low: 4
  critical: 1
  warning: 4
  info: 4
  total: 9
status: issues
---

# Phase 51: Code Review Report — The Task Loop

**Reviewed:** 2026-07-19
**Depth:** deep (cross-file: controller ↔ Python verifier ↔ envelope wire ↔ tests)
**Files Reviewed:** 19
**Status:** issues_found

## Summary

The deterministic-dominance core (TASK-04) — the reason this milestone exists — is **sound**. A red gate command can never be marked Succeeded: the Python entrypoint captures each pass-criterion exit code out-of-band (`_run_commands_out_of_band`), forces the verdict down to REPAIRABLE/BLOCKED on any non-zero (`_assemble_verdict`), and the controller re-checks with a defence-in-depth `hasDeterministicFailure` guard over `ClassifyVerdict` (which is itself fail-closed). The fail-closed verdict path (EVAL-03), the reservation-leak discipline in `dispatchVerifier` (ESC-04, cap-before-acquire + `releaseOnError` on every error arm), the infra-retry ≠ quality-iteration split (TASK-03), the CEL immutability rule (TASK-01), and `LoopStatus` current-iteration-only accumulation (LOOP-03) all hold up under trace.

**However, the anti-gaming invariant (TASK-06) — the milestone's second load-bearing safety property — is non-functional in production.** The detector reads a `RunEvidence` field the real verifier envelope never carries, and it is only wired on the REPAIRABLE branch, so the exact scenario it exists to stop (weaken the evaluator so the gate goes green → APPROVED) slips straight through to Succeeded. The envtest that "proves" it passes only because it injects data the production verifier never writes. This is a BLOCKER.

One HIGH follows: verify-exhaustion marks the Task `Failed`, which re-invokes Failed-wave / conservative-FailureHalt semantics that ESC-03 explicitly requires a VerifyHalt to leave untouched.

Ranked most-severe first.

---

## BLOCKER

### BL-01: Anti-gaming detector is dead in production — a weakened evaluator passes as Succeeded (TASK-06)

**File:** `internal/controller/task_controller.go:2481-2493` (`repairOrHalt`), `:2678-2719` (`handleVerifierCompletion`), `:2437` (`markVerifiedSucceeded`); `cmd/tide-langgraph-verifier/verifier/__main__.py:199-204`

**Issue:** TASK-06 requires "an attempt that edits fixtures/thresholds/the evaluator itself is flagged as systemic, never counted as a pass — enforced, not documented." The implementation fails this in **two independent ways**:

1. **Wrong envelope / always-nil source.** `repairOrHalt` gates the check on `out.RunEvidence != nil && intersectsProtected(out.RunEvidence.ChangedFiles, ...)`. Here `out` is the **verifier's** `EnvelopeOut`. The verifier is read-only and its Python entrypoint calls `envelope.write_envelope_out(..., exit_code=0, result=..., verdict=verdict_out)` with **no `run_evidence` argument** (`__main__.py:199-204`), so the verifier's out.json never contains a `runEvidence` key. Both executor and verifier write the same PVC path (`envelopes/<taskUID>/out.json`), and `ReadOut(projectUID, taskUID)` returns that single file, so by the time `handleVerifierCompletion` reads it, the executor's `RunEvidence` (which *did* carry `ChangedFiles`) has been overwritten by the verifier's verdict-only envelope. Result: `out.RunEvidence` is always `nil`, the `&&` short-circuits, and `intersectsProtected` is never evaluated in production.

2. **Only checked on the REPAIRABLE branch.** Even with a populated manifest, `intersectsProtected` runs only inside `repairOrHalt`, reached on `VerdictRepairable` or `VerdictApproved && hasDeterministicFailure`. A *successful* gaming attempt — the executor edits `internal/eval/` or `task_verifier.tmpl` so the gate command exits 0 — produces `VerdictApproved` with **no** gate-command finding, so `handleVerifierCompletion` (`:2701`) routes straight to `markVerifiedSucceeded`, which performs **no anti-gaming check at all**. The precise attack TASK-06 names ("edits the evaluator to make a Task pass") is stamped Succeeded.

**Why the test misses it:** `task_verify_loop_test.go:474-483` injects `RunEvidence: &pkgdispatch.RunEvidence{ChangedFiles: [...]}` through the mock `EnvReader` **and** forces `Verdict: VerdictRepairable`. Both conditions are unreachable from the real verifier: it never writes `runEvidence`, and a passing gaming attempt returns APPROVED, not REPAIRABLE. The test proves the mechanism only against fabricated inputs.

**Failure scenario:** Planner locks a contract with `gateCommand: make test-verify`. Executor attempt edits `internal/eval/scorer.go` to make `make test-verify` exit 0 (the anti-gaming target). Verifier runs the gate out-of-band → exit 0 → no gate-command finding → LLM judge returns APPROVED → verifier out.json = `{verdict: APPROVED}`, no `runEvidence`. Controller: `ClassifyVerdict → APPROVED`, `hasDeterministicFailure → false` → `markVerifiedSucceeded`. Task Succeeded. Evaluator successfully gamed.

**Fix:** Capture the **executor's** changed-file manifest where it is actually available — `handleJobCompletion`, at the exit-0 arm, has the executor's `out.RunEvidence.ChangedFiles` in hand *before* it is overwritten. Evaluate the protected-path intersection there (or persist the intersection boolean / the manifest onto `Task.Status`) and route to `escalateSystem` on any hit, on **every** verdict path — not just REPAIRABLE. Concretely, in `handleJobCompletion` (`:1479`, the `hasVerificationContract` arm):

```go
} else if hasVerificationContract(task) {
    if out.RunEvidence != nil &&
        intersectsProtected(out.RunEvidence.ChangedFiles, protectedPathsFor(task)) {
        // Executor touched a protected evaluator/fixture path — system
        // escalation, never dispatch a verifier that could bless it.
        return r.escalateSystem(ctx, task, project, out)
    }
    task.Status.Phase = tideprojectv1alpha3.LevelPhaseVerifying
    // ...
}
```

Then keep (or drop) the `repairOrHalt` check as belt-and-suspenders, but it can no longer be the sole enforcement point, and it must not read the verifier's nil `RunEvidence`. Update the true-positive test to drive the **executor** completion with a protected-path `RunEvidence` and assert escalation on the APPROVED path too (the currently-untested and actually-dangerous case).

---

## HIGH

### HI-01: Verify-exhaustion marks the Task `Failed`, re-invoking Failed-wave + conservative-FailureHalt semantics ESC-03 forbids

**File:** `internal/controller/task_controller.go:2400-2419` (`haltVerify` sets `LevelPhaseFailed`), interacting with `:351-372` (`gateChecks` Failed terminal short-circuit → `setFailureHaltIfNeeded`)

**Issue:** ESC-03 states a BLOCKED/exhausted verify is "a distinct halt class, never a reinterpretation of `Failed` wave semantics — the checked level's phase, wave siblings, and conservative-profile propagation are untouched by a VerifyHalt." `haltVerify` sets `task.Status.Phase = LevelPhaseFailed` and stamps `ConditionVerifyHalt` (via `setVerifyHaltIfNeeded`). On the **next** reconcile, `gateChecks` Step-1b (`task.Status.Phase == LevelPhaseFailed`, `:351`) calls `setFailureHaltIfNeeded`, which under `FailureProfile==conservative` stamps `ConditionFailureHalt` **in addition** to the VerifyHalt. The exhausted Task also now participates in Failed-wave dependent semantics (its dependents' global indegree can never reach 0).

**Failure scenario (conservative profile):** A Task exhausts `maxIterations`. `haltVerify` → Phase=Failed + VerifyHalt. Next reconcile → FailureHalt stamped too. Operator runs `tide resume` (clears VerifyHalt, per the message `haltVerify` writes). Dispatch is still parked because FailureHalt remains, and the Task is still `Failed` so its dependents' indegree is stuck. To recover, the operator must *also* run `tide resume --retry-failed` — a verb the VerifyHalt path never told them about. The "distinct halt class" is distinct only at the `setVerifyHaltIfNeeded` helper level (which `co_occurring_holds_test.go:367` tests in isolation); the actual `haltVerify` flow that drives Phase=Failed is untested against ESC-03 and violates it.

**Note:** This fails in the safe (over-halting) direction, so it is HIGH, not BLOCKER — no incorrect Succeeded, no spend leak. But it directly contradicts an accepted requirement and can strand a conservative-profile project on the documented `tide resume` alone.

**Fix:** Either (a) do not drive verify-exhaustion through `LevelPhaseFailed` — introduce a distinct terminal (e.g. `LevelPhaseVerifyHalted`) that the `gateChecks` Failed short-circuit does not treat as an execution failure, so `setFailureHaltIfNeeded` is not triggered and Failed-wave dependent semantics are not invoked; or (b) if Phase=Failed is intentional, add an envtest that drives the full `haltVerify` flow under conservative profile and explicitly asserts `ConditionFailureHalt` is **not** stamped and dependents behave per the VerifyHalt (not Failed) contract — then reconcile the requirement text with the chosen behavior. Whichever is chosen, cover the interaction end-to-end, not just the helper.

---

## MEDIUM

### ME-01: `TasksCompletedTotal` over-counts; a verify-halted Task is counted as both completed and failed (OBS metrics correctness)

**File:** `internal/controller/task_controller.go:1534-1540` (Verifying-transition `emitTaskMetrics` call) + `:1706-1710` (`emitTaskMetrics` counter), reached again via `finishVerifierTerminal` (`:2381`)

**Issue:** At the executor exit-0 → Verifying transition, `handleJobCompletion` calls `emitTaskMetrics(..., metricReason)` where `metricReason == ""` (the Failed branch that sets it is skipped because Phase is Verifying). `emitTaskMetrics` treats `failureReason == ""` as a completion and does `TasksCompletedTotal.Inc()` (`:1707`). So:

- A contract-bearing Task that eventually succeeds increments `TasksCompletedTotal` **once per attempt** at each Verifying transition, **plus** once more at `markVerifiedSucceeded → finishVerifierTerminal` — a 3-attempt repair inflates the counter to 4 completions for one Task.
- A Task that verify-halts increments `TasksCompletedTotal` (at the Verifying transition) **and** `TasksFailedTotal` (at `haltVerify → finishVerifierTerminal`, reason `"verify-halt"`). The same Task is counted as both completed and failed, corrupting the Failure Rate panel.

Token/cost/duration metrics are **not** double-counted (executor usage and verifier usage are distinct real spends), so this is scoped to the completion/failure counters.

**Fix:** Do not emit the completion/failure counter at the non-terminal Verifying transition. Simplest: at `:1538`, when `task.Status.Phase == LevelPhaseVerifying`, either skip `emitTaskMetrics` entirely (defer all terminal metrics to `finishVerifierTerminal`) or pass a sentinel that suppresses only the `TasksCompletedTotal/TasksFailedTotal` increment while still recording the executor's token/cost/duration spend.

### ME-02: Verifier prompt's "Original task prompt" section always renders empty (EVAL-04)

**File:** `internal/controller/task_controller.go:2205-2214` (`buildVerifierEnvelopeIn`); `internal/subagent/common/templates/task_verifier.tmpl:63-65`

**Issue:** The template ends with `Original task prompt (... judge the candidate against this intent ...): {{.Prompt}}`. `buildVerifierEnvelopeIn` executes the template against `envIn` **before** assigning `envIn.Prompt = promptBuf.String()` (`:2214`), and it never loads the executor's original task prompt (e.g. from `task.Spec.PromptPath`) onto the verifier envelope. So `{{.Prompt}}` resolves to the empty string at render time, and the rendered verifier prompt contains a blank "Original task prompt" section. The verifier is explicitly told to judge against the task intent but is given none — it falls back to "its own preferences," the very mis-calibration coverage-not-conservatism is meant to avoid. `task_verify_dispatch_test.go:369` only asserts the rendered prompt contains the gate command, so the empty intent section is untested.

**Fix:** Populate the original task intent onto the verifier envelope from the locked spec before rendering — e.g. read the executor's prompt/`PromptPath` and expose it to the template under a dedicated field (not the self-referential `envIn.Prompt`), then assert its presence in the dispatch test.

### ME-03: A timed-out gate command crashes the verifier instead of recording a deterministic failure (TASK-04 robustness)

**File:** `cmd/tide-langgraph-verifier/verifier/__main__.py:79-100` (`_run_commands_out_of_band`), called at `:186` outside the try/except

**Issue:** `_run_commands_out_of_band` invokes `subprocess.run(command, ..., timeout=tools.GATE_COMMAND_TIMEOUT_SECONDS, check=False)`. A command exceeding the timeout raises `subprocess.TimeoutExpired`, which is **not** caught here, and the call site (`:186`) is **outside** the `try/except` that wraps model construction / agent run (`:188-192`). An uncaught `TimeoutExpired` aborts `main()` with a traceback before `write_envelope_out` / `write_termination_stub` run — so no structured verdict and no clean termination stub are written. A hanging gate command (a flaky/deadlocked test — common) therefore loses its deterministic gate-command finding. The controller still fails closed (unreadable envelope → `haltVerify` BLOCKED), so it is not a correctness BLOCKER, but a timed-out gate is semantically a **failing** gate and should be recorded as a non-zero command with a `dimension="gate-command"` finding, not an unstructured crash.

**Fix:** Wrap the `subprocess.run` in `_run_commands_out_of_band` in `try/except subprocess.TimeoutExpired` and append a synthetic non-zero result (e.g. `(command, 124)`) so `_assemble_verdict` emits the gate-command blocker finding and forces REPAIRABLE/BLOCKED, preserving the structured verdict + termination stub.

---

## LOW

### LO-01: Misleading `main.go` comment — `dispatchVerifier` has no empty-`VerifierImage` skip

**File:** `cmd/manager/main.go:213-216`; `internal/controller/task_controller.go:2044-2135` (`dispatchVerifier`)

**Issue:** The wiring comment claims "When empty, the verifier dispatch site logs and skips (mirrors the TIDE_REPORTER_IMAGE skip)." `dispatchVerifier` does **not** check for an empty `VerifierImage` — it builds a Job with `SubagentImage: r.Deps.VerifierImage` unconditionally. Only the non-empty dev-head default (`ghcr.io/.../tide-langgraph-verifier:v0.1.0-dev`) prevents an empty image in practice. If an operator sets `TIDE_VERIFIER_IMAGE=""`, a contract-bearing Task creates a verifier Job with an empty image ref (ImagePullBackOff / invalid spec) and sits in `Verifying` indefinitely — with no log-and-skip.

**Fix:** Either implement the documented skip (if `VerifierImage == ""`, log and leave the Task in a benign held/parked state rather than creating an unschedulable Job) or correct the comment to state the field is always defaulted and never empty.

### LO-02: Evidence packet ships empty `ChangedFiles` in production (TASK-02 compact packet thinner than intended)

**File:** `internal/controller/task_controller.go:2520-2556` (`stageEvidencePacket`)

**Issue:** `stageEvidencePacket` reads `out.RunEvidence` — the **verifier's** envelope, which never carries `runEvidence` (same root cause as BL-01) — so `packet.ChangedFiles` and `packet.Commands` are always empty in production. Only `packet.Findings`/`Summary` (from the verdict) populate. The repair attempt's compact evidence packet is therefore missing the diff context TASK-02/D-04 intends it to carry. Fixing BL-01 (capturing the executor's `RunEvidence`) also supplies the packet's changed-file context.

**Fix:** Source `ChangedFiles`/`Commands` from the executor's run evidence (persisted per BL-01's fix), not the verifier's verdict-only envelope.

### LO-03: `LockedSHA` records `LastPushedSHA`, not the contract-lock commit (TASK-01 reproducibility approximation)

**File:** `internal/controller/task_controller.go:1495-1501`

**Issue:** TASK-01 requires `git show <lockedSHA>` to "reproduce exactly what was dispatched." The code sets `task.Status.LockedSHA = project.Status.Git.LastPushedSHA` — the most recent run-branch push, not the commit at which `spec.verification` transitioned to Locked (the comment acknowledges this). Because the contract lives in the CRD (not in git), `git show <lockedSHA>` does not actually reproduce the verification contract at all; `LockedSHA` is at best a coarse temporal anchor. Documented as a known approximation, so LOW, but it does not satisfy the literal TASK-01 repudiation guarantee.

**Fix:** Track the commit SHA at the Draft→Locked transition (or persist the resolved contract alongside the run branch) so the anchor reproduces the dispatched contract; otherwise soften the TASK-01 claim.

### LO-04: CEL immutability does not enforce version monotonicity nor block content change during Locked→Superseded

**File:** `api/v1alpha3/task_types.go:78` (XValidation rule)

**Issue:** The rule `oldSelf.phase != 'Locked' || self == oldSelf || self.phase == 'Superseded'` permits changing `gateCommand`/`commands`/etc. **in the same update** that flips `phase` to `Superseded`, and never checks that `version` increments monotonically (documented as monotonic but unenforced). The core invariant (a Locked contract cannot be silently mutated while staying Locked) holds; these are edge gaps in the supersede lifecycle. LOW.

**Fix:** If the lifecycle should be strict, extend the rule to require `self.version > oldSelf.version` on a Locked→Superseded transition and/or freeze the pass-criteria fields across supersede.

---

## Verified sound (no finding)

Recorded so the fixer does not re-litigate these:

- **Deterministic dominance (TASK-04):** No path marks a red gate Succeeded. Python out-of-band capture (`_run_commands_out_of_band` → `_assemble_verdict` forces REPAIRABLE/BLOCKED on any non-zero, and BLOCKED on empty `command_results`) + controller `hasDeterministicFailure` re-check over the fail-closed `ClassifyVerdict`. `commands` always includes `gateCommand` first (`buildVerifierEnvelopeIn:2179-2185`), so the canonical gate always runs.
- **Fail-closed verdict (EVAL-03):** Unreadable envelope, nil verdict, and marshal failure all route to `haltVerify` (`handleVerifierCompletion:2683-2699`); `ClassifyVerdict` collapses empty/malformed/unknown to BLOCKED.
- **Reservation leak (ESC-04):** `dispatchVerifier` checks `verifierInFlightCount` cap **before** `Reserve` (`:2050-2062`), and `releaseOnError` fires on every subsequent error arm (sign/build/ownerref/create). `handleJobCompletion`'s `settleExecutorReservation` suppression correctly hands the single shared store key to the verifier reservation; cap-hit path settles cleanly with no leak.
- **infra-retry vs quality-iteration (TASK-03):** Eviction rerun stays on the same attempt via `checkRunningState`'s Job re-read; quality-iteration mints a fresh attempt via `dispatchRepairAttempt` (`nextAttempt`++), never reachable from the infra path. `nextAttempt` label-counting is unaffected by verifier Jobs (same attempt value).
- **Resumability / LOOP-03:** `applyLoopStatus` writes current-iteration summary + exit reason only; no history accumulation; re-derivable from `Status.Attempt` + the deterministic `VerifierJobName` re-read.
- **VerifyHalt time-fence (ESC-02):** `setVerifyHaltIfNeeded` carries the Phase-25 `taskCompletedAt < resumedAt` fence correctly (fail-closed on zero/unparseable), and `checkDispatchHolds`/`project_controller.go` wire the gate into all five dispatch chains in a uniform Billing→Failure→Verify→Budget→Import order.

---

_Reviewed: 2026-07-19_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
