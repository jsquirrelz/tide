---
phase: 43-task-level-parity-trace-context-propagation
plan: 02
subsystem: observability
tags: [opentelemetry, traceparent, w3c-trace-context, k8s-jobs, reporter, tide-reporter]

# Dependency graph
requires:
  - phase: 42-trace-context-foundation-planner-level-span-emission
    provides: pkg/otelai/tracecontext.go primitives (TraceIDFromUID, FormatTraceparent, ExtractRemoteParent) — zero production call sites until this phase
provides:
  - podjob.BuildOptions.TraceParent + conditional TRACEPARENT env on the subagent container in BuildJobSpec (all five dispatch levels)
  - controller.ReporterOptions.TraceParent + conditional --traceparent Arg on the reporter container in BuildReporterJob (four existing reporter Jobs)
  - cmd/tide-reporter parseFlags(args) (reporterConfig, error) extraction with the --traceparent flag registered in the same commit as the Arg emission
affects: [43-01, 43-03, 43-04, 43-05]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Inert-today, load-bearing-later carrier: add the field + conditional env/arg append with zero consumers this phase; the value threads through starting a later plan"
    - "Reporter Job traceparent transport is Args, not Env (departs from PROP-01's literal 'env' wording, scoped correctly per Pitfall 3 — reporter_jobspec.go is 100% Args-based)"
    - "flag.ContinueOnError extraction (parseFlags) to make CLI flag registration unit-testable without process exit"

key-files:
  created: []
  modified:
    - internal/dispatch/podjob/jobspec.go
    - internal/dispatch/podjob/jobspec_test.go
    - internal/controller/reporter_jobspec.go
    - internal/controller/reporter_jobspec_test.go
    - cmd/tide-reporter/main.go
    - cmd/tide-reporter/main_test.go

key-decisions:
  - "Reporter Job's traceparent carrier is a --traceparent Arg, not a corev1.EnvVar — reporter_jobspec.go sets zero Env entries today and is 100% Args-based via stdlib flag. PROP-01/D-05's 'env' wording is informal shorthand for 'injected as data'; D-06's mirror-the-credproxy-env pattern is correctly scoped to jobspec.go only (RESEARCH.md Pitfall 3)."
  - "cmd/tide-reporter/main.go's flag block extracted into parseFlags(args []string) (reporterConfig, error) using flag.ContinueOnError (was flag.ExitOnError inline in main()), so the 7th --traceparent registration is unit-testable and the crash-on-unknown-flag contract is regression-tested rather than just inline dead code under ExitOnError (RESEARCH.md Pitfall 4)."

patterns-established:
  - "New traceparent-carrying fields land alongside their nearest structural analog (TraceParent next to PricingOverridesJSON in BuildOptions) and reuse that field's exact conditional-append shape — not the credproxy feature-bundle shape."

requirements-completed: [PROP-01]

# Metrics
duration: 20min
completed: 2026-07-16
---

# Phase 43 Plan 02: Traceparent Carriers on Both Job Builders Summary

**Added `TraceParent` fields to `podjob.BuildOptions` and `controller.ReporterOptions` with conditional TRACEPARENT env / `--traceparent` Arg injection, plus the matching `cmd/tide-reporter` flag registration — zero behavior change when empty, both carriers unit-tested.**

## Performance

- **Duration:** ~20 min
- **Completed:** 2026-07-16
- **Tasks:** 2
- **Files modified:** 6

## Accomplishments

