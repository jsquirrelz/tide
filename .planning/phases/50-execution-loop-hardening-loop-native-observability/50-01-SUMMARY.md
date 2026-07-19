---
phase: 50-execution-loop-hardening-loop-native-observability
plan: 01
subsystem: api
tags: [go, dispatch-envelope, wire-contract, fail-closed-enum, tdd]

# Dependency graph
requires:
  - phase: 49-common-loop-contract-verdict-envelope-persistence-schema
    provides: "GateDecision/Verdict fail-closed classifier pattern (pkg/dispatch/verdict.go) and the gate_decision_golden.json golden-fixture precedent this plan mirrors"
provides:
  - "TerminalReason closed 5-value enum on EnvelopeOut, fail-closed on the zero value (D-02)"
  - "RunEvidence bounded, references-only struct mapping the evals/README.md run-evidence contract 1:1 (D-03)"
  - "LoopRunID/AttemptID identity fields on both EnvelopeIn and EnvelopeOut (D-01)"
  - "TerminationStub extensions (TerminalReason, ChangedFileCount) flattened the same way GateDecision already is"
  - "envelope_out_golden.json — the shared Go+Python parity fixture for Plan 50-05"
  - "EXEC-04 negative guard (TestEnvelopeOut_NoCorrectnessField) pinning that no envelope field may assert Task correctness"
affects: [51-task-loop, 50-04-write-sites, 50-05-python-mirror]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Bare-return fail-closed enum (mirrors pkg/dispatch/verdict.go ClassifyVerdict) — TerminalReason.Valid() has no error return to forget"
    - "References-only evidence struct — RunEvidence holds only net-new fields, doc-comments the 1:1 mapping back to already-existing EnvelopeOut fields"
    - "Bounded() pre-marshal truncation control for DoS mitigation (T-50-01), mirroring NewTerminationStub's existing counts-only flattening discipline"

key-files:
  created:
    - pkg/dispatch/terminal_reason.go
    - pkg/dispatch/run_evidence.go
    - pkg/dispatch/terminal_reason_test.go
    - pkg/dispatch/testdata/envelope_out_golden.json
  modified:
    - pkg/dispatch/envelope.go
    - pkg/dispatch/envelope_test.go

key-decisions:
  - "Task 1's TDD test file (terminal_reason_test.go) was written before run_evidence.go/envelope.go existed, so the RED failure was a build error (undefined types/fields) rather than a runtime assertion failure — still a valid RED per the TDD discipline since the suite provably failed before the implementation."
  - "TestTerminalReason_ZeroValueIsInvalid and TestRunEvidence_BoundedTruncatesPathological were authored in Task 1 (not Task 2) so their names satisfy both tasks' verify -run patterns without duplication; Task 2 extended the same file rather than re-creating it."
  - "RunEvidence.Bounded() does not truncate EvaluatorVersions — the plan's action text enumerates exactly 5 fields + ChangedFiles + Commands for bounding, and EvaluatorVersions is Phase-51-populated (empty today), so no untested behavior was added beyond what the plan specified."

requirements-completed: [EXEC-01, EXEC-02, EXEC-03, EXEC-04]

# Metrics
duration: 14min
completed: 2026-07-19
---

# Phase 50 Plan 01: Envelope Schema Extension Summary

**TerminalReason fail-closed 5-value enum, bounded RunEvidence struct, and loopRunID/attemptID identity fields land on `pkg/dispatch`'s EnvelopeIn/EnvelopeOut/TerminationStub, backed by a shared Go+Python golden fixture and an EXEC-04 negative guard.**

## Performance

- **Duration:** 14 min
- **Started:** 2026-07-18T23:58:48-04:00 (first task commit)
- **Completed:** 2026-07-19T00:04:24-04:00
- **Tasks:** 2 (Task 1 TDD: 2 sub-commits; Task 2: 1 commit)
- **Files modified:** 6 (4 created, 2 modified)

