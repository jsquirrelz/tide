---
phase: "07"
plan: "01"
subsystem: stub-subagent, integration-test
tags: [wave-0, tdd-red, test-scaffolds, planner-dispatch, layer-b]
dependency_graph:
  requires: []
  provides:
    - "cmd/stub-subagent/planner_test.go — RED unit tests for stub planner ChildCRD output per level (REQ-3)"
    - "test/integration/kind/testdata/bare-project.yaml — bare Project fixture for Layer B spec (REQ-5)"
  affects:
    - "cmd/stub-subagent (test coverage)"
    - "test/integration/kind (new fixture available for bare_project_test.go)"
tech_stack:
  added: []
  patterns:
    - "withWorkspaceRoot test isolation pattern (same package as main_test.go)"
    - "makePlannerEnvelope + assertSpecContainsKey helpers for key-presence assertions"
key_files:
  created:
    - "cmd/stub-subagent/planner_test.go"
    - "test/integration/kind/testdata/bare-project.yaml"
  modified: []
decisions:
  - "Tests call run() not dispatchPlannerSuccess directly — file compiles before Wave 1 implementation"
  - "Dev is nil in planner envelopes — planner dispatch must branch on Role, not TestMode"
  - "Key-presence assertions only (not value assertions) so tests survive child-name variants"
  - "spec.subagent.image omitted from fixture — controller uses helm default (stub image) in test env"
  - "spec.subagent.model: stub set as documentation intent; stub ignores model field"
metrics:
  duration: "8min"
  completed: "2026-05-31"
  tasks_completed: 2
  files_created: 2
---

# Phase 07 Plan 01: Wave 0 Test Scaffolds — Summary

**One-liner:** RED planner unit tests (6 test functions) + bare-Project Layer B fixture (no pre-applied Milestone) committed as Wave 0 scaffolds before any production code.

## What Was Built

### Task 1: cmd/stub-subagent/planner_test.go (RED unit tests)

Six test functions covering the stub planner ChildCRD contract:

- `TestPlannerProject` — asserts project level emits 1 Milestone ChildCRD with `projectRef`
- `TestPlannerMilestone` — asserts milestone level emits 1 Phase ChildCRD with `milestoneRef`
- `TestPlannerPhase` — asserts phase level emits 1 Plan ChildCRD with `phaseRef`
- `TestPlannerPlan` — asserts plan level emits 1 Task ChildCRD with `planRef` + `filesTouched` + `declaredOutputPaths` + `dev.testMode:"success"`
- `TestPlannerTaskLeaf` — asserts task level emits 0 ChildCRDs (leaf path; passes today)
- `TestExecutorPathUnchanged` — no-regression guard for the executor path (passes today)

Status: **intentionally RED** — 4 of 6 tests fail because `dispatchPlannerSuccess` does not exist yet. `go build ./cmd/stub-subagent/...` exits 0 (file compiles). Tests will turn GREEN when Plan 07-03 implements the stub planner branch.

### Task 2: test/integration/kind/testdata/bare-project.yaml

Bare Project fixture for the Layer B bare-project spec (Plan 07-05 / `bare_project_test.go`):
- Namespace `bare-project-test` + `kind: Project` (bare-project)
- `absoluteCapCents: 0` ($0 path — no LLM API cost)
- No `spec.git` block (D-04: must not trigger clone/push Jobs)
- No pre-applied Milestone (the ProjectReconciler self-bootstraps it via planner dispatch)
- All gates auto; `pauseBetweenWaves: false`
- Subagent image resolved from helm chart default (stub image in kind test environment)

## Verification Results

| Check | Result |
|-------|--------|
| `go build ./cmd/stub-subagent/...` exits 0 | PASS |
| `go test ./cmd/stub-subagent/...` exits non-zero (RED) | PASS (4 failures) |
| `grep -c "kind: Project" bare-project.yaml` = 1 | PASS |
| No `spec.git` in YAML spec body | PASS (comment mentions only) |
| `absoluteCapCents: 0` present | PASS |
| `milestone: auto` present | PASS |

## Deviations from Plan

None — plan executed exactly as written.

The plan's task action mentioned `spec.subagent.image: TIDE_STUB_IMAGE_PLACEHOLDER` as a placeholder, but confirmed that suite_test.go has no injection mechanism for fixture files (no `sed` replacement, no env substitution at `applyFile` time). The fixture correctly omits `spec.subagent.image` so the controller uses the helm chart default (`--subagent-image` flag set to stub image tag via helm `--set images.stubSubagent.tag=test`). This matches the pattern of all existing Layer B fixtures, none of which set `spec.subagent.image`.

## Commits

| Task | Commit | Files |
|------|--------|-------|
| Task 1: planner unit tests | 4528f0a | cmd/stub-subagent/planner_test.go |
| Task 2: bare-project fixture | f4ca2f3 | test/integration/kind/testdata/bare-project.yaml |

## Known Stubs

None — both files are scaffolds that are intentionally incomplete pending Wave 1 implementation. The RED tests are not stubs; they are intentional TDD RED gates per VALIDATION.md.

## Threat Flags

No new network endpoints, auth paths, file access patterns, or schema changes introduced. The two threat register entries from the plan (T-07-01-01, T-07-01-02) are both accepted — test files use t.TempDir() and the fixture has absoluteCapCents=0 with no providerSecretRef.

## Self-Check: PASSED

- `cmd/stub-subagent/planner_test.go` — FOUND
- `test/integration/kind/testdata/bare-project.yaml` — FOUND
- Commit 4528f0a — FOUND (git log confirmed)
- Commit f4ca2f3 — FOUND (git log confirmed)
