---
phase: 45
slug: runtime-neutral-adapter-seam
status: ready
nyquist_compliant: true
wave_0_complete: false
created: 2026-07-16
---

# Phase 45 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Populated from 45-RESEARCH.md §"Validation Architecture".

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` (plain `go test`) for `pkg/dispatch` and `cmd/tide-reporter`; Ginkgo v2.28 + Gomega + envtest for `internal/controller` (only if optional spawn-helper envtest coverage is added) |
| **Config file** | none dedicated — `go.mod` at repo root; envtest bootstrap in `internal/controller/suite_test.go` |
| **Quick run command** | `go test ./pkg/dispatch/... ./cmd/tide-reporter/... ./internal/reporter/... ./internal/controller/...` (no `KUBEBUILDER_ASSETS` needed — heavy Ginkgo specs skip under `-short`/default) |
| **Full suite command** | `make test` (unit tier incl. vet/fmt/manifests/generate prerequisites) |
| **Estimated runtime** | ~60 seconds (quick) / minutes (full) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./pkg/dispatch/... ./cmd/tide-reporter/... ./internal/reporter/... ./internal/controller/...`
- **After every plan wave:** Run `make test`
- **Before `/gsd:verify-work`:** `make test` green PLUS the no-branch source-inspection gate: `grep -rn "if.*[Vv]endor.*==" internal/controller cmd/tide-reporter internal/reporter` returns zero hits (the pre-existing `internal/subagent/anthropic/subagent.go` vendorSentinel fail-fast is a different, allowed pattern; `pkg/dispatch/vendor_capabilities.go` is the sole capability-data site)
- **Max feedback latency:** ~60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| (planner fills per-task rows; requirement→test map below is the source) | | | ADAPT-01 | T-45-01 | flag manager-authored only | unit | see map below | ❌ W0 | ⬜ pending |

**Requirement → test map (from RESEARCH.md):**

| Criterion | Behavior | Automated Command | File |
|-----------|----------|-------------------|------|
| ADAPT-01 c1 (flag-as-data) | `SelfInstruments` pure lookup; every vendor + unknown default false | `go test ./pkg/dispatch/... -run TestSelfInstruments -v` | ❌ W0 — new `vendor_capabilities_test.go` |
| ADAPT-01 c1 (flag threads both Job shapes) | `BuildReporterJob` emits the skip arg only when the new `ReporterOptions` field is true | `go test ./internal/controller/... -run TestBuildReporterJob_SkipMessageSpansArg -v` | ❌ W0 — extend `reporter_jobspec_test.go` |
| ADAPT-01 c1 (reporter parses flag) | `parseFlags` → `reporterConfig` field | `go test ./cmd/tide-reporter/... -run TestParseFlags -v` | ❌ W0 — extend `main_test.go` |
| ADAPT-01 c2 (reporter skips) | `synthesizeSpans` returns before `ReconstructConversation` when flag set; no sentinel written | `go test ./cmd/tide-reporter/... -run TestRunTraceOnly -v` (skip-specific case) | ❌ W0 — extend `main_test.go` |
| ADAPT-01 c2 inverse (default-safe) | Existing `TestRunTraceOnly_EmitsSpans` stays green UNMODIFIED (unset flag still synthesizes) | `go test ./cmd/tide-reporter/... -run TestRunTraceOnly_EmitsSpans -v` | ✅ existing, must stay green |
| ADAPT-01 c3 (contract test) | Stub self-instrumenting runtime emits 1 span; reporter with flag + real events.jsonl emits 0 more; exporter shows exactly 1 span with expected TraceID | `go test ./cmd/tide-reporter/... -run TestAdapterSeam -v` | ❌ W0 — new `adapter_seam_test.go` |
| Regression (Phase 44 byte-identical) | Full existing suite green with default flag | `make test` | ✅ existing suite |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `pkg/dispatch/vendor_capabilities_test.go` — ADAPT-01 criterion 1 (default polarity, D-10)
- [ ] `internal/controller/reporter_jobspec_test.go` addition — criterion 1 flag-threading (D-11)
- [ ] `cmd/tide-reporter/main_test.go` additions — `TestParseFlags*` extension + skip-when-flag-set case (criterion 2)
- [ ] `cmd/tide-reporter/adapter_seam_test.go` — criterion 3 contract test (D-09), the phase's headline proof
- [ ] Framework install: none — all frameworks present and pinned; zero new test tooling

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| No per-runtime branch outside the capability table | ADAPT-01 c1 | Source-shape property, not runtime behavior | `grep -rn "if.*[Vv]endor.*==" internal/controller cmd/tide-reporter internal/reporter` → zero hits (allowed exceptions: `pkg/dispatch/vendor_capabilities.go` data site; pre-existing `internal/subagent/anthropic/subagent.go` vendorSentinel fail-fast) |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 90s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
