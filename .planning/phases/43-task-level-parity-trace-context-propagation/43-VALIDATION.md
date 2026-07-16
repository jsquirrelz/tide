---
phase: 43
slug: task-level-parity-trace-context-propagation
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-07-16
---

# Phase 43 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Ginkgo v2.28 + Gomega (envtest, `Label("envtest","heavy")`) for handler-level tests; plain `testing.T` for pure-function tests |
| **Config file** | none dedicated — driven by `internal/controller/suite_test.go`'s existing `BeforeSuite`/envtest bootstrap (unchanged this phase) |
| **Quick run command** | `go test ./internal/controller/ -run 'TestSpanEndTime\|TestSynthesizePlannerSpan'` |
| **Full suite command** | `make test-heavy` (label-filtered `heavy` envtest specs) or `make test-int-fast` for the broader Layer A tier |
| **Estimated runtime** | ~20 minutes (`make test-heavy`) |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/controller/ -run 'TestSpanEndTime|TestSynthesizePlannerSpan'` (pure-function tier, seconds) plus a targeted `-ginkgo.focus` run for the level under active work.
- **After every plan wave:** Run `make test-heavy` (full `heavy`-labeled envtest tier).
- **Before `/gsd:verify-work`:** `make test-int-fast` (Layer A envtest, no kind needed — this phase touches no kind-tier fixtures) must be green.
- **Max feedback latency:** ~20 minutes (bounded by `make test-heavy`; per-task pure-function tier is seconds).

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 43-01-01 | 01 | 0 | PROP-02 | — | N/A | envtest | `go test ./... -run TestCRDTypes` (new field compiles + CRD manifests regen) | ❌ W0 | ⬜ pending |
| 43-0X-0X | TBD | TBD | TRACE-01 | — | N/A | envtest (Ginkgo) | `go test ./internal/controller/... -ginkgo.label-filter='heavy' -ginkgo.focus='SpanEmission — Task level'` | ❌ W0 | ⬜ pending |
| 43-0X-0X | TBD | TBD | TRACE-02 | — | N/A | envtest (Ginkgo) | same file, new `span.Parent.SpanID()` / `span.SpanContext.TraceID()` assertions per existing `Describe` block | ❌ W0 | ⬜ pending |
| 43-0X-0X | TBD | TBD | PROP-01 | — | N/A | unit | `go test ./internal/dispatch/podjob/... -run TestBuildJobSpec` / `go test ./internal/controller/... -run TestBuildReporterJob` | ❌ W0 | ⬜ pending |
| 43-0X-0X | TBD | TBD | PROP-02 | — | N/A | envtest (Ginkgo) | same `span_emission_test.go` specs — assert on the re-fetched CRD's status field | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

*Task IDs/plans/waves are TBD — this phase has no plans yet; the planner assigns exact task IDs. The requirement/test-command mapping above is locked from RESEARCH.md and must be preserved when the planner fills in task IDs.*

---

## Wave 0 Requirements

- [ ] `internal/controller/span_emission_test.go` — new `Describe("SpanEmission — Task level", ...)` block, mirroring the four existing ones (same fixture helpers: `succeededPlannerJob`/`failedPlannerJob`-equivalent, `mapEnvReader`, WR-04 TracerProvider capture-before-failable-step ordering)
- [ ] New parent-linkage assertions added to ALL FIVE existing/new `Describe` blocks (`span.Parent.SpanID()` checks) — genuinely new test surface, not just a Task addition
- [ ] `api/v1alpha3/*_types.go` CRD field additions (`{Level}TraceSpanID`, `TaskSpanEmittedUID`) + `make manifests generate` regen — a prerequisite for the PROP-02 status-field assertions to even compile
- [ ] Confirm exact existing test file names for `jobspec.go`/`reporter_jobspec.go` unit coverage during planning (grep `internal/dispatch/podjob/*_test.go` and `internal/controller/reporter_jobspec*_test.go` at plan time)

---

## Manual-Only Verifications

*None — all phase behaviors have automated verification per RESEARCH.md's Phase Requirements → Test Map. Cross-pod clock skew (multi-node only) and the live end-to-end Phoenix trace tree are explicitly deferred to Phase 47's proof, not this phase's scope.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 1200s (`make test-heavy` full-suite ceiling)
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
