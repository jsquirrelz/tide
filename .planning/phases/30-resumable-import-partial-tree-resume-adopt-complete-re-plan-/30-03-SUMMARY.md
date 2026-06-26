---
phase: 30-resumable-import-partial-tree-resume-adopt-complete-re-plan-
plan: "03"
subsystem: testing
tags: [kind, e2e, import, resume, partial-tree, namespace-teardown, cross-tier-contention]

requires:
  - phase: 30-resumable-import-partial-tree-resume-adopt-complete-re-plan-
    plan: "01"
    provides: IsEnvelopeComplete gating + empty-Status export for incomplete nodes (RESUME-PARTIAL-01/04)
  - phase: 30-resumable-import-partial-tree-resume-adopt-complete-re-plan-
    plan: "02"
    provides: post-ImportComplete project-planner guard suppressing re-dispatch on complete plans (RESUME-PARTIAL-02)

provides:
  - Tier c E2E spec: partial fixture (1 complete plan + 1 incomplete plan) drives all the way to Project.Status.Phase == Complete
  - deleteNamespaceAndWait helper: polls until namespace is fully NotFound before returning, eliminating cross-tier load contention
  - Cross-tier contention fix: all three import-resume tiers (a/b/c) now pass together on a shared kind cluster

affects:
  - future import-resume tests that add new tiers (should use deleteNamespaceAndWait in AfterEach)
  - any spec that calls deleteNamespace in a sequential suite that feeds subsequent import-heavy specs

tech-stack:
  added: []
  patterns:
    - "deleteNamespaceAndWait: issue kubectl delete then Eventually-poll namespace NotFound (3 min / 5 s) before returning; preserves KEEP_KIND_NAMESPACES escape hatch"
    - "Targeted AfterEach fix: apply deleteNamespaceAndWait only to import-resume tiers; leave deleteNamespace unchanged so unrelated specs are unaffected"

key-files:
  created: []
  modified:
    - test/integration/kind/suite_test.go
    - test/integration/kind/import_resume_test.go

key-decisions:
  - "Targeted fix over global change: added deleteNamespaceAndWait alongside the existing deleteNamespace rather than mutating deleteNamespace globally — other specs rely on fire-and-forget timing and a global poll would slow the full suite."
  - "Poll budget of 3 min / 5 s: matches the deletion latency budget already used elsewhere in the suite; on healthy clusters namespace disappears in <30s."

patterns-established:
  - "Cross-tier teardown: any import-resume AfterEach that can leave terminating pods behind should call deleteNamespaceAndWait, not deleteNamespace."

requirements-completed: [RESUME-PARTIAL-03]

duration: 45min
completed: 2026-06-26
---

# Phase 30 Plan 03: Partial-Tree Resume + Cross-Tier Contention Fix Summary

**Tier c E2E proves partial-import partial-tree resumes all the way to Project=Complete; deleteNamespaceAndWait eliminates inter-tier namespace contention so all three import-resume tiers pass together**

## Performance

- **Duration:** ~45 min (including kind cluster setup + 3 × tier runs ~317s)
- **Started:** 2026-06-26T10:40:00Z (continuation from checkpoint resolution)
- **Completed:** 2026-06-26T10:48:07Z
- **Tasks:** 3 (Tasks 1 + 2 already committed; Task 3 = checkpoint resolution + this fix)
- **Files modified:** 2

## Accomplishments

- Fixed the root-cause cross-tier contention defect: Tier a's fire-and-forget deleteNamespace left terminating pods running when Tier b started its 80-envelope salvage import, causing the 3-minute waitForImportComplete deadline to be missed on all 3 prior flake attempts.
- Added `deleteNamespaceAndWait` to `suite_test.go` that polls until the namespace reaches NotFound, with the KEEP_KIND_NAMESPACES escape hatch preserved.
- Updated all three import-resume tiers (a, b, c) to call `deleteNamespaceAndWait` in their AfterEach blocks; the existing `deleteNamespace` helper is untouched.
- All three tiers now pass together in a single focused kind run (exit 0, no FAIL lines): Tier a 106s, Tier b 101s, Tier c 50s.

## Task Commits

