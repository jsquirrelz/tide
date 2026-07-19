---
phase: 51-the-task-loop
verified: 2026-07-19T17:13:48Z
status: human_needed
score: 5/5 roadmap success criteria achieved at code (Layer A) level
overrides_applied: 0
re_verification:
  previous_status: none
  note: "Initial verification. Runs AFTER an executor VerifierImage-wiring fix and a deep code review (51-REVIEW.md, 9 findings) whose fixes were independently re-checked against the codebase here."
human_verification:
  - test: "Live billable Task-loop proof on a kind cluster (Plan 08 Task 2, checkpoint:human-verify gate=blocking)"
    expected: "A contract-bearing Task whose LOCKED gate command deterministically fails: executor runs and believes complete → Task.Status.Phase=Verifying → an independent role=verifier Job dispatches → verdict REPAIRABLE → a FRESH attempt (Attempt incremented, seeded with locked spec + evidence packet) → at maxIterations without APPROVED, Project shows ConditionVerifyHalt=True and dispatch parks. Then the green-gate path: fix the Task so the gate passes → verifier returns APPROVED → Task Succeeded. Verifier ran as an independent read-only Job (RO worktree, no git-write creds); reservation settled (no BudgetCents overrun)."
    why_human: "Requires the operator's real Anthropic key (~/.tide/anthropic.key), a real billable API spend, and a live single-node kind cluster (OOM-discipline: delete→recreate→prewarm). Not auto-approvable — money is spent. The concurrency cap + cost-bound (SC5/ESC-04) and the full loop are proven at Layer A (envtest + a compiled, vetted, lint-clean kind spec); only the live end-to-end billable proof remains."
  - test: "Run the authored kind concurrent-dispatch spec live (test/integration/kind/verifier_concurrency_test.go via make test-int) — deferred from Plan 08 Task 1's session"
    expected: "MAKE_EXIT=0 with no '^--- FAIL|^FAIL\\s' lines; concurrent role=verifier Job count stays under the sized cap (2) across a 90s window, drains to zero, and no Task is stranded in Verifying."
    why_human: "Standing up a kind cluster and running the Layer B suite is part of the same operator step as the live billable run (VerifierImage now wired, so the spec's terminal assertion will no longer trivially fail-loud as the SUMMARY predicted). The spec is written, compiles, vets, and lints clean; only the live kind execution is outstanding."
---

# Phase 51: The Task Loop — Verification Report

**Phase Goal:** `TaskReconciler` drives a real verification-driven quality loop — a locked, planner-authored verification contract dispatches an independent LangGraph evaluator against the real gate command, and a repairable failure produces a fresh, evidence-seeded attempt bounded by `maxIterations`, with concurrency/tracing/halt safety wired at the same dispatch sites (not deferred to a follow-up phase).
**Verified:** 2026-07-19T17:13:48Z
**Status:** human_needed
**Re-verification:** No — initial verification (post-review, all 9 REVIEW findings independently re-checked in code)

## Goal Achievement

Goal-backward result: **the code goal is achieved and verified at Layer A.** Both load-bearing safety properties are airtight in the actual codebase — deterministic dominance (a red gate can never be marked Succeeded) and anti-gaming (a weakened-evaluator attempt escalates on every verdict path, including the previously-dangerous APPROVED path). All 9 code-review findings were root-fixed, confirmed by direct code read (not SUMMARY claims). The single open item is a **billable live kind run** deliberately held as a human checkpoint — not a code gap.

### Observable Truths (Roadmap Success Criteria)

