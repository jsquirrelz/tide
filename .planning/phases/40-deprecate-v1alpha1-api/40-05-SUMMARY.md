---
phase: 40-deprecate-v1alpha1-api
plan: 05
subsystem: api
tags: [kubebuilder, crd, controller-gen, helm, makefile, go-test]

# Dependency graph
requires:
  - phase: 40-deprecate-v1alpha1-api (waves 1-2)
    provides: api/v1alpha3 served+storage, zero non-api/v1alpha1|2 importers
provides:
  - api/v1alpha1 and api/v1alpha2 Go packages fully deleted
  - version-agnostic, fail-closed verify-no-aggregates Makefile gate (api/v1alpha* glob)
  - PROJECT kubebuilder metadata repointed to api/v1alpha3 (6/6 resources), stale conversion:true dropped
  - relocated dogfood-fixture strict-decode test at test/schema/, v1alpha3-only, unit tier
  - 6 single-version (v1alpha3 served+storage) CRD manifests + matching chart templates
affects: [40-06 (docs sweep — migration doc v1alpha2-to-v1alpha3.md still pending), 41 (refactoring review)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Version-agnostic build-tooling globs with an explicit empty-glob fail-closed guard (prevents a future crank from silently disabling a CI gate)"

key-files:
  created:
    - test/schema/dogfood_manifests_test.go
  modified:
    - Makefile
    - PROJECT
    - config/crd/bases/tideproject.k8s_{milestones,phases,plans,projects,tasks,waves}.yaml
    - charts/tide-crds/templates/{milestone,phase,plan,project,task,wave}-crd.yaml
  deleted:
    - api/v1alpha1/** (14 files)
    - api/v1alpha2/** (11 files)

key-decisions:
  - "verify-no-aggregates hardened to api/v1alpha* glob + exit-1 empty-glob guard, landed in the same commit as the package deletion (D-12 mandatory — prevents the silently-dead-gate class)"
  - "dogfood_manifests_test.go relocated (not dropped): collapsed to v1alpha3-only, package test/schema, stays in the make test unit tier"
  - "aggregates_guard_test.go, phase3_schema_test.go, phase4_constants_test.go intentionally NOT relocated — their concerns are carried by the hardened gate and by api/v1alpha3/schema_test.go (landed in an earlier plan)"

patterns-established:
  - "Fail-closed empty-glob guard pattern for build-tooling gates that assert properties across a versioned package set"

requirements-completed: [CRANK-05]

duration: 19min
completed: 2026-07-12
---

# Phase 40 Plan 05: Delete api/v1alpha1+v1alpha2, Regenerate Single-Version CRDs Summary

**Deleted the api/v1alpha1 and api/v1alpha2 Go packages, hardened `verify-no-aggregates` to a version-agnostic fail-closed glob, fixed stale PROJECT kubebuilder metadata, and regenerated all 6 CRD manifests + chart templates down to a single v1alpha3 served+storage version block.**

## Performance

- **Duration:** 19 min
- **Started:** 2026-07-11T20:44:00-04:00 (approx.)
- **Completed:** 2026-07-11T21:03:00-04:00
- **Tasks:** 2
- **Files modified:** 39 (25 deleted, 1 created, 13 modified)

## Accomplishments
- `api/` now contains only `v1alpha3` — the two legacy schema packages (14 + 11 files) are gone
- `make verify-no-aggregates` can never again silently stop checking anything: the glob is version-agnostic (`api/v1alpha*/*_types.go`) and exits 1 with a clear message if it ever resolves to zero files
- The dogfood-fixture strict-decode coverage survived the deletion — relocated to `test/schema/dogfood_manifests_test.go`, confirmed present in the `make test` unit-tier package list
- All 6 CRD manifests (`config/crd/bases/*.yaml`) and their chart-template copies (`charts/tide-crds/templates/*.yaml`) now carry exactly one version block (`v1alpha3`, served+storage) — roughly half their prior size
- `PROJECT` kubebuilder metadata now correctly describes the current world (was stale since Phase 23): all 6 resource entries point at `api/v1alpha3`/`version: v1alpha3`, and the decorative-but-wrong `conversion: true` on the Plan entry (webhook retired Phase 23) is gone

## Task Commits

1. **Task 1: Relocate the dogfood test, delete both packages, repoint tooling** - `410c7c5` (feat)
2. **Task 2: Regenerate single-version CRD manifests + chart** - `93161ef` (feat)

_No plan-metadata commit yet — orchestrator commits STATE.md/ROADMAP.md centrally after the wave merges (worktree mode)._

## Files Created/Modified
- `test/schema/dogfood_manifests_test.go` - relocated dogfood strict-decode test (v1alpha3-only, unit tier)
- `Makefile` - `verify-no-aggregates` hardened to `api/v1alpha*` glob + empty-glob fail-closed guard; one stale `api/v1alpha1` comment reference in `verify-dispatch-imports` also fixed to `api/v1alpha*` (acceptance criteria required zero `v1alpha1`/`v1alpha2` hits in the Makefile)
- `PROJECT` - all 6 resource entries repointed to `api/v1alpha3`/`v1alpha3`; `conversion: true` dropped from the Plan entry
- `config/crd/bases/*.yaml` (6 files) - regenerated via `make manifests`, single v1alpha3 version block each
- `charts/tide-crds/templates/*.yaml` (6 files) - regenerated via `make helm-crds`, matches CRD bases
- `api/v1alpha1/**` (14 files, deleted) - legacy Go package, fully removed
- `api/v1alpha2/**` (11 files, deleted) - legacy Go package, fully removed

## Decisions Made
- Relocated the dogfood test BEFORE deleting the packages so the tree was never coverage-less at any commit (per plan's explicit ordering instruction).
- Kept the Makefile's version-agnostic glob as `api/v1alpha*/*_types.go` rather than pinning to `api/v1alpha3` literally — this is the durable form the plan's Open Question flagged; a future v1alpha4 crank needs zero Makefile changes to this gate.
- Left the `aggregates_guard_test.go`, `phase3_schema_test.go`, and `phase4_constants_test.go` test coverage dropped (not relocated) per the plan's explicit accounting: their concerns are carried by the hardened `verify-no-aggregates` gate itself and by `api/v1alpha3/schema_test.go` (landed in an earlier wave-1/2 plan).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed a second stale `api/v1alpha1` comment reference in the Makefile**
- **Found during:** Task 1 acceptance-criteria check (`grep -n 'api/v1alpha1\|api/v1alpha2' Makefile` must return 0 hits)
- **Issue:** `verify-dispatch-imports`'s comment block (line ~528) referenced `api/v1alpha1` by name in prose explaining why `pkg/dispatch` is allowed to import `k8s.io/apimachinery/pkg/runtime` — this comment predates the plan and wasn't in the plan's explicit read_first list, but the acceptance criteria for Task 1 explicitly demands zero `v1alpha1`/`v1alpha2` hits anywhere in the Makefile (not just the `verify-no-aggregates` target).
- **Fix:** Changed the comment to the version-agnostic `api/v1alpha*` (same substitution as the primary gate hardening).
- **Files modified:** `Makefile`
- **Verification:** `grep -n 'api/v1alpha1\|api/v1alpha2' Makefile` now returns 0 hits (exit 1); `grep -c 'api/v1alpha\*' Makefile` == 5.
- **Committed in:** `410c7c5` (Task 1 commit)

**2. [Rule 3 - Blocking] Materialized the gitignored `cmd/tide-demo-init/fixture/` directory to unblock `go build ./...`**
- **Found during:** Task 1 verification (`go build ./...`)
- **Issue:** `cmd/tide-demo-init/main.go` embeds `//go:embed all:fixture`, and `fixture/` is intentionally gitignored (materialized from `examples/tide-demo-fixture/` via `go generate`/Dockerfile per a documented D-B3/MEDIUM-11 lock). The fresh worktree checkout never had this directory materialized, so `go build ./...` and even `go list ./...` failed hard for the entire module (embed pattern errors abort package listing, not just that one package) — unrelated to this plan's v1alpha1/v1alpha2 changes.
- **Fix:** Ran `make demo-fixture` (the documented target, `go generate ./cmd/tide-demo-init/...`) to materialize the gitignored fixture directory locally. No source files changed; nothing new was committed (the directory stays gitignored).
- **Files modified:** none (generated content only, gitignored)
- **Verification:** `go build ./...` and `make test` both succeeded afterward.
- **Committed in:** N/A (gitignored, no commit)

---

**Total deviations:** 2 auto-fixed (1 bug, 1 blocking)
**Impact on plan:** Both were necessary to satisfy this plan's own literal acceptance criteria / verification commands. No scope creep — no files outside the plan's declared set were touched (the demo-fixture materialization is a local, gitignored build artifact, not a source change).

## Known Exceptions to Acceptance Criteria (documented, not fixed — out of declared scope)

- **`grep -rn 'v1alpha1\|v1alpha2' config/crd/bases/ charts/tide-crds/templates/` returns 1 hit (not 0):** `config/crd/bases/tideproject.k8s_projects.yaml:339` and `charts/tide-crds/templates/project-crd.yaml:341` both carry the string `docs/migration/v1alpha2-to-v1alpha3.md` inside a generated OpenAPI schema description. This is controller-gen faithfully reproducing a Go doc-comment on `ProjectSpec.SchemaRevision` in `api/v1alpha3/project_types.go` (a file from an earlier wave-1/2 plan, outside this plan's declared `files_modified`). The string is a correct forward-reference to the migration doc chapter D-06 mandates (`docs/migration/v1alpha2-to-v1alpha3.md`), which lands in the parallel Plan 40-06 (docs sweep) — not stale content, not an accidental leftover. Fixing it would require editing `api/v1alpha3/project_types.go`, which is out of this plan's scope and was not flagged as broken by any of this plan's tasks. Left as-is; will self-resolve once 40-06's migration doc exists (the string then matches a real file).

## Issues Encountered

None beyond the two auto-fixes documented above.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- `api/` is single-version (`v1alpha3` only); `go build ./...`, `make test`, `make verify-no-aggregates`, `go test ./test/schema/...`, `make verify-chart-reproducible`, and `make test-int-fast` all pass green.
- Gate liveness proven both directions: seeded a `CachedDag` token into `api/v1alpha3/project_types.go`, observed `make verify-no-aggregates` exit 1; reverted (confirmed via `git diff --stat` showing no residual diff), observed exit 0.
- Ready for Plan 40-06 (parallel, docs/README/SECURITY.md/config/samples sweep) to land the `docs/migration/v1alpha2-to-v1alpha3.md` migration doc, which will close the one known documented exception above.
- Ready for Plan 40-04 (parallel, internal/controller + internal/dispatch/podjob) — no file overlap; both worktrees stayed within their declared scopes.

## Self-Check: PASSED

- FOUND: test/schema/dogfood_manifests_test.go
- FOUND: api/v1alpha1 deleted
- FOUND: api/v1alpha2 deleted
- FOUND: .planning/phases/40-deprecate-v1alpha1-api/40-05-SUMMARY.md
- FOUND commit: 410c7c5 (Task 1)
- FOUND commit: 93161ef (Task 2)
- FOUND commit: d7c47c2 (SUMMARY.md)

---
*Phase: 40-deprecate-v1alpha1-api*
*Completed: 2026-07-12*
