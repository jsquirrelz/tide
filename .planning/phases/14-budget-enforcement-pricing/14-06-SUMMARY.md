---
phase: 14-budget-enforcement-pricing
plan: "06"
subsystem: dashboard-api
tags: [budget, conditions, sse, blocking-conditions, tdd]
dependency_graph:
  requires: [14-02]
  provides: [blockingConditions-backend, sse-update-regression]
  affects: [cmd/dashboard/api/projects.go, cmd/dashboard/api/informer_bridge_test.go]
tech_stack:
  added: []
  patterns: [TDD-RED/GREEN, whitelist-filter, pre-alloc-empty-array]
key_files:
  created: []
  modified:
    - cmd/dashboard/api/projects.go
    - cmd/dashboard/api/projects_test.go
    - cmd/dashboard/api/informer_bridge_test.go
decisions:
  - "Placed projectCondition near projectSummary with doc comment noting taskCondition mirror — keeps the two parallel structs visually adjacent for future maintainers"
  - "Used make([]projectCondition, 0, 2) as pre-alloc bound matching the whitelist size — documents the max at the allocation site"
metrics:
  duration: "~2 minutes"
  completed: "2026-06-12"
  tasks_completed: 2
  files_modified: 3
---

# Phase 14 Plan 06: Dashboard Project Blocking Conditions Backend Summary

Exposes whitelisted Project blocking conditions on the dashboard REST API and regression-locks the SSE live-update precondition for the BudgetBlocked badge.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 (RED) | Failing tests for blockingConditions | 7fa275a | cmd/dashboard/api/projects_test.go |
| 1 (GREEN) | Implement blockingConditions on projectSummary | 91f654a | cmd/dashboard/api/projects.go |
| 2 | Status-only Project update SSE regression test | 1c4987c | cmd/dashboard/api/informer_bridge_test.go |

## What Was Built

**Task 1 — `blockingConditions` on project endpoints:**

- Added `projectCondition` struct with `type`/`reason`/`message`/`age` JSON fields. Mirrors `taskCondition` in tasks.go with the addition of `message` for the badge tooltip (UI-SPEC C1).
- Added `BlockingConditions []projectCondition` with `json:"blockingConditions"` to `projectSummary`. Because `projectDetail` embeds `projectSummary`, both GET `/api/v1/projects` and GET `/api/v1/projects/{name}` carry it with no further handler changes.
- In `summarize()`: iterates `p.Status.Conditions`, whitelists entries where Type equals `tidev1alpha1.ConditionBudgetBlocked` or `tidev1alpha1.ConditionBillingHalt` AND `c.Status == metav1.ConditionTrue`. Pre-allocated with `make([]projectCondition, 0, 2)` — zero matches serialize as `[]` not `null`. Age via `formatAge(now.Sub(c.LastTransitionTime.Time))`.

**Task 2 — SSE precondition regression test:**

- Added `TestInformerBridgePublishesOnStatusOnlyProjectUpdate`: fires `handler.OnUpdate(oldObj, newObj)` where objects are identical except `newObj.Status.Conditions` gains a True BudgetBlocked condition and ResourceVersion bumps "1"→"2". Asserts hub event `Type="project.update"` with `payload["name"]=="alpha"` arrives within 200ms.
- `informer_bridge.go` is unmodified — `newKindHandler.UpdateFunc` already publishes unconditionally. The test pins that contract against future no-op filtering.

## Verification

```
go test ./cmd/dashboard/... -count=1
ok  github.com/jsquirrelz/tide/cmd/dashboard          1.487s
ok  github.com/jsquirrelz/tide/cmd/dashboard/api      1.217s
ok  github.com/jsquirrelz/tide/cmd/dashboard/hub      0.877s

go test ./cmd/dashboard/api/ -run TestInformerBridge -count=1 -v
--- PASS: TestInformerBridgeWiresAllKinds
--- PASS: TestInformerBridgePublishesOnAdd
--- PASS: TestInformerBridgePublishesMilestoneCreateWithProjectKey
--- PASS: TestInformerBridgePublishesOnStatusOnlyProjectUpdate
PASS

go build ./cmd/dashboard/... # exits 0
```

## Acceptance Criteria Met

- `BlockingConditions []projectCondition` field present in `projectSummary` (verified: line 74)
- `json:"blockingConditions"` tag present (grep -c returns 1)
- `ConditionBudgetBlocked` referenced (grep -c returns 2)
- `ConditionBillingHalt` referenced (grep -c returns 2)
- `make([]projectCondition, 0, 2)` pre-allocation (grep -c returns 2: one in summarize, one in test)
- All 5 behavior tests + pre-existing TestZeroMutationRoutes/XSS/budget tests green
- Raw body contains `"blockingConditions":[]` for zero-condition Project (TestBlockingConditionsEmptyIsNotNull)
- `TestInformerBridgePublishesOnStatusOnlyProjectUpdate` passes with `OnUpdate` call
- `informer_bridge.go` unmodified

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None — `blockingConditions` is fully wired from `p.Status.Conditions` through `summarize()` to the JSON response. No placeholder values.

## Threat Flags

No new security-relevant surface beyond the plan's threat model. The `blockingConditions` field exposes only controller-stamped budget arithmetic (cents, caps) and is bounded to ≤2 entries by the whitelist. The existing `writeJSON` HTML-escape mitigation (T-14-06-02) applies to the new `message` field without additional changes.

## Self-Check: PASSED

- `cmd/dashboard/api/projects.go` modified: confirmed (91f654a)
- `cmd/dashboard/api/projects_test.go` modified: confirmed (7fa275a)
- `cmd/dashboard/api/informer_bridge_test.go` modified: confirmed (1c4987c)
- All commits exist: 7fa275a, 91f654a, 1c4987c — confirmed via git log
- `go test ./cmd/dashboard/... -count=1` exits 0 — verified above