| # | Truth (SC) | Status | Evidence |
| --- | ------- | ---------- | -------------- |
| 1 | `TaskSpec.verification` (commands, requiredArtifacts, evaluator, maxIterations, onExhaustion, GateCommand) is **immutable once locked** (Draft→Locked→Superseded + version) | ✓ VERIFIED (core) | `VerificationSpec` CEL rule `oldSelf.phase != 'Locked' \|\| self == oldSelf \|\| self.phase == 'Superseded'` (`api/v1alpha3/task_types.go:90`); Phase enum `Draft;Locked;Superseded` (`:97`); rule generated into CRD (`config/crd/.../tasks.yaml:294-297`). `TaskStatus.LockedSHA` observation present (`:341`). **Caveat (LO-03):** `git show <lockedSHA>` does not literally reproduce the contract — `LockedSHA = project.Status.Git.LastPushedSHA` (`task_controller.go:1531`), a coarse temporal anchor; the contract lives in the CRD, not git. Documented in code + REVIEW, reviewer-classified LOW. Immutability (the safety-critical core) holds. |
| 2 | A REPAIRABLE result creates a **fresh attempt** seeded with the locked spec + a **compact evidence packet** — never the prior agent's full context; infra-retry stays a distinct preserved path | ✓ VERIFIED | `dispatchRepairAttempt` (`:2731`): `nextAttempt`++, re-seeds `task.Spec.PromptPath` (original locked spec, re-read fresh), stages a bounded packet (`stageEvidencePacket`, `≤20` findings). Packet sources `ChangedFiles`/`Commands` from the executor's persisted `LastAttemptEvidence` (LO-02 fix, `:2691`). Infra-retry (eviction, same attemptID) flows through `checkRunningState`'s Job re-read — never reaches `repairOrHalt` (comment `:2717`). |
| 3 | The evaluator dispatch (LangGraph image, `SelfInstruments` sentinel registered **this phase**) runs as a **logically independent process**; a deterministic command failure **always dominates** — never pass on LLM APPROVED over a red gate | ✓ VERIFIED | `SelfInstruments("langgraph")→true`, all others false/fail-closed (`vendor_capabilities.go:38-45`). Verifier is a separate image/process: `JobKindVerifier`, `Provider.Vendor="langgraph"`, `Role="verifier"` (`buildVerifierEnvelopeIn:2250`). Dominance is triple-layered: Python `_assemble_verdict` forces verdict DOWN on any non-zero exit, never up (`__main__.py:145-149`); empty commands → BLOCKED (`:142`); controller `hasDeterministicFailure` re-check over fail-closed `ClassifyVerdict` (`handleVerifierCompletion:2868-2879`). Proven by `test_gate_command_dominance_forces_non_approved_on_red_command` (injects APPROVED LLM + a failing command → REPAIRABLE/BLOCKED). |
| 4 | Bounded by `maxIterations` → `onExhaustion` → `ConditionVerifyHalt` (both planner + task tiers, mirroring `failure_halt.go` + Phase-25 time-fence) as a **halt class distinct from `Failed`**; resumable across restart; **anti-gaming enforced** | ✓ VERIFIED | `repairOrHalt` bounds on `Attempt >= MaxIterations` → `setVerifyHaltIfNeeded` (`:2638`). `verify_halt.go` is a file-for-file clone of `failure_halt.go` incl. resume time-fence (`:93-106`). Gates both tiers: `checkDispatchHolds` chain Billing→Failure→**Verify**→Budget→Import (`dispatch_helpers.go:653`) + `project_controller.go:1557`; `gateChecks` delegates to `checkDispatchHolds`. **HI-01 fix:** exhaustion uses `LevelPhaseVerifyHalted` (`:2548`), NOT `LevelPhaseFailed` — `gateChecks` Step 1a short-circuits it WITHOUT `setFailureHaltIfNeeded` (`:360-362`), so no conservative FailureHalt over-stamp. Resumable: `applyLoopStatus` writes current-iteration summary only (LOOP-03), re-derivable from `Status.Attempt` + deterministic `VerifierJobName`. **BL-01 anti-gaming fix:** enforced at executor completion on EVERY verdict path (`handleJobCompletion:1505` → `escalateSystem`) BEFORE a verifier can bless a gaming attempt, plus belt-and-suspenders over persisted `LastAttemptEvidence` in `repairOrHalt` (`:2634`). |
| 5 | Evaluator dispatches count against the concurrency gate (`verifierInFlightCount`) and `LoopPolicy.BudgetCents` bounds cost via the reservation store — verified by a kind concurrent-dispatch test under the sized cap | ✓ VERIFIED (code) / ⚠ live proof = human item | `dispatchVerifier` cap-before-acquire: `verifierInFlightCount >= defaultVerifierConcurrencyCap` checked BEFORE `Reserve` (`:2119-2136`); `releaseOnError` on every subsequent error arm. `BudgetCents` reserved via `ReservationStore` (`:2133`), settled in `settleVerifierSpend`. Kind spec `verifier_concurrency_test.go` written, `go vet` clean, asserts count-under-cap + drain-to-zero. **The LIVE kind run is the human-verify checkpoint** (Plan 08 Task 2). |

