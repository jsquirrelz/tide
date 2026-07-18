---
phase: 49
slug: common-loop-contract-verdict-envelope-persistence-schema
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-18
---

# Phase 49 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> This phase is **schema/contract definition** — validation is dominated by
> type round-trip, fail-closed classification, and size-invariant tests across
> **two languages** (Go + Python), plus deepcopy codegen determinism.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `go test` + Python `pytest` (dual-language parity) |
| **Config file** | `Makefile` (Go targets); `cmd/tide-langgraph-verifier/` uv/pytest (`make test-langgraph-verifier`) |
| **Quick run command** | `go test ./pkg/dispatch/... ./api/v1alpha3/... && make test-langgraph-verifier` |
| **Full suite command** | `make test && make test-langgraph-verifier && make generate && git diff --exit-code` |
| **Estimated runtime** | ~60–120 seconds (Go unit + pytest; envtest not required for pure-type tests) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./pkg/dispatch/... ./api/v1alpha3/...` (+ `make test-langgraph-verifier` for any task touching `verifier/`)
- **After every plan wave:** Run the full suite command above
- **Before `/gsd:verify-work`:** Full suite green AND `make generate` produces no diff AND `make manifests` produces no CRD-YAML diff (LoopPolicy/LoopStatus are standalone this phase — zero embedding)
- **Max feedback latency:** ~120 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| {planner fills per task} | | | LOOP-01/02/03 · EVAL-03/05 | T-49-xx / — | fail-closed verdict never yields APPROVED | unit | `go test ./pkg/dispatch/...` / pytest | ✅ / ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Shared golden-JSON verdict fixture placed where **both** `go test ./pkg/dispatch/...` and `make test-langgraph-verifier` (pytest) consume it (parity harness for EVAL-03)
- [ ] Go test scaffold in `pkg/dispatch/` for `GateDecision`/`Finding` round-trip + fail-closed classifier
- [ ] Python test scaffold extending `cmd/tide-langgraph-verifier/verifier/tests/` (`conftest.py` factory) for the mirrored Pydantic pair + classifier

*Existing infrastructure (Go `go test`, verifier pytest via uv) covers the run harness; only the fixtures/scaffolds above are net-new.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| — | — | All phase behaviors have automated verification | — |

*All phase behaviors have automated verification (round-trip, fail-closed 3 shapes, <4KB TerminationStub, LoopStatus current-iteration-only size test, deepcopy/manifests no-diff).*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers the shared golden fixture + both test scaffolds
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