- `podjob.BuildOptions.TraceParent` conditionally stamps `TRACEPARENT` on the subagent container in `BuildJobSpec`, mirroring the existing `PricingOverridesJSON` append shape — applies uniformly to all five dispatch levels since they funnel through one function.
- `controller.ReporterOptions.TraceParent` conditionally appends `--traceparent=<value>` to the reporter container's `Args` in `BuildReporterJob` — Args, not Env, per the file's existing 100%-Args convention (Pitfall 3 departure from PROP-01's literal "env" wording, correctly scoped).
- `cmd/tide-reporter/main.go`'s flag parsing extracted into a testable `parseFlags(args []string) (reporterConfig, error)` (via `flag.ContinueOnError`), registering `--traceparent` in the same commit `BuildReporterJob` starts emitting the Arg — an unknown-flag crash-loop is now structurally impossible and regression-tested (Pitfall 4).
- Six new unit tests prove presence-when-set and absence-when-empty for both carriers, plus the flag-parse contract.

## Task Commits

Each task was committed atomically:

1. **Task 1: BuildOptions.TraceParent + conditional TRACEPARENT env in BuildJobSpec** - `70bbdc5` (feat)
2. **Task 2: ReporterOptions.TraceParent + --traceparent Arg + flag registration** - `40de10c` (feat)

**Plan metadata:** committed together with this SUMMARY.md (see below)

## Files Created/Modified

- `internal/dispatch/podjob/jobspec.go` - `BuildOptions.TraceParent` field + conditional `TRACEPARENT` env append in `BuildJobSpec`, mirroring the `PricingOverridesJSON` block
- `internal/dispatch/podjob/jobspec_test.go` - `TestBuildJobSpec_TraceparentEnvPresentWhenSet` / `TestBuildJobSpec_TraceparentEnvAbsentWhenEmpty` (executor + planner Kinds, table-driven like the `PricingOverridesJSON` tests)
- `internal/controller/reporter_jobspec.go` - `ReporterOptions.TraceParent` field + conditional `--traceparent=<value>` Arg append in `BuildReporterJob`
- `internal/controller/reporter_jobspec_test.go` - `TestBuildReporterJob_TraceparentArg` (present-when-set + absent-when-empty subtests)
- `cmd/tide-reporter/main.go` - flag block extracted into `parseFlags(args []string) (reporterConfig, error)` using `flag.NewFlagSet(..., flag.ContinueOnError)`; new `--traceparent` flag registered; `reporterConfig.TraceParent` field added; `main()` now calls `parseFlags(os.Args[1:])` and handles the returned error
- `cmd/tide-reporter/main_test.go` - `TestParseFlagsTraceparent` (accepts `--traceparent` and returns it verbatim) + `TestParseFlagsUnknownFlagErrors` (proves the crash-on-unknown-flag contract survived the `ExitOnError` → `ContinueOnError` extraction)

## Decisions Made

- **Args, not Env, for the reporter's traceparent transport** (Pitfall 3): `reporter_jobspec.go` sets zero `Env` entries today and is 100% Args-based via stdlib `flag`. PROP-01/D-05's "env" wording is informal shorthand for "injected as data"; the literal env-var mechanism (D-06, mirroring the credproxy pattern) is correctly scoped to `jobspec.go` only. Verified post-commit: `grep -c 'Env:' internal/controller/reporter_jobspec.go` returns `0`.
- **`flag.ContinueOnError` extraction, not a wrapper around the inline `flag.ExitOnError` block**: extracting to `parseFlags` makes the 7th flag registration unit-testable without a process exit, and turns the previously-dead `if err != nil { ... os.Exit(exitInvariant) }` branch in `main()` into the live, testable error handler for unknown flags (Pitfall 4).

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

`go build ./...` fails on the unrelated `cmd/tide-demo-init` package (`pattern all:fixture: no matching files found` — its `fixture/` directory is positioned at build time per its own doc comment and is absent from this worktree; confirmed via `git log --all -- cmd/tide-demo-init/fixture` that it has never been a tracked path). This is a pre-existing environmental gap, not touched by this plan's changes (`cmd/tide-demo-init` neither imports nor is imported by any file this plan modified). Per the executor's scope boundary rule this was left alone; all plan-scoped verification (`go build ./internal/controller/... ./cmd/tide-reporter/... ./internal/dispatch/podjob/...` and `go vet` on the same set) is clean.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Both pod hops can now carry a traceparent as inert data: `podjob.BuildOptions.TraceParent` (all five dispatch levels) and `controller.ReporterOptions.TraceParent` (the four existing reporter Jobs). Empty value produces byte-identical Job specs to today (verified by the `AbsentWhenEmpty` tests).
- `cmd/tide-reporter` accepts `--traceparent` and still hard-fails on unregistered flags — the reporter crash-loop failure mode (Pitfall 4) is structurally impossible and regression-tested.
- Nothing in this plan wires a *real* traceparent value in — that's 43-01 (span_emission.go parenting retrofit) supplying the value and 43-03/04/05 (controller call sites) threading it through `BuildOptions.TraceParent` / `spawnReporterIfNeeded` → `ReporterOptions.TraceParent`. This plan only makes the carriers exist and proves they're inert when empty.
- No blockers.

## Self-Check: PASSED

- `internal/dispatch/podjob/jobspec.go` — FOUND
- `internal/dispatch/podjob/jobspec_test.go` — FOUND
- `internal/controller/reporter_jobspec.go` — FOUND
- `internal/controller/reporter_jobspec_test.go` — FOUND
- `cmd/tide-reporter/main.go` — FOUND
- `cmd/tide-reporter/main_test.go` — FOUND
- `70bbdc5` — FOUND (`git log --oneline --all | grep 70bbdc5`)
- `40de10c` — FOUND (`git log --oneline --all | grep 40de10c`)
- `go test ./internal/dispatch/podjob/ -run 'TestBuildJobSpec' -count=1` — PASS
- `go test ./internal/controller/ -run 'TestBuildReporterJob' -count=1` — PASS
- `go test ./cmd/tide-reporter/ -count=1` — PASS (8/8, including the 2 new)
- `grep -n 'Name:  "TRACEPARENT"' internal/dispatch/podjob/jobspec.go` — exactly 1 hit, inside `if opts.TraceParent != ""`
- `grep -c 'fs.String("traceparent"' cmd/tide-reporter/main.go` — 1
- `grep -c 'Env:' internal/controller/reporter_jobspec.go` — 0
- `git show --stat 40de10c` — lists both `internal/controller/reporter_jobspec.go` and `cmd/tide-reporter/main.go`

---
*Phase: 43-task-level-parity-trace-context-propagation*
*Completed: 2026-07-16*
