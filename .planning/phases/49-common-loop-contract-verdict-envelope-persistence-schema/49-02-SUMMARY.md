---
phase: 49-common-loop-contract-verdict-envelope-persistence-schema
plan: 02
subsystem: api
tags: [go, json, envelope-schema, verdict-classifier, wire-contract]

# Dependency graph
requires:
  - phase: 49-01
    provides: LoopPolicy/LoopStatus shared API types (api/v1alpha3/loop_types.go)
provides:
  - "pkg/dispatch/verdict.go: Verdict terminal set (APPROVED|REPAIRABLE|BLOCKED), Finding, GateDecision, fail-closed ClassifyVerdict"
  - "pkg/dispatch/testdata/gate_decision_golden.json: canonical cross-language verdict fixture"
  - "EnvelopeIn.Verify *VerifyContext (pointer+omitempty, 4th Role=\"verifier\")"
  - "EnvelopeOut.Verdict *GateDecision (pointer+omitempty, unpopulated until Phase 51)"
  - "TerminationStub bounded verdict summary (GateDecision string + FindingsCount + HighSeverityCount, <4KB under 50-finding worst case)"
affects: [49-03, 51]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Fail-closed classifier signature: bare Verdict return (no accompanying error), so 'unknown' is structurally inexpressible as anything but BLOCKED"
    - "Bounded-by-construction status carrier: TerminationStub gains an enum string + int counts, never a free-text summary or the findings array itself"
    - "Golden-fixture cross-language parity: hand-authored Go+Pydantic pair kept honest by a single committed JSON fixture, asserted via decoded-value comparison (not byte comparison)"

key-files:
  created:
    - pkg/dispatch/verdict.go
    - pkg/dispatch/verdict_test.go
    - pkg/dispatch/testdata/gate_decision_golden.json
  modified:
    - pkg/dispatch/envelope.go
    - pkg/dispatch/envelope_test.go

key-decisions:
  - "GateDecision/Finding live in pkg/dispatch (not api/v1alpha3) per D-01 — the verdict is a wire-format document crossing the file-envelope seam, not a CRD type"
  - "ClassifyVerdict returns a bare Verdict (never (Verdict, error)) so a caller cannot forget to map an error to the safe BLOCKED terminal"
  - "highSeverityFindingToken is a package const (\"blocker\") rather than a literal at the call site, so Phase 51's severity rubric can retune it in one place"

patterns-established:
  - "Verdict classification is fail-closed by construction: empty input, malformed JSON, and any unrecognized verdict string all route through the same default->BLOCKED branch"

requirements-completed: [EVAL-03, EVAL-05]

# Metrics
duration: 5min
completed: 2026-07-18
---

# Phase 49 Plan 02: Verdict Schema + Envelope Seam Plumbing Summary

**Go half of the gate_decision wire contract: fail-closed `ClassifyVerdict` classifier, the canonical Go+Python golden fixture, and the `VerifyContext`/`Verdict`/bounded-TerminationStub plumbing on the dispatch envelope.**

## Performance

- **Duration:** 5 min
- **Started:** 2026-07-18T21:56:01Z
- **Completed:** 2026-07-18T22:01:11Z
- **Tasks:** 2 completed
- **Files modified:** 5 (3 created, 2 modified)

## Accomplishments

- `pkg/dispatch/verdict.go`: `Verdict` terminal set (`APPROVED | REPAIRABLE | BLOCKED`), `Finding` (free-string dimension/severity/confidence/evidence/suggestedFix), `GateDecision`, and `ClassifyVerdict(json.RawMessage) Verdict` — fail-closed by construction, the direct structural fix for the 2026-07-03 silent-`Complete` incident this milestone exists to prevent.
- `pkg/dispatch/testdata/gate_decision_golden.json`: the single-source-of-truth canonical verdict fixture (`REPAIRABLE` + a fully-populated finding) Plan 49-03's Python test will validate and re-emit.
- `EnvelopeIn.Verify *VerifyContext` (pointer+omitempty, mirrors `Dispatch`/`Dev`) with the four D-03 fields (`GateCommand`/`RequiredArtifacts`/`EvaluatorRef`/`EvidencePacketPath`); `Role` godoc extended to name the fourth `"verifier"` value.
- `EnvelopeOut.Verdict *GateDecision` (pointer+omitempty) — schema/plumbing only, Phase 51 populates it.
- `TerminationStub` gains `GateDecision`/`FindingsCount`/`HighSeverityCount` (enum string + two bounded ints, never a free-text summary); `NewTerminationStub` flattens `out.Verdict` nil-safely, mirroring the existing `out.Git` flatten. Re-verified `<4096` bytes under a 50-all-high-severity-finding worst case.

