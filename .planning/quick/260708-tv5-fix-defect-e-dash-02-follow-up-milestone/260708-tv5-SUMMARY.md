---
phase: quick-260708-tv5
plan: 01
subsystem: infra
tags: [kubernetes, controller-runtime, boundary-push, envtest, ginkgo, dash-02, defect-e]

# Dependency graph
requires:
  - phase: 37-06
    provides: collectStageEnvelopes cumulative planner-artifact staging map
  - phase: dash-02
    provides: bounded boundary-push auto-retry state machine + readPushEnvelope headSHA capture
provides:
  - Stale-subset boundary-push supersede — a shared single-flight push Job that staged a strict-subset cumulative map is deleted and re-dispatched with the FULL map instead of being accepted as terminal
  - stagedEnvelopesAnnotation provenance stamp on every push Job carrying a --stage-envelopes arg
  - isStaleArtifactPush strict-superset detector (absent stamp = unknown provenance = never stale)
  - dispatchBoundaryPush now threads PushOptions.StageEnvelopes (closes the latent project-Complete-staged-no-map gap)
affects: [dash-02, layer-b-acceptance, milestone-artifact-staging]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Provenance-stamp-then-compare: stamp a Job's cumulative staged set as a create-time annotation, read it back on a later reconcile and compare against a fresh collect to detect a stale single-flight winner"
    - "Absent-annotation = unknown provenance = trust-as-is — keeps bare/pre-fix Jobs behaving unchanged, no test churn"

key-files:
  created: []
  modified:
    - internal/controller/push_helpers.go
    - internal/controller/project_controller.go
    - internal/controller/project_boundary_push_test.go

key-decisions:
  - "Stamp the staged set as a Job annotation under the SAME len(StageEnvelopes)>0 guard as the --stage-envelopes arg, so annotation and arg are always stamped together — no Job carries a map it cannot prove"
  - "isStaleArtifactPush requires a STRICT superset AND every staged entry still present in current; a missing staged entry (level CR deleted) returns false so an already-succeeded push is never second-guessed"
  - "Supersede check runs BEFORE the terminal patch in the isJobSucceeded arm — a stale-map success must never set BoundaryPushed=True"
  - "D-B5 single-writer deterministic name tide-push-<uid> left untouched — the fix lives entirely in supersede logic, never in naming (hard constraint honored)"

patterns-established:
  - "Cumulative map computed ONCE per reconcile pass and threaded into all four dispatchBoundaryPush call sites (first dispatch, headSHA-unreadable retry, stale-map supersede, generic terminal-failure retry)"

requirements-completed: [TV5-01]

coverage:
  - id: D1
    description: "A succeeded shared push Job that staged a strict-subset cumulative map (project-only) while a milestone materialized after is superseded — deleted and re-dispatched with the FULL map including the milestone entry; not accepted as terminal"
    requirement: TV5-01
    verification:
      - kind: integration
        ref: "internal/controller/project_boundary_push_test.go#Test 8: succeeded Job stamped with a partial (subset) staged map → supersede, not terminal"
        status: pass
    human_judgment: false
  - id: D2
    description: "A succeeded push Job whose stamped map already covers the current full map goes terminal (BoundaryPushed=True/Pushed, LastPushedSHA advances) with zero extra Job churn"
    requirement: TV5-01
    verification:
      - kind: integration
        ref: "internal/controller/project_boundary_push_test.go#Test 9: succeeded Job already stamped with the FULL cumulative map → terminal success, no re-dispatch"
        status: pass
    human_judgment: false
  - id: D3
    description: "Every dispatchBoundaryPush call site threads the freshest collectStageEnvelopes map into PushOptions.StageEnvelopes; Tests 1-7 (bare unstamped Jobs) unchanged via the no-annotation→not-stale guard"
    requirement: TV5-01
    verification:
      - kind: integration
        ref: "go test ./internal/controller/... -ginkgo.label-filter='debug13b' → 9/9 pass"
        status: pass
      - kind: unit
        ref: "make test → MAKE_EXIT=0, internal/controller ok, no --- FAIL"
        status: pass
    human_judgment: false
  - id: D4
    description: "Live Layer-B confirmation that .tide/planning/milestone/<name>/MILESTONE.md lands on the run branch"
    verification: []
    human_judgment: true
    rationale: "Orchestrator-gated Layer-B kind run; explicitly OUT OF SCOPE per plan hard constraint. ENVTEST is the allowed verification surface here. Owned by the orchestrator's next Layer-B run."

# Metrics
duration: 11min
completed: 2026-07-08
status: complete
---

# Phase quick-260708-tv5: Fix Defect E (DASH-02 milestone-artifact staging gap) Summary

**Boundary-push now supersedes a stale single-flight winner: a succeeded `tide-push-<uid>` Job that snapshotted a strict-subset staged map is deleted and re-dispatched carrying the full cumulative map, so milestone/phase/plan planning artifacts reach the run branch instead of only the project artifact.**

## Performance

- **Duration:** ~11 min
- **Started:** 2026-07-08T21:48Z (RED test authoring)
- **Completed:** 2026-07-08T21:59:53-04:00
- **Tasks:** 2
- **Files modified:** 3

