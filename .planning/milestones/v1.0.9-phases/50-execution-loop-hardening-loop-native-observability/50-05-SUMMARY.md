---
phase: 50-execution-loop-hardening-loop-native-observability
plan: 05
subsystem: langgraph-verifier
tags: [python, dispatch-envelope, wire-contract, go-python-duality, tdd]

# Dependency graph
requires:
  - phase: 50-execution-loop-hardening-loop-native-observability
    provides: "TerminalReason enum, RunEvidence struct, LoopRunID/AttemptID fields on EnvelopeOut/TerminationStub, and the shared envelope_out_golden.json fixture (Plan 50-01)"
provides:
  - "Python mirror of TerminalReason/RunEvidence/loopRunID/attemptID on write_envelope_out + write_termination_stub"
  - "ENVELOPE_OUT_GOLDEN_FIXTURE path constant, reusing verdict.py's _repo_root idiom"
  - "Go+Python cross-language parity proof against the shared envelope_out_golden.json fixture"
affects: [51-task-loop]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Unconditional-join for Go no-omitempty fields (terminalReason) vs conditional-join for Go omitempty fields (loopRunID/attemptID/runEvidence) — mirrors the Go struct tags field-by-field, not a single blanket join policy"
    - "Caller-owns-bounding for run_evidence — the Python writer does no truncation on the dict it's handed, matching the plan's D-03 scope note that RunEvidence.Bounded() is a Go-side-only concern this phase"
    - "Cross-language golden-fixture round-trip: decode-assert-values then re-marshal/re-decode for value-equivalence, never a byte compare (same discipline as TestGateDecision_GoldenFixtureRoundTrip / test_golden_fixture_round_trip)"

key-files:
  created: []
  modified:
    - cmd/tide-langgraph-verifier/verifier/envelope.py
    - cmd/tide-langgraph-verifier/verifier/tests/test_envelope.py

key-decisions:
  - "Task 1 followed a true RED/GREEN TDD cycle: envelope.py was reverted to its pre-change state via `git checkout --` after test authoring so the RED commit's failure was a real pytest AssertionError (the trivial-shape test's exact-equality check), not a build error — then the implementation was re-applied for the GREEN commit."
  - "ENVELOPE_OUT_GOLDEN_FIXTURE imports verdict.py's private _repo_root() helper directly (`from verifier.verdict import _repo_root`) rather than duplicating it, per the plan's explicit 'reuse/import the existing _repo_root helper' instruction — no import cycle exists since verdict.py never imports envelope.py."
  - "test_write_envelope_out_terminal_reason_never_defaults and test_write_termination_stub_with_loop_fields_stays_small were authored during Task 1 (matching 50-01's precedent) since their names/shapes satisfy Task 2's spec verbatim; Task 2 upgraded the stays-small test to the WR-01/WR-02 daemon-thread-timeout-guard shape and added the golden-fixture parity test rather than re-authoring duplicates."
  - "_write_stub_with_timeout is duplicated locally in test_envelope.py (not imported from test_verdict.py) — no cross-test-module imports exist anywhere in this package today, so this preserves that boundary rather than introducing a new one."

requirements-completed: [EXEC-02, EXEC-03]

# Metrics
duration: 3min
completed: 2026-07-19
---

# Phase 50 Plan 05: Python Envelope Mirror Summary

**Hand-ported TerminalReason/RunEvidence/loopRunID/attemptID into the Python verifier's `write_envelope_out`/`write_termination_stub`, proven byte-for-value against the same Go-authored `envelope_out_golden.json` fixture Plan 50-01 wrote.**

## Performance

- **Duration:** 3 min
- **Started:** 2026-07-19T01:01:34-04:00 (first task commit)
- **Completed:** 2026-07-19T01:04:25-04:00
- **Tasks:** 2 (Task 1 TDD: RED + GREEN commits; Task 2: 1 commit)
- **Files modified:** 2

## Accomplishments
- `write_envelope_out` gains `terminal_reason` (joined unconditionally as `terminalReason`, empty-string sentinel never silently defaulted to `"completed"` — mirrors Go's field with no `omitempty`), `loop_run_id`/`attempt_id` (joined only when non-empty, mirroring Go's `omitempty`), and `run_evidence` (a caller-bounded dict joined only when not `None`).
- `write_termination_stub` gains `terminal_reason`/`changed_file_count`, joined unconditionally alongside the existing `gate_decision`/`findings_count`/`high_severity_count` — never subject to the reason-only truncation loop, and proven to stay strictly under the 4096-byte termination-message budget even with a 10KB `reason`.
- `ENVELOPE_OUT_GOLDEN_FIXTURE` module constant added, reusing `verdict.py`'s `_repo_root()` walk-to-go.mod idiom, pointing at the same `pkg/dispatch/testdata/envelope_out_golden.json` the Go suite reads.
- `test_envelope_out_golden_fixture_parity` — reads the shared fixture, asserts `terminalReason == "cap_exceeded"` (the deliberately non-`completed` pinned value), non-empty `loopRunID`/`attemptID` with the `-<digit>` attempt suffix, all 9 `runEvidence` sub-fields non-empty/non-zero, `changedFiles[0]` shape, then round-trips the fixture's own values through `write_envelope_out` and asserts value-equivalence on every mirrored key — matching `TestEnvelopeOut_GoldenFixtureRoundTrip` field-for-field on the Go side.
- `test_write_termination_stub_with_loop_fields_stays_small` upgraded to the WR-01/WR-02 daemon-thread-timeout-guard shape, proving no truncation-loop hang and that the 10KB `reason` was actually shortened.

