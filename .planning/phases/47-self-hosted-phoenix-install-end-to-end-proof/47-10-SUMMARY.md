---
phase: 47-self-hosted-phoenix-install-end-to-end-proof
plan: 10
subsystem: testing
tags: [envtest, ginkgo, reporter-spawn, ttl-gc, cr-01, reporter-jobspec, milestone-controller, project-controller, task-controller]

# Dependency graph
requires:
  - phase: 47-self-hosted-phoenix-install-end-to-end-proof (plan 07)
    provides: durable *ReporterSpawnedUID status markers gating all five reporter spawn sites
provides:
  - envtest proof that a reporter Job deleted after its first spawn (TTL-GC simulated) is NOT re-created for the same completed-Job attempt, at all three distinct spawn-site code shapes
  - negative-control proof that the marker gate is per-attempt equality, not a permanent one-shot latch
affects: [47-VERIFICATION.md gap #2 closure, future reporter-spawn-site refactors]

# Tech tracking
tech-stack:
  added: []
  patterns: ["explicit reporter-Job Delete + Eventually-NotFound as the envtest TTL-GC simulation (mirrors child_rollup_idempotency_test.go / BYPASS-03)", "mgrClient cache-sync Eventually-wait after a direct k8sClient.Status().Patch before re-driving a reconciler call"]

key-files:
  created: [internal/controller/reporter_spawn_idempotency_test.go]
  modified: []

key-decisions:
  - "Milestone spec stands in for the shared-helper shape (milestone/phase/plan all route through spawnReporterIfNeeded with the identical gate+stamp idiom) — one representative spec pins all three, per the plan's deliberate scope decision"
  - "Negative control uses the plan's own 'reset the marker' allowance (direct Status().Patch to a mismatched value) rather than driving a second real completedJob object, avoiding unrelated span-emission side effects while still proving per-attempt gate semantics"

requirements-completed: [PROOF-01]

# Metrics
duration: ~35min
completed: 2026-07-17
---

# Phase 47 Plan 10: Reporter-Spawn TTL-GC Idempotency Proof Summary

**Envtest proof (`internal/controller/reporter_spawn_idempotency_test.go`, 3 Ginkgo specs) that a TTL-GC'd reporter Job is never re-created for an already-observed spawn attempt at all three reporter-spawn-site code shapes, while a genuinely new attempt still spawns.**

## Performance

- **Duration:** ~35 min
- **Completed:** 2026-07-17
- **Tasks:** 1
- **Files modified:** 1 (new file)

## Accomplishments
- Landed the demanded envtest proof for verification gap #2 (CR-01): milestone (representative of the shared `spawnReporterIfNeeded` helper shape used by milestone/phase/plan), project (inline spawn arm), and task (trace-only path) each get a spec that spawns a reporter, deletes it (envtest's TTL-GC simulation — no TTL controller exists in envtest, so an explicit background-propagation Delete waited to NotFound IS the end state), re-drives the same completion, and asserts via `Consistently` that no reporter Job is re-created and the durable marker still holds its original value.
- Added a negative control in the milestone spec: resetting `MilestoneReporterSpawnedUID` to a mismatched value and re-driving proves the gate is a per-attempt equality check, not a permanent one-shot latch once any reporter has ever been observed.
- Confirmed the new suite coexists cleanly with the existing rollup-idempotency and reporter-spawn envtests in the same heavy tier: `make test-heavy` is green end-to-end (0 failures across the full heavy Ginkgo + Go-test bundle).

## Task Commits

Each task was committed atomically:

1. **Task 1: Author the ReporterSpawnIdempotency envtest suite** - `5bfd1a8` (test)

_No feat/refactor commits — this plan is test-only per its own verification scope (production code is fixed in the referenced plan 47-07)._

## Files Created/Modified
- `internal/controller/reporter_spawn_idempotency_test.go` - Three `Label("envtest", "heavy")` Ginkgo `Describe` blocks (`ReporterSpawnIdempotency — Milestone level`, `— Project level`, `— Task level`), each driving a completion handler directly (mirrors the established `child_rollup_idempotency_test.go` / `project_planner_completion_test.go` direct-call convention), deleting the spawned reporter Job to simulate 300s TTL-GC, re-driving, and asserting non-recreation plus marker persistence via `Consistently`.

## Decisions Made
- Milestone stands in for the shared-helper shape (milestone/phase/plan share `spawnReporterIfNeeded`'s identical gate+stamp idiom) rather than writing three near-duplicate specs — per the plan's own deliberate scope framing.
- The milestone negative control uses direct marker-reset via `k8sClient.Status().Patch` (an option the plan text explicitly allows: "e.g. after resetting the marker") rather than driving a second real `completedJob` object, keeping the spec free of unrelated span-emission plumbing while still exercising the exact equality check the gate uses (`ms.Status.MilestoneReporterSpawnedUID == spawnKey`).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed a cache-sync race in the milestone negative-control step**
- **Found during:** Task 1 verification run (focused Ginkgo run passed in isolation but failed intermittently when all three specs ran together)
- **Issue:** The negative control wrote the marker reset directly via `k8sClient.Status().Patch` (bypassing the manager's informer cache) and immediately re-fetched via `mgrClient.Get` before re-driving `handleJobCompletion`. Under load from the two preceding heavy specs, the manager's cache had not yet observed the direct write, so the reconciler still saw the OLD (matching) marker value and skipped the spawn — a false negative on the proof, not a defect in the production gate itself.
- **Fix:** Added an `Eventually` poll confirming `mgrClient.Get` observes the reset marker value before re-driving the completion call.
- **Files modified:** `internal/controller/reporter_spawn_idempotency_test.go`
- **Verification:** Ran the focused 3-spec suite 3 consecutive times (`go test ./internal/controller/... -ginkgo.label-filter='heavy' -ginkgo.focus='ReporterSpawnIdempotency'`) — all green, no flakes. Also verified in the full `make test-heavy` run.
- **Committed in:** `5bfd1a8` (part of the single Task 1 commit — caught during the same verification pass, no separate commit needed)

---

**Total deviations:** 1 auto-fixed (1 bug — test-harness cache-sync race, not a production defect)
**Impact on plan:** Test-only fix necessary for the proof to be deterministic under suite load. No scope creep; no production code touched.

## Issues Encountered
- `bin/setup-envtest use 1.33 --bin-dir bin -p path` (the plan's literal verify command) resolves `KUBEBUILDER_ASSETS` to a path relative to the invoking shell's cwd; since `go test` runs the compiled test binary with its cwd set to the package directory (`internal/controller/`), the relative path silently failed to locate `etcd`. Resolved by passing an absolute `--bin-dir "$(pwd)/bin"` — a shell-invocation nuance, not a code or plan defect, so not logged as a deviation.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Verification gap #2 (CR-01) is now fully closed: plan 47-07's implementation plus this plan's proof together demonstrate a TTL-GC'd reporter Job is never re-created for an already-observed completion, at all five spawn sites (three code shapes proven directly; phase/plan share milestone's exact code path).
- `make test-heavy` is green with the new suite in place — no regressions to the existing rollup-idempotency or reporter-spawn envtest coverage.
- No blockers for Phase 47 closeout; the remaining phase-level work (per STATE.md) is the human evidence review outstanding from `47-VERIFICATION.md`, unrelated to this plan's scope.

---
*Phase: 47-self-hosted-phoenix-install-end-to-end-proof*
*Completed: 2026-07-17*
