---
phase: 08-medium-sample-http-transport-and-production-git-transport-po
plan: "01"
subsystem: testing
tags: [ginkgo, envtest, kind, cel-validation, test-fixtures, git-transport]

# Dependency graph
requires: []
provides:
  - "test/integration/kind/testdata/bare-project.yaml migrated from file:// to RFC 2606 https:// sentinel"
  - "examples/projects/small/project.yaml migrated to https://git.example.internal/stub/no-such-repo.git sentinel"
  - "examples/projects/small/README.md updated: sentinel section replaces file:// placeholder rationale"
  - "test/integration/envtest/admission_test.go: Project CEL targetRepo Describe block (RED Test A, GREEN Tests B/C/D)"
  - "test/integration/kind/medium_http_test.go: Ginkgo spec skeleton for medium-http kind test (all Skip until 08-05+08-07)"
affects:
  - 08-03  # CEL marker change — admission_test.go Test A turns GREEN after 08-03 controller-gen + make helm
  - 08-05  # git-http server image + manifests — medium_http_test.go Specs 1+2 turn GREEN
  - 08-07  # CI nightly wiring — medium_http_test.go Spec 3 turns GREEN

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "RFC 2606 .example TLD sentinel for non-routable test targetRepo values"
    - "RED scaffold pattern: Ginkgo It blocks Skip immediately; assertions wired in later plans"
    - "Project CEL admission test pattern: same k8sClient/admissionNamespace shape as Plan Admission block"

key-files:
  created:
    - test/integration/kind/medium_http_test.go
  modified:
    - test/integration/kind/testdata/bare-project.yaml
    - examples/projects/small/project.yaml
    - examples/projects/small/README.md
    - test/integration/envtest/admission_test.go

key-decisions:
  - "RFC 2606 .example TLD chosen as sentinel (non-routable, intent unmistakable, passes https:// CEL rule)"
  - "Project CEL admission Test A is intentionally RED until 08-03 CEL marker change lands — scaffold approach preserves compile correctness while marking future GREEN point"
  - "medium_http_test.go uses Ordered + BeforeAll/AfterAll for namespace lifecycle (matches Ordered Describe convention in kind_integration package)"

patterns-established:
  - "Sentinel pattern: use https://git.example.internal/stub/no-such-repo.git for test fixtures that must pass future CEL validation but be ignored by stub-subagent"
  - "RED scaffold: create test file with Skip() blocks before production code exists; annotate with GREEN condition"

requirements-completed: []

# Metrics
duration: 15min
completed: 2026-06-03
---

# Phase 08 Plan 01: Medium HTTP Transport Wave 0 Summary

