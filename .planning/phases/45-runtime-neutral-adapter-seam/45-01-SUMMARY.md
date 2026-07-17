---
phase: 45-runtime-neutral-adapter-seam
plan: 01
subsystem: observability
tags: [opentelemetry, openinference, tracing, adapter-seam, go]

# Dependency graph
requires:
  - phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary
    provides: internal/reporter/tracesynth.go (the events.jsonl message-array-span synthesizer this plan wraps), ReporterOptions/BuildReporterJob's Args-based transport convention, all 5 reporter-spawn call sites
provides:
  - pkg/dispatch.SelfInstruments(vendor string) bool — the ADAPT-01 capability-routing datum, fail-closed default
  - ReporterOptions.SkipMessageSpans bool + BuildReporterJob's --skip-message-spans bareword Args append (both Job shapes)
  - Manager-side wiring at all 5 reporter-spawn sites (milestone/phase/plan/project/task) computing the flag from a fresh ResolveProvider(...).Vendor call
affects: [45-02-runtime-neutral-adapter-seam, 46-span-enrichment-dashboard-deep-link]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Capability-as-data lookup (pkg/dispatch.SelfInstruments) instead of per-runtime if/switch branches at call sites — the same 'provider identity derived from dispatch data' move Phase 42 D-07 established for the vendor string itself"
    - "Fresh, independent ResolveProvider(project, level, ...) call at each of 5 completion sites (recompute, don't thread a return value) — mirrors span_emission.go's established 'second, envelope-independent call' precedent"

key-files:
  created:
    - pkg/dispatch/vendor_capabilities.go
    - pkg/dispatch/vendor_capabilities_test.go
  modified:
    - internal/controller/reporter_jobspec.go
    - internal/controller/reporter_jobspec_test.go
    - internal/controller/dispatch_helpers.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/task_controller.go

key-decisions:
  - "SelfInstruments is a bare func over a switch (no Capabilities struct) — only one capability bit exists today; D-01/discretion call from CONTEXT.md"
  - "--skip-message-spans is a bareword flag (not =value), appended only when true, so Go zero-value false satisfies D-03's absent-means-synthesize rule at every layer"
  - "task_controller.go computes skipMessageSpans ONCE inside spawnTaskTraceReporterIfNeeded's body, not at each of its 2 call sites (success/failure paths both route through the one computation)"

requirements-completed: [ADAPT-01]

duration: 12min
completed: 2026-07-16
---

# Phase 45 Plan 01: Runtime-Neutral Adapter Seam (manager side) Summary

**New `pkg/dispatch.SelfInstruments` fail-closed vendor-capability lookup, threaded as a `--skip-message-spans` bareword Job Arg through `ReporterOptions`/`BuildReporterJob`, and wired at all 5 reporter-spawn call sites (milestone/phase/plan/project/task) from a fresh `ResolveProvider(...).Vendor` call each — zero per-runtime branches, behavior byte-identical today since every vendor resolves false.**

## Performance

- **Duration:** 12 min (first task commit 22:33:55 → last task commit 22:42:11)
- **Started:** 2026-07-16T22:30:35-04:00 (worktree base)
- **Completed:** 2026-07-16T22:42:11-04:00
- **Tasks:** 3 completed
- **Files modified:** 8 (2 created, 6 modified)

## Accomplishments
- `pkg/dispatch.SelfInstruments(vendor string) bool` lands as the ADAPT-01 routing datum: a fail-closed switch over the 5 canonical vendor literals plus a `default` arm, all returning `false` today, with a doc contract citing D-02 (manager-computed/Job-carried trust) and D-03 (default-safe polarity) and naming `internal/reporter/tracesynth.go` as the anthropic-CLI adapter it routes around.
- `ReporterOptions.SkipMessageSpans` + `BuildReporterJob`'s conditional `--skip-message-spans` Args append land immediately after the existing `--traceparent` block, so the flag composes uniformly with both the materialization and trace-only Job shapes (D-05) via Args-only transport (D-04) — no Env entry added.
- All 5 reporter-spawn call sites (`milestone_controller.go`, `phase_controller.go`, `plan_controller.go`, `project_controller.go`, `task_controller.go`) now compute `skipMessageSpans` from a fresh `pkgdispatch.SelfInstruments(ResolveProvider(project, "<level>", r.Deps.HelmProviderDefaults).Vendor)` call using the same level literal their neighboring `synthesizePlannerSpan` call already uses, and thread it into `ReporterOptions`/`spawnReporterIfNeeded`. Zero `if vendor ==` branches anywhere in `internal/controller`.

## Task Commits

Each task was committed atomically (Tasks 1 and 2 are TDD: RED then GREEN):

1. **Task 1: SelfInstruments vendor-capability table + D-10 polarity guard tests**
   - `00e6f06` (test) — failing `TestSelfInstruments_*` tests, build fails (function undefined) = RED
   - `31cf7f0` (feat) — `pkg/dispatch/vendor_capabilities.go` implementation, both tests PASS = GREEN
2. **Task 2: ReporterOptions.SkipMessageSpans + --skip-message-spans Args append**
   - `60c219b` (test) — failing `TestBuildReporterJob_SkipMessageSpansArg`, build fails (unknown struct field) = RED
   - `825bb82` (feat) — field + Args-append implementation, all 3 subtests PASS = GREEN
