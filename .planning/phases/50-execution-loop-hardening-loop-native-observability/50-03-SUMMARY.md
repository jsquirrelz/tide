---
phase: 50-execution-loop-hardening-loop-native-observability
plan: 03
subsystem: observability
tags: [go/analysis, prometheus, cardinality-guard, static-analyzer, OBS-02, D-06]

# Dependency graph
requires:
  - phase: 50-02
    provides: loop.*/evaluation.*/human_intervention otelai span attribute helpers (the future consumer these labels must never leak into)
provides:
  - metriccardinality analyzer forbiddenLabels set grown from 1 ("task") to 9 (task, run_id, loop_run_id, run, attempt, attempt_id, trace_id, task_uid, uid)
  - per-label badlabels fixtures (8 new violations, spread across all four prometheus.New*Vec constructors)
  - goodlabels positive control proving bounded enum labels (terminal_reason, exit_reason, loop_kind, evaluator_type, risk_tier) are never rejected
  - internal/metrics/wave_label_test.go runtime source-grep guard extended to the same 9-name list, guarding internal/metrics/registry.go
  - explicit no-new-metric decision documented beside telem03labelChecks (RESEARCH Open Question 3 resolved)
affects: [51-task-loop, 52-loop-level-parameterization]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Dual cardinality guard: go/analysis static analyzer (go-vet-time) + a hand-synced runtime source-grep test — the same discipline first proven for the single 'task' label now generalized to a locked 9-name set"

key-files:
  created: []
  modified:
    - tools/analyzers/metriccardinality/analyzer.go
    - tools/analyzers/metriccardinality/analyzer_test.go
    - tools/analyzers/metriccardinality/testdata/src/badlabels/registry.go
    - tools/analyzers/metriccardinality/testdata/src/goodlabels/registry.go
    - internal/metrics/wave_label_test.go

key-decisions:
  - "Phase 50 adds NO new Prometheus metric (RESEARCH Open Question 3 resolved: guard-hardening only; loop-outcome metrics wait for Phase 51's real EvaluationSummary.Decision/LoopStatus.ExitReason consumer)"
  - "The two forbidden-label lists (analyzer.go's map, wave_label_test.go's slice) are intentionally NOT shared via import — kept in sync by hand with cross-referencing doc comments, so a bug in one guard layer cannot silently disable the other"

patterns-established:
  - "Set-membership rejection (map[string]struct{}) generalizes a single-literal go/analysis check without touching the AST-walk scaffolding — vecConstructors/isStringSliceType stay untouched"

requirements-completed: [OBS-02]

# Metrics
duration: 7min
completed: 2026-07-19
---

# Phase 50 Plan 03: Prometheus Cardinality Guard Hardening Summary

**Extended the dual OBS-02/D-06 cardinality guard (go/analysis static analyzer + runtime source-grep) from a single hardcoded `"task"` literal to the full 9-name run-ID-shaped forbidden set, with per-label fixture proof and positive controls for bounded enum labels — no new Prometheus metric added.**

## Performance

- **Duration:** ~7 min
- **Started:** 2026-07-19T00:14:39-04:00
- **Completed:** 2026-07-19T00:21:06-04:00
- **Tasks:** 2 completed
- **Files modified:** 5

## Accomplishments
- `tools/analyzers/metriccardinality/analyzer.go`'s single `unquoted == "task"` comparison replaced with a `forbiddenLabels` set-membership check covering all 9 D-06 names; diagnostic now names whichever label matched and cites OBS-02/D-06 alongside the original Pitfall 17 reference.
- `testdata/src/badlabels/` gained 8 new violation fixtures (one per new forbidden label), spread two-per-constructor across `NewCounterVec`/`NewHistogramVec`/`NewGaugeVec`/`NewSummaryVec` so set-membership is proven per constructor, not just for `"task"`.
- `testdata/src/goodlabels/` gained a positive-control fixture proving `terminal_reason`, `exit_reason`, `loop_kind`, `evaluator_type`, `risk_tier` never trip the guard (T-50-07 mitigation).
- `internal/metrics/wave_label_test.go`'s `"registry.go carries no task label"` subtest generalized to loop over the same 9-name list, with an explicit doc comment stating the two lists (analyzer + runtime grep) are kept in sync by hand and exist independently by design.
- `telem03labelChecks` table left byte-for-byte structurally unchanged (verified via `git diff`), with a new comment documenting the Phase 50 no-new-metric decision.
- Confirmed via `go run ./cmd/tide-lint ./...` (which wires the extended `metriccardinality.Analyzer`) that the extended forbidden-label set produces zero diagnostics against the real codebase — no existing label trips the guard.

