---
phase: 49-common-loop-contract-verdict-envelope-persistence-schema
plan: 04
subsystem: infra
tags: [git-artifact-store, tide-push, findings-persistence, verdict-schema]

# Dependency graph
requires:
  - phase: 49-01/49-02/49-03
    provides: LoopPolicy/LoopStatus, GateDecision/Finding verdict schema, VerifyContext/TerminationStub envelope fields
provides:
  - stageEnvelopeArtifacts generalized to stage a task/<name> findings-only dir (findings.json, no *.md, no children/)
  - Existing Milestone/Phase/Plan/Project *.md + children/*.json staging path unchanged
  - Regression coverage proving the generalization is additive (positive task case + preserved planner empty-md negative case)
affects: [51-task-loop-reconciler-verifier-dispatch]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "kind derivation via strings.Cut(es.DestPrefix, \"/\") to branch consumer-side staging logic per envelope kind, without touching the producer (collectStageEnvelopes)"

key-files:
  created: []
  modified:
    - cmd/tide-push/main.go
    - cmd/tide-push/main_test.go

key-decisions:
  - "Derived the entry kind from EnvelopeStage.DestPrefix's first path segment (strings.Cut) rather than adding a new field to EnvelopeStage - matches RESEARCH's recommended mechanism and keeps parseStageEnvelopes/collectStageEnvelopes untouched"
  - "A task-kind entry missing findings.json fails loudly (artifact-stage-failed, nonzero exit) rather than being silently skipped - mirrors the existing *.md-empty guard's fail-closed discipline (T-49-04-02)"

patterns-established:
  - "Consumer-side (tide-push) capability can be added ahead of producer-side (collectStageEnvelopes) wiring when the change is purely additive and gated by a kind discriminator - lets a future phase add the producer without a push-breaking regression"

requirements-completed: [EVAL-05]

# Metrics
duration: 8s
completed: 2026-07-18
---

# Phase 49 Plan 04: Task Findings Envelope Staging Generalization Summary

**Generalized `tide-push`'s `stageEnvelopeArtifacts` glob so a `task/<name>` envelope dir stages on `findings.json` alone, closing the highest-risk plumbing trap (RESEARCH Pitfall 1) before Phase 51 ever produces one.**

## Performance

- **Duration:** 8s (task-commit-to-task-commit; plan authored well in advance)
- **Started:** 2026-07-18T22:07:49Z
- **Completed:** 2026-07-18T22:07:57Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments
- `stageEnvelopeArtifacts` now derives an entry's kind from `EnvelopeStage.DestPrefix`'s first path segment and branches: `task` kind requires only `findings.json` (no `*.md`, no `children/*.json` glob); every other kind (`project`/`milestone`/`phase`/`plan`, and unknown/empty) keeps the existing `*.md`-must-be-non-empty hard-fail and `children/*.json` glob byte-identical.
- A `task`-kind entry missing `findings.json` still fails loudly with reason `artifact-stage-failed` and nonzero exit — no silent skip, matching the existing fail-closed discipline.
- Added `TestStageEnvelopesTaskFindingsOnly`, proving a findings-only `task/t1` dir stages to `.tide/planning/task/t1/findings.json` with byte-identical content and exit 0.
- Confirmed `TestStageEnvelopesEmptyDirFailsLoud` (the non-task planner empty-`*.md` guard) is unchanged and still asserts the hard-fail — the generalization did not weaken it.
- `internal/controller/artifact_push.go` (`collectStageEnvelopes`) is untouched — no Task entry added; nothing produces `findings.json` yet, correctly deferred to Phase 51.

## Task Commits

Each task was committed atomically:

1. **Task 1: Generalize stageEnvelopeArtifacts glob per DestPrefix kind** - `009fd14e` (feat)
2. **Task 2: main_test.go regression — findings-only task dir stages; non-task empty-md still fails** - `9d77825b` (test)

**Plan metadata:** (this commit, docs)

## Files Created/Modified
- `cmd/tide-push/main.go` - `stageEnvelopeArtifacts` branches on `kind := strings.Cut(es.DestPrefix, "/")`; `task` kind stages `findings.json` only, all other kinds unchanged; `EnvelopeStage` doc comment updated to note the new `task` kind
- `cmd/tide-push/main_test.go` - `TestStageEnvelopesTaskFindingsOnly` added as a sibling of `TestStageEnvelopesEmptyDirFailsLoud`

## Decisions Made
- Derived kind from `DestPrefix`'s first path segment via `strings.Cut` (RESEARCH's recommended mechanism among Claude's-Discretion options) rather than adding a new `Glob`/`Kind` field to `EnvelopeStage` — keeps `parseStageEnvelopes` and the CLI flag surface unchanged.
- Kept the non-task branch's logic (variables, error messages, glob calls) verbatim inside an `else` block rather than refactoring into a shared helper — minimizes diff risk against the "byte-identical" success criterion for existing kinds.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness
- Phase 51 can add a `<taskUID>:task/<name>` entry to `collectStageEnvelopes` once `findings.json` is actually produced by a Task/verifier dispatch, without shipping a push-breaking regression on day one.
- The full-findings tier (EVAL-05c, D-05(c)) of the size×locality persistence contract is now plumbed on the consumer side; producer-side wiring remains Phase 51's job.
- No blockers.

---
*Phase: 49-common-loop-contract-verdict-envelope-persistence-schema*
*Completed: 2026-07-18*

## Self-Check: PASSED

- FOUND: .planning/phases/49-common-loop-contract-verdict-envelope-persistence-schema/49-04-SUMMARY.md
- FOUND: 009fd14e (Task 1 commit)
- FOUND: 9d77825b (Task 2 commit)
- FOUND: 869bcca3 (SUMMARY commit)