3. **Task 3: Compute the flag at all 5 reporter-spawn sites**
   - `d505fe2` (feat) — `spawnReporterIfNeeded` signature extension + inline computations at all 5 controller files

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified
- `pkg/dispatch/vendor_capabilities.go` - `SelfInstruments(vendor string) bool`, fail-closed switch, D-08 doc contract
- `pkg/dispatch/vendor_capabilities_test.go` - `TestSelfInstruments_KnownVendorsDefaultFalse` + `TestSelfInstruments_UnknownVendorDefaultsFalse` (D-10)
- `internal/controller/reporter_jobspec.go` - `ReporterOptions.SkipMessageSpans` field + conditional Args append in `BuildReporterJob`
- `internal/controller/reporter_jobspec_test.go` - `TestBuildReporterJob_SkipMessageSpansArg` (3 subtests: both Job shapes present, absent when false)
- `internal/controller/dispatch_helpers.go` - `spawnReporterIfNeeded` gains trailing `skipMessageSpans bool` param, threaded into its `ReporterOptions` literal
- `internal/controller/milestone_controller.go` - `skipMessageSpans := pkgdispatch.SelfInstruments(ResolveProvider(project, "milestone", ...).Vendor)` before the spawn call
- `internal/controller/phase_controller.go` - same shape with literal `"phase"`
- `internal/controller/plan_controller.go` - same shape with literal `"plan"`, inline `ReporterOptions` literal
- `internal/controller/project_controller.go` - same shape with literal `"project"`, inline `ReporterOptions` literal
- `internal/controller/task_controller.go` - computed once inside `spawnTaskTraceReporterIfNeeded`, literal `"task"`; the two call sites at handleJobCompletion's success/failure paths are unchanged

## Decisions Made
- `SelfInstruments` is a bare func over a `switch`, not a `Capabilities` struct — only one capability bit exists (CONTEXT.md discretion call, D-01).
- Bareword `--skip-message-spans` (not `=value`) — matches `--trace-only`'s existing shape and makes Go's stdlib `flag.Bool` presence=true/absence=false do D-03's "absent means synthesize" for free at the reporter (reporter-side flag parsing is out of this plan's scope, landing in the sibling plan that touches `cmd/tide-reporter`).
- No test-injection hook or package-level table variable was added to `pkg/dispatch` — tests call `SelfInstruments` directly, keeping the production table unpolluted, per CONTEXT.md's explicit discretion grant.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Reworded a doc comment that accidentally broke the plan's exact-5-hits acceptance check**
- **Found during:** Task 3, post-implementation verification
- **Issue:** The `spawnReporterIfNeeded` doc comment I wrote for the new `skipMessageSpans` param literally contained the string `pkgdispatch.SelfInstruments(ResolveProvider(project, level, ...).Vendor)`, which matched the plan's verification grep (`grep -rln "SelfInstruments(ResolveProvider" internal/controller | wc -l`) as a 6th file, breaking the "= 5" acceptance criterion and the ADAPT-01 "flag travels as data ... never a hard-coded per-runtime branch" success-criterion grep gate.
- **Fix:** Reworded the doc comment to describe the mechanism in prose ("the caller's fresh vendor capability lookup result (pkgdispatch.SelfInstruments on the level's resolved ProviderSpec.Vendor)") without literally reproducing the grep-matched call expression.
- **Files modified:** `internal/controller/dispatch_helpers.go`
- **Verification:** `grep -rln "SelfInstruments(ResolveProvider" internal/controller | wc -l` now returns exactly `5`; `go build ./...` and the full envtest suite (`go test ./internal/controller/... ./pkg/dispatch/...`) still pass.
- **Committed in:** `d505fe2` (part of Task 3 commit — caught before commit, not a separate fix commit)

---

**Total deviations:** 1 auto-fixed (1 bug, self-caught before commit)
**Impact on plan:** No scope creep — a same-task self-correction to an in-progress doc comment before it was committed.

## Issues Encountered
- `go build ./...` initially failed on `cmd/tide-demo-init/main.go` with a missing `//go:embed all:fixture` directory. This is pre-existing and unrelated to this plan's files (confirmed via `git log` on that file and the Makefile's `demo-fixture` target, which materializes a gitignored fixture directory via `go generate`). Ran `make demo-fixture` to unblock local verification; no source changes made, and the fixture directory remains gitignored (confirmed via `git check-ignore`).
- The local envtest control-plane binaries (etcd/kube-apiserver) were not yet downloaded in this worktree. Ran `make setup-envtest` (downloads to `bin/k8s/...`, not committed) and set `KUBEBUILDER_ASSETS` for the verification run — standard one-time per-worktree setup, not a code issue.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

The manager-side half of ADAPT-01 is complete: the capability flag travels as data from `ResolveProvider`'s `Vendor` through `SelfInstruments` into the reporter Job's Args at all 5 spawn sites, with zero per-runtime branches, and behavior is unchanged today (every vendor resolves false). The reporter-side consumption of `--skip-message-spans` (parsing the flag in `cmd/tide-reporter/main.go` and gating `synthesizeSpans`) plus the D-09 contract test with a stub self-instrumenting runtime were NOT part of this plan's scope (Tasks 1-3 only cover `pkg/dispatch` + the manager's `internal/controller` half per this plan's `files_modified` list) — that work belongs to a sibling plan in this phase (or a subsequent wave) before ADAPT-01's full 3-criteria success bar is met end-to-end.

---
*Phase: 45-runtime-neutral-adapter-seam*
*Completed: 2026-07-16*

## Self-Check: PASSED

All created/modified files verified present on disk; all 6 commit hashes (00e6f06, 31cf7f0, 60c219b, 825bb82, d505fe2, 3b966b6) verified present in git log.
