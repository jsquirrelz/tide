---
phase: 14-budget-enforcement-pricing
reviewed: 2026-06-12T17:55:00Z
depth: standard
files_reviewed: 11
files_reviewed_list:
  - cmd/dashboard/api/informer_bridge_test.go
  - cmd/dashboard/api/projects_test.go
  - cmd/dashboard/api/projects.go
  - dashboard/web/src/components/__tests__/dag-views.test.tsx
  - dashboard/web/src/components/__tests__/nodes.test.tsx
  - dashboard/web/src/components/ConditionBadge.test.tsx
  - dashboard/web/src/components/ConditionBadge.tsx
  - dashboard/web/src/components/PlanningDAGView.tsx
  - dashboard/web/src/components/ProjectNode.tsx
  - dashboard/web/src/components/TideNodeShell.tsx
  - dashboard/web/src/lib/api.ts
findings:
  critical: 0
  warning: 1
  info: 4
  total: 5
status: issues_found
---

# Phase 14: Code Review Report (gap-closure wave — plans 14-06/14-07)

**Reviewed:** 2026-06-12T17:55:00Z
**Depth:** standard
**Files Reviewed:** 11
**Status:** issues_found

> Re-review scoped to the gap-closure wave only (plans 14-06/14-07). The earlier
> 14-REVIEW.md covering plans 14-01…14-05 (1 critical / 10 warnings / 4 info, all
> fixed per 14-REVIEW-FIX.md) is preserved in git history.

## Summary

Re-review scoped to the BudgetBlocked dashboard surface (diff base `6641de1`): backend `blockingConditions` exposure on `projectSummary` (14-06) and the `ConditionBadge` → `TideNodeShell` → `ProjectNode` → `PlanningDAGView` wiring (14-07). The three security focus areas from the scope note were each verified against the actual code, not assumed:

- **XSS via condition message passthrough — clean.** The controller-stamped `Message` flows through `json.Encoder` with `SetEscapeHTML` left at its default (`projects.go:392-405`), then lands only in a React JSX `title` attribute (`ConditionBadge.tsx:107`). React escapes all JSX attribute values; no `dangerouslySetInnerHTML`, `innerHTML`, or `eval` anywhere in the reviewed files. `condition.type` reaches `data-testid`/`data-condition` only after the `CONDITION_TABLE` whitelist check returns a row.
- **Condition-type whitelist enforcement — correct server-side.** `summarize` (`projects.go:357-370`) admits only `ConditionBudgetBlocked`/`ConditionBillingHalt` (constants verified at `api/v1alpha1/shared_types.go:226,259`) with `Status == ConditionTrue`. The 2-entry bound holds because `Project.Status.Conditions` carries `+listType=map +listMapKey=type` (`project_types.go:403-404`), so the apiserver rejects duplicate types. Client-side, however, the whitelist is applied inconsistently — see WR-01.
- **Read-only route invariant — intact.** No route registrations changed in the diff (`cmd/dashboard/router.go` untouched); `TestZeroMutationRoutes` (router_test.go:62) still guards the chi tree. The 14-06 change is payload-shape-only.

The end-to-end badge-liveness chain was traced and holds: controller status-only patch → informer `OnUpdate` → unconditional `hub.Publish` (pinned by the new `TestInformerBridgePublishesOnStatusOnlyProjectUpdate`) → SSE payload includes `kind: "Project"` (`informer_bridge.go:282`) → `PLANNING_KINDS` includes `"Project"` (`PlanningDAGView.tsx:222`) → debounced `runFetch` re-renders the badge. Empty-array contract (`[]` never `null`) holds via the pre-allocated non-nil slice with no `omitempty` tag. Negative/clock-skewed condition ages clamp to `0s` via `humanizeDuration` (`tasks.go:294-296`).

Verification evidence: `go test ./cmd/dashboard/api/` → `ok 1.227s`; `npx vitest run` on the three reviewed test files → 37/37 passed.

One warning (client-side whitelist inconsistency in `TideNodeShell`) and four informational items.

## Warnings

### WR-01: TideNodeShell blocked-state signals bypass the client-side condition-type whitelist

