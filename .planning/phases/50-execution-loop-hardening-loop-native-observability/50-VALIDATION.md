---
phase: 50
slug: execution-loop-hardening-loop-native-observability
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-18
---

# Phase 50 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `go test` (Ginkgo/Gomega for integration) + pytest (Python verifier parity) |
| **Config file** | none — existing infra (`Makefile` targets) |
| **Quick run command** | `go test ./pkg/dispatch/... ./pkg/otelai/... ./internal/metrics/...` |
| **Full suite command** | `make test` (Go unit) + `make test-langgraph-verifier` (Python parity) |
| **Estimated runtime** | ~60–120 seconds (unit); `make test-int` (kind) longer |

---

## Sampling Rate

- **After every task commit:** Run the quick package command for the touched package
- **After every plan wave:** Run `make test` + `make test-langgraph-verifier`
- **Before `/gsd:verify-work`:** Full suite green + `make lint` + `go vet ./...` (metriccardinality analyzer)
- **Max feedback latency:** ~120 seconds

---

## Per-Task Verification Map

> Filled by the planner / gsd-nyquist-auditor once plans exist. Derived from RESEARCH.md §"Validation Architecture".

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 50-01-01 | 01 | 1 | EXEC-02 | — | terminal reason never silent-defaults | unit | `go test ./pkg/dispatch/...` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `pkg/dispatch/envelope_test.go` — round-trip + `<4KB` TerminationStub assertions for new fields (TerminalReason, RunEvidence, loopRunID/attemptID)
- [ ] `cmd/tide-langgraph-verifier/verifier/tests/test_envelope.py` — Go↔Python parity for the mirrored fields
- [ ] `pkg/otelai/attrs_test.go` — `TestKeysUseSemconvModule` coverage for the new `loop.*`/`evaluation.*` helpers
- [ ] `internal/metrics/*_test.go` + `tools/analyzers/metriccardinality/` testdata — run-ID-shaped-label rejection

*Existing infrastructure covers the framework; Wave 0 adds the new test files above.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live loop.*/evaluation.* span rendering in Phoenix | OBS-01 | Requires a running cluster + Phoenix (deferred proof; unit tests assert attribute presence) | Optional — assert via unit test of span attributes; live render is a Phase 51/53 concern |

*Most phase behaviors have automated verification; the live Phoenix render is optional and covered structurally by attribute-presence unit tests.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 120s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
