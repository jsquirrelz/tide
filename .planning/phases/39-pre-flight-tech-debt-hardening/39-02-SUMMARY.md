---
phase: 39-pre-flight-tech-debt-hardening
plan: 02
subsystem: controller
tags: [controller-runtime, retry, optimistic-lock, budget, envtest, ginkgo]

# Dependency graph
requires:
  - phase: 39-pre-flight-tech-debt-hardening/39-01
    provides: chart configmap plannerConcurrency default fix (PREFLIGHT-01 parallel hardening)
provides:
  - project-level PlannerRolledUpUID stamp hardened with RetryOnConflict + MergeFromWithOptimisticLock (exactly-once, no best-effort window)
  - project_rollup_idempotency_test.go envtest proving no double-count across reporter-Job TTL-GC
affects: [35-infra-fresh-deploy, 37-launch-operate-run2]

# Tech tracking
tech-stack:
  added: ["k8s.io/client-go/util/retry (already a transitive dep; now imported in project_controller.go)"]
  patterns:
    - "RetryOnConflict + re-fetch latest + MergeFromWithOptimisticLock + return-error-on-exhaustion for all budget rollup marker stamps (now uniform across project/milestone/phase/plan levels)"

key-files:
  created:
    - internal/controller/project_rollup_idempotency_test.go
  modified:
    - internal/controller/project_controller.go

key-decisions:
  - "Project-level PlannerRolledUpUID stamp now returns an error on RetryOnConflict exhaustion rather than swallowing it — requeue is the correct behavior when the marker cannot be made durable"
  - "ReporterImage='' in the new test forces isFirstCompletion=true on every call without a PVC, mirroring the post-TTL-GC condition that exposes the double-count window"

patterns-established:
  - "Budget rollup marker stamp pattern (all 4 levels): re-fetch latest → idempotent short-circuit → MergeFromWithOptimisticLock → return error on exhaustion"

requirements-completed: [PREFLIGHT-02]

# Metrics
duration: 82min
completed: 2026-06-29
---

# Phase 39 Plan 02: Pre-flight Tech-Debt Hardening — PlannerRolledUpUID Summary

**Project-level budget rollup marker hardened from best-effort last-write-wins to RetryOnConflict + MergeFromWithOptimisticLock, closing the double-count window that could corrupt $100-cap accounting under TTL-GC; envtest proves exactly-once property.**

## Performance

- **Duration:** 82 min
- **Started:** 2026-06-29T22:01:00Z
- **Completed:** 2026-06-29T23:23:00Z
- **Tasks:** 2
- **Files modified:** 2

## Accomplishments

- Replaced the best-effort `client.MergeFrom` + logged-only error swallow at `project_controller.go:1376-1380` with a `retry.RetryOnConflict(retry.DefaultRetry, ...)` block that re-fetches the latest Project, short-circuits idempotently if the marker is already set, patches with `MergeFromWithOptimisticLock`, and returns an error on retry-budget exhaustion — matching the milestone/phase/plan controller pattern exactly (PREFLIGHT-02 / T-39-03)
- Added `k8s.io/client-go/util/retry` import to `project_controller.go` (already a transitive dependency; imported in `milestone_controller.go` since Phase 31)
- Created `internal/controller/project_rollup_idempotency_test.go` — a Ginkgo envtest (Label("envtest")) mirroring `child_rollup_idempotency_test.go` at the project level, proving: (1) accrual on first call and (2) no double-count on a second post-TTL-GC call where `isFirstCompletion=true` but `PlannerRolledUpUID==plannerJobName` short-circuits the rollup
- New test verified: 1/168 specs passed in 25s (`-ginkgo.focus=ProjectRollupIdempotency`); 1 Passed, 0 Failed

## Task Commits

Each task was committed atomically:

1. **Task 1: Port exactly-once RetryOnConflict stamp to project-level PlannerRolledUpUID** - `db7abe8` (fix)
2. **Task 2: Project-level envtest — no double-count across TTL-GC** - `057047b` (test)

## Files Created/Modified

- `internal/controller/project_controller.go` - Added `retry` import; replaced best-effort stamp at ~line 1373 with RetryOnConflict + re-fetch + MergeFromWithOptimisticLock + return-error-on-exhaustion pattern
- `internal/controller/project_rollup_idempotency_test.go` - New Ginkgo envtest: PREFLIGHT-02 accrual + idempotency-across-TTL-GC assertions using ReporterImage="" to force isFirstCompletion=true

## Decisions Made

- The stamp returns an error on RetryOnConflict exhaustion (not a log-only swallow) because the marker MUST be durable before the reporter Job's 300s TTL-GC window reopens the isFirstCompletion path — returning an error causes a requeue, giving the marker another chance to be written.
- ReporterImage="" in the test forces `isFirstCompletion=true` on every call without needing a PVC: when the reporter image is unconfigured, `spawnReporterIfNeeded` skips spawn and sets `isFirstCompletion=true` unconditionally, simulating the post-TTL-GC state where the reporter Job is absent.

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered

- `go build ./...` fails on `cmd/tide-demo-init` due to a missing `fixture/` directory (the directory is generated locally from `examples/tide-demo-fixture/` but is not tracked in git in the worktree). This is a pre-existing worktree issue unrelated to Phase 39 — confirmed by `git ls-tree HEAD cmd/tide-demo-init/` showing no `fixture` entry at HEAD. Build was verified via `go build ./internal/...` which succeeded.
- Layer B kind tests (caps AC5, credproxy HARN-03) showed flakiness during `make test-int`, but with `-ginkgo.flake-attempts=3` they pass on retry. Zero Phase-39 commits touch `test/integration/kind/` — confirmed by `git diff --stat HEAD~2 HEAD -- test/integration/` returning empty output.

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes introduced. The only change is to the marker-stamp logic inside an existing reconcile path (`handleProjectJobCompletion`), and the pattern narrows the race window rather than introducing new surface.

## Self-Check (pre-commit)

- `internal/controller/project_controller.go` — exists and contains `RetryOnConflict(`, `MergeFromWithOptimisticLock`, `fmt.Errorf("patch PlannerRolledUpUID`
- `internal/controller/project_rollup_idempotency_test.go` — exists (171 lines, created mode)
- `db7abe8` — fix(39-02) commit confirmed present: `git log --oneline -5` shows it
- `057047b` — test(39-02) commit confirmed present: `git log --oneline -5` shows it
- New test: 1/168 specs passed in 25s (TestControllers -ginkgo.focus=ProjectRollupIdempotency) — PASS confirmed

## Next Phase Readiness

- PREFLIGHT-02 satisfied: project-level rollup marker is now uniform with milestone/phase/plan level (RetryOnConflict + MergeFromWithOptimisticLock, no best-effort window)
- Phase 39 (PREFLIGHT-01 + PREFLIGHT-02) are both complete — pre-flight tech-debt hardening is done
- Phase 35 (Infra + Fresh v1.0.7 Deploy) can proceed: the two v1.0.7 load-bearing fixes are in place

---
*Phase: 39-pre-flight-tech-debt-hardening*
*Completed: 2026-06-29*
