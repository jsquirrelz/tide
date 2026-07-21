---
phase: 51
slug: the-task-loop
status: planned
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-19
updated: 2026-07-19
---

# Phase 51 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `go test` (Layer A: controller-runtime envtest) + Ginkgo/kind (Layer B) + pytest (verifier Python parity) |
| **Config file** | none — envtest binaries via `setup-envtest`; kind cluster per heavy run |
| **Quick run command** | `go test ./internal/controller/... ./pkg/dispatch/...` |
| **Full suite command** | `make test-int` (envtest + kind) · `make test-langgraph-verifier` (Python) · `make lint` · `make manifests` (zero-diff) |
| **Estimated runtime** | ~30–60s envtest quick; several min full kind |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/controller/... ./pkg/dispatch/...` (+ the touched-package quick run in the task's `<verify>`).
- **After every plan wave:** Run `make test` + affected-package envtest; `make test-langgraph-verifier` when the verifier image/verdict changes.
- **Before `/gsd:verify-work`:** `make test-int` green (envtest Layer A + kind Layer B), `make lint` clean, `make manifests` zero-diff.
- **Max feedback latency:** ~60 seconds (quick envtest); kind concurrent-dispatch test (ESC-04) is full-suite only.
- **`make test-int` caveat (CLAUDE.md):** `MAKE_EXIT` ≠ Ginkgo-green — a bundled plain go-test can fail the package while Ginkgo prints SUCCESS. Always read `MAKE_EXIT` AND `grep -nE '^--- FAIL|^FAIL\s'`.

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 51-01-01 | 01 | 1 | TASK-01 | T-51-01 | Locked verification is immutable; Draft mutable; Superseded escape (CEL) | envtest | `go test ./internal/controller/... -run VerificationImmutab -count=1` | ❌ W0 | ⬜ pending |
| 51-01-02 | 01 | 1 | ESC-03 | T-51-01c | VerifyHalt vocabulary exists as a distinct halt class | build/unit | `go build ./api/...` | ✅ | ⬜ pending |
| 51-01-03 | 01 | 1 | TASK-01, TASK-05 | T-51-01 | CRD carries the transition rule from the marker; LoopStatus embeds | envtest | `go test ./internal/controller/... -run VerificationImmutab -count=1` | ❌ W0 | ⬜ pending |
| 51-02-01 | 02 | 1 | OBS-03 | T-51-02c | SelfInstruments("langgraph")=true, all others false | unit | `go test ./pkg/dispatch/... -run SelfInstruments -count=1` | ⚠️ partial | ⬜ pending |
| 51-02-02 | 02 | 1 | TASK-04 | T-51-02 | Red gate exit deterministically dominates an LLM APPROVED (verifier-side) | pytest | `make test-langgraph-verifier` | ⚠️ partial | ⬜ pending |
| 51-03-01 | 03 | 1 | EVAL-04 | T-51-03 | Verifier prompt = coverage-not-conservatism; version co-bumped | unit | `go test ./internal/subagent/common/... -run 'Template\|Verifier' -count=1` | ❌ W0 | ⬜ pending |
| 51-03-02 | 03 | 1 | OBS-03 | T-51-03c | EvaluatorInvocation routes through the semconv module | unit | `go test ./pkg/otelai/... -count=1` | ⚠️ partial | ⬜ pending |
| 51-03-03 | 03 | 1 | OBS-03 | T-51-03c | EVALUATOR span is a sibling of the AGENT span, no double-emit | unit | `go test ./internal/controller/... -run 'EvaluatorSpan\|SpanEmission' -count=1` | ⚠️ partial | ⬜ pending |
| 51-04-01 | 04 | 1 | TASK-04, ESC-04 | T-51-04 | JobKindVerifier + VerifierJobName grep-distinct; verify floor | unit | `go test ./internal/dispatch/podjob/... -run 'Caps\|JobName\|Verifier' -count=1` | ❌ W0 | ⬜ pending |
| 51-04-02 | 04 | 1 | TASK-04 | T-51-04 | TIDE_GATE_COMMAND injected; RO worktree preserved + RW envelopes/ mount; no git-write creds | unit | `go test ./internal/dispatch/podjob/... -run 'Verifier\|ReadOnly\|JobSpec' -count=1` | ⚠️ partial | ⬜ pending |
| 51-05-01 | 05 | 2 | ESC-02, ESC-03 | T-51-05c | checkVerifyHalt/setVerifyHaltIfNeeded mirror failure_halt + resume time-fence | envtest | `go test ./internal/controller/... -run VerifyHalt -count=1` | ❌ W0 | ⬜ pending |
| 51-05-02 | 05 | 2 | ESC-02, ESC-03 | T-51-05, T-51-05b, T-51-05d | Uniform hold order across levels; Project chain holds; distinct from Failed | envtest | `go test ./internal/controller/... -run 'CoOccurring\|VerifyHalt\|DispatchHolds' -count=1` | ❌ W0 | ⬜ pending |
| 51-06-01 | 06 | 3 | ESC-04 | T-51-06 | verifierInFlightCount (self-excluding, role=verifier) | envtest | `go test ./internal/controller/... -run VerifierInFlight -count=1` | ❌ W0 | ⬜ pending |
| 51-06-02 | 06 | 3 | TASK-01, TASK-04, ESC-04, OBS-03 | T-51-06, T-51-06b, T-51-06d | Contract-bearing Task dispatches independent verifier (locked GateCommand, lockedSHA, cap+budget, no leak); contract-less skips | envtest | `go test ./internal/controller/... -run 'VerifyDispatch\|VerifierDispatch' -count=1` | ❌ W0 | ⬜ pending |
| 51-07-01 | 07 | 4 | TASK-04, OBS-03 | T-51-07 | Fail-closed verdict; red gate dominates APPROVED controller-side; EVALUATOR span emitted | envtest | `go test ./internal/controller/... -run VerifyLoop -count=1` | ❌ W0 | ⬜ pending |
| 51-07-02 | 07 | 4 | TASK-02, TASK-03, TASK-05, TASK-06 | T-51-07b, T-51-07c, T-51-07d, T-51-07e | Fresh evidence-seeded attempt; anti-gaming TP+TN; infra-retry distinct; maxIterations→VerifyHalt; resume | envtest | `go test ./internal/controller/... -run 'VerifyLoop\|AntiGaming\|InfraRetry\|Resume' -count=1` | ❌ W0 | ⬜ pending |
| 51-08-01 | 08 | 5 | ESC-04 | T-51-08 | Concurrent verifier dispatch stays under the sized cap; no slot/reservation leak | kind | `make test-int` (grep `^--- FAIL\|^FAIL\s`; MAKE_EXIT=0) | ❌ W0 | ⬜ pending |
| 51-08-02 | 08 | 5 | ESC-04 (live TASK-04/EVAL-04) | T-51-08, T-51-08b | Live: red gate → REPAIRABLE → fresh attempt → ConditionVerifyHalt; RO verifier; cost-bounded | manual (human-verify) | live kind run (billable) | N/A | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

Net-new test files created within the plan that produces the behavior (no separate Wave-0-only plan — each plan scaffolds its own test):

- [ ] `internal/controller/verification_immutability_test.go` — TASK-01 CEL admission (envtest against a real API server; CEL runs only there). **Plan 01 Task 3.**
- [ ] `cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py` (extend) — D-06 deterministic-dominance case. **Plan 02 Task 2.**
- [ ] `internal/subagent/common/prompt_templates_test.go` (extend) + `pkg/otelai/attrs_test.go` (extend) + `internal/controller/span_emission_test.go` (extend). **Plan 03.**
- [ ] `internal/dispatch/podjob/jobspec_test.go` (extend) — RO-preserved + RW-envelopes + no-git-cred + TIDE_GATE_COMMAND. **Plan 04 Task 2.**
- [ ] `internal/controller/verify_halt_test.go` — ESC-02/03 mirror + resume time-fence. **Plan 05 Task 1.**
- [ ] `internal/controller/co_occurring_holds_test.go` — D-09 gate-order-unification proof (folds both todos). **Plan 05 Task 2.**
- [ ] `internal/controller/task_verify_dispatch_test.go` — verifier dispatch + backward-compat + no-leak. **Plan 06 Task 2.**
- [ ] `internal/controller/task_verify_loop_test.go` — verdict tree, fresh attempt, anti-gaming TP+TN, infra-vs-quality, resume. **Plan 07.**
- [ ] `test/integration/kind/verifier_concurrency_test.go` — ESC-04 live concurrent-dispatch under cap. **Plan 08 Task 1.**
- [ ] Framework install: none — Ginkgo/Gomega/envtest/pytest all present.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live verifier dispatch against a real gate command (credproxy-gated) | TASK-04 / EVAL-04 / ESC-04 | Requires real Anthropic API + kind cluster; billable | Plan 08 Task 2: run a Task with a red gate command on kind; confirm REPAIRABLE → fresh attempt → ConditionVerifyHalt at maxIterations, then green-gate → APPROVED |

*Most phase behaviors have automated envtest/kind coverage; the live billable dispatch is the one manual proof.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or a Wave-0 test scaffolded in the same plan (Plan 08 Task 2 is the sole `<human-check>` checkpoint).
- [x] Sampling continuity: no 3 consecutive tasks without automated verify (only the terminal checkpoint is manual).
- [x] Wave 0 covers all MISSING references (each net-new test is created in the plan that produces its behavior).
- [x] No watch-mode flags.
- [x] Feedback latency < 60s (quick envtest; kind is full-suite only).
- [x] `nyquist_compliant: true` set in frontmatter.

**Approval:** planned 2026-07-19