## Task Commits

Task 1 followed the TDD RED/GREEN cycle — tests were authored first, `envelope.py` was reverted to its pre-change state via `git checkout --` to produce a real failing-assertion RED, then re-applied for GREEN:

1. **Task 1 (RED): failing tests for the mirrored fields** - `46194e85` (test) — added `test_write_envelope_out_mirrors_go_camelcase_keys`, `test_write_envelope_out_terminal_reason_never_defaults`, `test_write_termination_stub_with_loop_fields_stays_small`; fixed `test_write_envelope_out_trivial_shape`'s exact-equality assertion for the upcoming unconditional `terminalReason` key. Confirmed `AssertionError` (not a collection error) against the unmodified `envelope.py` before proceeding.
2. **Task 1 (GREEN): mirror the fields into both writers** - `3c43e834` (feat) — extended `write_envelope_out`/`write_termination_stub` with the new keyword params, added `ENVELOPE_OUT_GOLDEN_FIXTURE`; full suite green (68 passed).
3. **Task 2: golden-fixture parity + truncation regression** - `a9e393bb` (test) — added `test_envelope_out_golden_fixture_parity` and `_write_stub_with_timeout`; upgraded the stays-small test to the daemon-thread-guard shape; full suite green (69 passed).

**Plan metadata:** pending (this SUMMARY's own commit)

## Files Created/Modified
- `cmd/tide-langgraph-verifier/verifier/envelope.py` - `write_envelope_out` gains `terminal_reason`/`loop_run_id`/`attempt_id`/`run_evidence`; `write_termination_stub` gains `terminal_reason`/`changed_file_count`; module gains `ENVELOPE_OUT_GOLDEN_FIXTURE` (imports `verdict._repo_root`)
- `cmd/tide-langgraph-verifier/verifier/tests/test_envelope.py` - 4 new tests (camelCase-key mirror, never-defaults, stays-small-with-loop-fields, golden-fixture-parity) + `_write_stub_with_timeout` helper + fixed exact-equality assertion on the pre-existing trivial-shape test

## Decisions Made
- `ENVELOPE_OUT_GOLDEN_FIXTURE` imports `verdict.py`'s private `_repo_root()` rather than duplicating it, per the plan's explicit instruction to reuse the helper — no circular import (`verdict.py` has zero dependency on `envelope.py`).
- `run_evidence` bounding stays entirely on the Go side this phase (`RunEvidence.Bounded()`); the Python writer accepts and joins whatever dict the caller passes, matching the plan's D-03 scope note that full evidence population/bounding is Phase 51's job — this phase only makes the schema round-trip.
- `_write_stub_with_timeout` is duplicated locally in `test_envelope.py` rather than imported from `test_verdict.py`, preserving the existing no-cross-test-module-import boundary in this package.

## Deviations from Plan

None — plan executed as specified. One process note: Task 1's TDD cycle used `git checkout --` on the single in-progress file to produce a genuine RED (failing assertion) rather than a build error, since Python has no compile step to fail on missing kwargs the way Go's compiler would; this is a stricter TDD proof than Plan 50-01's Go-side "RED was a build failure" precedent, not a deviation from the plan's tdd="true" requirement.

## Issues Encountered
None.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Go↔Python envelope duality is locked for Phase 50's new fields: `TerminalReason`, `RunEvidence`, `LoopRunID`/`AttemptID` all round-trip identically through the shared `envelope_out_golden.json` fixture in both languages.
- `make test-langgraph-verifier` green (69 passed); `make verify-dispatch-imports` confirms the import firewall is intact (the Python image still cannot import Go types).
- Scope fence confirmed: only `write_envelope_out`/`write_termination_stub` extended — no verifier evidence-population logic, no `ConditionVerifyHalt`, no `evaluation.*` wiring added (all Phase 51).
- `go test ./pkg/dispatch/...` re-confirmed green after the Python changes (no cross-language regression).

---
*Phase: 50-execution-loop-hardening-loop-native-observability*
*Completed: 2026-07-19*

## Self-Check: PASSED

All modified files and task commit hashes verified present on disk / in `git log --oneline --all`.
