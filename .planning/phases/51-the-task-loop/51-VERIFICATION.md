---
phase: 51-the-task-loop
verified: 2026-07-20T01:37:07Z
status: passed
gate_decision: APPROVED
score: 5/5 roadmap success criteria verified (Layer A code + Layer B live proof)
overrides_applied: 0
re_verification:
  previous_status: human_needed
  previous_score: "5/5 (Layer A only; two live billable proofs held as human checkpoints)"
  gaps_closed:
    - "Live billable Task-loop proof (red gate → REPAIRABLE → fresh attempt → VerifyHalted@maxIterations; green gate → APPROVED → Succeeded) — PASSED live 2026-07-20 on kind-tide-test, real key, claude-sonnet-4-6 verifier"
    - "Kind concurrent-dispatch spec (verifier_concurrency_test.go) — PASSED live 2026-07-20 (1 Passed | 0 Failed in 157s)"
  gaps_remaining: []
  regressions: []
  note: "Superseding the human_needed verification. Both UAT items now PASSED live (51-HUMAN-UAT.md, status: passed, 2/2). Getting the live proof green surfaced 5 latent defects (the shipped verify path had never run end-to-end; envtest fed fake readers/models). All 5 root-fixed on main in 29e31374 (verifier-image packaging + build-time import guard) and 076c9637 (verdict relay + structured output + prompt semantics). Both commits confirmed on main (ancestors of HEAD 00b20eb0) and their claimed artifacts re-read in the codebase — not trusted from SUMMARY."
---

# Phase 51: The Task Loop — Verification Report (Superseding)

**Phase Goal:** `TaskReconciler` drives a real verification-driven quality loop — a locked, planner-authored verification contract dispatches an independent LangGraph evaluator against the real gate command, and a repairable failure produces a fresh, evidence-seeded attempt bounded by `maxIterations`, with concurrency/tracing/halt safety wired at the same dispatch sites (not deferred to a follow-up phase).
**Verified:** 2026-07-20T01:37:07Z
**Status:** passed
**Gate Decision:** APPROVED
**Re-verification:** Yes — supersedes the 2026-07-19 `human_needed` verification after both live-proof human checkpoints PASSED and the 5 defects the live run exposed were root-fixed and re-confirmed in code.

## Goal Achievement

Goal-backward result: **the phase goal is achieved and now proven end-to-end.** The prior verification confirmed both load-bearing safety properties airtight at Layer A (deterministic dominance — a red gate can never be marked Succeeded; anti-gaming — a weakened-evaluator attempt escalates on every verdict path incl. APPROVED). This re-verification adds the missing Layer B evidence: the live billable proof ran the complete loop on a real cluster with a real key, and the concurrency spec passed live under the sized cap. Critically, the live run surfaced that the *shipped* verify path had never actually executed end-to-end (envtest substitutes fake readers/models), exposing 5 latent defects — all root-fixed on `main` and re-verified here by direct code read.

### Live Proof Resolution (was the sole reason for `human_needed`)

| Live gate | Observed (51-HUMAN-UAT.md, 2026-07-20) | Status |
| --------- | -------------------------------------- | ------ |
| RED (`gateCommand: "test -f VERIFIED.md"`, maxIter 2) | a1 executor → Verifying (independent role=verifier Job, credproxy sidecar, real API) → verdict REPAIRABLE → fresh attempt a2 → Verifying → **VerifyHalted, exitReason=iterationsExhausted, iteration=2**, `ConditionVerifyIterationsExhausted` | ✓ PASS |
| GREEN (`gateCommand: "test -f proof.go"`, maxIter 1) | a1 executor → Verifying (~60s real sonnet agent) → **Succeeded, exitReason=approved, lastEvaluation.decision=APPROVED**, `Succeeded=True VerifierApproved` | ✓ PASS |
| `tide resume` (HI-01 recovery) | Cleared project-wide `ConditionVerifyHalt` + reset the VerifyHalted task for re-dispatch, exercised live repeatedly | ✓ PASS |
| ESC-04 kind concurrency | `--ginkgo.focus 'Verifier concurrent-dispatch'` → `1 Passed \| 0 Failed` in 157s; count-under-cap + drain-to-zero + no stranded Task | ✓ PASS |

