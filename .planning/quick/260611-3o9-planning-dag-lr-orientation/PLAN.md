---
phase: quick-260611-3o9
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - dashboard/web/src/components/PlanningDAGView.tsx
  - dashboard/web/src/components/__tests__/dag-views.test.tsx
autonomous: true
requirements: [QUICK-260611-3o9]

must_haves:
  truths:
    - "Planning panel DAG lays out left-to-right (Project at left, Plans fanning right), so a shallow-wide 5-phase/17-plan milestone fits the viewport without horizontal clipping at max zoom"
    - "Execution panel DAG behavior is unchanged"
    - "Dashboard test suite passes"
  artifacts:
    - path: "dashboard/web/src/components/PlanningDAGView.tsx"
      provides: "Planning DAG view invoking applyDagreLayout with LR"
      contains: 'applyDagreLayout(nodes, edges, "LR")'
    - path: "dashboard/web/src/components/__tests__/dag-views.test.tsx"
      provides: "Vitest spec asserting LR direction marker on PlanningDAGView"
  key_links:
    - from: "dashboard/web/src/components/PlanningDAGView.tsx"
      to: "dashboard/web/src/lib/layout.ts"
      via: "applyDagreLayout direction argument"
      pattern: 'applyDagreLayout\(nodes, edges, "LR"\)'
---

<objective>
Flip the dashboard Planning panel DAG layout from top-down (dagre rankdir TB) to left-right (rankdir LR), matching ExecutionDAGView's invocation of the shared layout helper.

Purpose: The planning DAG is shallow-and-wide (project → milestone → phases → plans; 17+ plans fan out). TB orientation clips horizontally at max zoom on real runs — observed live during dogfood run 1 (2026-06-11) on a 5-phase/17-plan milestone. LR turns the wide fan-out into vertical stacking, which the pane scrolls/fits far better.

Output: PlanningDAGView renders with LR layout; updated test assertion; single commit to main (user explicitly approved committing to main).
</objective>

<execution_context>
@$HOME/.claude/get-shit-done/workflows/execute-plan.md
@$HOME/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@dashboard/web/src/components/PlanningDAGView.tsx
@dashboard/web/src/lib/layout.ts
@dashboard/web/src/components/__tests__/dag-views.test.tsx

<interfaces>
From dashboard/web/src/lib/layout.ts (shared helper — do NOT modify):

```typescript
export type DagreDirection = "TB" | "LR";

export function applyDagreLayout(
  nodes: Node[],
  edges: Edge[],
  direction: DagreDirection,
): Node[];
```

The helper's `nodesep: 24` / `ranksep: 80` are internal constants, NOT per-call
parameters. The signature exposes only `direction`. Per scope guard: do not
refactor the helper to expose them — no spacing tune in this change.

Reference invocation in dashboard/web/src/components/ExecutionDAGView.tsx
(line ~252): `applyDagreLayout(nodes, edges, "LR")` and its container sets
`data-dagre-direction="LR"` (line ~282).
</interfaces>
</context>

<tasks>

<task type="auto">
  <name>Task 1: Switch PlanningDAGView to LR layout and update the direction test</name>
  <files>dashboard/web/src/components/PlanningDAGView.tsx, dashboard/web/src/components/__tests__/dag-views.test.tsx</files>
  <action>
In dashboard/web/src/components/PlanningDAGView.tsx, change exactly four TB touchpoints to LR:

1. Header comment line 32: "Layout: dagre top-down (rankdir TB)." becomes "Layout: dagre left-right (rankdir LR)." — keep the rest of the doc comment intact.
2. Layout-effect comment line 295: "run dagre TB layout" becomes "run dagre LR layout".
3. Line 301: `applyDagreLayout(nodes, edges, "TB")` becomes `applyDagreLayout(nodes, edges, "LR")` — matching ExecutionDAGView's invocation.
4. Line 329: `data-dagre-direction="TB"` becomes `data-dagre-direction="LR"` on the `data-testid="planning-dag-view"` container.

Do NOT change node width/height values in buildPlanningGraph (they are content-driven, not orientation-driven), do NOT touch the helper's nodesep/ranksep constants (signature does not expose them per-call), do NOT touch ExecutionDAGView, the status-chip/coerce mapping, or fitView options. No new dependencies.

In dashboard/web/src/components/__tests__/dag-views.test.tsx, update the PlanningDAGView direction spec (lines ~120-135): the describe title "hierarchy renders with ≥13 nodes and TB direction" becomes "... and LR direction", the inline comment about data-dagre-direction="TB" becomes "LR", and the `.toBe("TB")` assertion becomes `.toBe("LR")`. Leave the ExecutionDAGView LR assertion (line ~219) untouched.
  </action>
  <verify>
    <automated>cd dashboard/web && npm test</automated>
  </verify>
  <done>
`grep -c 'applyDagreLayout(nodes, edges, "LR")' dashboard/web/src/components/PlanningDAGView.tsx` returns 1; `grep -c '"TB"' dashboard/web/src/components/PlanningDAGView.tsx` returns 0; `npm test` (vitest run) exits 0 with the updated LR assertion passing.
  </done>
</task>

</tasks>

<verification>
- `cd dashboard/web && npm test` exits 0 (vitest run — includes dag-views.test.tsx, layout.test.ts, bundle-size guard).
- `grep -n 'data-dagre-direction' dashboard/web/src/components/PlanningDAGView.tsx` shows `"LR"`.
- `grep -n '"LR"' dashboard/web/src/components/ExecutionDAGView.tsx` unchanged (no diff to that file: `git diff --stat dashboard/web/src/components/ExecutionDAGView.tsx` is empty).
</verification>

<success_criteria>
- PlanningDAGView lays out the planning DAG left-to-right via the shared dagre helper, identically to how ExecutionDAGView invokes it.
- All four TB references in PlanningDAGView.tsx (two comments, the layout call, the data attribute) read LR; no stray TB remains in the file.
- Test suite green; ExecutionDAGView and layout.ts untouched.
- Single commit to main: `fix(dashboard): flip Planning DAG layout to left-right (rankdir LR)`.
</success_criteria>

<output>
Create `.planning/quick/260611-3o9-planning-dag-lr-orientation/SUMMARY.md` when done.
</output>