**Score:** 5/5 roadmap success criteria achieved at the code (Layer A) level. SC1 carries a documented LO-03 approximation on the `git show` reproducibility sub-clause (see override suggestion below); SC5's live billable proof is a human-verification item.

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | ----------- | ------ | ------- |
| `api/v1alpha3/task_types.go` | VerificationSpec + CEL + LoopStatus/LockedSHA/LastAttemptEvidence | ✓ VERIFIED | VerificationSpec (`:91`), CEL XValidation (`:90`), RunEvidenceSummary/ChangedFileRef (`:243,:263`), LockedSHA/LastAttemptEvidence status fields |
| `api/v1alpha3/shared_types.go` | ConditionVerifyHalt vocabulary + LevelPhaseVerifying/VerifyHalted | ✓ VERIFIED | `ConditionVerifyHalt`, `ReasonVerifyExhausted`, `AnnotationVerifyResumedAt` (`:362-373`); `LevelPhaseVerifying` (`:498`), `LevelPhaseVerifyHalted` (`:511`, HI-01) |
| `config/crd/bases/tideproject.k8s_tasks.yaml` | Generated CEL immutability rule | ✓ VERIFIED | `x-kubernetes-validations` with the immutability rule (`:294-297`); `make manifests` zero-diff (orchestrator-confirmed) |
| `pkg/dispatch/vendor_capabilities.go` | SelfInstruments langgraph case | ✓ VERIFIED | `case "langgraph": return true` (`:40-41`), fail-closed default |
| `cmd/tide-langgraph-verifier/verifier/__main__.py` | vendor sentinel + out-of-band multi-command deterministic verdict | ✓ VERIFIED | `SUPPORTED_VENDOR="langgraph"` (`:40`), refuses others (`:181`); `_run_commands_out_of_band` + `_assemble_verdict` dominance; ME-03 TimeoutExpired→124 (`:101-111`) |
| `internal/subagent/common/templates/task_verifier.tmpl` | role=verifier coverage-not-conservatism prompt | ✓ VERIFIED | Explicit "Report a finding for EVERY deviation ... Coverage is your job, not triage" (EVAL-04); ME-02 uses `{{.PromptPath}}` for original task intent |
| `internal/dispatch/podjob/{caps,names,jobspec}.go` | JobKindVerifier + VerifierJobName + TIDE_GATE_COMMAND + RW envelopes/ mount | ✓ VERIFIED | `JobKindVerifier` 900s floor (`caps.go:38`); `VerifierJobName` (`names.go:69`); RO /workspace + RW `envelopes/<uid>` subPath (`jobspec.go:448-468`); `TIDE_GATE_COMMAND` from `opts.GateCommand` (`:489-495`) |
| `internal/controller/verify_halt.go` | checkVerifyHalt + setVerifyHaltIfNeeded (failure_halt clone + time-fence) | ✓ VERIFIED | Full file, faithful clone with CR-02 time-fence (`:93-106`); gates both tiers per header |
| `internal/controller/task_controller.go` | verifier dispatch sub-state + verdict consumption + anti-gaming + LoopStatus + halt | ✓ VERIFIED | dispatchVerifier/handleVerifierCompletion/repairOrHalt/escalateSystem/haltVerify/markVerifiedSucceeded all present and correctly wired |
| `cmd/manager/main.go` | VerifierImage production wiring | ✓ VERIFIED | `verifierImage := envOrDefault("TIDE_VERIFIER_IMAGE", ...)` (`:219`), `VerifierImage: verifierImage` in TaskReconcilerDeps (`:562`) — closes the gap the 51-08 SUMMARY flagged |
| `test/integration/kind/verifier_concurrency_test.go` | Layer B cap spec | ✓ VERIFIED (compiles) | Present, `go vet` clean, cap/drain/no-strand assertions; live run = human item |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | --- | --- | ------ | ------- |
| Python deterministic gate capture | `EnvelopeOut.Verdict` | verdict assembly forcing non-APPROVED on non-zero exit | ✓ WIRED | `_assemble_verdict` → `verdict_out` → `write_envelope_out(verdict=...)` (`__main__.py:206-216`) |
| `handleJobCompletion` (executor exit-0) | anti-gaming escalation | `intersectsProtected(out.RunEvidence.ChangedFiles, protectedPathsFor)` on all verdict paths | ✓ WIRED | `:1505` → `escalateSystem`; persists `LastAttemptEvidence` (`:1536`) for belt-and-suspenders |
| `handleVerifierCompletion` | `pkgdispatch.ClassifyVerdict` | verdict consumption decision tree (fail-closed) | ✓ WIRED | `:2868` switch; APPROVED+det-failure→repairOrHalt; nil/unreadable/BLOCKED→haltVerify |
| `repairOrHalt` (exhaustion) | `setVerifyHaltIfNeeded` | onExhaustion → ConditionVerifyHalt | ✓ WIRED | `:2638-2641` → `haltVerify` → `setVerifyHaltIfNeeded` |
| `gateChecks` | `checkDispatchHolds` | task tier migrated onto shared chain | ✓ WIRED | Uniform Billing→Failure→Verify→Budget→Import order; VerifyHalted short-circuit at Step 1a |
| `dispatchVerifier` cap gate | `verifierInFlightCount` | cap-before-acquire at verifier dispatch site | ✓ WIRED | `:2123` before `Reserve` (`:2133`) |
| `synthesizeEvaluatorSpan` | `pkg/otelai` EVALUATOR span | sibling to AGENT span at verifier completion | ✓ WIRED | `emitEvaluatorSpanForVerifier` (`:2471`) resolves the same parentSpanID; called before terminal patches (OBS-03/D-11) |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
| -------- | ------------- | ------ | ------------------ | ------ |
| verifier verdict | `out.Verdict` | Python entrypoint writes real out-of-band command exit codes into GateDecision.findings | ✓ FLOWING | Not hardcoded; `_run_commands_out_of_band` runs actual `subprocess.run` per resolved command |
| anti-gaming manifest | `task.Status.LastAttemptEvidence.ChangedFiles` | executor's real `out.RunEvidence.ChangedFiles`, persisted at exit-0 | ✓ FLOWING | BL-01 fix sources the manifest where it actually exists (executor envelope), not the verifier's nil RunEvidence |
| evidence packet | `packet.ChangedFiles/Commands` | persisted `LastAttemptEvidence` (LO-02 fix) | ✓ FLOWING | No longer empty in production; findings from verdict, diff context from executor |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Whole module compiles | `go build ./...` | exit 0 | ✓ PASS |
| Anti-gaming + verdict-consumption unit tests | `go test ./internal/controller -run 'TestAntiGaming\|TestClassifyVerdictConsumption\|TestHasDeterministicFailure'` | `ok  0.878s` | ✓ PASS |
| Kind concurrency spec compiles/vets | `go vet ./test/integration/kind/` | exit 0, no diagnostics | ✓ PASS |
| Python verdict-dominance suite (inspected) | test_verdict.py T-51-02 | red-command dominance + all-green-stays-APPROVED + empty-fail-closed present | ✓ PASS (by inspection; pytest not in system env — orchestrator ran `make test-langgraph-verifier`: 76 passed) |
| Full internal/controller envtest suite | (orchestrator-run) | `ok 124.953s`, exit 0, incl. rewritten anti-gaming APPROVED-path + HI-01 conservative-profile specs | ✓ PASS (relayed; consistent with code read) |

