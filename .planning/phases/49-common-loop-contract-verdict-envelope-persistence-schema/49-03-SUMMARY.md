---
phase: 49-common-loop-contract-verdict-envelope-persistence-schema
plan: 03
subsystem: api
tags: [python, pydantic, json, envelope-schema, verdict-classifier, wire-contract, langgraph]

# Dependency graph
requires:
  - phase: 49-02
    provides: "pkg/dispatch/verdict.go (Verdict/Finding/GateDecision/ClassifyVerdict) + pkg/dispatch/testdata/gate_decision_golden.json + EnvelopeIn.Verify/EnvelopeOut.Verdict/TerminationStub plumbing on the Go side"
provides:
  - "verifier/verdict.py: Pydantic Verdict(str,Enum)/Finding/GateDecision pair + fail-closed classify_verdict, field-for-field mirror of pkg/dispatch/verdict.go"
  - "verifier/verdict.py: go.mod-anchored GOLDEN_FIXTURE path resolving pkg/dispatch/testdata/gate_decision_golden.json regardless of pytest cwd"
  - "verifier/envelope.py: EnvelopeIn.verify (dict|None) with WR-01 fail-closed non-object guard, mirroring the existing provider extraction"
  - "verifier/envelope.py: write_termination_stub extended with gate_decision/findings_count/high_severity_count params"
  - "verifier/tests/conftest.py: gate_decision_dict factory fixture for future Task-loop plans"
affects: [51]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Hand-authored Go<->Pydantic parity under the import firewall: JSON tags/aliases are the contract, a single shared golden fixture is the cross-language proof (value-equivalence via re-validate, never raw byte-compare — key order differs across serializers)"
    - "Fail-closed classifier mirrored 1:1 across languages: empty/malformed/missing-or-unrecognized verdict field all route to the same BLOCKED default, never a caller-supplied fallback"
    - "go.mod-anchored path resolution for cross-language shared fixtures: walk Path(__file__).parents until go.mod is found, since make test-langgraph-verifier cd's into cmd/tide-langgraph-verifier before pytest runs"

key-files:
  created:
    - cmd/tide-langgraph-verifier/verifier/verdict.py
    - cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py
  modified:
    - cmd/tide-langgraph-verifier/verifier/envelope.py
    - cmd/tide-langgraph-verifier/verifier/tests/conftest.py

key-decisions:
  - "classify_verdict uses Verdict(verdict_str) inside try/except ValueError to collapse the missing-field and unrecognized-field cases into one BLOCKED branch, matching the Go switch/default's identical collapsing"
  - "write_termination_stub adds gateDecision/findingsCount/highSeverityCount to the stub dict UNCONDITIONALLY (not gated on non-zero/non-empty, unlike the Go side's omitempty) per the plan's explicit instruction — bounded-by-construction fields need no truncation-loop change"
  - "verify extraction on EnvelopeIn stays an untyped dict (not a typed VerifyContext dataclass) — this phase only locks the fail-closed guard; Phase 51 consumes the concrete fields"

patterns-established:
  - "Every new pkg/dispatch wire-format field gets both a Go implementation (Plan 49-02) and an independently hand-authored Pydantic mirror (this plan), proven honest by one shared golden JSON fixture exercised in both languages' test suites"

requirements-completed: [EVAL-03]

# Metrics
duration: 3min
completed: 2026-07-18
---

# Phase 49 Plan 03: Python Verdict Schema + Envelope verify Extraction Summary

**Pydantic `GateDecision`/`Finding` pair + fail-closed `classify_verdict` in the LangGraph verifier image, validating and re-emitting the SAME `pkg/dispatch/testdata/gate_decision_golden.json` Plan 49-02 authored — completing the Go↔Python parity proof for EVAL-03.**

## Performance

- **Duration:** 3 min
- **Started:** 2026-07-18T18:15:51-04:00
- **Completed:** 2026-07-18T18:18:01-04:00
- **Tasks:** 2 completed
- **Files modified:** 4 (2 created, 2 modified)

## Accomplishments

