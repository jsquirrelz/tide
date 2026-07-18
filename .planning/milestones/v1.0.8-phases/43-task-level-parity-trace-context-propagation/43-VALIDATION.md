---
phase: 43
slug: task-level-parity-trace-context-propagation
status: approved
nyquist_compliant: true
wave_0_complete: true
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
| 43-01-01 | 01 | 1 | PROP-02 | — | N/A | build + source | `go build ./api/...` + field greps (six new status fields compile) | ✅ types files exist | ⬜ pending |
| 43-01-02 | 01 | 1 | PROP-02 | — | N/A | manifests | `make manifests generate && git diff --exit-code config/crd/bases/` (idempotent regen, new JSON props present) | ✅ | ⬜ pending |
| 43-02-01 | 02 | 1 | PROP-01 | T-43-03 | manager-authored value only | unit | `go test ./internal/dispatch/podjob/ -run TestBuildJobSpec -count=1` | ✅ jobspec_test.go (assertions added same task) | ⬜ pending |
| 43-02-02 | 02 | 1 | PROP-01 | T-43-04 | unknown-flag crash contract preserved | unit | `go test ./internal/controller/ -run TestBuildReporterJob -count=1` + `go test ./cmd/tide-reporter/ -count=1` | ✅ reporter_jobspec_test.go / main_test.go (assertions added same task) | ⬜ pending |
| 43-03-01 | 03 | 2 | TRACE-02 | T-43-07 | TraceIDFromUID error → skip emission | unit | `go test ./internal/controller/ -run 'TestSynthesizePlannerSpan\|TestSpanIDFromHexOrZero\|TestTraceparentForLevel' -count=1` | ✅ span_emission_unit_test.go (updated same task) | ⬜ pending |
| 43-03-02 | 03 | 2 | TRACE-02, PROP-02 | — | N/A | envtest (Ginkgo) | `make test-heavy` — new `span.Parent.SpanID()` / `span.SpanContext.TraceID()` / re-fetched `.status.{Level}TraceSpanID` assertions in all four planner `Describe` blocks | ✅ span_emission_test.go (assertions added same task) | ⬜ pending |
| 43-04-01 | 04 | 3 | PROP-01 | T-43-08 | parent-status-sourced env | build + unit | `go build ./...` + `go test ./internal/controller/ -run TestTraceparentForLevel -count=1` | ✅ | ⬜ pending |
| 43-04-02 | 04 | 3 | PROP-01 | T-43-08 | own-span reporter Args | build | `go build ./...` + `go vet ./internal/controller/` | ✅ | ⬜ pending |
| 43-04-03 | 04 | 3 | PROP-01 | — | N/A | envtest (Ginkgo) | `make test-heavy` — new `dispatch_traceparent_test.go`: full W3C string on dispatch Job env + reporter Job Args | ❌ W0 — file created in this task | ⬜ pending |
| 43-05-01 | 05 | 3 | TRACE-01 | T-43-10/T-43-11 | envelope data → attributes only, never SpanContext | build + lint | `go build ./...` + `golangci-lint run internal/controller/task_controller.go` | ✅ | ⬜ pending |
| 43-05-02 | 05 | 3 | TRACE-01, TRACE-02, PROP-01, PROP-02 | — | N/A | envtest (Ginkgo) | `make test-heavy` — `-ginkgo.focus='SpanEmission — Task level'` block + `task_dispatch_traceparent_test.go`; phase gate `make test-int-fast` | ❌ W0 — Describe block + file created in this task | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

*Task IDs assigned by the planner 2026-07-16 (plans 43-01…43-05). The requirement/test-command mapping locked from RESEARCH.md is preserved: TRACE-01 → `SpanEmission — Task level` focus; TRACE-02 → parent/TraceID assertions per Describe block; PROP-01 → TestBuildJobSpec/TestBuildReporterJob (unit) + controller-level envtest; PROP-02 → re-fetched CRD status assertions. Wave 0 test scaffolds land in the same plan/task as their implementation (test-with-change), so no standalone Wave 0 plan exists.*

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

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 1200s (`make test-heavy` full-suite ceiling)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-07-16 (gsd-plan-checker verification: 0 blockers, 5 non-blocking hygiene warnings — see 43-01..43-05 PLAN.md for detail)
