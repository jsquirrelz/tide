---
phase: 26-multi-milestone-drive-spec-conformance
plan: "01"
subsystem: project-planner-template, depgraph, project-controller, docs
tags: [ms-01, ms-02, n-milestone, depgraph, template, planning-dag]
dependency_graph:
  requires: []
  provides: [N-milestone-authoring-path, zero-execution-edges-for-milestone-deps, n-milestone-idempotency-guard]
  affects: [internal/subagent/common/templates/project_planner.tmpl, internal/controller/depgraph.go, internal/controller/project_controller.go]
tech_stack:
  added: []
  patterns: [goldie-golden-update, strict-byte-ratchet, job-existence-idempotency-guard]
key_files:
  created: []
  modified:
    - internal/subagent/common/templates/project_planner.tmpl
    - internal/eval/testdata/goldie/project_planner.golden
    - internal/eval/testdata/goldie/project_planner_with_shared_context.golden
    - internal/eval/testdata/ratchets/project_planner.txt
    - internal/controller/depgraph.go
    - internal/controller/depgraph_test.go
    - internal/controller/project_controller.go
    - README.md
    - .planning/REQUIREMENTS.md
decisions:
  - "Milestone.dependsOn is a planning-DAG edge only — contributes zero execution edges (DEPS-02 reinterpretation)"
  - "buildGlobalEdges signature drops ms parameter after §6d removal (no unused-parameter lint offense)"
  - "Idempotency guard gates on tide-project-<uid>-1 Job existence, not owned-Milestone count (N-safe)"
metrics:
  duration: 8m
  completed_date: "2026-06-17T14:35:54Z"
  tasks_completed: 3
  files_modified: 9
---

# Phase 26 Plan 01: Multi-Milestone Template + §6d Removal Summary

**One-liner:** N-milestone project planner template with dependsOn wiring + §6d Milestone execution fan-out removed, enabling ζ to be free in Wave 0 of the global Execution DAG.

## Tasks Completed

| Task | Name | Commit | Key Files |
|------|------|--------|-----------|
| 1 | Rewrite project_planner.tmpl + regenerate golden/ratchet | 9b1f4f5 | project_planner.tmpl, project_planner.golden, project_planner.txt |
| 2 | Remove depgraph §6d + widen project_controller idempotency guard | 21926a4 | depgraph.go, project_controller.go |
| 2-fix | Remove unused ms param from buildGlobalEdges (lint fix) | c279972 | depgraph.go, depgraph_test.go, project_controller.go |
| 3 | README two-DAGs note + DEPS-02 reinterpretation | 05e2f5d | README.md, REQUIREMENTS.md |

## What Was Done

### Task 1 — project_planner.tmpl N-milestone emission

Removed the single-milestone constraint (`sole structural output is exactly one Milestone child-CRD`) and rewrote the HOW-TO-EMIT block to instruct the planner to emit **one file per milestone** in the DAG, each with `dependsOn` wired to its predecessors. The template now includes:

- Explicit scope instruction per Opus 4.x literal-interpretation guidance: "Emit one Milestone child-CRD per milestone in the DAG, each with its `dependsOn` wired to its predecessors"
- `dependsOn` field in the JSON shape (empty array for root milestone; predecessor names for later milestones)
- Concrete two-file example (foundation + surface)
- Updated artifact note: one MILESTONE.md per milestone or a combined MILESTONES.md

Golden regenerated from 2193 → 2668 bytes (run from worktree, not main repo). Ratchet updated to 2668. Both land in commit 9b1f4f5 per STRICT byte ratchet rule.

### Task 2 — §6d removal + idempotency guard

**depgraph.go:** Deleted the entire §6d block (Milestone-level DependsOn all-to-all fan-out). §6a/§6b/§6c (task/plan/phase fan-out) are preserved unchanged. Updated `buildGlobalEdges` doc comment to reference "6a–6c" and explain the planning-DAG-only nature of Milestone.dependsOn.

**project_controller.go:** Replaced the Milestone-count-based idempotency guard (bails on first owned Milestone, aborts N-milestone emit mid-stream) with a Job-existence check on `tide-project-<uid>-1`. Job presence is the stable "planner already dispatched" signal; N child Milestones materializing incrementally no longer trips the guard.

**Lint fix (c279972):** §6d removal made the `ms []tideprojectv1alpha2.Milestone` parameter unused. Dropped it from `buildGlobalEdges` signature and updated the call site in project_controller.go and four nil-arg sites in depgraph_test.go. Resolves the `unparam` lint offense introduced by Task 2.

### Task 3 — README + REQUIREMENTS.md

**README.md:**
- "Two distinct DAGs" section: added clarifying note that `Milestone.dependsOn` is a planning-DAG edge only, contributing zero execution edges; explains why ζ (Milestone B) is free in execution Wave 1 even when Milestone B's planning depends on Milestone A.
- "Two-DAG application" section: annotated both Planning DAG and Execution DAG bullets to distinguish Milestone-level DependsOn (planning order/gate-descent) from task/plan/phase DependsOn (execution edges).

**REQUIREMENTS.md:**
- DEPS-02 entry annotated with Phase 26 D-03 reinterpretation: §6b/6c fan-out retained; §6d Milestone all-to-all fan-out removed; DEPS-02 stays Complete (Phase 23).

## Verification Results

- `go test ./internal/eval/... -count=1` → PASS (golden matches + ratchet matches 2668)
- `go build ./internal/controller/...` → exit 0
- `go vet ./internal/controller/...` → clean
- `make verify-dag-imports` → OK: pkg/dag imports are clean
- golangci-lint on controller package: 5 pre-existing issues only (task_controller.go modernize, project_controller_cycle_test.go duplicate import, project_controller.go unparam on pre-existing functions); zero issues introduced by this plan
- `grep -c 'exactly one Milestone' project_planner.tmpl` → 0
- `grep -c 'per milestone' project_planner.tmpl` → 3
- `grep -c 'dependsOn' project_planner.tmpl` → 5
- `grep -c '6d. Milestone-level DependsOn fan-out' depgraph.go` → 0
- `grep -c '6c. Phase-level' depgraph.go` → 1
- `grep -c 'tide-project-%s-1' project_controller.go` → 1
- `grep -c 'existingMilestones' project_controller.go` → 0
- `grep -ciE 'planning-DAG edge' README.md` → 3
- DEPS-02 entry in REQUIREMENTS.md contains reinterpretation note
- Schedule line `[{α,β,γ,ζ}, {δ,η}, {ε,θ}]` unchanged in README

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed unused `ms` parameter in `buildGlobalEdges` after §6d removal**
- **Found during:** Task 2 post-commit lint check
- **Issue:** Removing §6d made the `ms []tideprojectv1alpha2.Milestone` parameter unused, triggering an `unparam` golangci-lint offense directly caused by the §6d deletion
- **Fix:** Removed `ms` from `buildGlobalEdges` function signature; updated call site in `project_controller.go:1533` and four nil-arg call sites in `depgraph_test.go`
- **Files modified:** `internal/controller/depgraph.go`, `internal/controller/project_controller.go`, `internal/controller/depgraph_test.go`
- **Commit:** c279972

## Known Stubs

None — all changes are functional (template instruction text, Go function deletion, doc updates).

## Threat Flags

None — no new network endpoints, auth paths, or schema changes at trust boundaries beyond what is already covered by T-26-01 and T-26-02 in the plan threat model.

## Self-Check: PASSED

All files found. All 4 commits verified in git log.