1. **Task 1: Author the mixed partial-tree fixture bundle** - `23e7ead` (chore — from prior executor)
2. **Task 2: Tier c spec** - `85da26a` (feat — from prior executor) plus Makefile fixes `ca0c8bc`, `bdb8c4f`
3. **Task 3 (checkpoint resolution): deleteNamespaceAndWait cross-tier contention fix** - `9e61775` (fix)

**Plan metadata:** (this commit)

## Files Created/Modified

- `test/integration/kind/suite_test.go` — added `deleteNamespaceAndWait` helper (polls until namespace NotFound; 3 min / 5 s budget; KEEP_KIND_NAMESPACES respected)
- `test/integration/kind/import_resume_test.go` — updated AfterEach in Tier a, Tier b, and Tier c to call `deleteNamespaceAndWait` instead of `deleteNamespace`

## Decisions Made

- **Targeted fix, not global change.** The `deleteNamespace` fire-and-forget helper is left exactly as-is; `deleteNamespaceAndWait` is a new peer. Other specs (failure_test.go, push_lease_test.go, etc.) were written expecting the existing timing and a global poll would lengthen their teardown without benefit.
- **3 min / 5 s poll parameters.** Matches the deletion budget already used in `waitForImportComplete` and similar helpers. Namespaces with finalizers on a healthy single-node cluster vanish within 30s; the 3 min ceiling handles slow CI.
- **All three import-resume tiers patched, not just Tier a→b boundary.** Tier b→c has the same structural contention risk (salvage fixture is 80 envelopes; Tier c import runs immediately after).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Tier b cross-tier namespace contention — pre-existing test hygiene defect**
- **Found during:** Task 3 / checkpoint resolution (prior executor reported 3 consecutive Tier b flakes)
- **Issue:** `deleteNamespace` (suite_test.go:982) fires `kubectl delete --timeout=30s` and RETURNS after 30s even if the namespace is still Terminating. Tier a's pods were still running when Tier b's import started, exhausting the 3-minute `waitForImportComplete` deadline. No production code touched by Phase 30 caused this — it is a pre-existing contention defect in the test sequencing.
- **Fix:** Added `deleteNamespaceAndWait` that issues delete then polls via `Eventually` (3 min / 5 s) until the namespace is NotFound. Updated Tier a, b, and c AfterEach to use it.
- **Files modified:** `test/integration/kind/suite_test.go`, `test/integration/kind/import_resume_test.go`
- **Verification:** Focused kind run — all 3 tiers PASSED, MAKE_EXIT=0, no FAIL lines. Tier b no longer times out.
- **Committed in:** `9e61775`

---

**Total deviations:** 1 auto-fixed (Rule 1 — pre-existing test hygiene bug surfaced by cross-tier load)
**Impact on plan:** Essential to make the plan's success criteria provable; no scope creep. All three tiers now pass together on the single-node kind cluster the CI uses.

## Issues Encountered

The prior executor's checkpoint reported Tier b failing all 3 flake retries with a `waitForImportComplete` 3-minute timeout. Root cause was verified by the orchestrator (see `<verified_root_cause>` in the plan prompt) and confirmed as the fire-and-forget `deleteNamespace` pattern, not a regression in any Phase 30 production code.

## Known Stubs

None — no placeholder or hardcoded empty values in the fixture or spec.

## Threat Flags

None — this fix is test-infra only; no new network endpoints, auth paths, or schema changes.

## Self-Check: PASSED

- `test/integration/kind/suite_test.go` — exists and modified (deleteNamespaceAndWait added)
- `test/integration/kind/import_resume_test.go` — exists and modified (3 AfterEach blocks updated)
- Fix commit `9e61775` — exists (`git log --oneline -5` confirmed)
- Kind run result: MAKE_EXIT=0, Ran 3 of 22 Specs, SUCCESS!, no FAIL lines

## Next Phase Readiness

Phase 30 is complete. All three plans (30-01, 30-02, 30-03) are merged to main. The partial-tree resumption defect (run #2 zombie stall) cannot recur without failing Tier c. Dogfood run #2 is unblocked.

---
*Phase: 30-resumable-import-partial-tree-resume-adopt-complete-re-plan-*
*Completed: 2026-06-26*
