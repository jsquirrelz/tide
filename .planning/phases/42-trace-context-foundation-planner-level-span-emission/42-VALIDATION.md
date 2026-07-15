---
phase: 42
slug: trace-context-foundation-planner-level-span-emission
status: planned
nyquist_compliant: true
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
| 42-01/T1 | 42-01 | 1 | ATTR-03 (module legitimacy gate) | T-42-SC | Human verifies proxy.golang.org + sum.golang.org evidence before go get | checkpoint:human-verify | — (blocking human gate, never auto-approved) | n/a | ⬜ pending |
| 42-01/T2 | 42-01 | 1 | ATTR-01 + ATTR-02 (helpers) + D-05/D-07/D-08 | — | N/A | unit (tdd) | `go test ./pkg/otelai/... -v` | ❌ W0 (extends attrs_test.go) | ⬜ pending |
| 42-01/T3 | 42-01 | 1 | ATTR-03 | — | N/A | unit (source-grep guard) | `go test ./pkg/otelai/ -run TestKeysUseSemconvModule -v` | ❌ W0 | ⬜ pending |
| 42-02/T1 | 42-02 | 1 | ATTR-03 (trace-context primitives, goal-mandated) | T-42-03 | Malformed traceparent never panics; parsing via propagation API only | unit (tdd) | `go test ./pkg/otelai/ -run TestTraceContext -v` | ❌ W0 (tracecontext_test.go) | ⬜ pending |
| 42-02/T2 | 42-02 | 1 | ATTR-03 (purity guard) | — | N/A | unit (source-grep guard) | `go test ./pkg/otelai/ -run TestTraceContextNoK8sImports -v` | ❌ W0 | ⬜ pending |
| 42-03/T1 | 42-03 | 1 | ATTR-01/ATTR-02 (marker storage) | T-42-05 | Marker is telemetry-only, no control-flow authority | build + manifest grep | `make manifests && make generate && go build ./...` + `grep -rn spanEmittedUID config/crd/bases/` | n/a (generated) | ⬜ pending |
| 42-04/T1 | 42-04 | 2 | ATTR-01 + ATTR-02 (synthesis mechanics) + D-01/D-03/D-04, Pitfalls 1/4/5 | T-42-09 | Envelope ints attached as telemetry only | unit | `go test ./internal/controller/ -run 'TestSpanEndTime\|TestSynthesizePlannerSpan' -v` | ❌ W0 (span_emission_unit_test.go) | ⬜ pending |
| 42-04/T2 | 42-04 | 2 | ATTR-01/ATTR-02 (milestone+phase wiring) | T-42-08 | envReadOK-independent marker gate | build + vet + unit rerun | `go build ./internal/controller/... && go vet ./internal/controller/...` | n/a | ⬜ pending |
| 42-04/T3 | 42-04 | 2 | ATTR-01/ATTR-02 + D-01/D-02 (span-per-Job-UID, succeeded AND failed) + D-04 (degraded span) | T-42-07/T-42-08 | Reason string flows as attribute value only | envtest `Label("envtest","heavy")` | `go test ./internal/controller/ -ginkgo.label-filter='heavy' -ginkgo.focus='SpanEmission'` | ❌ W0 (span_emission_test.go) | ⬜ pending |
| 42-05/T1 | 42-05 | 3 | ATTR-01/ATTR-02 (plan+project wiring) | T-42-11 | No ImportSource span suppression (budget-only); marker exactly-once | build + vet + unit rerun | `go build ./internal/controller/... && go vet ./internal/controller/...` | n/a | ⬜ pending |
| 42-05/T2 | 42-05 | 3 | ATTR-01/ATTR-02 + D-01/D-02 (plan+project levels) | T-42-10/T-42-11 | Same accepted disclosure class as 42-04 | envtest + full Layer A | `go test ./internal/controller/ -ginkgo.label-filter='heavy' -ginkgo.focus='SpanEmission'` then `make test-int-fast` (MAKE_EXIT + grep-FAIL) | ❌ W0 (extends span_emission_test.go) | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `pkg/otelai/tracecontext_test.go` — pure unit tests for `TraceIDFromUID`/`FormatTraceparent`/`ExtractRemoteParent` → **42-02/T1** (tdd: tests written first)
- [ ] `pkg/otelai/attrs_test.go` extensions — D-05 key-string expectations, `llm.token_count.total` (D-08), `llm.model_name`/`llm.provider` (ATTR-01) → **42-01/T2** (tdd: tests written first)
- [ ] `internal/controller/span_emission_test.go` (new) — envtest specs using `tracetest.NewInMemoryExporter()` + `otel.SetTracerProvider(...)` per `It(...)`, direct-call shape mirroring `child_rollup_idempotency_test.go` → **42-04/T3** (milestone+phase), **42-05/T2** (plan+project)
- [ ] `completedJob == nil` regression test per level asserting zero spans recorded → **42-04/T3** (milestone, phase) + **42-05/T2** (plan, project); plus unit-level coverage in **42-04/T1**
- [ ] Failed-Job test per level asserting span end timestamp derives from `JobFailed.LastTransitionTime` (no nil-panic/zero-value) → **42-04/T3** (milestone, phase) + **42-05/T2** (plan, project); plus unit-level coverage in **42-04/T1**
- Framework install: none — Ginkgo/Gomega/envtest/`tracetest` already in `go.mod`/`go.sum`

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Spans render in a live OTLP backend (Phoenix) | ATTR-01 success criterion #1 phrasing ("operator pointed at any OTLP-compatible backend sees...") | Live-backend rendering is milestone-close proof (PROOF-01, Phase 47); envtest in-memory exporter is the phase-level proxy | Deferred to Phase 47 live proof; phase-level: assert exported spans via in-memory exporter |
| Module legitimacy human gate | ATTR-03 / T-42-SC | slopcheck [SLOP] verdict contradicted by Go registries — a human, not either tool, makes the final call | 42-01 Task 1 checkpoint: operator reviews proxy.golang.org + sum.golang.org output |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (checkpoint task exempt — blocking human gate)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references (every W0 item assigned to a creating task above)
- [x] No watch-mode flags
- [x] Feedback latency < 90s (quick loop ~1s; envtest focus runs bounded by -timeout)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