### Probe Execution

Not applicable — Phase 51 declares no `scripts/*/tests/probe-*.sh`. Its terminal validation gate is the Plan 08 kind live-run (a human checkpoint), covered under Human Verification.

### Requirements Coverage

Every phase requirement ID appears in ≥1 plan's `requirements` frontmatter (union across 51-01…51-08). Zero orphaned requirements.

| Requirement | Source Plan(s) | Description | Status | Evidence |
| ----------- | ---------- | ----------- | ------ | -------- |
| TASK-01 | 01, 06 | Verification contract, immutable once locked | ✓ SATISFIED | CEL immutability + Draft/Locked/Superseded; lockedSHA observation (git-show is LO-03 approximation) |
| TASK-02 | 07 | REPAIRABLE → fresh attempt + compact evidence packet | ✓ SATISFIED | dispatchRepairAttempt + stageEvidencePacket (LO-02 fixed) |
| TASK-03 | 07 | Infra-retry distinct from quality-iteration | ✓ SATISFIED | checkRunningState (same attemptID) vs dispatchRepairAttempt (Attempt++) |
| TASK-04 | 02, 04, 06, 07 | Independent evaluator; deterministic failure dominates | ✓ SATISFIED | langgraph sentinel + triple-layer dominance + fail-closed |
| TASK-05 | 01, 07 | Bounded by maxIterations; onExhaustion; resumable | ✓ SATISFIED | repairOrHalt bound + LoopStatus re-derivable |
| TASK-06 | 07 | Anti-gaming enforced, not documented | ✓ SATISFIED | BL-01 fix: executor-completion enforcement all verdict paths + escalateSystem + rewritten APPROVED-path test |
| EVAL-04 | 03 | Coverage-not-conservatism verifier template | ✓ SATISFIED | task_verifier.tmpl explicit coverage directive; ME-02 fixed |
| ESC-02 | 05 | ConditionVerifyHalt mirrors failure_halt.go + time-fence, both tiers | ✓ SATISFIED | verify_halt.go clone + dispatch-hold chain + project_controller |
| ESC-03 | 01, 05 | Distinct halt class, never Failed reinterpretation | ✓ SATISFIED | HI-01 fix: LevelPhaseVerifyHalted + gateChecks Step 1a; co_occurring_holds + HI-01 conservative-profile envtest |
| ESC-04 | 04, 06, 08 | Concurrency gate + BudgetCents; kind test under cap | ✓ SATISFIED (code) | verifierInFlightCount cap-before-acquire + BudgetCents; kind spec written/vetted — **live proof = human item** |
| OBS-03 | 02, 03, 06, 07 | langgraph SelfInstruments this phase + EVALUATOR sibling span | ✓ SATISFIED | SelfInstruments langgraph + synthesizeEvaluatorSpan sibling to AGENT span |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| (none) | — | No `TBD`/`FIXME`/`XXX` debt markers in any phase-modified source | — | Debt-marker gate: PASS |
| (none) | — | No `TODO`/`HACK`/`PLACEHOLDER` in phase-modified source | — | Clean |
| `task_controller.go` | 1531 | LockedSHA = LastPushedSHA (LO-03) | ℹ INFO | Documented approximation, not a stub — value flows, just coarser than git-show literal (see SC1 caveat) |
| `task_types.go` | 84-88 | CEL rule permits content change during Locked→Superseded; no version monotonicity (LO-04) | ℹ INFO | Documented edge gap in supersede lifecycle; core "cannot mutate while staying Locked" holds |

