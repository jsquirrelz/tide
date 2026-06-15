---
phase: 18-eval-harness
plan: "01"
subsystem: eval
tags: [eval, goldie, byte-ratchet, prompt-templates, test-infra]
dependency_graph:
  requires: []
  provides: [internal/eval package, goldie golden baseline, byte-ratchet ceilings]
  affects: [make test, go test ./...]
tech_stack:
  added: [github.com/sebdah/goldie/v2 v2.8.0 (test-only)]
  patterns: [goldie golden snapshot testing, offline byte-count ratchet proxy]
key_files:
  created:
    - internal/eval/doc.go
    - internal/eval/render_test.go
    - internal/eval/testdata/goldie/project_planner.golden
    - internal/eval/testdata/goldie/milestone_planner.golden
    - internal/eval/testdata/goldie/phase_planner.golden
    - internal/eval/testdata/goldie/plan_planner.golden
    - internal/eval/testdata/goldie/task_executor.golden
    - internal/eval/testdata/ratchets/project_planner.txt
    - internal/eval/testdata/ratchets/milestone_planner.txt
    - internal/eval/testdata/ratchets/phase_planner.txt
    - internal/eval/testdata/ratchets/plan_planner.txt
    - internal/eval/testdata/ratchets/task_executor.txt
  modified:
    - go.mod
    - go.sum
decisions:
  - "Goldie -update generates golden files from deterministic fixedEnvelope fixture; ratchet ceilings set to golden file sizes (golden bytes == rendered bytes)."
  - "Provider.Params left nil in fixedEnvelope to avoid map-iteration nondeterminism per RESEARCH Q1/Pitfall 2."
  - "Ratchet ceiling = exact current byte count (not ceiling+N); any growth trips the gate, zero slack."
metrics:
  duration: "~20 minutes"
  completed: "2026-06-15T17:01:29Z"
  tasks_completed: 2
  files_created: 12
  files_modified: 2
  commits: 3
---

# Phase 18 Plan 01: Eval Package Foundation and Golden Baseline Summary

goldie snapshot testing + offline byte-count ratchet for all five compiled-in prompt templates (v1.0.1 baseline frozen).

## What Was Built

Established `internal/eval/` package with:

1. **`internal/eval/doc.go`** — package documentation with Apache 2.0 header, import boundary declaration (forbidden: `internal/controller`, `internal/budget`, `internal/metrics`, `api/v1alpha1`), and pointer to the offline ratchet design rationale (EVAL-01 artifact).

2. **`internal/eval/render_test.go`** — 10 tests (5 golden render + 5 byte ratchet) covering all five compiled-in prompt templates via `common.LoadPromptTemplate`:
   - `TestGoldenRender_{ProjectPlanner,MilestonePlanner,PhasePlanner,PlanPlanner,TaskExecutor}` — goldie snapshot assertions against committed `.golden` files
   - `TestByteRatchet_{ProjectPlanner,MilestonePlanner,PhasePlanner,PlanPlanner,TaskExecutor}` — offline byte-count gate; hard-fails if rendered length exceeds committed ceiling

3. **Five `.golden` files** — `testdata/goldie/` — v1.0.1 baseline renders from deterministic `fixedEnvelope` fixture (TaskUID fixed, nil Params).

4. **Five `.txt` ratchet ceilings** — `testdata/ratchets/` — byte counts at v1.0.1 un-trimmed levels:

   | Template | Ceiling (bytes) |
   |----------|----------------|
   | project_planner | 2471 |
   | milestone_planner | 2209 |
   | phase_planner | 2270 |
   | plan_planner | 4281 |
   | task_executor | 1960 |

5. **`go.mod` / `go.sum`** — `github.com/sebdah/goldie/v2 v2.8.0` added as test-only indirect dep.

## TDD Execution

| Gate | Commit | Status |
|------|--------|--------|
| RED (failing tests) | fc88e8a | Confirmed: all 10 tests failed (golden not found, ratchet files missing) |
| GREEN (implementation) | 08b0e56 | Confirmed: all 10 tests pass |

## Task Commits

| Task | Name | Commit | Key Files |
|------|------|--------|-----------|
| 1 | Add goldie dep + eval package doc | 21a98a6 | go.mod, go.sum, internal/eval/doc.go |
| 2 (RED) | Failing golden render + byte ratchet tests | fc88e8a | internal/eval/render_test.go |
| 2 (GREEN) | Goldie goldens and ratchet ceilings | 08b0e56 | testdata/goldie/*.golden, testdata/ratchets/*.txt |

## Verification Results

- `go test ./internal/eval/...` exits 0 — all 10 tests pass
- Runs twice with identical results (determinism confirmed)
- Ratchet tripwire confirmed: setting ceiling to 1 causes `TestByteRatchet_PlanPlanner` to fail with message naming the ratchet file
- `grep -q "WithFixtureDir(\"testdata/goldie\")" internal/eval/render_test.go` passes
- Exactly 5 `.golden` files and 5 `.txt` ratchet files exist
- `go build ./internal/eval/...` exits 0 (package compiles with no production-code deps)
- `internal/eval` imports only `internal/subagent/common` and `pkg/dispatch` from project packages — import boundary intact

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None.

## Threat Flags

None. This plan introduces only test-only code and testdata files with no network surface, no credentials, and no runtime-accessible endpoints.

## TDD Gate Compliance

- RED commit (test/...): fc88e8a — `test(18-01): add failing golden render + byte ratchet tests for all five templates`
- GREEN commit (feat/...): 08b0e56 — `feat(18-01): add goldie goldens and byte-ratchet ceilings for all five templates`

Both gates present in correct order. No REFACTOR step needed (no cleanup identified).

## Self-Check: PASSED

Files confirmed:
- internal/eval/doc.go: exists
- internal/eval/render_test.go: exists
- internal/eval/testdata/goldie/plan_planner.golden: exists
- internal/eval/testdata/ratchets/plan_planner.txt: exists (value: 4281)

Commits confirmed:
- 21a98a6: chore(18-01): add goldie/v2 test dep and create internal/eval package
- fc88e8a: test(18-01): add failing golden render + byte ratchet tests for all five templates
- 08b0e56: feat(18-01): add goldie goldens and byte-ratchet ceilings for all five templates