**Wave 0 fixture migration and test scaffolding: bare-project.yaml sentinel migrated, CEL admission tests added (RED for file:// rejection, GREEN for https/http/ssh acceptance), medium_http_test.go RED scaffold created for 08-05/08-07 wiring**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-06-03T00:00:00Z
- **Completed:** 2026-06-03T00:15:00Z
- **Tasks:** 3
- **Files modified:** 5

## Accomplishments

- Migrated all test YAML fixtures from `file://` targetRepo to `https://git.example.internal/stub/no-such-repo.git` sentinel (Pitfall 6 prevention — CEL change in 08-03 will not break Layer B suite)
- Added `Project CEL targetRepo admission` Describe block to envtest suite: Test A (file:// reject, RED until 08-03), Tests B/C/D (https/http/git@ accept, compile and pass immediately)
- Created `test/integration/kind/medium_http_test.go` as a RED scaffold for the hermetic medium-http kind test — three It blocks all Skip pending 08-05 (git-http server image) and 08-07 (CI nightly wiring)
- Updated `examples/projects/small/README.md` to describe RFC 2606 sentinel rationale; removed all references to old `file:///tmp/no-such-repo` placeholder

## Task Commits

1. **Task 1: Migrate all test fixtures from file:// to sentinel targetRepo** - `f284fd8` (feat)
2. **Task 2: Add Project CEL admission tests to envtest admission_test.go** - `740d8c1` (test)
3. **Task 3: Scaffold medium_http_test.go in Layer B kind suite** - `3059e7f` (test)
4. **gofmt fix for medium_http_test.go** - `9c851c7` (style)

## Files Created/Modified

- `test/integration/kind/testdata/bare-project.yaml` — targetRepo changed from `file:///tmp/no-such-repo` to `https://git.example.internal/stub/no-such-repo.git` sentinel; comment updated
- `examples/projects/small/project.yaml` — targetRepo changed to sentinel; inline comment updated to explain https:// requirement and RFC 2606 TLD
- `examples/projects/small/README.md` — "On the placeholder targetRepo" → "On the sentinel targetRepo" rewrite; all file:///tmp references removed
- `test/integration/envtest/admission_test.go` — New `Project CEL targetRepo admission` Describe block appended; 4 It blocks (A=RED/file://, B=GREEN/https://, C=GREEN/http://, D=GREEN/git@)
- `test/integration/kind/medium_http_test.go` — New Ginkgo spec file; Ordered Describe with BeforeAll/AfterAll; 3 It blocks all Skip pending 08-05/08-07

## Decisions Made

- RFC 2606 `.example` TLD chosen as sentinel: passes the upcoming CEL https:// rule; non-routable by DNS design; stub-subagent ignores targetRepo entirely (MEDIUM-7 lock)
- Project CEL Test A left RED intentionally: the test asserts the POST-08-03 CEL behavior; leaving it RED documents the expected GREEN point without requiring the CEL change to land first
- medium_http_test.go uses `Ordered` + `BeforeAll`/`AfterAll` for namespace lifecycle: the specs are sequential (init Job must complete before server Deployment can mount the PVC — Pitfall 3)

## Deviations from Plan

None - plan executed exactly as written.

The admission_test.go's `grep -rn 'targetRepo.*file://' test/` returns one Go source match (the test value passed to the API for rejection testing in the new Describe block). This is intentional — the grep must_have truth refers to YAML test fixtures, not Go source asserting rejection. All YAML fixtures return zero results.

## Issues Encountered

None.

## Known Stubs

- `test/integration/envtest/admission_test.go` Test A: asserts `apierrors.IsInvalid` on `file:///tmp/test` Project creation. This will FAIL at runtime until 08-03 CEL marker change + controller-gen + make helm regenerates the CRD schema. The test is marked with a `// RED until 08-03 lands` comment.
- `test/integration/kind/medium_http_test.go` Specs 1/2/3: all `Skip("pending 08-05")` or `Skip("pending 08-05 + 08-07 wiring")`. GREEN when 08-05 (git-http server image + manifests) and 08-07 (CI nightly wiring) land.

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes introduced in this plan. Changes are: YAML fixture value migration + test file additions (compile only, all Skip).

## Next Phase Readiness

- Wave 0 complete: fixture migration done before 08-03 CEL change lands — Layer B suite will remain green after 08-03
- 08-02 (revert 93595b9 core images) can proceed in parallel (no dependency on this plan's output)
- 08-03 (CEL marker change) will turn admission_test.go Test A GREEN
- 08-05 (git-http server image + manifests) will turn medium_http_test.go Specs 1+2 GREEN
- 08-07 (CI nightly wiring) will turn medium_http_test.go Spec 3 GREEN

---
*Phase: 08-medium-sample-http-transport-and-production-git-transport-po*
*Completed: 2026-06-03*

## Self-Check: PASSED

Files verified:
- `test/integration/kind/testdata/bare-project.yaml` — FOUND with git.example.internal sentinel
- `examples/projects/small/project.yaml` — FOUND with git.example.internal sentinel
- `examples/projects/small/README.md` — FOUND with sentinel section, no old placeholder
- `test/integration/envtest/admission_test.go` — FOUND with Project CEL Describe block
- `test/integration/kind/medium_http_test.go` — FOUND with 3 Skip It blocks

Commits verified:
- f284fd8 — feat(08-01): migrate test fixtures from file:// to https sentinel
- 740d8c1 — test(08-01): add Project CEL targetRepo admission tests (RED scaffold)
- 3059e7f — test(08-01): scaffold medium_http_test.go in Layer B kind suite (RED)
- 9c851c7 — style(08-01): gofmt fix for medium_http_test.go package doc comment