## Task Commits

Each task was committed atomically:

1. **Task 1: verdict.go — Verdict/Finding/GateDecision + fail-closed ClassifyVerdict + golden fixture + tests** - `48eca139` (feat)
2. **Task 2: envelope.go — VerifyContext on EnvelopeIn, Verdict on EnvelopeOut, bounded TerminationStub verdict summary** - `4c2489e5` (feat)

**Plan metadata:** (this commit) - `docs(49-02): complete verdict/envelope plumbing plan`

## Files Created/Modified

- `pkg/dispatch/verdict.go` - `Verdict`/`Finding`/`GateDecision` types + fail-closed `ClassifyVerdict` classifier
- `pkg/dispatch/verdict_test.go` - golden round-trip + 3-shape fail-closed regression + 2 positive controls
- `pkg/dispatch/testdata/gate_decision_golden.json` - canonical cross-language verdict fixture
- `pkg/dispatch/envelope.go` - `VerifyContext` struct, `EnvelopeIn.Verify`, `EnvelopeOut.Verdict`, `TerminationStub` verdict fields + `NewTerminationStub` extension
- `pkg/dispatch/envelope_test.go` - `VerifyContext` + `EnvelopeOut.Verdict` round-trip/omitempty coverage, `TestNewTerminationStub_StaysSmall` worst-case extension, `TestTerminationStub_NoForbiddenFields` literal updated

## Decisions Made

- Reworded a doc comment in `verdict.go` that originally contained the literal string `(Verdict, error)` (explaining what the signature deliberately avoids) to `(no accompanying error value)`, so the acceptance-criteria grep for "no `(Verdict, error)` return in verdict.go" checks the actual function signature rather than false-positiving on the comment's own explanation of the anti-pattern it avoids. No behavioral change — cosmetic wording only.
- Added `Verify *VerifyContext` to the shared `fullyPopulatedEnvelopeIn()` test fixture (mirroring how `Dispatch`/`Dev` are already populated there) rather than constructing it ad hoc only in a dedicated test, so the general `assertRoundTripIn` coverage and the `NilVerify` subtest-table case both exercise it for free.

## Deviations from Plan

None - plan executed exactly as written. The doc-comment wording adjustment above is a same-task correction to hit the plan's own acceptance-criteria grep intent, not a deviation from scope.

## Issues Encountered

None.

## User Setup Required

None - no external service configuration required.

## Verification

- `go test ./pkg/dispatch/... -count=1` — all green (golden round-trip, 5-case fail-closed table + unrecognized-verdict-field regression, VerifyContext/Verdict round-trip + omitempty coverage, StaysSmall under 50-finding worst case, NoForbiddenFields literal compiles).
- `make verify-dispatch-imports` — import firewall intact; `verdict.go` uses only `encoding/json` + stdlib.
- `go build ./...` and `go vet ./...` — clean across the whole repo (new pointer fields don't break any existing consumer).
- `bin/golangci-lint run ./pkg/dispatch/...` — 0 issues.

## Next Phase Readiness

- The Go half of the `gate_decision` wire contract is locked: `EnvelopeOut.Verdict` and the shared golden fixture are in place for Plan 49-03's Python/Pydantic mirror and cross-language parity proof.
- `EnvelopeOut.Verdict` and the `TerminationStub` verdict fields are intentionally unpopulated by any dispatch path this phase (Phase 51's job) — no stub concerns, this is the documented scope boundary.
- No blockers for Plan 49-03.

---
*Phase: 49-common-loop-contract-verdict-envelope-persistence-schema*
*Completed: 2026-07-18*
