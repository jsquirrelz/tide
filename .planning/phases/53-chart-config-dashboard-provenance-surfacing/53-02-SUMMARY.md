---
phase: 53-chart-config-dashboard-provenance-surfacing
plan: 02
subsystem: dispatch-config
tags: [go, verify-tier, loop-policy, fail-closed-parser, manager-wiring]

# Dependency graph
requires:
  - phase: 51
    provides: "VerificationSpec CRD field + Task loop dispatch sites"
  - phase: 52
    provides: "ResolveVerificationSpec/ResolveLoopPolicy per-level resolvers, projectLevelVerificationDefault"
provides:
  - "pkg/dispatch.ParseVerifyLevelDefaults — fail-closed per-level verify-posture JSON parser"
  - "internal/controller.VerifyDefaults struct threaded onto both PlannerReconcilerDeps and TaskReconcilerDeps"
  - "internal/controller.verificationEnabledForLevel — the single D-04 enablement chokepoint"
  - "cmd/manager --verify-levels-json flag + TIDE_VERIFIER_MODEL env read, both fail-closed at startup"
affects: [53-05-chart-surface, 53-06-dispatch-gates]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "pkg/dispatch fail-closed structured-config parser mirrored byte-for-byte from ParsePricingOverrides (closed keyspace variant)"
    - "One shared enablement chokepoint beside the existing precedence-walk resolver, not a per-dispatch-site check"

key-files:
  created:
    - pkg/dispatch/verify_defaults.go
    - pkg/dispatch/verify_defaults_test.go
    - internal/controller/verification_enabled_unit_test.go
  modified:
    - cmd/manager/main.go
    - internal/controller/dispatch_helpers.go
    - internal/controller/task_controller.go

key-decisions:
  - "verificationEnabledForLevel placed immediately beside ResolveVerificationSpec (not beside the new VerifyDefaults struct) per the plan's explicit D-04 placement instruction"
  - "TIDE_VERIFIER_MODEL defaults to empty string, preserving today's borrow-the-level-executor-model fallback until the chart supplies a value (53-05 scope)"

patterns-established:
  - "Closed-keyspace JSON validator: reject unknown level keys outright (task|plan|phase|milestone|project) rather than silently ignoring them, unlike PriceOverride's open model-ID keyspace"

requirements-completed: [CFG-01]

# Metrics
duration: 8min
completed: 2026-07-21
---

# Phase 53 Plan 02: Verify-Tier Config Parser + Manager Wiring + Enablement Chokepoint Summary

**Fail-closed `--verify-levels-json` parser + `TIDE_VERIFIER_MODEL` env read wired onto both reconciler Deps tiers, with one `verificationEnabledForLevel` chokepoint implementing D-04's authored-config-beats-chart-default precedence.**

## Performance

- **Duration:** 8 min
- **Started:** 2026-07-20T23:59:08-04:00 (first commit)
- **Completed:** 2026-07-21T00:07:06-04:00 (last commit)
- **Tasks:** 2 completed
- **Files modified:** 6 (3 created, 3 modified)

## Accomplishments
- `ParseVerifyLevelDefaults` fail-closed validator (closed level keyspace, `MaxIterations >= 0`, `OnExhaustion` enum) landed via a genuine RED→GREEN TDD cycle, mirroring `ParsePricingOverrides`' exact shape.
- Manager now fail-closes on malformed/out-of-range `--verify-levels-json` at startup (`os.Exit(1)`, verified live: `go run ./cmd/manager --verify-levels-json='{bad'` exits 1 with the exact error string).
- `VerifyDefaults{Image,Model,Levels}` constructed once in `main.go` and assigned onto both `plannerDeps` and `TaskReconcilerDeps.Deps` — no dispatch tier can silently lose chart enablement.
- `verificationEnabledForLevel` is the sole D-04 chokepoint: authored `Project.Spec.Verification.<level>` wins, else chart per-level `Enabled`, else off — proven by 6 genuinely-executing subtests in a standalone `TestX` file (not a `-ginkgo.focus` filter against the shared envtest suite).

## Task Commits

Each task was committed atomically:

