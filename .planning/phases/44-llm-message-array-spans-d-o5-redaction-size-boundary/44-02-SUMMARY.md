---
phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary
plan: 02
subsystem: infra
tags: [kubernetes, jobspec, otlp, batch-span-processor, reporter, controller-runtime]

# Dependency graph
requires:
  - phase: 43-task-level-parity-trace-context-propagation
    provides: "ReporterOptions.TraceParent + the conditional --traceparent= Arg append in BuildReporterJob; spawnReporterIfNeeded's trailing traceParent param"
provides:
  - "ReporterOptions.OTLPEndpoint — reporter container gains OTEL_EXPORTER_OTLP_ENDPOINT + OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6 Env when the manager has an OTLP endpoint configured"
  - "ReporterOptions.TraceOnly/TraceOnlyJobKey — the trace-only Job shape (distinct 'tide-reporter-trace-<key>' name, minimal Args, no parent-CR flags) plan 44-05 will spawn for completed Task dispatch Jobs"
  - "PlannerReconcilerDeps.OTLPEndpoint wired from cmd/manager/main.go through all four planner-tier reporter spawn sites"
affects: [44-04-reporter-binary-message-spans, 44-05-task-level-trace-only-spawn, 45-adapter-seam]

# Tech tracking
tech-stack:
  added: []
  patterns: ["conditional Env append (append-only-when-configured, mirroring jobspec.go's subagentEnv house pattern)", "collision-free deterministic Job naming via a distinct name-key namespace ('tide-reporter-trace-' vs 'tide-reporter-')"]

key-files:
  created: []
  modified:
    - internal/controller/reporter_jobspec.go
    - internal/controller/reporter_jobspec_test.go
    - internal/controller/dispatch_helpers.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - cmd/manager/main.go

key-decisions:
  - "OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6 is a hardcoded literal in reporter_jobspec.go, not a Helm value — values.yaml is a FIXED contract; the constant is derived from RESEARCH's Size-Boundary Model (6 x 512 KiB whole-span cap = 3 MiB, ~25% headroom under the 4 MiB OTLP gRPC ceiling)"
  - "Trace-only Job name keys on the completed dispatch Job's UID (TraceOnlyJobKey) in a distinct 'tide-reporter-trace-' namespace, not the parent's UID — guarantees zero collision with the materialization reporter's 'tide-reporter-<parentUID>' name for the same parent"
  - "OTLPEndpoint travels as Env (not Args) — the only Env-based field on an otherwise 100% Args-based Job spec — because it targets the reporter's own TracerProvider bootstrap (os.Getenv), not a CLI flag"

patterns-established:
  - "Reporter Job shape selection via a boolean discriminator on ReporterOptions (TraceOnly) rather than a second builder function — keeps the shared PVC/SA/SecurityContext/label wiring in one place"

requirements-completed: [MSG-03]

# Metrics
duration: ~25min
completed: 2026-07-16
---

# Phase 44 Plan 02: Reporter Job-Spec Plumbing (OTLP Env + Trace-Only Shape) Summary

