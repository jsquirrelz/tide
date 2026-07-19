---
phase: 51
slug: the-task-loop
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-19
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

- **After every task commit:** Run `go test ./internal/controller/... ./pkg/dispatch/...`
- **After every plan wave:** Run `make test` + affected-package envtest; `make test-langgraph-verifier` when the verifier image/verdict changes
- **Before `/gsd:verify-work`:** `make test-int` green (envtest Layer A + kind Layer B), `make lint` clean, `make manifests` zero-diff
- **Max feedback latency:** ~60 seconds (quick envtest); kind concurrent-dispatch test (ESC-04) is full-suite only

---

## Per-Task Verification Map

> Populated during planning — one row per task. Requirement/Threat/Test-Type columns bind each task to a Phase-51 requirement (TASK-01..06, EVAL-04, ESC-02/03/04, OBS-03) and its test layer.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 51-XX-XX | XX | X | TASK-0X | T-51-0X / — | {expected secure behavior or "N/A"} | envtest / kind / pytest | `{command}` | ✅ / ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `pkg/dispatch/verdict.go` deterministic-dominance test fixtures (D-06) — red gate exit forces non-APPROVED
- [ ] `internal/controller/verify_halt_test.go` — `ConditionVerifyHalt` mirror + Phase-25 resume time-fence (ESC-02) + wave-semantics-untouched regression (ESC-03)
- [ ] kind concurrent-dispatch fixture for `verifierInFlightCount` under the sized cap (ESC-04)
- [ ] `verifier/tests/` parity for the deterministic-gate-exit capture path

*Existing infrastructure (envtest + kind + pytest) covers all phase requirements; the above are net-new test files per requirement.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live verifier dispatch against a real gate command (credproxy-gated) | TASK-04 / EVAL-04 | Requires real Anthropic API + kind-tide-dogfood; billable | Run a Task with a red gate command on kind; confirm REPAIRABLE → fresh attempt, then `ConditionVerifyHalt` at maxIterations |

*Most phase behaviors have automated envtest/kind coverage; the live billable dispatch is the one manual proof.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