### Observable Truths (Roadmap Success Criteria)

| # | Truth (SC) | Status | Evidence |
| --- | ------- | ---------- | -------------- |
| 1 | `TaskSpec.verification` immutable once locked (Draft→Locked→Superseded + version) | ✓ VERIFIED (core) | CEL rule `oldSelf.phase != 'Locked' \|\| self == oldSelf \|\| self.phase == 'Superseded'` (`api/v1alpha3/task_types.go:90`); Phase enum `Draft;Locked;Superseded` (`:97`); rule generated into CRD (`config/crd/bases/tideproject.k8s_tasks.yaml:294`). **Documented caveat (LO-03, reviewer LOW):** `git show <lockedSHA>` does not literally reproduce the contract — `LockedSHA = project.Status.Git.LastPushedSHA` (`task_controller.go:1531`), a coarse temporal anchor; the contract lives in the CRD, not git (comment `:1523-1530`). The safety-critical core (cannot mutate while Locked) is CEL-enforced and holds. |
| 2 | REPAIRABLE → fresh attempt seeded with locked spec + compact evidence packet; infra-retry stays distinct | ✓ VERIFIED | `dispatchRepairAttempt`: `nextAttempt`++, re-seeds original locked `PromptPath`, stages bounded packet (`stageEvidencePacket`, ≤20 findings) sourced from persisted `LastAttemptEvidence` (LO-02). Infra-retry flows through `checkRunningState`'s Job re-read (same attemptID), never `repairOrHalt`. **Live:** RED gate minted a2 as a fresh attempt (`"dispatching fresh quality-iteration attempt 2"`). |
| 3 | Independent LangGraph evaluator (`SelfInstruments` sentinel this phase); deterministic command failure always dominates — never pass on LLM APPROVED over a red gate | ✓ VERIFIED | `case "langgraph": return true` (`pkg/dispatch/vendor_capabilities.go:40`), fail-closed default. Verifier is a separate image/process (`JobKindVerifier`, `Provider.Vendor=langgraph`, `Role=verifier`). Triple-layer dominance: Python `_assemble_verdict` forces verdict DOWN on any non-zero exit, never up (`__main__.py:115`); empty-commands → BLOCKED; controller `hasDeterministicFailure` re-check (`task_controller.go:2349`, `:2896`) over fail-closed `ClassifyVerdict`. **Live:** verifier ran as an independent read-only Job; RED gate produced REPAIRABLE (semantically repairable), APPROVED unreachable over a red command. |
| 4 | Bounded by `maxIterations` → `onExhaustion` → `ConditionVerifyHalt` (both tiers, mirrors `failure_halt.go` + Phase-25 time-fence) as a halt class distinct from `Failed`; resumable; anti-gaming enforced | ✓ VERIFIED | `repairOrHalt` bounds on `Attempt >= MaxIterations` → `setVerifyHaltIfNeeded` (`verify_halt.go:85`); `verify_halt.go` is a faithful `failure_halt.go` clone incl. resume time-fence. Gates both tiers (`checkDispatchHolds` Billing→Failure→**Verify**→Budget→Import + `project_controller.go`). **HI-01:** exhaustion uses `LevelPhaseVerifyHalted` (`shared_types.go:511`), NOT `Failed`; `gateChecks` Step 1a short-circuits without `setFailureHaltIfNeeded`. **BL-01 anti-gaming:** enforced at executor completion on EVERY verdict path (`task_controller.go:1505` → `escalateSystem`) BEFORE a verifier can bless, plus belt-and-suspenders `intersectsProtectedRefs` in `repairOrHalt` (`:2640`). **Live:** RED → VerifyHalted@iter2 (distinct terminal); `tide resume` recovery exercised live. |
| 5 | Evaluator dispatches count against `verifierInFlightCount`; `LoopPolicy.BudgetCents` bounds cost via the reservation store — verified by a kind concurrent-dispatch test under the cap | ✓ VERIFIED | Cap-before-acquire: `verifierInFlightCount >= defaultVerifierConcurrencyCap` (=2) checked at `task_controller.go:2123` BEFORE `Reserve` at `:2133`; `releaseOnError` on every subsequent error arm. `BudgetCents` reserved via `ReservationStore`, settled in `settleVerifierSpend`. **Live:** kind `verifier_concurrency_test.go` ran `1 Passed \| 0 Failed` (157s) — count stayed under cap, drained to zero, no stranded Task; live billable run settled without BudgetCents overrun. |

