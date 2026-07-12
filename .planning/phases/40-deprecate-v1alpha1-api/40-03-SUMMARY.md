---
phase: 40-deprecate-v1alpha1-api
plan: 03
subsystem: api
tags: [kubebuilder, crd, controller-gen, webhook, go, kind, envtest]

# Dependency graph
requires:
  - phase: 40-01
    provides: api/v1alpha3 Go package (compiling, deepcopy-generated) + 3-version transitional CRD manifests
  - phase: 40-02
    provides: decoupled envelope contract group (dispatch.tideproject.k8s/v1alpha1)
provides:
  - Every binary (manager, dashboard, tide CLI, tide-import, tide-reporter) and every test compiling and running against api/v1alpha3 only
  - Generalized checkSchemaRevisionGuard (expectedSchemaRevision + migrationGuideDocPath constants)
  - internal/webhook/v1alpha3 (whole-package rename from v1alpha2, all 3 webhooks serving v1alpha3)
  - Owner-ref resolution narrowed to tideproject.k8s/v1alpha3 only (dual-accept removed)
  - All live fixtures (kind testdata, examples, e2e testdata, salvage bundle) applying cleanly under v1alpha3
affects: [40-04, 40-05, 40-06, 40-07]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Version-crank guard generalization: two named constants (expectedSchemaRevision, migrationGuideDocPath) are the entire diff surface for a future vNext crank"
    - "Repack derived binary artifacts (pvc-envelopes.tgz) from their edited source directory rather than hand-patching the tarball — verified via the existing childcount integrity script"