1. **Task 1: ParseVerifyLevelDefaults — fail-closed per-level JSON validator** - `641b3d3e` (test, RED) → `b91f37c6` (feat, GREEN)
2. **Task 2: Manager wiring + VerifyDefaults on both Deps + verificationEnabledForLevel helper** - `a29958e3` (feat)

_TDD Task 1 landed as two commits (test → feat) per the RED/GREEN gate protocol; no REFACTOR commit was needed._

## Files Created/Modified
- `pkg/dispatch/verify_defaults.go` - `LevelVerifyDefault` type + `ParseVerifyLevelDefaults` fail-closed validator (D-01 chart transport schema)
- `pkg/dispatch/verify_defaults_test.go` - 8-subtest table-driven coverage (empty/empty-object, valid 5-level config, malformed JSON, unknown level key, negative maxIterations, invalid onExhaustion, whitespace-padded empty)
- `internal/controller/dispatch_helpers.go` - `VerifyDefaults` struct beside `ProviderDefaults`; `VerifyDefaults` field on `PlannerReconcilerDeps`; `verificationEnabledForLevel` beside `ResolveVerificationSpec`
- `internal/controller/task_controller.go` - `VerifyDefaults` field on `TaskReconcilerDeps` beside `VerifierImage`
- `internal/controller/verification_enabled_unit_test.go` - standalone `TestVerificationEnabledForLevel`, 6 subtests proving D-04 precedence + per-level pointer-field routing (no cross-level leakage)
- `cmd/manager/main.go` - `TIDE_VERIFIER_MODEL` env read; `--verify-levels-json` flag + fail-closed startup validation; one `VerifyDefaults` construction assigned onto both Deps literals

## Decisions Made
- Placed `verificationEnabledForLevel` immediately beside `ResolveVerificationSpec` (not beside the new `VerifyDefaults` struct near `ProviderDefaults`) per the plan interfaces block's explicit placement instruction — an initial draft placed it near the struct and was moved during self-review.
- Kept the `verifyLevelDefaults, err := pkgdispatch.ParseVerifyLevelDefaults(...)` call outside any `if verifyLevelsJSON != ""` guard (unlike the pricing block) because `ParseVerifyLevelDefaults` already fast-paths empty string to an empty map with nil error — one fewer branch, same fail-closed behavior.

## Deviations from Plan

None — plan executed exactly as written. The `verificationEnabledForLevel` placement was corrected during the same editing pass (not a deviation from the committed result, since only the final placement was committed).

## Issues Encountered
- `go build ./...` fails on a pre-existing, unrelated `cmd/tide-demo-init` embed pattern error (`pattern all:fixture: no matching files found`) — confirmed via `git log` that this file was untouched by this plan and the failure reproduces identically with this plan's changes fully removed. Verification instead scoped to `go build ./cmd/manager/... ./internal/controller/... ./pkg/dispatch/...`, which is clean.
- The `internal/controller` envtest Ginkgo suite (`TestControllers`) fails in this sandbox for lack of `/usr/local/kubebuilder/bin/etcd` (no `KUBEBUILDER_ASSETS`, no `setup-envtest` binary present) — a pre-existing environmental limitation, not caused by this plan. The new pure-function test (`TestVerificationEnabledForLevel`) runs and passes independently of the envtest `BeforeSuite`.
- `golangci-lint` is not installed in this sandbox; `go vet ./cmd/manager/... ./internal/controller/... ./pkg/dispatch/...` is clean as a substitute signal.

## User Setup Required

None.

## Next Steps
- Plan 53-05 wires the chart surface (`subagent.verify` values block → `TIDE_VERIFIER_MODEL`/`--verify-levels-json` env/args) that populates the `VerifyDefaults` this plan threads onto both reconciler Deps.
- Plan 53-06 adds the AND-gates at the three verifier dispatch sites (`task_controller.go`, `plan_controller.go`, `level_verify.go`) that call `verificationEnabledForLevel` — this plan intentionally makes no dispatch-site behavior change.

## Self-Check: PASSED

All 4 created/modified artifact files found on disk; all 4 commit hashes (`641b3d3e`, `b91f37c6`, `a29958e3`, `87c33fc5`) found in git history.