## Accomplishments
- `TerminalReason` closed enum (`completed | cap_exceeded | blocked | tool_failure | invalid_output`) with a zero-value sentinel that is structurally invalid — `Valid()` rejects `""` and any unrecognized string, mirroring `ClassifyVerdict`'s never-collapses-to-APPROVED discipline.
- `RunEvidence`/`ChangedFile` — a references-only struct mapping the canonical `evals/README.md` run-evidence contract 1:1, with a doc comment enumerating exactly which fields are net-new vs. already sourced from `EnvelopeOut`. `Bounded()` truncates a pathological 500-file/50-command input to the 5 bounds consts while preserving `ChangedFileTotal` as the pre-truncation count (T-50-01 DoS mitigation).
- `LoopRunID`/`AttemptID` identity fields added to both `EnvelopeIn` and `EnvelopeOut`, round-tripping through JSON.
- `TerminationStub` extended with `TerminalReason` + `ChangedFileCount`, flattened by `NewTerminationStub` the same way `GateDecision` already is (counts/scalars only, never the full arrays) — proven to stay under the 4096-byte termination-message budget even with a maximally pathological `RunEvidence`.
- `envelope_out_golden.json` — the shared Go+Python parity fixture Plan 50-05's Python tests will read, deliberately pinning `terminalReason: "cap_exceeded"` (a non-`completed` value) so a silent-default bug in either language surfaces as a value mismatch.
- `TestEnvelopeOut_NoCorrectnessField` — the EXEC-04 negative guard: a compile-time exhaustive `EnvelopeOut{}` struct literal (adding a correctness field breaks compilation) plus a runtime JSON-key-absence check against `taskCorrect`/`correctness`/`verified`/`approved`/`passed`.

## Task Commits

