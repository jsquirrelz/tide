---
phase: 50
slug: execution-loop-hardening-loop-native-observability
status: ready
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-18
updated: 2026-07-18
---

# Phase 50 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Synced post-planning against the 7 finalized plans (16 tasks, 3 waves). `wave_0_complete`
> flips to `true` once the Wave 0 test stubs are created during execute-phase.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework (Go)** | stdlib `testing` + Ginkgo v2.28/Gomega (envtest, Layer A) |
| **Framework (Python)** | pytest (`cmd/tide-langgraph-verifier/verifier/tests/`) |
| **Config file** | none dedicated — `go test ./...` + `make test-langgraph-verifier` |
| **Quick run command** | `go test ./pkg/dispatch/... ./pkg/otelai/... ./internal/reporter/... ./internal/controller/... ./tools/analyzers/metriccardinality/... ./internal/metrics/...` |
| **Full suite command** | `make test` + `make test-langgraph-verifier` + `make lint` (golangci-lint + import firewalls + `tide-lint` custom analyzers incl. `metriccardinality`) + `go vet ./...` |
| **Estimated runtime** | ~60–120 seconds (unit); Layer A envtest longer |

---

## Sampling Rate

- **After every task commit:** Run the quick package command for the touched package
- **After every plan wave:** Run `make test` + `make test-langgraph-verifier`
- **Before `/gsd:verify-work`:** Full suite green + `make lint` + `go vet ./...` (the `metriccardinality` analyzer is a security control here, not just quality — OBS-02)
- **Max feedback latency:** ~120 seconds

---

## Per-Task Verification Map

> Mapped at plan granularity from the finalized frontmatter (requirements + wave) + the package
> test command. Individual task IDs follow the `50-<plan>-<task>` convention.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|--------|
| 50-01-01 | 01 | 1 | EXEC-02 | evidence-injection | `TerminalReason` enum + fail-closed-on-zero-value | unit (tdd) | `go test ./pkg/dispatch/...` | ⬜ pending |
| 50-01-02 | 01 | 1 | EXEC-01/03/04 | evidence-injection | `RunEvidence` + IDs + `<4KB` stub + EXEC-04 belief-only guard | unit | `go test ./pkg/dispatch/...` | ⬜ pending |
| 50-02-01 | 02 | 1 | OBS-01 | — | `loop.*`/`evaluation.*` helper keys, not `tide.`-prefixed | unit (tdd) | `go test ./pkg/otelai/...` | ⬜ pending |
| 50-02-02 | 02 | 1 | OBS-01 | — | `TestKeysUseSemconvModule` passes for new helpers | unit | `go test ./pkg/otelai/...` | ⬜ pending |
| 50-03-01 | 03 | 1 | OBS-02 | label-cardinality DoS | analyzer rejects run-ID-shaped labels | unit (tdd) | `go test ./tools/analyzers/metriccardinality/...` | ⬜ pending |
| 50-03-02 | 03 | 1 | OBS-02 | label-cardinality DoS | runtime source-grep guard extended | unit | `go vet ./... && go test ./internal/metrics/...` | ⬜ pending |
| 50-04-01 | 04 | 2 | EXEC-02 | evidence-injection | every `EnvelopeOut{}` exit path sets `TerminalReason` | unit (tdd) | `go test ./internal/subagent/... ./cmd/...` | ⬜ pending |
| 50-04-02 | 04 | 2 | EXEC-03 | secret-leak | bounded `RunEvidence` assembled; no creds in evidence | unit (tdd) | `go test ./internal/subagent/... ./cmd/...` | ⬜ pending |
| 50-04-03 | 04 | 2 | EXEC-01/02 | — | `CheckCaps` wired → in-pod `cap_exceeded`; AST fail-closed guard | unit | `go test ./internal/subagent/...` | ⬜ pending |
| 50-05-01 | 05 | 2 | EXEC-02/03 | evidence-injection | Python `envelope.py` mirror of new fields | unit (tdd) | `make test-langgraph-verifier` | ⬜ pending |
| 50-05-02 | 05 | 2 | EXEC-02/03 | — | shared golden-fixture Go↔Python parity | unit | `make test-langgraph-verifier` | ⬜ pending |
| 50-06-01 | 06 | 2 | EXEC-01 | — | `buildEnvelopeIn` stamps `LoopRunID`/`AttemptID` (derived) | unit (tdd) | `go test ./internal/controller/...` | ⬜ pending |
| 50-06-02 | 06 | 2 | EXEC-02 | — | controller synthesizes `cap_exceeded` for DeadlineExceeded kills | unit (tdd) | `go test ./internal/controller/...` | ⬜ pending |
| 50-06-03 | 06 | 2 | OBS-01 | — | `synthesizePlannerSpan` stamps `loop.*` on AGENT span; `grep VerifyHalt`=0 | unit (tdd) | `go test ./internal/controller/...` | ⬜ pending |
| 50-07-01 | 07 | 3 | EXEC-01/OBS-01 | — | `ReporterOptions`/`BuildReporterJob` Args threading | unit (tdd) | `go test ./internal/controller/...` | ⬜ pending |
| 50-07-02 | 07 | 3 | EXEC-01/OBS-01 | — | `EmitSpans` stamps `loop.run_id` + 1-indexed `loop.iteration` | unit (tdd) | `go test ./internal/reporter/...` | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `pkg/dispatch/envelope_test.go` — round-trip + `<4KB` `TestNewTerminationStub_StaysSmall` extension for `TerminalReason`/`RunEvidence`/IDs
- [ ] `pkg/dispatch/testdata/envelope_out_golden.json` — shared Go↔Python parity fixture (mirror `gate_decision_golden.json`)
- [ ] `cmd/tide-langgraph-verifier/verifier/tests/test_envelope.py` — Python mirror assertions
- [ ] `pkg/otelai/attrs_test.go` — `TestKeysUseSemconvModule` coverage for `loop.*`/`evaluation.*`
- [ ] `tools/analyzers/metriccardinality/testdata/` — run-ID-shaped-label rejection fixture

*Existing infrastructure covers the framework; Wave 0 adds the new test files above.*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Live `loop.*` span rendering in Phoenix | OBS-01 | Requires a running cluster + Phoenix (deferred; structurally covered by attribute-presence unit tests) | Optional — assert via span-attribute unit tests; live render is a Phase 51/53 concern |

*All Phase-50 behaviors have automated verification; the live Phoenix render is optional and covered structurally by attribute-presence unit tests.*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 120s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-07-18 (post-planning sync; plan-checker confirmed Dimension 8 passes across all 16 tasks)