**File:** `dashboard/web/src/components/TideNodeShell.tsx:157` (also 179, 262-266)
**Issue:** The client-side whitelist defense (T-14-07-02) is enforced in `ConditionBadge` (unknown type → `null`) and in the aria-label builder (`CONDITION_TABLE[c.type]?.label` + `filter(Boolean)`), but NOT in `isBlocked`:

```tsx
const isBlocked = blockingConditions.length > 0;
```

A payload whose `blockingConditions` contains only unknown types — exactly the vocabulary-drift scenario the whitelist exists to defend against — produces a purple `border-l-4` and `data-blocked="true"` with **no badge rendered and no "blocked:" suffix in the aria-label**. Sighted users get an unexplained visual signal; screen-reader users get nothing at all. The three blocked-state surfaces (border, badge, aria-label) disagree about whether the node is blocked. Unreachable today only because the server whitelist holds; the component's own doc comment claims unknown types are "defensive against vocabulary drift," which the border path contradicts.
**Fix:** Filter once against the whitelist and drive all three surfaces from the filtered list:

```tsx
const knownConditions = blockingConditions.filter((c) => CONDITION_TABLE[c.type]);
const isBlocked = knownConditions.length > 0;
// ... and map over knownConditions for the badge row;
// the aria-label builder can then drop its ?./filter(Boolean) hedge.
```

## Info

### IN-01: Wire-type layer imports from the component layer

**File:** `dashboard/web/src/lib/api.ts:13`
**Issue:** `lib/api.ts` — documented as the verbatim mirror of the Go wire structs — imports `ProjectBlockingCondition` from `../components/ConditionBadge`, inverting the codebase's lib ← components dependency direction. It is `import type` (erased at compile time, no runtime cycle), but `PlanningDAGView` → `lib/api` → `components/ConditionBadge` is one accidental value-import away from a real cycle, and the wire shape now lives outside the file that claims to own wire shapes.
**Fix:** Move the `ProjectBlockingCondition` type into `lib/api.ts` (next to `ProjectSummary`) and have `ConditionBadge.tsx` import it from there.

### IN-02: SSE-refetch failure silently strands the badge (pre-existing gap, now load-bearing)

**File:** `dashboard/web/src/components/PlanningDAGView.tsx:247-255, 278-281`
**Issue:** Pre-existing (not introduced by this diff): `runFetch` has no error handling, and both call sites invoke it as `void runFetch()`. A `fetchProject` rejection becomes an unhandled promise rejection; the DAG — and now the BudgetBlocked badge, whose liveness depends on this refetch path — silently stays stale with no operator-visible surface. Flagged because the gap-closure wave made this path load-bearing for budget-state visibility.
**Fix:** Catch in `runFetch` (or at the call sites) and surface via the existing toast emitter / connection pill, e.g. `void runFetch().catch((err) => toast(...))`.

### IN-03: No frontend test covers the project.update → badge liveness path

**File:** `dashboard/web/src/components/__tests__/dag-views.test.tsx:318-361`
**Issue:** The new blockingConditions tests use `initialData`, which short-circuits `fetchProject` entirely (`initialData ?? (await fetchProject(...))`) — so the SSE-driven badge update (a `project.update` event causing a refetch that delivers new conditions) is never exercised in the frontend suite. The existing SSE test only asserts a `plan.update` event triggers a refetch. The backend half is pinned (`TestInformerBridgePublishesOnStatusOnlyProjectUpdate`), but the frontend `kind: "Project"` routing through `PLANNING_KINDS` has no test.
**Fix:** Add a case to the SSE live-update describe block emitting `project.update` with `{kind: "Project"}` and asserting `fetchFn` fires (mirroring the existing Plan-event case).

### IN-04: Test helper panics instead of failing cleanly on the condition it guards

**File:** `cmd/dashboard/api/projects_test.go:487-488, 500-501`
**Issue:** `assertBlockingConditions` reports a length mismatch via non-fatal `t.Errorf`, then returns; callers immediately index `bc[0]`. If the backend regresses to zero entries, the test fails via an index-out-of-range panic rather than the helper's diagnostic message — the assertion output that explains the regression gets buried under a panic trace.
**Fix:** Use `t.Fatalf` for the length check in `assertBlockingConditions` (consistent with its other checks), making the helper safe to index after.

---

_Reviewed: 2026-06-12T17:55:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