key-files:
  created:
    - internal/webhook/v1alpha3/project_webhook.go
    - internal/webhook/v1alpha3/plan_webhook.go
    - internal/webhook/v1alpha3/wave_webhook.go
    - internal/webhook/v1alpha3/strict_mode.go
    - internal/webhook/v1alpha3/file_touch_utils.go
  modified:
    - internal/controller/project_controller.go (checkSchemaRevisionGuard generalized, D-04)
    - internal/controller/task_controller.go (owner-ref dual-accept removed, D-05)
    - internal/dispatch/podjob/backend.go (owner-ref dual-accept removed, D-05)
    - internal/controller/project_controller_v2_guard_test.go (dropped api/v1alpha1 cross-version scheme import)
    - cmd/manager/main.go (duplicate AddToScheme bug fixed; single v1alpha3 registration)
    - cmd/dashboard/main.go, cmd/tide/root_flags.go (scheme registration repointed)
    - cmd/dashboard/api/settings.go (ModelSelection replaced with Subagent.Levels resolution, D-10 fallout)
    - config/webhook/manifests.yaml, charts/tide/templates/validating-webhook-configuration.yaml (regenerated)
    - "~180 non-test + test files: import/alias repoint api/v1alpha2 -> api/v1alpha3"
    - test/integration/kind/testdata/**/*.yaml (apiVersion/schemaRevision bumped)
    - examples/projects/{small,medium,large}/project.yaml + examples/projects/dogfood/**/*.yaml
    - examples/projects/dogfood/salvage-20260618/** (CR YAMLs, envelope JSONs, pvc-envelopes.tgz repacked, seed regenerated)
    - test/e2e/testdata/live-claude-project.yaml (apiVersion bumped, schemaRevision field added)

key-decisions:
  - "cmd/dashboard/api/settings.go's ModelSelection read (Phase 37 dashboard-settings surface) is a real external consumer RESEARCH.md's 'zero readers outside api/' claim missed — replaced with Subagent.Levels[level].Model + Subagent.Model fallback rather than leaving a broken dashboard read"
  - "api/v1alpha1/dogfood_manifests_test.go's version-support switch case and supportedProjectAPIVersions map moved from v1alpha2 to v1alpha3 in the SAME commit as the dogfood YAML apiVersion bump (Task 2), not Task 1 — keeps the fixture and its validator co-changed and green at every commit boundary"
  - "pvc-envelopes.tgz (the artifact tide import-envelopes actually reads) is a derived binary distinct from the uncompressed pvc-envelopes/ directory (which scripts/check-salvage-childcount.sh reads) — repacked the tgz from the edited directory rather than leaving it stale, verified via the childcount script"
  - "Historical events.jsonl transcripts inside the salvage bundle are left untouched even though they contain literal 'v1alpha2'/'modelSelection' substrings — they are dated records of what the original salvaged run actually saw, same preservation principle as docs/audit/*.md (D-12)"

patterns-established:
  - "A version-crank's fail-closed guard is exactly two constants (expectedSchemaRevision, migrationGuideDocPath) plus a comment; the message and condition-setting logic stay untouched across future cranks"

requirements-completed: [CRANK-03]

# Metrics
duration: ~45min
completed: 2026-07-11
---

# Phase 40 Plan 03: Consumer Migration to api/v1alpha3 Summary

**Atomic repoint of every TIDE consumer (~180 non-test + ~150 test/fixture files) from api/v1alpha2 to api/v1alpha3: webhook package rename, scheme-registration dedup bug fix, generalized SchemaRevision guard (D-04), owner-ref dual-accept removal (D-05), and a full YAML/JSON fixture sweep including salvage-bundle tarball repacking.**

## Performance

- **Duration:** ~45 min
- **Tasks:** 2 completed
- **Files modified:** 331 (188 in Task 1, 154 in Task 2, some overlapping across the range)

## Accomplishments

- Every Go consumer of `api/v1alpha2` (controllers, webhooks, dispatch, CLI, dashboard, gates, budget, reporter — ~180 files) now imports `api/v1alpha3`; `internal/webhook/v1alpha2` renamed whole-package to `internal/webhook/v1alpha3` (markers, logger names, wiring call sites all repointed).
- Fixed a real pre-existing bug while touching `cmd/manager/main.go`'s scheme block: a duplicate `tidev1alpha2.AddToScheme(scheme)` call collapsed to a single `tidev1alpha3.AddToScheme(scheme)`, and the stale "v1alpha1 remains registered" comment (false since D-01's reinstall-only model) removed.
- `checkSchemaRevisionGuard` generalized under D-04: `expectedSchemaRevision`/`migrationGuideDocPath` constants are now the entire diff surface a future v1alpha4 crank needs to touch.
- D-05: owner-ref resolution in `task_controller.go` and `podjob/backend.go` narrowed from a `v1alpha1 || v1alpha3` dual-accept to a single `GroupVersion.String()` check; 7 test fixtures constructing hardcoded `v1alpha1` owner refs repointed.
- Full YAML/JSON fixture sweep: 13 kind testdata fixtures, 3 example project manifests, all 4 dogfood project YAMLs (+ run-2), and the entire salvage-20260618 bundle (CR YAMLs, 115 envelope in/out.json files, the push envelope, and the checked-in `pvc-envelopes.tgz` — repacked from the edited directory and re-verified via `scripts/check-salvage-childcount.sh`).
- `make test` (unit tier) and `make test-int-fast` (Layer A envtest, 55/55 specs) both green after each task commit; `make verify-chart-reproducible` and `make verify-import-firewall` (credproxy zero-`api/` imports) both pass.

## Task Commits

1. **Task 1: Atomic code crank — imports, webhook package, schemes, guard, Go literals, comments** - `03f8d8b` (feat)
2. **Task 2: Owner-ref dual-accept removal + YAML fixture/testdata sweep + salvage regeneration** - `8494f9f` (feat)

_No plan-metadata commit — this worktree agent does not update STATE.md/ROADMAP.md; the orchestrator commits those after the wave merges._

## Files Created/Modified

- `internal/webhook/v1alpha3/*.go` - whole-package rename from `internal/webhook/v1alpha2` (git mv + package decl + marker + logger repoint)
- `internal/controller/project_controller.go` - `checkSchemaRevisionGuard` generalized (D-04 constants)
- `internal/controller/task_controller.go`, `internal/dispatch/podjob/backend.go` - owner-ref dual-accept removed (D-05)
- `internal/controller/project_controller_v2_guard_test.go` - rewritten to construct old/stale-shape Projects directly on the current type, no cross-version scheme import
- `cmd/manager/main.go`, `cmd/dashboard/main.go`, `cmd/tide/root_flags.go` - scheme registration repoint + duplicate-registration bug fix + stale comment sweep
- `cmd/dashboard/api/settings.go` + `settings_test.go` - `ModelSelection` (dropped by D-10) replaced with `Subagent.Levels`-based resolution
- `config/webhook/manifests.yaml`, `charts/tide/templates/validating-webhook-configuration.yaml` - regenerated via `make manifests && make helm-controller`
- `test/e2e/testdata/live-claude-project.yaml` - apiVersion bumped, missing required `schemaRevision` field added
- `examples/projects/dogfood/salvage-20260618/**` - CR YAMLs, 115 envelope JSONs, salvage `project.yaml`/`seed-manifest.json` (regenerated), `pvc-envelopes.tgz` (repacked)
- ~150 additional test/fixture files - mechanical import repoint or apiVersion/schemaRevision literal bump

## Decisions Made

- **`cmd/dashboard/api/settings.go`'s `ModelSelection` read is a real consumer the phase's research missed** — fixed with `Subagent.Levels[level].Model` + `Subagent.Model` fallback rather than leaving a broken build (Rule 1).
- **`api/v1alpha1/dogfood_manifests_test.go`'s v1alpha2→v1alpha3 case-label flip was deliberately deferred from Task 1 to Task 2** so the test and the YAML fixtures it validates land in the same commit, never green-then-red across the plan's own task boundary.
- **`pvc-envelopes.tgz` is a derived artifact distinct from the uncompressed `pvc-envelopes/` directory** — the CLI's live import path (`cmd/tide/import_envelopes_run.go`) reads the `.tgz` directly, so editing only the directory would have left the actually-consumed artifact stale. Repacked with `tar --format=ustar` matching the original entry layout, verified via `scripts/check-salvage-childcount.sh`.
- **Historical `events.jsonl` transcripts inside the salvage bundle are untouched** even though they contain literal `v1alpha2`/`modelSelection` substrings (captured stdout from the original run's own `grep`/`cat` commands) — same preservation principle as `docs/audit/*.md` (D-12): these are dated records, not live config.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] `cmd/dashboard/api/settings.go` broke on D-10's `ModelSelection` removal**
- **Found during:** Task 1 (`go build` after the import repoint)
- **Issue:** Plan 40-01 dropped `ProjectSpec.ModelSelection` from v1alpha3 (D-10) based on RESEARCH.md's "zero readers outside api/" claim, but the Phase 37 dashboard settings surface reads `p.Spec.ModelSelection.{Milestone,Phase,Plan,Task}` — the repoint compiled against v1alpha3 and broke immediately.
- **Fix:** Added `resolvedLevelModel(levelCfg *LevelConfig, fallback string) string` and wired `Subagent.Levels.{Milestone,Phase,Plan,Task}` with `Subagent.Model` fallback — the already-wired resolution chain `ModelSelection` was a dead duplicate of.
- **Files modified:** `cmd/dashboard/api/settings.go`, `cmd/dashboard/api/settings_test.go`
- **Verification:** `go build ./...` green; `TestSettingsFullyPopulated`/`TestSettingsHonestDefaults` pass.
- **Committed in:** `03f8d8b` (Task 1 commit)

**2. [Rule 3 - Blocking] `test/integration/envtest/admission_test.go` hardcoded the old webhook directory path as separate `filepath.Join` args**
- **Found during:** Task 1 comment/reference sweep (missed by the initial `grep -rl 'api/v1alpha2\|webhook/v1alpha2'` import-repoint pass because `filepath.Join("internal", "webhook", "v1alpha2")` has no literal `"webhook/v1alpha2"` substring)
- **Issue:** The PLAN-03 cycle-recovery AST-scan test walked a directory that no longer existed post-rename (`internal/webhook/v1alpha2`), which would silently `filepath.WalkDir` over nothing and false-pass.
- **Fix:** Repointed to `"internal", "webhook", "v1alpha3"`.
- **Files modified:** `test/integration/envtest/admission_test.go`
- **Verification:** `make test-int-fast` — 55/55 specs green.
- **Committed in:** `03f8d8b` (Task 1 commit)

**3. [Rule 1 - Bug] `test/e2e/testdata/live-claude-project.yaml` was missing the required `schemaRevision` field entirely**
- **Found during:** Task 2 (E2E testdata bump)
- **Issue:** This fixture predates the `+kubebuilder:validation:Required` marker on `SchemaRevision` — it would fail-closed with `RequiresReinstall` on apply even after the apiVersion bump.
- **Fix:** Added `schemaRevision: v1alpha3` alongside the apiVersion bump.
- **Files modified:** `test/e2e/testdata/live-claude-project.yaml`
- **Verification:** Field presence confirmed by inspection; this fixture is build-tag-gated (`//go:build live-e2e`) and not exercised by `make test`/`make test-int-fast`.
- **Committed in:** `8494f9f` (Task 2 commit)

**4. [Rule 1 - Bug] `examples/projects/dogfood/salvage-20260618/projects.yaml` embedded a stale `modelSelection: {}` field**
- **Found during:** Task 2 (salvage bundle sweep, per plan's explicit D-10 removal instruction)
- **Issue:** The dead field (removed from v1alpha3 by D-10) was present in the salvaged CR YAML's plain-YAML spec block.
- **Fix:** Deleted the line per the plan's explicit instruction.
- **Files modified:** `examples/projects/dogfood/salvage-20260618/projects.yaml`
- **Verification:** `grep -rn 'modelSelection' examples/` returns 0 hits outside historical `events.jsonl` transcripts.
- **Committed in:** `8494f9f` (Task 2 commit)

---

**Total deviations:** 4 auto-fixed (2 Rule 1 bugs discovered by the migration itself, 1 Rule 1 missing-required-field, 1 Rule 3 blocking stale path)
**Impact on plan:** All four are direct fallout of the version crank surfacing latent staleness/bugs (a dead field, a missed consumer, a stale hardcoded path, a fixture that predates a schema requirement) — no scope creep beyond what the crank itself required to stay green.

## Issues Encountered

- **`grep -rl 'api/v1alpha2\|webhook/v1alpha2'` under-enumerated by one file class**: `filepath.Join("internal", "webhook", "v1alpha2")` (separate string args) doesn't contain the literal substring `"webhook/v1alpha2"` the initial enumeration grep searched for. Caught by a broader `git diff --name-only | xargs grep -in 'v1alpha1'` comment sweep pass before committing. No other instances of this pattern found repo-wide.
- **`api/v1alpha2/schema_test.go` was incorrectly swept up by the bulk import-repoint sed** (a filter-pattern bug: `grep -v '^./api/v1alpha2'` doesn't match paths without a literal leading `./`) — caught before committing by checking `git status --short api/v1alpha2/` and reverted via `git checkout -- api/v1alpha2/schema_test.go`. This file legitimately tests `api/v1alpha2`'s own types and must stay self-referential until that package is removed in a later plan.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- All consumers compile and test green on `api/v1alpha3`; the guard fail-closes on non-`v1alpha3` revisions; webhooks serve `v1alpha3` paths; every live fixture (kind Layer A, examples, salvage bundle) applies cleanly under the new schema.
- `api/v1alpha1` and `api/v1alpha2` packages themselves are UNTOUCHED (by design — this plan migrated consumers only). Plan 40-04+ owns the actual package removal, the `subagent.levels` semantic rename call-site edits, and the docs/samples sweep.
- Full `make test-int` (Layer B kind) deferred to the phase gate (plan 40-07) per VALIDATION.md sampling, as the plan's own `<verification>` section specifies.

---
*Phase: 40-deprecate-v1alpha1-api*
*Completed: 2026-07-11*

## Self-Check: PASSED

- Verified 11 key created/modified files exist on disk (webhook package files, guard/owner-ref sites, dashboard settings, salvage tarball, e2e testdata, this summary).
- Verified all 3 commit hashes (`03f8d8b`, `8494f9f`, `5d136c6`) exist in `git log --oneline --all`.