**Score:** 5/5 roadmap success criteria verified — Layer A (CEL/envtest/unit/Python/lint) **and** Layer B (live billable loop + live kind concurrency). SC1 carries a documented reviewer-LOW LO-03 approximation on the literal `git show` sub-clause; its load-bearing invariant (immutability once Locked) is fully achieved. An override remains available (see prior report §"Suggested Override") but is not required — the SC's core is satisfied.

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | ----------- | ------ | ------- |
| `api/v1alpha3/task_types.go` | VerificationSpec + CEL + LoopStatus/LockedSHA/LastAttemptEvidence | ✓ VERIFIED | CEL XValidation (`:90`), Enum `Draft;Locked;Superseded` (`:97`) |
| `api/v1alpha3/shared_types.go` | ConditionVerifyHalt vocab + LevelPhaseVerifying/VerifyHalted | ✓ VERIFIED | `LevelPhaseVerifyHalted` (`:511`, HI-01) |
| `config/crd/bases/tideproject.k8s_tasks.yaml` | Generated CEL immutability rule | ✓ VERIFIED | `x-kubernetes-validations` (`:294`) |
| `pkg/dispatch/vendor_capabilities.go` | SelfInstruments langgraph case | ✓ VERIFIED | `case "langgraph"` (`:40`), fail-closed default |
| `cmd/tide-langgraph-verifier/verifier/__main__.py` | vendor sentinel + out-of-band multi-command deterministic verdict | ✓ VERIFIED | `_assemble_verdict` dominance (`:115`); TimeoutExpired→124 (`:108`) |
| `cmd/tide-langgraph-verifier/verifier/agent.py` | structured-output wiring (live-proof fix) | ✓ VERIFIED | `response_format=ToolStrategy(GateDecision)` (`:54`), `RECURSION_LIMIT = 50` (`:32`) — closes defect #2 (prose verdicts fail-closed to BLOCKED, making APPROVED/REPAIRABLE unreachable) |
| `cmd/tide-langgraph-verifier/Dockerfile` | importable entrypoint + build-time guard (live-proof fix) | ✓ VERIFIED | `PYTHONPATH=/app` (`:57`), `verdict.py` in COPY (`:50`), build-time `RUN cd / && python -c "import verifier, verifier.__main__"` guard (`:68`) — closes defects #1/#3 |
| `internal/subagent/common/templates/task_verifier.tmpl` | coverage-not-conservatism prompt (rewritten v4) | ✓ VERIFIED | Rewrite defines verdict semantics + honest tool contract + termination pressure (defect #4) |
| `internal/subagent/common/prompt_templates.go` | PromptTemplateVersion bump | ✓ VERIFIED | `const PromptTemplateVersion = "v4"` (`:48`) |
| `internal/dispatch/podjob/backend.go` | role-aware ReadVerifierOut + ReadOut skips verifier (ship-blocker fix) | ✓ VERIFIED | `ReadVerifierOut` selects role=verifier, highest attempt, grafts `TerminationStub.GateDecision`→`Verdict` (`:211-268`); `ReadOut` skips verifier pods (`:170`) — closes defect #5 (verdict was ALWAYS nil in a real cluster) |
| `internal/dispatch/podjob/{caps,names,jobspec}.go` | JobKindVerifier + VerifierJobName + TIDE_GATE_COMMAND + RO/RW mounts | ✓ VERIFIED | Confirmed by prior report; unchanged by fix commits |
| `internal/controller/verify_halt.go` | checkVerifyHalt + setVerifyHaltIfNeeded (clone + time-fence) | ✓ VERIFIED | `checkVerifyHalt` (`:58`), `setVerifyHaltIfNeeded` (`:85`) |
| `internal/controller/task_controller.go` | verifier dispatch/consumption + anti-gaming + halt + read seam | ✓ VERIFIED | `readVerifierEnvelope` seam additive (`:2845`); anti-gaming (`:1505`), dominance (`:2896`), cap-before-acquire (`:2123`) all intact |
| `cmd/manager/main.go` | VerifierImage production wiring | ✓ VERIFIED | `TIDE_VERIFIER_IMAGE` env → `VerifierImage` in TaskReconcilerDeps |
| `test/integration/kind/verifier_concurrency_test.go` | Layer B cap spec | ✓ VERIFIED (ran live) | `1 Passed \| 0 Failed`, 157s |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | --- | --- | ------ | ------- |
| verifier pod TerminationStub | `EnvelopeOut.Verdict` | `ReadVerifierOut` role-aware graft | ✓ WIRED | `backend.go:258-266`; **live-proven** — verdict now relays (was nil before defect #5 fix) |
| `handleVerifierCompletion` | `readVerifierEnvelope` | type-assert to `verifierEnvelopeReader`, else ReadOut | ✓ WIRED | `task_controller.go:2846`; envtest fakes fall back unchanged |
| `handleJobCompletion` (executor exit-0) | anti-gaming escalation | `intersectsProtected(RunEvidence.ChangedFiles, protected)` on ALL verdict paths | ✓ WIRED | `:1505` → `escalateSystem`; persists `LastAttemptEvidence` (`:1536`) |
| `handleVerifierCompletion` | `ClassifyVerdict` | fail-closed decision tree; APPROVED+det-failure→repairOrHalt | ✓ WIRED | `:2894`; nil/unreadable/BLOCKED→haltVerify |
| `repairOrHalt` (exhaustion) | `setVerifyHaltIfNeeded` | onExhaustion → ConditionVerifyHalt | ✓ WIRED | live-proven: VerifyHalted@iter2 |
| `dispatchVerifier` cap gate | `verifierInFlightCount` | cap-before-acquire before Reserve | ✓ WIRED | `:2123` before `:2133` |
| Python deterministic gate | `EnvelopeOut.Verdict` | `_assemble_verdict` forcing non-APPROVED on non-zero exit | ✓ WIRED | `__main__.py:115`; agent.py structured-output now emits a real GateDecision |
| `emitEvaluatorSpanForVerifier` | `pkg/otelai` EVALUATOR span | sibling to AGENT span, before terminal patches | ✓ WIRED | `:2870/:2881` |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
| -------- | ------------- | ------ | ------------------ | ------ |
| verifier verdict | `out.Verdict` | `ReadVerifierOut` grafts real `TerminationStub.GateDecision` from the terminated verifier pod's termination message | ✓ FLOWING | Live-proven: REPAIRABLE (red) and APPROVED (green) both relayed to the controller; previously ALWAYS nil (defect #5) |
| anti-gaming manifest | `task.Status.LastAttemptEvidence.ChangedFiles` | executor's real `out.RunEvidence.ChangedFiles`, persisted at exit-0 (`:1536`) | ✓ FLOWING | Sourced where the manifest exists (executor envelope), not the verifier's nil RunEvidence |
| LLM verdict | `structured_response` (GateDecision) | agent.py `ToolStrategy(GateDecision)` structured output | ✓ FLOWING | Previously prose (defect #2) → fail-closed BLOCKED; now a validated GateDecision instance |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Changed Go packages compile | `go build ./internal/controller/... ./internal/dispatch/podjob/... ./pkg/dispatch/... ./cmd/manager/...` | exit 0 | ✓ PASS |
| Fix commits on main | `git branch --contains 29e31374 / 076c9637` | both on `main` (ancestors of HEAD `00b20eb0`) | ✓ PASS |
| Full envtest suite | `make test` (this session) | green (relayed; consistent with code read) | ✓ PASS |
| Lint | `make lint` (this session) | 0 issues (relayed) | ✓ PASS |
| Python verifier suite | `make test-langgraph-verifier` (this session) | 78/78 (57 `test_` defs × parametrization; relayed) | ✓ PASS |
| Live billable Task loop | UAT (kind-tide-test, real key) | RED→REPAIRABLE→VerifyHalted; GREEN→APPROVED→Succeeded | ✓ PASS |
| Live kind concurrency | `--ginkgo.focus 'Verifier concurrent-dispatch'` | 1 Passed \| 0 Failed (157s) | ✓ PASS |

### Probe Execution

Not applicable — Phase 51 declares no `scripts/*/tests/probe-*.sh`. Its terminal validation gate was the Plan 08 kind live-run, now PASSED (recorded in `51-HUMAN-UAT.md`).

### Requirements Coverage

All 11 phase requirement IDs appear in ≥1 plan's `requirements` frontmatter and map 1:1 to Phase 51 in `REQUIREMENTS.md` (lines 110-127). Zero orphaned requirements.

| Requirement | Source Plan(s) | Description | Status | Evidence |
| ----------- | ---------- | ----------- | ------ | -------- |
| TASK-01 | 01, 06 | Verification contract, immutable once locked | ✓ SATISFIED | CEL immutability + Draft/Locked/Superseded (git-show is LO-03 approximation) |
| TASK-02 | 07 | REPAIRABLE → fresh attempt + compact evidence packet | ✓ SATISFIED | dispatchRepairAttempt + stageEvidencePacket; live-proven fresh a2 |
| TASK-03 | 07 | Infra-retry distinct from quality-iteration | ✓ SATISFIED | checkRunningState (same attemptID) vs dispatchRepairAttempt (Attempt++) |
| TASK-04 | 02, 04, 06, 07 | Independent evaluator; deterministic failure dominates | ✓ SATISFIED | langgraph sentinel + triple-layer dominance; live independent read-only verifier |
| TASK-05 | 01, 07 | Bounded by maxIterations; onExhaustion; resumable | ✓ SATISFIED | repairOrHalt bound + LoopStatus re-derivable; live VerifyHalted@iter2 + `tide resume` |
| TASK-06 | 07 | Anti-gaming enforced, not documented | ✓ SATISFIED | BL-01: executor-completion enforcement all verdict paths + escalateSystem |
| EVAL-04 | 03 | Coverage-not-conservatism verifier template | ✓ SATISFIED | task_verifier.tmpl coverage directive (rewritten v4) |
| ESC-02 | 05 | ConditionVerifyHalt mirrors failure_halt.go + time-fence, both tiers | ✓ SATISFIED | verify_halt.go clone + dispatch-hold chain + project_controller |
| ESC-03 | 01, 05 | Distinct halt class, never Failed reinterpretation | ✓ SATISFIED | HI-01: LevelPhaseVerifyHalted + gateChecks Step 1a; live distinct VerifyHalted terminal |
| ESC-04 | 04, 06, 08 | Concurrency gate + BudgetCents; kind test under cap | ✓ SATISFIED | cap-before-acquire + BudgetCents; kind spec ran live 1 Passed/0 Failed |
| OBS-03 | 02, 03, 06, 07 | langgraph SelfInstruments this phase + EVALUATOR sibling span | ✓ SATISFIED | SelfInstruments langgraph + emitEvaluatorSpanForVerifier sibling to AGENT span |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| (none) | — | No `TBD`/`FIXME`/`XXX` debt markers in any phase-modified source (incl. the two fix commits' files) | — | Debt-marker gate: PASS |
| `task_controller.go` | 1531 | LockedSHA = LastPushedSHA (LO-03) | ℹ INFO | Documented reviewer-LOW approximation, not a stub — value flows, just coarser than git-show literal |

### Human Verification Required

None. Both prior human-verify checkpoints (live billable Task-loop proof; live kind concurrency spec) PASSED live on 2026-07-20 and are transcribed in `51-HUMAN-UAT.md` (status: passed, 2/2). No new human items surfaced.

### Gaps Summary

**No gaps.** The prior verification was `human_needed` solely because two live billable proofs were held as explicit escalation-gate checkpoints — not code gaps. Both are now PASSED live.

Achieving the green live proof exposed that the shipped verify path had never actually executed end-to-end (envtest substitutes fake envelope readers and fake models, so 5 real-cluster-only defects were latent). All 5 were root-fixed on `main` and re-confirmed here by direct code read (not SUMMARY claims):

1. **Verifier image entrypoint dead** — `verdict.py` missing from the Dockerfile COPY, no `PYTHONPATH` for the pod's `/workspace` workingDir, import-time repo-root walk unreachable in the image. Fixed + a build-time `import verifier, verifier.__main__` guard (`Dockerfile:50,57,68`). Commit `29e31374`.
2. **No structured output** — `create_agent` never wired `response_format=ToolStrategy(GateDecision)`, so every LLM verdict was prose that `classify_verdict` fail-closed to BLOCKED (APPROVED/REPAIRABLE structurally unreachable). Wired + `RECURSION_LIMIT` 10→50 (`agent.py:32,54`). Commit `076c9637`.
3. **(caught by the new build-time guard)** — see #1.
4. **Prompt semantics** — `task_verifier.tmpl` left verdict semantics undefined and told the model "non-zero exit dominates whatever verdict you would otherwise write," making every model answer BLOCKED on a red gate and foreclosing the repair loop; also referenced paths the two tools cannot reach. Rewritten (verdict definitions, honest tool contract, termination pressure); `PromptTemplateVersion` v3→v4 (`prompt_templates.go:48`). Commit `076c9637`.
5. **Ship-blocker: verdict relay** — the controller read the verdict via `ReadOut`, which listed pods by task-uid alone (executor and verifier share it — coin flip) and unmarshalled the termination message as `EnvelopeOut` while the verifier writes a tiny `TerminationStub` — so `out.Verdict` was ALWAYS nil in a real cluster and every verify fail-closed `VerifierVerdictMissing`. Fixed: role-aware `ReadVerifierOut` (verifier-role pods, highest attempt, stub→Verdict graft) + `ReadOut` skips verifier pods + a `readVerifierEnvelope` type-assert seam that leaves envtest fakes unchanged (`backend.go:170,211-268`; `task_controller.go:2845`). Commit `076c9637`.

Both fix commits are on `main` (ancestors of HEAD `00b20eb0`), the changed Go packages compile clean (`go build` exit 0), and none of the fixes regressed the load-bearing safety paths (the task_controller change is purely the additive read seam; dominance, anti-gaming at `:1505`, halt, and cap-before-acquire are all intact by direct re-read).

**Gate decision: APPROVED.** Phase 51 goal achieved and proven live. Ready to mark the phase complete and advance to Phase 52.

---

_Verified: 2026-07-20T01:37:07Z_
_Verifier: Claude (gsd-verifier)_
