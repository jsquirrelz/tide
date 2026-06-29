---
phase: 33
slug: d4-planner-failure-semantics
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-29
---

# Phase 33 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Ginkgo v2 + Gomega (envtest specs) + stdlib `testing` (unit) |
| **Config file** | none — suite boots via `TestControllers` in `internal/controller/suite_test.go` |
| **Quick run command** | `go test ./internal/controller/... -run "PLANFAIL\|TestIsPlannerFailure" -count=1` |
| **Full suite command** | `go test ./internal/controller/... ./cmd/tide/... -count=1` (Layer A, ~30–60s) |
| **Estimated runtime** | ~45 seconds (Layer A); `make test-int` for Layer A+B at phase gate |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/controller/... -run "PLANFAIL" -count=1`
- **After every plan wave:** Run `go test ./internal/controller/... ./cmd/tide/... -count=1`
- **Before `/gsd:verify-work`:** `make test-int` green (Layer A + Layer B)
- **Max feedback latency:** ~60 seconds

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| isPlannerFailure helper | 01 | 0 | — | — | N/A | unit | `go test ./internal/controller/... -run TestIsPlannerFailure -count=1` | ❌ W0 | ⬜ pending |
| PLANFAIL-01 (phase fail) | TBD | 1 | PLANFAIL-01 | — | N/A | envtest | `go test ./internal/controller/... -run "PLANFAIL-01" -count=1` | ❌ W0 | ⬜ pending |
| PLANFAIL-02 (milestone fail) | TBD | 1 | PLANFAIL-02 | — | N/A | envtest | `go test ./internal/controller/... -run "PLANFAIL-02" -count=1` | ❌ W0 | ⬜ pending |
| PLANFAIL-03 (leaf still succeeds) | TBD | 1 | PLANFAIL-03 | — | N/A | envtest | `go test ./internal/controller/... -run "PLANFAIL-03" -count=1` | ❌ W0 | ⬜ pending |
| PLANFAIL-04 (retry-failed recovery) | TBD | 1 | PLANFAIL-04 | — | N/A | unit (fake client) | `go test ./cmd/tide/... -run "TestResume.*PlannerFailed" -count=1` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/controller/planner_failure.go` — `isPlannerFailure(out, envReadOK) bool` helper + package doc (stub for the helper unit tests)
- [ ] `internal/controller/planner_failure_test.go` — 4-case pure unit table (PLANFAIL helper):
  - `(EnvelopeOut{ExitCode:1, ChildCount:0}, true)` → `true`
  - `(EnvelopeOut{ExitCode:0, ChildCount:0}, true)` → `false` (genuine leaf — PLANFAIL-03 invariant)
  - `(EnvelopeOut{ExitCode:1, ChildCount:0}, false)` → `false` (envelope unreadable)
  - `(EnvelopeOut{ExitCode:1, ChildCount:3}, true)` → `false` (children present)
- [ ] Envtest specs (phase): PLANFAIL-01 (`exitCode=1, childCount=0` → `Failed` + `ConditionFailed`/`ReasonPlannerFailed`) and PLANFAIL-03 (`exitCode=0, childCount=0` → `Succeeded`), via the `envReader.SetOut` + `makeFakeJobTerminal` injection pattern (`phase_controller_test.go` Test-5 shape)
- [ ] Envtest specs (milestone): PLANFAIL-02 + PLANFAIL-03 (same injection pattern at milestone level)
- [ ] `cmd/tide/resume_test.go` — extend `TestResumeRetryFailedAllFourKinds` (or new `TestResumePlannerFailed`): a `Failed` Phase/Milestone carrying `ReasonPlannerFailed` is reset by `resumeRun(retryFailed=true)` (`Status.Phase != "Failed"`)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| No controller retry-storm on a falsely-Failed parent | PLANFAIL-04 | Negative/timing property hard to assert deterministically in envtest | Optional kubectl smoke: apply a Project whose phase planner exits nonzero with zero children; observe `Phase.Status.Phase=Failed` and that no new planner Jobs spawn on a tail of the manager log. Covered structurally by the guard returning `ctrl.Result{}` (no error) — the unit/envtest assertions are the primary proof. |

*Primary recovery (PLANFAIL-04) IS automated via the resume_test.go extension; the row above is an optional belt-and-suspenders live check.*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
