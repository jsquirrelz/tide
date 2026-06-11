---
phase: 10-task-execution-reliability-clone-idempotency-per-run-workspa
plan: "04"
subsystem: dashboard-api
tags: [dashboard, api, cross-namespace, bug-fix, tdd]
dependency_graph:
  requires: []
  provides: [dashboard-project-detail-cross-namespace-get]
  affects: [cmd/dashboard/api/projects.go]
tech_stack:
  added: []
  patterns: [cross-namespace-list-fallback, buildDetail-helper]
key_files:
  created: []
  modified:
    - cmd/dashboard/api/projects.go
    - cmd/dashboard/api/projects_test.go
decisions:
  - "Cross-namespace Get fallback uses ProjectList (no filter) + first-match-by-name, mirrors List handler's all-namespace behavior"
  - "buildDetail helper extracts child-listing with p.Namespace throughout — prevents RESEARCH Pitfall 4 (child lists using stale outer namespace variable)"
  - "Fast path preserved: explicit ?namespace= still uses direct h.Client.Get (no list overhead)"
metrics:
  duration: 2m
  completed_date: "2026-06-09"
  tasks: 1
  files_modified: 2
---

# Phase 10 Plan 04: Dashboard Cross-Namespace Project Get Fix Summary

**One-liner:** Cross-namespace Get fallback via ProjectList + first-match-by-name; `buildDetail` helper pins all child Lists to `p.Namespace`.

## What Was Built

Fixed `ProjectsHandler.Get` in `cmd/dashboard/api/projects.go` (SC-7): removed the `namespace = "default"` fallback that caused 404 for any project not in the default namespace. The dashboard's List endpoint already searched all namespaces; Get now behaves consistently.

Two changes:

1. **`buildDetail(ctx, p)` helper** — extracted the inline child-listing logic (MilestoneList, PhaseList, PlanList) into a method. All three `h.Client.List` calls use `client.InNamespace(p.Namespace)`, never any outer namespace variable. This is RESEARCH Pitfall 4's prescribed fix.

2. **Rewritten `Get` handler** — two paths:
   - Fast path (`namespace != ""`): direct `h.Client.Get` by key → `buildDetail`. Preserves NotFound→404, error→500 behavior.
   - Cross-namespace fallback (`namespace == ""`): `h.Client.List` across all namespaces → loop to find first `Name == name` → `buildDetail`. No match → 404.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 (RED) | Add failing cross-namespace Get test | c534fd6 | cmd/dashboard/api/projects_test.go |
| 1 (GREEN) | Fix Get handler + buildDetail helper | e820cfd | cmd/dashboard/api/projects.go |

## Verification Results

- `go test ./cmd/dashboard/api/ -run 'TestGetProject' -v -count=1` — 3/3 PASS
- `go test ./cmd/dashboard/api/... -count=1` — 44/44 PASS
- `grep -c "buildDetail" cmd/dashboard/api/projects.go` — returns 4 (>= 2 required)
- `grep 'namespace = "default"' cmd/dashboard/api/projects.go` — empty (removed)
- `go vet ./cmd/dashboard/...` — clean

## Deviations from Plan

None — plan executed exactly as written. TDD RED/GREEN cycle followed.

## TDD Gate Compliance

| Gate | Commit | Status |
|------|--------|--------|
| RED (test) | c534fd6 `test(10-04): add failing test...` | PASS — test fails before fix |
| GREEN (feat) | e820cfd `feat(10-04): fix dashboard Get handler...` | PASS — all tests green |
| REFACTOR | N/A | Not needed — code is clean |

RED was confirmed: `TestGetProjectWithoutNamespaceParamFindsAcrossNamespaces` returned `expected 200, got 404` before the production code change.

## Known Stubs

None.

## Threat Surface Scan

The cross-namespace List in the fallback path uses the same dashboard SA privilege surface as the existing all-namespace `List` handler. No new trust boundaries introduced. T-10-04-A (cross-namespace List) and T-10-04-B (first-match name ambiguity) are both `accept` per the plan's threat register.

## Self-Check

Files:
- `cmd/dashboard/api/projects.go` — FOUND (modified)
- `cmd/dashboard/api/projects_test.go` — FOUND (modified)

Commits:
- c534fd6 — FOUND in git log
- e820cfd — FOUND in git log