## Task Commits

1. **Task 1: Extend the analyzer's forbidden-label set + testdata fixtures** - `3e5d32e3` (feat)
2. **Task 2: Extend the runtime source-grep guard in wave_label_test.go** - `a3db98e6` (test)

_No plan-metadata commit issued separately — this SUMMARY commit serves that role per the sequential-executor protocol._

## Files Created/Modified
- `tools/analyzers/metriccardinality/analyzer.go` - `forbiddenLabels` set (9 names) replaces the single `"task"` literal comparison; generalized `pass.Reportf` diagnostic; updated `Analyzer.Doc`
- `tools/analyzers/metriccardinality/analyzer_test.go` - doc comment describes the generalized 9-label set and the goodlabels positive controls
- `tools/analyzers/metriccardinality/testdata/src/badlabels/registry.go` - 8 new `prometheus.New*Vec` fixtures (one per new forbidden label) with matching `// want` directives
- `tools/analyzers/metriccardinality/testdata/src/goodlabels/registry.go` - `OkLoopMetric` fixture proving bounded enum labels are never rejected
- `internal/metrics/wave_label_test.go` - `"registry.go carries none of the D-06 forbidden labels"` subtest (loops over the 9-name list); no-new-metric decision comment beside `telem03labelChecks`

## Decisions Made
- **No new Prometheus metric this phase.** Per RESEARCH Open Question 3 and the plan's explicit scope fence, run-detail belongs in traces (LOOP-03); the first loop-scoped bounded-label metric waits for Phase 51's real consumer. Verified via `git diff` that `telem03labelChecks` gained zero new entries and `grep -c 'prometheus.New' internal/metrics/registry.go` is unchanged (16, same as before this plan).
- **Kept the two forbidden-label lists un-shared (no common import).** The analyzer's `forbiddenLabels` map and the test's `forbiddenRuntimeLabels` slice are independent, hand-synced lists per the plan's explicit instruction — a bug in one guard layer (e.g. an accidental import cycle or test-only build tag) cannot silently disable the other.

## Deviations from Plan

None — plan executed exactly as written. One documentation follow-up: while running `make lint` (specified in the plan's `<verification>` block), 2 pre-existing `lll` (line-too-long) violations were found in files outside this plan's scope (`pkg/dispatch/terminal_reason.go:63` from Plan 50-01's commit `dd916dad8`, `pkg/otelai/attrs.go:441` from Plan 50-02's commit `551f23ab5`). Per the scope-boundary rule (only auto-fix issues directly caused by the current task's changes), these were **not** fixed here — logged to `.planning/phases/50-execution-loop-hardening-loop-native-observability/deferred-items.md` instead. Isolated confirmation that this plan's own guard is clean: `go run ./cmd/tide-lint ./...` (the metriccardinality multichecker) exits 0 with zero diagnostics against the whole tree.

## Issues Encountered
- `analysistest` initially failed to parse `testdata/src/goodlabels/registry.go` ("literal not terminated") because a prose comment contained the literal substring `// want`, which the fixture-comment scanner mis-parsed as a diagnostic-expectation directive. Fixed by rewording the comment to avoid the `// want` token; re-ran `go test ./tools/analyzers/metriccardinality/... -v` to confirm green.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Both guard layers (go-vet-time static analyzer + test-time runtime source-grep) now reject the full D-06 run-ID-shaped label set, closing the ROADMAP criterion #5 label-cardinality proof for Phase 50.
- `internal/metrics/registry.go` remains guard-clean; no Prometheus metric anywhere in the tree carries a run-ID-shaped label.
- Phase 51's `EVALUATOR`-kind span work and any future loop-scoped bounded-label metric will trip this analyzer immediately if a run/attempt/trace ID is accidentally routed to a label — the guard is proactive, not just documentary.
- One pre-existing, out-of-scope `lll` lint gap remains open (see Deviations above / `deferred-items.md`) — does not block this plan or Phase 50's own success criteria, but should be swept up by a future plan touching either file.

---
*Phase: 50-execution-loop-hardening-loop-native-observability*
*Completed: 2026-07-19*

## Self-Check: PASSED

All 7 claimed files verified present on disk; all 3 claimed commit hashes
(`3e5d32e3`, `a3db98e6`, `c7194f12`) verified present in `git log --oneline --all`.