- `verifier/verdict.py`: `Verdict(str, Enum)` (`APPROVED | REPAIRABLE | BLOCKED`), `Finding`/`GateDecision` `pydantic.BaseModel` pair (Pydantic, not `@dataclass`, so Phase 51's `create_agent(response_format=GateDecision)` needs no later conversion), and `classify_verdict(raw: str | bytes) -> Verdict` — the 1:1 mirror of `pkg/dispatch.ClassifyVerdict`'s fail-closed 3-branch shape.
- Cross-language golden round-trip proven live: `GateDecision.model_validate_json` reads the exact `pkg/dispatch/testdata/gate_decision_golden.json` bytes Plan 49-02 committed (no Python-local copy), re-dumps via `model_dump_json(by_alias=True)`, and re-validates for value-equivalence.
- `go.mod`-anchored `_repo_root()`/`GOLDEN_FIXTURE` resolves the shared fixture regardless of `make test-langgraph-verifier`'s `cd cmd/tide-langgraph-verifier` before invoking pytest.
- `EnvelopeIn.verify` (dict|None) extraction on the Python `read_envelope_in`, applying the identical WR-01 fail-closed `isinstance`-then-`EnvelopeError` guard already used for `provider` — a non-object `verify` raises a typed error, never an uncaught `AttributeError`.
- `write_termination_stub` extended with `gate_decision`/`findings_count`/`high_severity_count`, emitting `gateDecision`/`findingsCount`/`highSeverityCount` unconditionally while preserving the existing `reason`-only truncation loop and the ≤4096-byte invariant.

## Task Commits

Each task was committed atomically (TDD RED→GREEN per task):

1. **Task 1 RED: failing golden-fixture + fail-closed classifier tests** - `f99f0833` (test)
2. **Task 1 GREEN: verdict.py — Pydantic GateDecision/Finding + classify_verdict** - `bc0efdc3` (feat)
3. **Task 2 RED: failing verify-extraction + verdict-stub tests** - `fa88b342` (test)
4. **Task 2 GREEN: envelope.py — verify extraction + extended write_termination_stub** - `1262e34b` (feat)

**Plan metadata:** (this commit) - `docs(49-03): complete verdict schema + envelope verify extraction plan`

## Files Created/Modified

- `cmd/tide-langgraph-verifier/verifier/verdict.py` - `Verdict`/`Finding`/`GateDecision` Pydantic pair + fail-closed `classify_verdict` + `go.mod`-anchored `GOLDEN_FIXTURE`
- `cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py` - golden round-trip, `gate_decision_dict` factory exercise, 4-row fail-closed parametrize, `verify` extraction (round-trip / missing / 3-shape non-object reject / unknown-field tolerance preserved), `write_termination_stub` verdict-fields size assertion
- `cmd/tide-langgraph-verifier/verifier/envelope.py` - `EnvelopeIn.verify` field + fail-closed guard in `read_envelope_in`; `write_termination_stub` gains `gate_decision`/`findings_count`/`high_severity_count` params
- `cmd/tide-langgraph-verifier/verifier/tests/conftest.py` - `gate_decision_dict` factory fixture (mirrors `envelope_in_dict`) for this and future Task-loop plans

## Decisions Made

- `classify_verdict` collapses "missing verdict field" and "unrecognized verdict value" into a single `try: Verdict(verdict_str) except ValueError: return Verdict.BLOCKED` branch rather than two separate checks — behaviorally identical to the Go `switch`/`default` collapsing, and keeps the `grep -c 'return Verdict.BLOCKED' >= 3` acceptance criterion satisfied (3 distinct BLOCKED returns: empty, malformed, missing/unrecognized).
- `write_termination_stub`'s three new keys are added to the stub dict unconditionally, per the plan's explicit instruction — this differs from the Go `TerminationStub`'s `omitempty` behavior (which drops zero-value fields from JSON), but the plan text was unambiguous ("unconditionally... no truncation loop change is needed") and the existing single size-cap test (`< 4096` bytes) is unaffected by the few extra always-present bytes.
- `EnvelopeIn.verify` stays an untyped `dict[str, Any] | None` rather than a typed `VerifyContext` dataclass — matches the plan's minimal-fields scope; Phase 51 is where the concrete `gateCommand`/`requiredArtifacts`/`evaluatorRef`/`evidencePacketPath` fields get consumed.

## Deviations from Plan

None - plan executed exactly as written. Added one small test (`test_gate_decision_dict_factory_produces_valid_decision`) beyond the plan's explicit list to exercise the newly-added `gate_decision_dict` conftest fixture rather than leaving it unused by this plan's own tests — same-task addition, not a scope deviation.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Verification

- `make test-langgraph-verifier` — 61/61 passed (54 baseline-after-Task-1 + 7 new in Task 2; overall 48 pre-plan baseline + 13 new tests this plan).
- `make verify-langgraph-pins` — OK, no new/unpinned Python dependency introduced (pydantic was already pinned at `2.13.4` from Phase 48).
- `grep -c 'return Verdict.BLOCKED' cmd/tide-langgraph-verifier/verifier/verdict.py` → 3 (matches acceptance criterion `>= 3`).
- `grep -n 'go.mod'` and `grep -n 'gate_decision_golden.json'` both confirmed present in `verdict.py` (path-walk plumbing + shared-fixture read).
- The golden fixture read by pytest (`GOLDEN_FIXTURE`) is byte-identical to the file `go test ./pkg/dispatch/...` reads — both resolve to the single committed `pkg/dispatch/testdata/gate_decision_golden.json`.

## Next Phase Readiness

- EVAL-03 is now fully closed across languages: the Go half (Plan 49-02) and the Python half (this plan) both validate and re-emit the same golden fixture, and both `ClassifyVerdict`/`classify_verdict` are fail-closed by construction with matching 3-shape regression coverage.
- `EnvelopeIn.verify` is ready for Phase 51 to populate with concrete `VerifyContext`-shaped data once the verifier dispatch path exists; today it is unused by any runtime code path (schema-only, matching this phase's locked scope).
- No blockers for Phase 50 or Phase 51.

---
*Phase: 49-common-loop-contract-verdict-envelope-persistence-schema*
*Completed: 2026-07-18*

## Self-Check: PASSED

All created/modified files verified present on disk (`verdict.py`, `test_verdict.py`, `envelope.py`, `conftest.py`, this SUMMARY); all task commits (`f99f0833`, `bc0efdc3`, `fa88b342`, `1262e34b`) and the SUMMARY commit (`250a733c`) verified present in `git log`.