### Human Verification Required

**1. Live billable Task-loop proof on kind (Plan 08 Task 2 — `checkpoint:human-verify`, gate=blocking)**

**Test:** On a prewarmed single-node kind cluster with a real Anthropic key (`~/.tide/anthropic.key`), apply a contract-bearing Task whose LOCKED `gateCommand` deterministically fails, `maxIterations: 1–2`, `onExhaustion: requireApproval`. Drive the loop; then fix the Task so the gate passes.
**Expected:** executor completes → `Task.Status.Phase=Verifying` → independent `role=verifier` Job dispatches → verdict `REPAIRABLE` → fresh attempt (`Attempt` incremented) → at `maxIterations` without `APPROVED`, `Project` shows `ConditionVerifyHalt=True`, dispatch parks. Green-gate path: verifier `APPROVED` → Task `Succeeded`. Verifier ran read-only (RO worktree, no git-write creds); reservation settled (no `BudgetCents` overrun).
**Why human:** Real money is spent against the live API; not auto-approvable. `VerifierImage` wiring (the prerequisite the 51-08 SUMMARY flagged) is now closed, so the loop can dispatch a real verifier. The concurrency cap, cost-bound, deterministic dominance, anti-gaming, and halt semantics are all proven at Layer A (envtest); only the live end-to-end billable proof is outstanding.

