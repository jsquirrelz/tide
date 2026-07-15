---
phase: 42
slug: trace-context-foundation-planner-level-span-emission
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-15
---

# Phase 42 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Ginkgo v2.28 + Gomega (envtest specs) / plain `go test` (pure-function unit tests) |
| **Config file** | `internal/controller/suite_test.go` (envtest bootstrap); none for `pkg/otelai` |
| **Quick run command** | `go test ./pkg/otelai/... -v` |
| **Full suite command** | `make test-int-fast` (Layer A envtest) |
| **Estimated runtime** | ~1s quick / ~90s full Layer A |

---

## Sampling Rate

- **After every task commit:** Run `go test ./pkg/otelai/... -v` (sub-second, pure functions)
- **After every plan wave:** Run `make test-int-fast` (Layer A: envtest + heavy-labeled controller specs)
- **Before `/gsd:verify-work`:** Full `make test-int` green — read `MAKE_EXIT` AND `grep -nE '^--- FAIL|^FAIL\s'`, not just the Ginkgo summary (CLAUDE.md discipline)
- **Max feedback latency:** 90 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| TBD (planner fills) | — | — | ATTR-01 | — | N/A | unit + envtest | `go test ./pkg/otelai/... -v` + `go test ./internal/controller/... -ginkgo.focus="SpanEmission"` | ❌ W0 | ⬜ pending |
| TBD (planner fills) | — | — | ATTR-02 | — | N/A | unit | `go test ./pkg/otelai/... -run TestTokenCount -v` | ❌ W0 | ⬜ pending |
| TBD (planner fills) | — | — | ATTR-03 | — | N/A | unit (source-grep guard) | `go test ./pkg/otelai/... -run TestKeysUseSemconvModule -v` | ❌ W0 | ⬜ pending |
| TBD (planner fills) | — | — | D-01/D-02 (span-per-Job-UID, succeeded AND failed) | — | N/A | envtest `Label("envtest","heavy")` | `go test ./internal/controller/... -ginkgo.label-filter='heavy' -ginkgo.focus="SpanIdempotency"` | ❌ W0 | ⬜ pending |
| TBD (planner fills) | — | — | D-04 (degraded span on envReadOK=false) | — | N/A | envtest | same file, separate `It(...)` block | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `pkg/otelai/tracecontext_test.go` — pure unit tests for `TraceIDFromUID`/`FormatTraceparent`/`ExtractRemoteParent`
- [ ] `pkg/otelai/attrs_test.go` extensions — D-05 key-string expectations, `llm.token_count.total` (D-08), `llm.model_name`/`llm.provider` (ATTR-01)
- [ ] `internal/controller/span_emission_test.go` (new) — envtest specs using `tracetest.NewInMemoryExporter()` + `otel.SetTracerProvider(...)` per `It(...)`, direct-call shape mirroring `child_rollup_idempotency_test.go`
- [ ] `completedJob == nil` regression test per level asserting zero spans recorded
- [ ] Failed-Job test per level asserting span end timestamp derives from `JobFailed.LastTransitionTime` (no nil-panic/zero-value)
- Framework install: none — Ginkgo/Gomega/envtest/`tracetest` already in `go.mod`/`go.sum`

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Spans render in a live OTLP backend (Phoenix) | ATTR-01 success criterion #1 phrasing ("operator pointed at any OTLP-compatible backend sees...") | Live-backend rendering is milestone-close proof (PROOF-01, Phase 47); envtest in-memory exporter is the phase-level proxy | Deferred to Phase 47 live proof; phase-level: assert exported spans via in-memory exporter |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
