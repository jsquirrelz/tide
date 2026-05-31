---
phase: "07"
plan: "02"
subsystem: integration-test
tags: [wave-0, tdd-red, layer-b, bare-project, cascade-spec]
dependency_graph:
  requires:
    - "test/integration/kind/testdata/bare-project.yaml (from 07-01)"
  provides:
    - "test/integration/kind/bare_project_test.go — Layer B spec covering REQ-1/2/4/5/7a/7b (bare Project cascade)"
  affects:
    - "test/integration/kind (new spec discovered by make test-int)"
tech_stack:
  added: []
  patterns:
    - "filterByOwner local helper — inspects ObjectMeta.OwnerReferences to scope List results without MatchingFields"
    - "9 sequential Eventually assertions ordered by cascade level (Milestone→Milestone.Succeeded→Phase→Plan→Plan.Validated→Task→Wave→Task.Succeeded→Project.Complete)"
key_files:
  created:
    - "test/integration/kind/bare_project_test.go"
  modified: []
decisions:
  - "filterByOwner takes []metav1.ObjectMeta (not a generic interface slice) so it compiles without type assertions against the concrete list item type; each loop extracts obj.ObjectMeta before calling the helper"
  - "Assertion 2 (Milestone.Succeeded) intentionally positioned before assertions 3-9 per the research note that Milestone reaches Succeeded immediately after its own planner Job completes — before Phase/Plan/Task exist; this is correct per spec"
  - "Assertion 9 (Project.Complete) last in order despite possibly firing earlier — test waits for terminal state regardless of timing"
  - "skipIfCRDsOnlyMode() in BeforeEach matches the pattern from up_stack_dispatch_test.go — degrades gracefully when controller is not deployed"
metrics:
  duration: "5min"
  completed: "2026-05-31"
  tasks_completed: 1
  files_created: 1
---

# Phase 07 Plan 02: Layer B Bare-Project Cascade Spec — Summary

**One-liner:** Ginkgo v2 Layer B spec (9 Eventually assertions, 311 lines) for the bare-Project self-bootstrap cascade covering REQ-1/2/4/5/7a/7b, intentionally RED until production code lands in 07-03 through 07-05.

## What Was Built

### Task 1: test/integration/kind/bare_project_test.go

Layer B integration spec in package `kind_integration` asserting the full five-level cascade from a bare `kind: Project` application with no pre-applied Milestone.

**Spec structure:**
- `Describe` block: `"bare Project self-bootstraps full cascade to Project=Complete (REQ-1..5 + REQ-7a/b)"` with `Label("kind")`
- `BeforeEach`: `skipIfCRDsOnlyMode()` + `ensureSubagentSA`, `ensureProjectsPVC`, `pvcPrewarmPod`, `ensureSigningKeySecret`, `applyFile("testdata/bare-project.yaml")`
- `AfterEach`: `deleteNamespace(bareProjectNS)` + `exportKindLogs()` on failure
- Single `It` block with 9 `Eventually` assertions:

| # | Assertion | Timeout | REQ |
|---|-----------|---------|-----|
| 1 | Milestone materializes (owner=bare-project) | 3m | REQ-2 |
| 2 | Milestone.Status.Phase == "Succeeded" | 4m | REQ-1,REQ-2 |
| 3 | Phase materializes (owner=milestone) | 5m | REQ-5 |
| 4 | Plan materializes (owner=phase) | 6m | REQ-5 |
| 5 | Plan.Status.ValidationState == "Validated" | 7m | REQ-7a |
| 6 | Task materializes (owner=plan) | 7m | REQ-5,REQ-7b |
| 7 | Wave materializes in namespace | 8m | REQ-7a |
| 8 | Task.Status.Phase == "Succeeded" | 9m | REQ-7b |
| 9 | Project.Status.Phase == "Complete" | 10m | REQ-4 |

**Local helper:** `filterByOwner(items []metav1.ObjectMeta, ownerName string) []metav1.ObjectMeta` scopes List results to objects whose `OwnerReferences` contains `ownerName`. Used for assertions 1, 3, 4, 6.

**Total budget:** ~10 minutes max per spec, well within `kindTestTimeout=18m`. The 5-planner-Job + 1-executor-Job cascade takes ~160s of Job wall-clock time per 07-RESEARCH.md estimates.

## Verification Results

| Check | Result |
|-------|--------|
| `go build ./test/integration/kind/...` exits 0 | PASS |
| `grep -c "Eventually" bare_project_test.go` = 9 | PASS |
| Lines matching `Project=Complete\|ValidationState\|Succeeded\|Wave` >= 5 | PASS (40 lines) |
| `Label("kind")` present | PASS (Describe + It) |
| File contains all 9 assertions in order | PASS |

## Deviations from Plan

None — plan executed exactly as written.

The plan specified `filterByOwner(objects []metav1.Object, ownerName string)` but `metav1.Object` is an interface requiring a type assertion at every call site. The implementation uses `[]metav1.ObjectMeta` instead, with the concrete list item's `.ObjectMeta` extracted in the loop body before calling the helper. This produces identical behavior with cleaner compilation (no `.(tideprojectv1alpha1.Milestone)` type assertions).

## Commits

| Task | Commit | Files |
|------|--------|-------|
| Task 1: Layer B bare-project cascade spec | 3197b0f | test/integration/kind/bare_project_test.go |

## Known Stubs

None — the spec is intentionally RED (not a stub). It will turn GREEN when Plans 07-03 through 07-05 implement production code. The spec is complete as authored; the assertions are all load-bearing for REQ-1/2/4/5/7a/7b acceptance.

## Threat Flags

No new network endpoints, auth paths, file access patterns, or schema changes introduced. Test namespace "bare-project-test" uses `absoluteCapCents: 0` (from the fixture) and the stub image; `deleteNamespace` in `AfterEach` ensures cleanup.

## Self-Check: PASSED

- `test/integration/kind/bare_project_test.go` — FOUND
- Commit 3197b0f — FOUND (`git log --oneline -1` confirmed)