**2. Run the kind concurrent-dispatch spec live (`make test-int`, deferred from Plan 08 Task 1)**

**Test:** `make test-int` including `test/integration/kind/verifier_concurrency_test.go`.
**Expected:** `MAKE_EXIT=0`, no `--- FAIL`/`FAIL` lines; verifier Job count stays under the sized cap (2), drains to zero, no Task stranded in Verifying.
**Why human:** Same operator kind-cluster session as item 1. Spec compiles/vets/lints clean; only the live execution remains.

### Gaps Summary

**No code gaps.** All 9 findings from the deep code review (`51-REVIEW.md`) were root-fixed and independently re-confirmed against the codebase (not SUMMARY claims):

- **BL-01 (BLOCKER, anti-gaming dead)** — FIXED. Enforcement moved to executor completion (`handleJobCompletion:1505`) firing on EVERY verdict path (closing the APPROVED-gaming hole), the executor's changed-file manifest is persisted to `Task.Status.LastAttemptEvidence` (`:1536`), `repairOrHalt` re-checks the persisted manifest as belt-and-suspenders (`:2634`, never the verifier's nil RunEvidence), `escalateSystem` is a distinct terminal, and the rewritten true-positive test drives executor completion with a protected-path edit and asserts `VerifyHalted` + `AntiGamingDetected` + no verifier dispatched on the APPROVED path (`task_verify_loop_test.go:526-565`).
- **HI-01 (HIGH, verify-exhaustion marked Failed)** — FIXED. `haltVerify` now sets `LevelPhaseVerifyHalted` (`:2548`); `gateChecks` Step 1a short-circuits it without `setFailureHaltIfNeeded` (`:360-362`) — no conservative FailureHalt over-stamp, no Failed-wave dependent semantics.
- **ME-01/ME-02/ME-03** — FIXED (metric double-count via `countCompletion` gating; empty original-prompt via `envIn.PromptPath`; timed-out gate via `TimeoutExpired→124`).
- **LO-01/LO-02** — FIXED (empty-image skip + corrected main.go comment; evidence packet sourced from persisted executor manifest).
- **LO-03/LO-04** — documented, reviewer-classified LOW approximations (LockedSHA temporal anchor; CEL supersede edge gaps). Core invariants hold.

Additionally, the `VerifierImage` production-wiring gap that the 51-08 SUMMARY documented as an open prerequisite was subsequently closed — direct code read confirms `cmd/manager/main.go:219,562` wires it. SUMMARY was stale; code is correct.

The status is `human_needed` (not `passed`) solely because Plan 08 Task 2 is an explicit blocking human-verify checkpoint requiring a real billable spend — a deliberate escalation-gate hold, not a missing deliverable.

### Suggested Override (optional — for SC1's literal git-show sub-clause)

SC1's core (immutability once Locked) is fully enforced by CEL. Its `git show <locking-sha> reproduces exactly what was dispatched` sub-clause is an intentional documented approximation (the contract lives in the CRD, not git; LockedSHA is a coarse temporal anchor). This is reviewer-classified LOW, acknowledged in code comments, and does not affect the phase goal. To formally accept the deviation, add to this file's frontmatter:

```yaml
overrides:
  - must_have: "TaskSpec.verification immutable once locked and git show <locking-sha> reproduces exactly what was dispatched"
    reason: "Immutability core is CEL-enforced. git-show reproducibility is a documented LO-03 approximation — the contract lives in the CRD not git, LockedSHA is a coarse temporal anchor; no finer per-lock commit SHA is tracked anywhere in the codebase. Reviewer-classified LOW; core invariant holds."
    accepted_by: "<name>"
    accepted_at: "<ISO timestamp>"
```

---

_Verified: 2026-07-19T17:13:48Z_
_Verifier: Claude (gsd-verifier)_