Task 1 followed the TDD RED/GREEN cycle (the schema didn't exist yet, so RED was a build failure rather than a runtime assertion failure — the test file referenced `RunEvidence`, `ChangedFile`, and the new `EnvelopeOut` fields before they existed):

1. **Task 1 (RED): TerminalReason enum + RunEvidence struct + envelope field extensions** - `dd916dad` (test) — added `terminal_reason.go` (implemented ahead of the test per the type's simplicity) and `terminal_reason_test.go` with 3 failing tests; confirmed build failure before proceeding.
2. **Task 1 (GREEN): TerminalReason enum + RunEvidence struct + envelope field extensions** - `45dedc2f` (feat) — added `run_evidence.go`, extended `envelope.go` (`EnvelopeIn`/`EnvelopeOut`/`TerminationStub`/`NewTerminationStub`); all 3 tests pass, full `pkg/dispatch` suite green.
3. **Task 2: Golden fixture + guard tests** - `f5a162ba` (test) — added `envelope_out_golden.json`, `TestEnvelopeOut_GoldenFixtureRoundTrip`, extended `TestNewTerminationStub_StaysSmall`/`TestTerminationStub_NoForbiddenFields`, added `TestEnvelopeOut_NoCorrectnessField`.

_Note: Task 1 is `tdd="true"` — the RED commit's implementation file (`terminal_reason.go`) was written concurrently with the test file since `TerminalReason`'s enum shape is small enough to author in one pass; the test suite still provably failed to build until `run_evidence.go` + `envelope.go` landed in the GREEN commit, preserving the RED→GREEN discipline at the package level._

**Plan metadata:** pending (this SUMMARY's own commit)

## Files Created/Modified
- `pkg/dispatch/terminal_reason.go` - `TerminalReason` type, 5 consts, `Valid()` fail-closed classifier
- `pkg/dispatch/run_evidence.go` - `RunEvidence`/`ChangedFile` structs, 5 bounds consts, `Bounded()`
- `pkg/dispatch/envelope.go` - `EnvelopeIn` gains `LoopRunID`/`AttemptID`; `EnvelopeOut` gains `TerminalReason` (no omitempty), `LoopRunID`/`AttemptID`, `RunEvidence`; `TerminationStub` gains `TerminalReason`/`ChangedFileCount`; `NewTerminationStub` flattens both
- `pkg/dispatch/terminal_reason_test.go` - round-trip, `Valid()` table, `Bounded()` truncation, golden-fixture round-trip tests
- `pkg/dispatch/envelope_test.go` - extended `TestNewTerminationStub_StaysSmall`/`TestTerminationStub_NoForbiddenFields`; added `TestEnvelopeOut_NoCorrectnessField`
- `pkg/dispatch/testdata/envelope_out_golden.json` - shared Go+Python parity fixture

## Decisions Made
- `TerminalReasonCompleted`'s doc comment states the EXEC-04 belief-only scope explicitly on the const (not only the type), per the plan's Opus-literal-instruction-following guidance.
- `EnvelopeOut.TerminalReason` deliberately has NO `omitempty` — an unset reason must be visible as `""` on the wire so a silent-default bug is observable rather than hidden by JSON key omission; `LoopRunID`/`AttemptID`/`RunEvidence` all use `omitempty` since they're legitimately absent on non-Phase-50-aware or non-verify dispatches.
- `RunEvidence.Bounded()` bounds exactly the fields the plan's action text lists (`ChangedFiles`, `Commands`, `Model`/`PromptVersion`/`RuntimeVersion`/`SpecID`/`LockingCommit`) and leaves `EvaluatorVersions` untruncated — it's Phase-51-populated and empty today, and bounding it wasn't specified in the plan's `Bounded()` contract; revisit if Phase 51 populates it with untrusted-size data.
- Test naming: `TestTerminalReason_ZeroValueIsInvalid` and `TestRunEvidence_BoundedTruncatesPathological` were authored in Task 1 so they'd already exist (and pass) when Task 2's verify command runs them — avoided writing duplicate tests under Task 2's nominal "Create terminal_reason_test.go" action, which by the time Task 2 executed was more accurately "extend."

## Deviations from Plan

None functionally — plan executed as specified. One process note (not a deviation from scope/behavior, but from literal task sequencing) is documented in "Key Decisions" above: Task 1's `<files>` list didn't include a test file even though the task is `tdd="true"`, so `terminal_reason_test.go` was created as part of Task 1 (as the TDD protocol requires) rather than left to Task 2's `<action>` text alone, which nominally said "Create pkg/dispatch/terminal_reason_test.go." This is not a Rule 1/2/3/4 auto-fix — it's a sequencing clarification to satisfy both the plan's own `tdd="true"` requirement on Task 1 and Task 2's verify command (which references test names that had to exist for Task 1's own `-run` pattern to match anything).

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- The envelope schema Plan 50-02 (span attributes) and Plan 50-04 (write-site wiring) build on is locked: `TerminalReason`, `RunEvidence`, `LoopRunID`/`AttemptID` all exist, compile, and are guarded.
- `envelope_out_golden.json` is ready for Plan 50-05's Python mirror to validate against.
- Scope fence confirmed intact: `grep -rn "VerifyHalt" --include="*.go" pkg/` returns 0 hits; no `ConditionVerifyHalt`, verifier dispatch, or `evaluation.*` population was added.
- `make manifests` confirmed zero-diff (no CRD schema touched — this plan is `pkg/dispatch` only, no `api/v1alpha3` changes).
- No write sites were updated in this plan (that's Plan 50-04's scope) — `TerminalReason` is not yet set at any real executor exit path; the zero-value sentinel is currently what every real `EnvelopeOut{}` literal in `cmd/claude-subagent`, `internal/subagent/anthropic`, and `cmd/stub-subagent` would carry until 50-04 lands.

---
*Phase: 50-execution-loop-hardening-loop-native-observability*
*Completed: 2026-07-19*

## Self-Check: PASSED

All created files and task commit hashes verified present on disk / in `git log --oneline --all`.