**BuildReporterJob gains a conditional OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6 + OTEL_EXPORTER_OTLP_ENDPOINT Env block (T-44-04's aggregate-batch DoS mitigation) and a collision-free trace-only Job shape, with the manager's OTLP endpoint forwarded through all four planner reporter spawn sites.**

## Performance

- **Duration:** ~25 min
- **Completed:** 2026-07-16
- **Tasks:** 2/2
- **Files modified:** 8

## Accomplishments
- `ReporterOptions.OTLPEndpoint` stamps exactly two container Env entries (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6`) only when the manager has an OTLP endpoint configured; zero Env entries otherwise (byte-identical to pre-Phase-44 behavior)
- `ReporterOptions.TraceOnly`/`TraceOnlyJobKey` add the trace-only Job shape 44-05 will spawn: name `tide-reporter-trace-<dispatchJobUID>`, Args `{--trace-only, --workspace=/workspace, --task-uid=<uid>}` with no parent-CR flags, everything else (SA, PVC subPath, SecurityContext, role label) shared with the materialization shape
- `PlannerReconcilerDeps.OTLPEndpoint` forwards the manager's own `OTEL_EXPORTER_OTLP_ENDPOINT` (captured once in `cmd/manager/main.go`, the same source `otelinit.NewTracerProvider` reads) through both `spawnReporterIfNeeded` call sites (Milestone, Phase) and both inline `ReporterOptions` literals (Plan, Project)

## Task Commits

Each task was committed atomically (Task 1 followed RED/GREEN TDD):

1. **Task 1 RED: failing tests for OTLPEndpoint env + trace-only shape** - `8212e05` (test)
2. **Task 1 GREEN: OTLPEndpoint env block + trace-only Job shape in BuildReporterJob** - `e6354d0` (feat)
3. **Task 2: forward OTLP endpoint through PlannerReconcilerDeps to all four spawn sites** - `b2c300b` (feat)

_Note: Task 1 (`tdd="true"`) has two commits (test → feat); no refactor commit was needed._

## Files Created/Modified
- `internal/controller/reporter_jobspec.go` - `ReporterOptions` gains `OTLPEndpoint`/`TraceOnly`/`TraceOnlyJobKey`; `BuildReporterJob` branches Args/name on `TraceOnly` and conditionally appends the two-entry Env block
- `internal/controller/reporter_jobspec_test.go` - 4 new tests: `TestBuildReporterJob_OTLPEndpointEnv`, `_NoOTLPEndpointNoEnv`, `_TraceOnly`, `_TraceOnlyFalseUnchanged`
- `internal/controller/dispatch_helpers.go` - `PlannerReconcilerDeps.OTLPEndpoint` field; `spawnReporterIfNeeded` gains a trailing `otlpEndpoint` param threaded into its internal `ReporterOptions` literal
- `internal/controller/milestone_controller.go` - `spawnReporterIfNeeded` call site passes `r.Deps.OTLPEndpoint`
- `internal/controller/phase_controller.go` - `spawnReporterIfNeeded` call site passes `r.Deps.OTLPEndpoint`
- `internal/controller/plan_controller.go` - inline `ReporterOptions` literal gains `OTLPEndpoint: r.Deps.OTLPEndpoint`
- `internal/controller/project_controller.go` - inline `ReporterOptions` literal gains `OTLPEndpoint: r.Deps.OTLPEndpoint`
- `cmd/manager/main.go` - captures `otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")` once (adjacent to the `otelinit.NewTracerProvider` call that reads the same var) and sets it on the `plannerDeps` literal

## Decisions Made
- `OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6` is a hardcoded literal, not a Helm value, per CLAUDE.md's fixed-contract rule for `values.yaml` — cited inline with the Size-Boundary math (6 x 512 KiB whole-span cap = 3 MiB, ~25% headroom under the 4 MiB OTLP gRPC ceiling)
- Trace-only Job naming keys on `TraceOnlyJobKey` (the completed dispatch Job's UID) in a distinct `tide-reporter-trace-` prefix namespace, not the parent's UID, so a failed-then-retried planner Job's trace-only spawn can never collide with or block a later materialization spawn for the same parent

## Deviations from Plan

None - plan executed exactly as written. One incidental note: the plan's acceptance-criteria grep `grep -c 'OTLPEndpoint: r.Deps.OTLPEndpoint' internal/controller/plan_controller.go internal/controller/project_controller.go` (single space after the colon) does not match verbatim because `gofmt`'s struct-literal column alignment inserts two spaces there (aligning with the longer `ReporterImage:`/`TraceParent:` field names in the same literal). Confirmed semantically equivalent via `grep -cE 'OTLPEndpoint: +r\.Deps\.OTLPEndpoint'` — both files return 1. No behavior difference; purely a gofmt whitespace artifact the plan's literal grep pattern didn't anticipate.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `ReporterOptions.OTLPEndpoint` and the trace-only shape are in place and unit-tested; plan 44-04 (reporter binary changes) and 44-05 (Task-level trace-only spawn site) can now build on top of these without further `reporter_jobspec.go` changes
- `go build ./internal/controller/... ./cmd/manager/...` exits 0; `go test ./internal/controller/ -run 'TestBuildReporterJob' -count=1` exits 0 (17 tests, all green); `make test-heavy` Ginkgo heavy suite 38/38 SUCCESS, 0 Failed; `make lint` 0 issues
- No blockers for 44-04/44-05

---
*Phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary*
*Completed: 2026-07-16*