## Accomplishments
- **RED→GREEN pinned the Defect E supersede contract.** Test 8 (stale-subset → supersede) fails against the pre-fix controller (readable headSHA always goes terminal) and passes after the fix; Test 9 (full-map → terminal, no churn) guards against regression. 9/9 debug13b specs green.
- **Provenance stamp on every push Job.** `buildPushJob` now stamps `tideproject.k8s/staged-envelopes` under the same `len(StageEnvelopes)>0` guard as the `--stage-envelopes` arg, so a later boundary reconcile can detect a stale single-flight winner.
- **`isStaleArtifactPush` + supersede arm.** Strict-superset detector (absent stamp = unknown provenance = never stale) drives a delete + owned re-dispatch of the FULL map, running before the terminal patch so a stale-map success never wedges `BoundaryPushed=True`.
- **Closed the latent project-Complete gap.** `dispatchBoundaryPush` now sets `PushOptions.StageEnvelopes`; all four call sites thread the once-computed cumulative map.

## Task Commits

1. **Task 1: RED — pin the stale-subset supersede contract (2 envtest specs)** - `1b4bef8` (test)
2. **Task 2: GREEN — stamp staged set, detect staleness, supersede in reconcileBoundaryPush** - `6a65f4e` (fix)

_TDD plan: test (RED) → fix (GREEN)._

## Files Created/Modified
- `internal/controller/push_helpers.go` - Added `stagedEnvelopesAnnotation` const; `buildPushJob` stamps the staged-envelope CSV onto the Job at create time.
- `internal/controller/project_controller.go` - Added `isStaleArtifactPush`; `reconcileBoundaryPush` computes `collectStageEnvelopes` once, supersedes stale-subset succeeded Jobs before the terminal patch, threads the map into all 4 `dispatchBoundaryPush` sites; `dispatchBoundaryPush` gained a `stageEnvelopes` param wired into `PushOptions.StageEnvelopes`.
- `internal/controller/project_boundary_push_test.go` - Added `makeStampedPushJob` / `makeMaterializedMilestone` / `deleteMilestone` helpers, `strings` import, and Test 8 + Test 9.

## RED→GREEN Evidence
- **Test 8 RED (pre-fix):** `• [FAILED]` — "boundary push landed on remote" logged, `LastPushedSHA` = `aaaabbbb…3333` where `BeEmpty()` expected. Confirmed by reading Ginkgo output, not assumed.
- **Test 8 GREEN + full suite:** `go test ./internal/controller/... -ginkgo.label-filter='debug13b'` → **Ran 9 of 172 Specs … SUCCESS! -- 9 Passed | 0 Failed**. (Note: `-ginkgo.focus='debug13b'` from the plan's verify block matches zero specs — `debug13b` is a Ginkgo Label, not spec text; used `-ginkgo.label-filter` instead.)
- **`make test`:** `MAKE_EXIT=0`, `ok github.com/jsquirrelz/tide/internal/controller 69.823s`, no `--- FAIL` / `FAIL` lines.
- **`make lint`:** exits 2, but **all three modified files are lint-clean** (0 findings). The 8 remaining findings are pre-existing debt in `cmd/tide-push/main.go` and `cmd/dashboard/gitfetch/*` (last touched by 37-02 `e6a913c` / 37-03 `123448f`, not tv5). Logged to `deferred-items.md`.

## Decisions Made
None beyond the plan — followed the KEY DESIGN DECISION (annotation-stamp-then-compare) and all HARD CONSTRAINTS as specified.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Lint] Modernize `strings.Split` → `strings.SplitSeq` in `isStaleArtifactPush`**
- **Found during:** Task 2 (GREEN), on `make lint`
- **Issue:** golangci-lint `modernize (stringsseq)` flagged `for _, e := range strings.Split(raw, ",")` at `project_controller.go:1784` — the only lint finding in my code.
- **Fix:** Switched to `for e := range strings.SplitSeq(raw, ",")` (Go 1.24+, project is on 1.26).
- **Files modified:** internal/controller/project_controller.go
- **Verification:** Re-ran `make lint` — my files 0 findings; re-ran `make test` (MAKE_EXIT=0) and debug13b (9/9) with the final code.
- **Committed in:** `6a65f4e` (Task 2 commit)

---

**Total deviations:** 1 auto-fixed (1 lint-clean requirement on my own new code)
**Impact on plan:** Behavior-identical modernization; no scope creep.

## Issues Encountered
- **`-ginkgo.focus='debug13b'` from the plan's verify block matched 0 specs** (fast "ok" with no report). `debug13b` is a Ginkgo Label, not spec text — the correct invocation is `-ginkgo.label-filter='debug13b'`. Re-ran and confirmed 9/9.
- **8 pre-existing `make lint` findings in untouched files** (`cmd/tide-push`, `cmd/dashboard/gitfetch`) keep `make lint` at exit 2. Out of scope (scope-boundary rule); logged to `deferred-items.md`. My three modified files are lint-clean.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Controller fix is envtest-proven. **DEFERRED (out of scope, orchestrator-gated):** the Layer-B kind run confirming `.tide/planning/milestone/<name>/MILESTONE.md` lands on the run branch live — owned by the orchestrator's next Layer-B run.
- Pre-existing lint debt in `cmd/tide-push` + `cmd/dashboard` recommends a separate cleanup pass (see `deferred-items.md`) if repo-wide `make lint` must go green.

---
*Phase: quick-260708-tv5*
*Completed: 2026-07-08*

## Self-Check: PASSED

- FOUND: 260708-tv5-SUMMARY.md
- FOUND: deferred-items.md
- FOUND commit 1b4bef8 (Task 1 RED)
- FOUND commit 6a65f4e (Task 2 GREEN)
