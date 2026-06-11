---
phase: 09-cross-namespace-envelope-return-in-namespace-reporter
plan: 02
subsystem: dispatch
tags: [termination-message, envelope, stub-subagent, source-path, cross-namespace]

# Dependency graph
requires:
  - phase: 09-cross-namespace-envelope-return-in-namespace-reporter
    provides: "Phase 09 research + patterns + CONTEXT decision (Option C: in-namespace reporter)"
provides:
  - "pkg/dispatch.TerminationStub + NewTerminationStub (<4KB tiny-status carrier)"
  - "Both subagent shims write TerminationStub to termination-log (not full EnvelopeOut)"
  - "Stub Task ChildCRDSpec.SourcePath = children/stub-task-1.json (materializer MinLength=1 satisfied)"
affects:
  - 09-03-reporter-namespace-job
  - 09-04-manager-reads-termination-stub
  - internal/controller/dispatch_helpers (materializer uses SourcePath)

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "TerminationStub as cross-namespace tiny-status carrier: ExitCode+Reason+Usage+HeadSHA only, never ChildCRDs or Result"
    - "out.json PVC = full audit artifact + reporter input; termination-log = <4KB stub only"
    - "children/<name>.json convention for stub prompt artifacts (mirrors real runner relPrefix)"

key-files:
  created:
    - pkg/dispatch/envelope_test.go (TestNewTerminationStub_StaysSmall + subset assertions)
  modified:
    - pkg/dispatch/envelope.go (TerminationStub struct + NewTerminationStub)
    - cmd/claude-subagent/main.go (writeEnvelope marshals NewTerminationStub to termination-log)
    - cmd/stub-subagent/main.go (writeTerminationMessage takes stub; dispatchPlannerSuccess stamps SourcePath)
    - cmd/stub-subagent/main_test.go (TestWriteEnvelopeAlsoWritesTerminationMessage + TestPlannerSuccess_TaskChildHasSourcePath)

key-decisions:
  - "TerminationStub flattens Git.HeadSHA directly (no nested GitOutput sub-struct) — Manager only needs the SHA"
  - "Stub writes children/stub-task-1.json artifact and sets SourcePath = children/stub-task-1.json to mirror the real runner's relPrefix convention (anthropic/subagent.go:427)"
  - "writeTerminationMessage refactored to accept TerminationStub (not EnvelopeOut) so the type enforces exclusion of ChildCRDs/Result at compile time"

patterns-established:
  - "TerminationStub pattern: split cross-namespace return — tiny status via Pod termination message, verbose data via PVC out.json"
  - "children/<name>.json prompt artifact convention used by both real runner and stub for Task.Spec.PromptPath wiring"

requirements-completed: [REQ-09-03]

# Metrics
duration: 30min
completed: 2026-06-08
---

# Phase 09 Plan 02: Cross-Namespace Tiny-Status Split Summary

**TerminationStub (<4KB) carries ExitCode/Reason/Usage/HeadSHA via Pod termination message; full EnvelopeOut (ChildCRDs + result) stays on namespace PVC; stub Task children carry SourcePath so the reporter can stamp Task.Spec.PromptPath (MinLength=1)**

## Performance

- **Duration:** ~30 min
- **Started:** 2026-06-08T00:00:00Z
- **Completed:** 2026-06-08T00:30:00Z
- **Tasks:** 3
- **Files modified:** 5

## Accomplishments

- Added `TerminationStub` struct and `NewTerminationStub(EnvelopeOut) TerminationStub` to `pkg/dispatch/envelope.go` — provably <4KB by construction (excludes ChildCRDs, Result, Artifacts); covered by `TestNewTerminationStub_StaysSmall`
- Both `cmd/claude-subagent` and `cmd/stub-subagent` now marshal `NewTerminationStub(out)` to the termination-log path, while retaining the full `out.json` on the PVC as the audit artifact and reporter input
- `cmd/stub-subagent` `dispatchPlannerSuccess` (plan-level) writes `children/stub-task-1.json` and stamps `SourcePath = "children/stub-task-1.json"` on the Task ChildCRDSpec, satisfying the materializer's `Task.Spec.PromptPath = child.SourcePath` (MinLength=1 validation — defect #10b / T-09-04)

## Task Commits

Each task was committed atomically:

1. **Task 1: TerminationStub type + NewTerminationStub + <4KB test (RED)** - `e60f6a4` (test)
2. **Task 1: TerminationStub type + NewTerminationStub + <4KB test (GREEN)** - `588149d` (feat)
3. **Task 2: Both subagent shims write the tiny stub** - `d854e58` (feat)
4. **Task 3: Stub sets SourcePath on Task children** - `2d65892` (feat)

_Task 1 used TDD RED/GREEN pattern._

## Files Created/Modified

- `pkg/dispatch/envelope.go` - Added `TerminationStub` struct and `NewTerminationStub` constructor
- `pkg/dispatch/envelope_test.go` - Created with `TestNewTerminationStub_StaysSmall` + subset-field assertions
- `cmd/claude-subagent/main.go` - `writeEnvelope` now marshals `NewTerminationStub(out)` to termination-log
- `cmd/stub-subagent/main.go` - `writeTerminationMessage` takes `TerminationStub`; plan-case writes children file + stamps SourcePath
- `cmd/stub-subagent/main_test.go` - Added `TestWriteEnvelopeAlsoWritesTerminationMessage` + `TestPlannerSuccess_TaskChildHasSourcePath`

## Decisions Made

- **TerminationStub flattens HeadSHA:** Rather than nesting a `GitOutput` sub-struct, `HeadSHA string` is a direct field on `TerminationStub`. The Manager only needs the SHA; flattening keeps the type minimal and the <4KB invariant easier to reason about.
- **writeTerminationMessage refactored to take TerminationStub:** The function signature now takes `TerminationStub` directly (not `EnvelopeOut`), so the Go compiler enforces that no ChildCRDs or Result can leak into the termination message.
- **Stub uses children/<name>.json convention:** The stub mirrors `anthropic/subagent.go`'s `relPrefix = "children/"` exactly — writes the task spec JSON to `<workspaceRoot>/children/stub-task-1.json` and sets `SourcePath = "children/stub-task-1.json"`. This means the reporter (plan 09-05) can use an identical read path for both real and stub executions.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

- `go build ./cmd/...` fails on `cmd/tide-demo-init` due to a pre-existing missing `fixture` embed glob (unrelated to this plan). All plan-relevant commands (`cmd/stub-subagent`, `cmd/claude-subagent`) build cleanly.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `TerminationStub` is ready for plan 09-04 (Manager reads stub from PodStatus termination message)
- Stub SourcePath convention is ready for plan 09-05 (reporter creates Task CRs via API, reading PromptPath from SourcePath)
- `out.json` full-envelope PVC write is unchanged; plan 09-05 reporter reads it directly

---
*Phase: 09-cross-namespace-envelope-return-in-namespace-reporter*
*Completed: 2026-06-08*
