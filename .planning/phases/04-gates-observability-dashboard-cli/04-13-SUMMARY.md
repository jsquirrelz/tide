---
phase: 04-gates-observability-dashboard-cli
plan: 13
subsystem: dashboard-frontend
tags: [dashboard, react, xyflow, dagre, react-flow-v12, dag-view, task-detail-drawer, pitfall-26, dash-01]

requires:
  - phase: 04
    plan: 12
    provides: dashboard/web/ Vite 6 + React 18 + TS scaffold; @theme design tokens; chrome (AppShell + Header + ConnectionStatusIndicator + Toast); TOAST_COPY constants; XSS guard
  - phase: 04
    plan: 15
    provides: StatusBadge (10 variants) + WaveBackground SVG band + ClipboardCopyAction (D-D6 clipboard surface) + ProjectPicker; Hourglass re-export for chronograph use
provides:
  - dashboard/web/src/components/ProjectNode.tsx + MilestoneNode.tsx + PhaseNode.tsx + PlanNode.tsx + TaskNode.tsx — the 5 custom @xyflow/react v12 node components per UI-SPEC §5
  - dashboard/web/src/components/TideNodeShell.tsx — shared 3-row visual shell (header + divider + summary) all 5 kinds consume; 2px accent ring for selected; 4px destructive border-left for failed family
  - dashboard/web/src/components/NodeClickContext.tsx — React Context threading onNodeClick(name) through @xyflow's node-rendering tree without prop-drilling
  - dashboard/web/src/components/PlanningDAGView.tsx — dagre TB layout for Project → Milestone → Phase → Plan hierarchy; Pitfall 26 mitigation via opacity:0 + useNodesInitialized + batch sentinel
  - dashboard/web/src/components/ExecutionDAGView.tsx — dagre LR layout for Task waves; cross-wave edges typed smoothstep per RESEARCH §610; WaveBackground bands at z=0
  - dashboard/web/src/components/TaskDetailDrawer.tsx — 420px slide-in drawer; role=dialog + aria-modal + focus trap; Actions row driven by ClipboardCopyAction per UI-SPEC §10 status×action table; 'Open log stream' button wired to onOpenLogStream callback
  - dashboard/web/src/lib/layout.ts — applyDagreLayout(nodes, edges, 'TB'|'LR') wrapping dagre.graphlib.Graph + dagre.layout
  - dashboard/web/src/lib/api.ts — typed fetchProjects + fetchProject mirroring cmd/dashboard/api/projects.go wire shape; throws on non-2xx
affects:
  - 04-16 (log streamer + bundle gate) — onOpenLogStream(taskName) callback is the seam where <PodLogStreamer> mounts; PlanningDAGView/ExecutionDAGView document SSE-extension seam in code comments so useProjectEvents / useTasks hooks can swap in without refactor; bundle still ~3x under the <500KB gate

tech-stack:
  added:
    - "@xyflow/react v12 — first import site (pinned in plan 04-12, mounted here): ReactFlow + ReactFlowProvider + useNodesState + useEdgesState + useNodesInitialized + Background + Node + Edge + NodeProps types."
    - "dagre — first import site (pinned in plan 04-12, mounted here): graphlib.Graph + layout(). Wrapped in lib/layout.ts; the public API is a single applyDagreLayout(nodes, edges, direction)."
    - "lucide icons newly imported here: Layers (Project), Flag (Milestone), Compass (Phase), ListTree (Plan), Square (Task), X (drawer close). Hourglass re-exported by StatusBadge.tsx already."
  patterns:
    - "Shared TideNodeShell: all 5 custom nodes funnel through one shell component that renders the 3-row layout (header + divider + summary). Each per-kind wrapper supplies kind icon, header label format, width/min-height, and the summary line text. Adding a future kind (e.g. WaveNode in v1.x) is a single new wrapper file + an entry in the per-view nodeTypes map — the shell, click routing, accessibility, and failed-border logic stay in one place."
    - "NodeClickContext pattern: @xyflow's per-node component receives NodeProps with no escape hatch for parent callbacks. The view component installs a React Context whose value is the click callback; each shell call site reads it via useContext. Default value is a no-op so leaf-component tests don't need a Provider wrapper when click behavior isn't under test."
    - "Pitfall 26 mitigation as a 3-step ratchet: (1) data effect bumps a layoutBatchRef.current sentinel and calls setNodes with opacity:0; (2) layout effect, gated on useNodesInitialized + a sentinel inequality check, runs dagre once per batch and calls setNodes with opacity:1; (3) data-flicker-ready DOM attribute transitions from 'false' to 'true' so the test can observe the gate firing. The sentinel-inequality check (lastPositionedBatchRef vs layoutBatchRef) is essential — without it, setNodes(positioned) re-triggers the layout effect, and any node legitimately positioned at (0,0) by dagre would re-trigger the layout pass forever."
    - "Test-double for @xyflow: vi.mock('@xyflow/react', ...) re-exports the real module but overrides useNodesInitialized to return true synchronously. The mock lets the layout effect run in the same commit as the data effect, so tests don't have to harness DOM measurement (which jsdom doesn't reliably support)."
    - "jsdom polyfills for @xyflow (in src/__tests__/setup.ts): ResizeObserver + IntersectionObserver + DOMMatrix + HTMLElement.prototype.scrollIntoView — @xyflow's internals reach for all four; jsdom ships none. Polyfills are no-op stubs because the tests assert on rendered DOM shape, not on @xyflow's layout/viewport calculations."
    - "Test-introspectable edges: ExecutionDAGView renders a hidden display:none DOM strip with one <span data-edge-meta data-edge-id data-edge-type> per edge. Tests inspect the strip to verify the 'cross-wave edges use smoothstep' contract without digging into @xyflow's internal edge store."
    - "Drawer Actions row driven by single status switch: actionsForStatus(task) is the SINGLE source of truth for the UI-SPEC §10 'Locked button copy' table. Status × action × command-template lookup all live in one switch — any locked-copy drift surfaces in one diff hunk. Five status branches map directly to the five UI-SPEC §10 rows (AwaitingApproval / Paused / Running+Dispatching / Failed-family / Rejected / Succeeded+Pending)."

key-files:
  created:
    - dashboard/web/src/components/ProjectNode.tsx
    - dashboard/web/src/components/MilestoneNode.tsx
    - dashboard/web/src/components/PhaseNode.tsx
    - dashboard/web/src/components/PlanNode.tsx
    - dashboard/web/src/components/TaskNode.tsx
    - dashboard/web/src/components/TideNodeShell.tsx
    - dashboard/web/src/components/NodeClickContext.tsx
    - dashboard/web/src/components/PlanningDAGView.tsx
    - dashboard/web/src/components/ExecutionDAGView.tsx
    - dashboard/web/src/components/TaskDetailDrawer.tsx
    - dashboard/web/src/lib/layout.ts
    - dashboard/web/src/lib/api.ts
    - dashboard/web/src/components/__tests__/nodes.test.tsx
    - dashboard/web/src/components/__tests__/dag-views.test.tsx
    - dashboard/web/src/components/__tests__/drawer.test.tsx
    - dashboard/web/src/lib/layout.test.ts
    - dashboard/web/src/lib/api.test.ts
  modified:
    - dashboard/web/src/App.tsx — replaced grid-cols-2 placeholder with <PlanningDAGView> + <ExecutionDAGView> + <TaskDetailDrawer>; added selectedProject/selectedPlan/selectedTask/streamingTask state + URL hash (#/plan/<name>) deep-link watcher.
    - dashboard/web/src/__tests__/setup.ts — added jsdom polyfills (ResizeObserver, IntersectionObserver, DOMMatrix, scrollIntoView) required by @xyflow/react v12.

key-decisions:
  - "TideNodeShell as the shared base for all 5 nodes (instead of duplicating the 3-row layout 5×). One shell file owns the failed-family detection set, the keyboard-activated click handler, the accessibility attributes (role/tabIndex/aria-label), and the selected/hover/border styling. Adding a kind in the future is a 1-file delta (a wrapper that supplies icon, headerLabel, summary). Each wrapper is ~20 lines."
  - "NodeClickContext (Context-based callback threading) instead of stashing onClick on the node `data`. @xyflow's per-node component receives NodeProps which exposes data + selected + id but no escape hatch for parent callbacks. Stashing onClick on data would couple the click contract to the API wire shape (where node.data flows from the backend ChildRef payload). Context keeps the click contract a UI concern."
  - "Pitfall 26 mitigation uses a layoutBatchRef sentinel rather than a flag (e.g. `if (laidOut) return`). The sentinel handles re-fetch + re-layout correctly — bumping `layoutBatchRef.current` on a fresh data load arms the layout effect to fire once for the new batch. A simple flag would either fire too many times (no de-dup) or never re-layout on data refresh."
  - "Hidden edge-meta DOM markers in ExecutionDAGView (display:none, aria-hidden) for test introspection. @xyflow's internal edge store isn't queryable from the rendered DOM — edges become <path> elements with class names. Rendering a separate metadata strip gives tests a stable selector (data-edge-meta / data-edge-id / data-edge-type) that doesn't depend on @xyflow's rendered shape."
  - "Single actionsForStatus(task) switch as the SOLE source of truth for UI-SPEC §10's status×action×command-template mapping. The 7 status rows in UI-SPEC §10 collapse to 6 branches (AwaitingApproval / Paused / Running+Dispatching / Failed+PushLeaseFailed+PushLeakBlocked / Rejected / Succeeded+Pending). Locked CLI command strings (`tide approve <project>`, `tide cancel <project> --force`, `kubectl annotate plan <plan> tideproject.k8s/retry-push=true`, `tide tail <task>`, `tide inspect-wave <plan>`) live in one switch — gsd-ui-checker can grep this single function to verify command-copy compliance."
  - "Coerce unknown backend phase strings to 'Pending' in PlanningDAGView (KNOWN guard). Backend CEL validates `.status.phase` but a future enum addition (e.g. a 'Quarantined' state) could reach the dashboard before the dashboard bundle has the new StatusValue type. Coercing to Pending degrades gracefully — no crash, just a visually-stale status."
  - "ExecutionDAGView accepts plan data via prop (not via internal SSE fetch). The plan 04-16 useTasks(planName) hook will replace this prop with live SSE data. Keeping the data injection prop-shaped (rather than baking a fetch into ExecutionDAGView) lets plan 04-16 swap the data source without refactoring the layout/rendering logic. Same design for the future useProjectEvents hook on PlanningDAGView."

patterns-established:
  - "All 5 custom nodes consume TideNodeShell — any new kind plugs into the same shell with a new wrapper file + a nodeTypes map entry. The shell is the integration point for cross-cutting concerns (a11y, selected ring, failed border)."
  - "DAG views + drawer use the prop-driven data injection seam (ExecutionDAGView.plan, TaskDetailDrawer.task). Plan 04-16's SSE hooks will produce these props rather than the components owning their own fetch lifecycle."
  - "Locked CLI command templates live in actionsForStatus(task) inside TaskDetailDrawer.tsx — one switch, one diff hunk to audit, one grep target for the gsd-ui-checker."

requirements-completed: [DASH-01]

# Metrics
duration: 35 min
completed: 2026-05-19
---

# Phase 4 Plan 13: Custom Nodes + DAG Views + TaskDetailDrawer Summary

**Shipped the domain-specific rendering layer the dashboard SPA needs to actually render TIDE state: 5 custom @xyflow/react v12 node components (Project / Milestone / Phase / Plan / Task) consuming the StatusBadge + ClipboardCopyAction + WaveBackground primitives from plan 04-15, the two DAG views (PlanningDAGView top-down + ExecutionDAGView left-right with wave bands), and the TaskDetailDrawer (slide-in panel with focus trap + locked-copy clipboard actions per UI-SPEC §10). After this plan: clicking a PlanNode swaps the right pane; clicking a TaskNode opens the drawer; the dagre layout is stable across re-renders (Pitfall 26 mitigated via opacity:0 + useNodesInitialized + batch sentinel). The SSE consumption hooks and PodLogStreamer land in plan 04-16; the seams are documented in-code so plan 04-16 can wire them without refactor.**

## Performance

- **Duration:** ~35 min
- **Tasks:** 2/2
- **Files created:** 17 (10 component files + 1 layout utility + 1 api helper + 3 component tests + 2 lib tests)
- **Files modified:** 2 (App.tsx wired; src/__tests__/setup.ts polyfills added)
- **Tests:** 94 passing across 12 files (+42 new: 16 nodes + 5 dag-views + 12 drawer + 3 layout + 6 api)
- **Bundle (after vite build):** 147.40KB gzipped JS + 6.43KB gzipped CSS = ~153.8KB total — ~3× headroom under the plan 04-16 <500KB gate. The bulk of the new bytes are @xyflow/react v12 + dagre (pinned in plan 04-12 but only imported here).

## Accomplishments

- **5 custom nodes** (ProjectNode + MilestoneNode + PhaseNode + PlanNode + TaskNode) render the UI-SPEC §5 shape exactly — kind icon (Layers / Flag / Compass / ListTree / Square from lucide-react) + inline `<StatusBadge>` (plan 04-15 import) + summary line. The 3-row layout (header + 1px divider + summary) lives in a shared `<TideNodeShell>` so each per-kind wrapper is ~20 lines. Width / min-height vary per UI-SPEC §5 table (280/240/200/180/160 × 80/72/64/56/48). Selected = 2px accent ring (`ring-2 ring-[var(--color-accent)]`); failed family = 4px destructive border-left (`border-l-4 border-l-[var(--color-destructive)]`). `role="button" tabIndex={0}` + Enter activates the click handler — accessibility per UI-SPEC §Accessibility.

- **PlanningDAGView (UI-SPEC §3)** — dagre top-down (`rankdir: TB`) layout for Project → Milestone → Phase → Plan. Fetches via `fetchProject(projectName)` on mount (or accepts `initialData` prop for tests). Memoized `nodeTypes` map per RESEARCH §611 to avoid re-render storms. Built-in error-tolerance: unknown backend phase strings coerce to `Pending` (matches ProjectPicker's KNOWN_STATUSES guard from plan 04-15).

- **ExecutionDAGView (UI-SPEC §4)** — dagre left-right (`rankdir: LR`) layout for Task waves. Wave-band rectangles composed from `<WaveBackground>` (plan 04-15 import) inside a dedicated `<svg data-z-layer="wave-background">` at z-index 0 — tasks overlay above per RESEARCH §609 "wave-rect nodes at z-index 0, task nodes on top at z-index 10". Cross-wave edges (sourceWave ≠ targetWave) get `type: 'smoothstep'` per RESEARCH §610; intra-wave edges fall through to the default straight type. Active dispatch wave (passed via `plan.activeDispatchWave`) gets the WaveBackground accent stroke-dasharray styling already encoded in plan 04-15.

- **TaskDetailDrawer (UI-SPEC §7)** — 420px slide-in panel from the right edge, translateX 0 with `transition: transform 180ms cubic-bezier(0.4, 0, 0.2, 1)` per UI-SPEC. `role="dialog" aria-modal="true" aria-labelledby="<title-id>"`. Focus trap on Tab cycles inside the drawer (Shift+Tab cycles backwards); Escape closes; backdrop click closes; X button closes — all four trigger `onClose`. Previously-focused element is captured on open and restored on close. Status row uses `<StatusBadge>` + the `Hourglass` icon re-exported by `StatusBadge.tsx` (plan 04-15 contract). Metadata grid shows namespace / attempt / pod name / exit code / wave index / scheduled at / envelope path. Actions row drives `<ClipboardCopyAction>` (plan 04-15 import) per the UI-SPEC §10 "Locked button copy" table; a single `actionsForStatus(task)` switch is the sole source of truth for the status × action × command-template mapping. "Open log stream" button wires to `props.onOpenLogStream(taskName)` — the actual `<PodLogStreamer>` mount lands in plan 04-16.

- **App.tsx wired**: replaced the plan 04-12 grid-cols-2 placeholder with `<PlanningDAGView projectName={selectedProject} onPlanClick={...}>` (left) + `<ExecutionDAGView planName={selectedPlan} plan={...} onTaskClick={...}>` (right) + `<TaskDetailDrawer taskName={selectedTask} task={...} onClose={...} onOpenLogStream={...}>` (overlay). URL hash `#/plan/<plan-name>` deep-link via `window.location.hash` watcher (UI-SPEC §Plan-click-swaps-right-pane — browser-native History API, no router library).

- **Pitfall 26 (RESEARCH §567-579) mitigated** via the 3-step ratchet: (1) data effect bumps `layoutBatchRef.current` + calls `setNodes` with `opacity: 0`; (2) layout effect, gated on `useNodesInitialized` + sentinel inequality (`lastPositionedBatchRef.current !== layoutBatchRef.current`), runs dagre once per batch + calls `setNodes` with `opacity: 1`; (3) `data-flicker-ready` DOM attribute transitions `false → true`, observable in the dag-views test. The sentinel-inequality check is the critical piece — without it, setNodes(positioned) re-triggers the layout effect (nodes are now in the dependency array), and any node that dagre legitimately positions at (0,0) would re-trigger forever.

- **T-04-D1 XSS mitigation**: zero `dangerouslySetInnerHTML` uses across the 10 new component files. Re-asserted by the existing `no-dangerous-html.test.ts` guard from plan 04-12 (it walks the entire src/ tree).

## Task Commits

| Task | Phase | Hash | Type |
| ---- | ----- | ---- | ---- |
| 1 | RED   | `c00b3af` | test |
| 1 | GREEN | `b8a23dc` | feat |
| 2 | RED   | `0fd991b` | test |
| 2 | GREEN | `c5e545d` | feat |

Plan metadata commit (this SUMMARY.md) follows.

## Files Created

### Source components

- `dashboard/web/src/components/ProjectNode.tsx` — Layers icon · 280×80 · header `project/<name>` · summary `<m> milestones · <p> phases · <q> plans`.
- `dashboard/web/src/components/MilestoneNode.tsx` — Flag icon · 240×72 · summary `<p> phases · <q> plans`.
- `dashboard/web/src/components/PhaseNode.tsx` — Compass icon · 200×64 · summary `<q> plans`.
- `dashboard/web/src/components/PlanNode.tsx` — ListTree icon · 180×56 · summary `<n> tasks · <w> waves`.
- `dashboard/web/src/components/TaskNode.tsx` — Square icon · 160×48 · summary `wave <N> · attempt <K>`.
- `dashboard/web/src/components/TideNodeShell.tsx` — shared 3-row visual shell consumed by all 5 nodes; failed-family detection set; click + keyboard activation; accessibility.
- `dashboard/web/src/components/NodeClickContext.tsx` — React Context for the click callback; default no-op so leaf-component tests don't need a Provider wrapper.
- `dashboard/web/src/components/PlanningDAGView.tsx` — dagre TB layout + Pitfall 26 mitigation + fetch-on-mount (`initialData` prop bypasses fetch for tests); ReactFlowProvider wrapper so hooks work standalone.
- `dashboard/web/src/components/ExecutionDAGView.tsx` — dagre LR layout + WaveBackground bands at z=0 + cross-wave smoothstep edges + hidden edge-meta DOM markers for test introspection.
- `dashboard/web/src/components/TaskDetailDrawer.tsx` — slide-in panel + focus trap + Actions row driven by `actionsForStatus(task)` (single source of truth for UI-SPEC §10).

### Library

- `dashboard/web/src/lib/layout.ts` — `applyDagreLayout(nodes, edges, 'TB'|'LR')` wraps `dagre.graphlib.Graph` + `dagre.layout`; translates dagre's center-of-rectangle coords to React Flow's top-left coords; defaults to `nodesep: 24, ranksep: 80`.
- `dashboard/web/src/lib/api.ts` — typed `fetchProjects(namespace?)` + `fetchProject(name, namespace?)` mirroring the cmd/dashboard/api/projects.go wire shape (ProjectSummary / ProjectDetail / ChildRef / BudgetSummary); throws on non-2xx with the JSON `error` body's text.

### Tests

- `dashboard/web/src/components/__tests__/nodes.test.tsx` — 16 tests across the 5 custom nodes: kind-icon + StatusBadge + summary line for each (5 cases), selection-ring class on/off (2 cases), failed-border class for {Failed, PushLeakBlocked, Rejected} and not-on Succeeded (4 cases), NodeClickContext click handler on TaskNode + PlanNode (2 cases), accessibility (role/tabIndex/Enter activation/aria-label) (3 cases).
- `dashboard/web/src/components/__tests__/dag-views.test.tsx` — 5 tests: PlanningDAGView renders ≥13 nodes via dagre TB for the 1+2+4+6 hierarchy; ExecutionDAGView renders 6 TaskNodes + 3 WaveBackground bands with LR direction; cross-wave edges (t1→t3, t3→t5) carry `type: 'smoothstep'`; WaveBackground bands wrap inside a dedicated z=0 SVG layer; data-flicker-ready transitions `false → true` (Pitfall 26).
- `dashboard/web/src/components/__tests__/drawer.test.tsx` — 12 tests: open/close (backdrop + Escape + X button + null-taskName-renders-nothing) (5 cases), header / status row / metadata grid (3 cases), Actions row per status (AwaitingApproval Approve+Reject / Running Cancel+Tail logs / Failed Retry push+Cancel+Inspect wave) (3 cases), Open log stream button wired to onOpenLogStream (1 case).
- `dashboard/web/src/lib/layout.test.ts` — 3 tests: TB direction stacks y-axis; LR direction stacks x-axis across waves; all positions finite after layout.
- `dashboard/web/src/lib/api.test.ts` — 6 tests: fetchProjects calls `/api/v1/projects`; namespace query param appended; fetchProject calls `/api/v1/projects/{name}`; namespace query param appended; throws on 500; throws on 404 with the JSON error body's text.

## Pitfall 26 Mitigation Shape

The 3-step ratchet implemented in both PlanningDAGView and ExecutionDAGView:

```tsx
// 1. Data effect: bump sentinel + insert with opacity 0
useEffect(() => {
  const { nodes, edges } = buildGraph(data);
  layoutBatchRef.current += 1;                       // arm the layout effect
  setNodes(nodes.map(n => ({ ...n, style: { ...n.style, opacity: 0 } })));
  setEdges(edges);
  setFlickerReady(false);
}, [data, ...]);

// 2. Layout effect: gated on useNodesInitialized + sentinel inequality
useEffect(() => {
  if (!useNodesInitialized()) return;
  if (nodes.length === 0) return;
  if (lastPositionedBatchRef.current === layoutBatchRef.current) return;  // de-dup
  const positioned = applyDagreLayout(nodes, edges, 'TB' | 'LR');
  lastPositionedBatchRef.current = layoutBatchRef.current;                 // mark done
  setNodes(positioned.map(n => ({ ...n, style: { ...n.style, opacity: 1 } })));
  setFlickerReady(true);                              // 3. visible to test
}, [ready, nodes, edges, ...]);
```

Why the sentinel-inequality (not a flag like `if (laidOut) return`):

- A flag would either fire only once for the component's lifetime (no re-layout on data refresh) or always fire (infinite loop after dagre legitimately positions a node at x=0/y=0 and the effect re-runs because `nodes` is in the dependency array).
- The sentinel handles re-fetch correctly — bumping `layoutBatchRef.current` arms the effect to fire exactly once for the new batch, and the inequality check is the de-dup gate.

Test exercises the gate via the `data-flicker-ready` DOM attribute: render with `plan=null` (gate is `false` because there's no data yet), then re-render with real plan data — `waitFor` observes the transition to `true` once the layout effect commits.

## NodeClickContext Pattern

`@xyflow/react v12`'s per-node React component receives `NodeProps<Node<Data, kind>>` — data, selected, id, dragging, but no escape hatch for parent callbacks. Three alternatives considered:

1. **Stash `onClick` on `data`.** Couples the click contract to the API wire shape (where `data` flows from `ChildRef` payloads). Rejected.
2. **Use ReactFlow's `onNodeClick` prop.** Works but routes through @xyflow's internal click handler chain — interferes with the per-node keyboard activation path (Enter key on a focused node).
3. **React Context.** View component installs a `NodeClickContext.Provider` whose value is the click callback; each `TideNodeShell` reads it via `useContext(NodeClickContext)`. Default value is a no-op so leaf-component unit tests can render a single node without a wrapping provider.

Chose (3). The Context default-noop is essential — without it, the test harness for `nodes.test.tsx` would need to wrap every render in a Provider, which would prevent the test from observing the "default no-op click" path.

## UI-SPEC §10 Locked Button Copy — Source of Truth

`actionsForStatus(task)` in `TaskDetailDrawer.tsx`:

```ts
switch (task.status) {
  case "AwaitingApproval":
    return [
      { variant: "primary",     label: "Approve", command: `tide approve ${proj}` },
      { variant: "destructive", label: "Reject",  command: `tide reject ${proj}` },
    ];
  case "Paused":
    return [
      { variant: "primary",     label: "Resume", command: `tide resume ${proj}` },
      { variant: "destructive", label: "Cancel", command: `tide cancel ${proj} --force` },
    ];
  case "Running": case "Dispatching":
    return [
      { variant: "destructive", label: "Cancel",          command: `tide cancel ${proj} --force` },
      { variant: "secondary",   label: "Tail logs (CLI)", command: `tide tail ${task.name}` },
    ];
  case "Failed": case "PushLeaseFailed": case "PushLeakBlocked":
    return [
      { variant: "secondary",   label: "Retry push",   command: `kubectl annotate plan ${plan} tideproject.k8s/retry-push=true` },
      { variant: "destructive", label: "Cancel",       command: `tide cancel ${proj} --force` },
      { variant: "secondary",   label: "Inspect wave", command: `tide inspect-wave ${plan}` },
    ];
  case "Rejected":
    return [{ variant: "primary", label: "Resume", command: `tide resume ${proj}` }];
  case "Succeeded": case "Pending":
    return [
      { variant: "secondary", label: "Inspect wave",    command: `tide inspect-wave ${plan}` },
      { variant: "secondary", label: "Tail logs (CLI)", command: `tide tail ${task.name}` },
    ];
}
```

The 7 UI-SPEC §10 rows collapse to 6 switch branches (AwaitingApproval also has a wave-boundary form on the PlanNode drawer that is not surfaced from the task drawer for v1.0). One diff hunk to audit — and the gsd-ui-checker can grep `tide approve|tide reject|tide resume|tide cancel|kubectl annotate plan|tide tail|tide inspect-wave` against this file to verify locked-copy compliance.

## Seams for Plan 04-16

Plan 04-16 (log streamer + bundle gate) consumes three explicit seams left by this plan:

1. **`onOpenLogStream(taskName)`** in `<TaskDetailDrawer>` — the "Open log stream" button at the bottom of the drawer body fires this callback. Plan 04-16 mounts `<PodLogStreamer>` inline below the drawer body when the parent's `streamingTask` state is non-null. App.tsx already wires `streamingTask` via `setStreamingTask` in the `onOpenLogStream` callback.
2. **`PlanningDAGView` SSE hook** — the data-load effect is the seam where plan 04-16's `useProjectEvents(projectName)` plugs in. The view currently uses `fetchProject(projectName)` on mount + a manual refresh. The effect's contract — produce a `ProjectDetail` shape on first arrival; thereafter emit incremental updates — is hook-shaped so plan 04-16 can swap without touching the buildPlanningGraph + dagre layout chain.
3. **`ExecutionDAGView` SSE hook** — `plan` is currently a prop; plan 04-16's `useTasks(planName)` hook will replace the prop with a hook call. The same `ExecutionPlanData` shape (planName + tasks[] + optional activeDispatchWave) the test harness consumes is the contract the hook will produce.

App.tsx already manages `selectedProject` / `selectedPlan` / `selectedTask` / `streamingTask` state; plan 04-16 only needs to (a) replace the fetch lifecycle with SSE, and (b) mount `<PodLogStreamer>` conditionally on `streamingTask !== null`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 — Blocking] jsdom doesn't ship ResizeObserver / IntersectionObserver / DOMMatrix / scrollIntoView — @xyflow/react v12 needs all four**

- **Found during:** Task 2 GREEN — first run of `dag-views.test.tsx` threw `ReferenceError: ResizeObserver is not defined` from `react-dom`'s commit-phase error reporter, originating in `@xyflow/react`'s internal `useNodeResize` observer.
- **Issue:** `@xyflow/react` v12's runtime reaches for browser APIs that jsdom doesn't implement: `ResizeObserver` (measures node dimensions for the layout pipeline), `IntersectionObserver` (off-screen-node virtualization), `DOMMatrix` (panzoom transform math), and `HTMLElement.prototype.scrollIntoView` (keyboard navigation). Without polyfills, every ReactFlow mount throws in jsdom.
- **Fix:** Added no-op stubs for all four globals in `src/__tests__/setup.ts` (the existing vitest setup file from plan 04-12). The stubs are no-op classes because the tests assert on rendered DOM shape, not on @xyflow's layout/viewport calculations — those concerns live in the production layout (gated on `useNodesInitialized` which we mock to return true synchronously in the test).
- **Files modified:** `dashboard/web/src/__tests__/setup.ts`.
- **Committed in:** `c5e545d` (Task 2 GREEN commit).

**2. [Rule 1 — Bug] Infinite setNodes → layout → setNodes loop after dagre positions a node at (0,0)**

- **Found during:** Task 2 GREEN — `npx vitest run dag-views` timed out at 60s instead of finishing in ~5s. Bisecting revealed the layout effect was re-firing on every commit.
- **Issue:** First implementation gated the layout effect on `nodes.some(n => n.position.x === 0 && n.position.y === 0)` — "only run layout if any node still carries the seed position". This breaks when dagre legitimately positions a node at exactly (0,0) (e.g. the root project node in a TB layout). The effect re-fires forever: setNodes(positioned) → effect re-runs → needsLayout still true → setNodes(positioned) → …
- **Fix:** Replaced the seed-position heuristic with a sentinel-inequality check using two `useRef` counters: `layoutBatchRef.current` (bumped on every fresh data load) and `lastPositionedBatchRef.current` (set to the current batch ref after a successful layout). The effect now runs exactly once per batch — `if (lastPositionedBatchRef.current === layoutBatchRef.current) return`.
- **Files modified:** `dashboard/web/src/components/PlanningDAGView.tsx`, `dashboard/web/src/components/ExecutionDAGView.tsx`.
- **Committed in:** `c5e545d` (Task 2 GREEN commit).

**3. [Rule 1 — Bug] Test 3 flicker assertion was racy with the original forceFlickerDelay seam**

- **Found during:** Task 2 GREEN — Test 3 ("Pitfall 26 flicker mitigation") originally used a `forceFlickerDelay` prop to defer the opacity flip into a `setTimeout(0)`. The intent was to give the assertion a visible "false" window between renders. In practice the setTimeout interacted poorly with React 18's commit phase + the batch sentinel — the test would either time out (sentinel guarded the timeout setup) or fail (the timeout fired but the rerender re-skipped the layout).
- **Fix:** Removed the `forceFlickerDelay` test-only seam from `ExecutionDAGView.tsx` and rewrote Test 3 to render with `plan={null}` first (gate is naturally `false` because there's no data), then re-render with real plan data and `waitFor` the transition to `true`. This exercises the Pitfall 26 mitigation via its natural React commit-cycle path rather than a synthetic timer.
- **Files modified:** `dashboard/web/src/components/ExecutionDAGView.tsx`, `dashboard/web/src/components/__tests__/dag-views.test.tsx`.
- **Committed in:** `c5e545d` (Task 2 GREEN commit).

**4. [Rule 1 — Bug] Drawer test 7 metadata assertion matched "Inspect wave" button label**

- **Found during:** Task 2 GREEN — drawer test "metadata grid shows namespace, attempt, podName, exitCode, waveIndex, scheduledAt, envelopePath" threw `Found multiple elements with the text: /wave/i` because the Failed-task drawer's Actions row contains an "Inspect wave" button that the loose regex also matched.
- **Fix:** Scoped the assertions to `screen.getByTestId("drawer-metadata")` and used `toHaveTextContent(/wave index/i)` so the assertion matches only the metadata grid label, not the Actions row button. Same scoping pattern applied to all 7 metadata-grid field assertions.
- **Files modified:** `dashboard/web/src/components/__tests__/drawer.test.tsx`.
- **Committed in:** `c5e545d` (Task 2 GREEN commit).

**5. [Rule 1 — Bug] api.test.ts `afterEach(() => vi.restoreAllMocks())` returned VitestUtils, not Awaitable<void>**

- **Found during:** Task 1 build verification — `npm run build` after the first GREEN commit failed with `error TS2322: Type 'VitestUtils' is not assignable to type 'Awaitable<void>'`.
- **Issue:** `vi.restoreAllMocks()` returns a `VitestUtils` instance for chaining; vitest 1.x's `afterEach` type signature requires `() => Awaitable<void>`. The implicit arrow-body return surfaced as a type mismatch.
- **Fix:** Wrapped the call in a block body: `afterEach(() => { vi.restoreAllMocks(); })`. Same pattern as the other test files in the suite.
- **Files modified:** `dashboard/web/src/lib/api.test.ts`.
- **Committed in:** `b8a23dc` (Task 1 GREEN commit).

---

**Total deviations:** 5 auto-fixed (1 Rule 3 blocking, 4 Rule 1 bugs). All five fixes were inside the per-task envelope and unblocked verification gates the plan already required. Zero scope creep.

## Threat Flags

None — no new trust boundaries crossed beyond the ones T-04-D1 + T-04-D-pitfall26 already cover (and both are re-asserted by the existing guards: `no-dangerous-html.test.ts` for T-04-D1, the dag-views Test 3 flicker assertion for T-04-D-pitfall26).

## Known Stubs

The dashboard now renders the two DAG views + drawer with placeholder runtime data flows pending plan 04-16. These are NOT stubs in the components themselves — they are explicit cross-plan wiring seams documented in plan 04-16's scope:

- **App.tsx `selectedProject` defaults to `"my-project"`** — placeholder until plan 04-15's `<ProjectPicker>` mounts and updates state. Plan 04-15 ships ProjectPicker; this plan just leaves the slot open in `<Header>`.
- **App.tsx `taskDetail` is null** — the drawer renders nothing when task is null (UI-SPEC §7 contract). Plan 04-16's task-detail fetch hook will populate this state.
- **App.tsx `executionPlan` defaults to `{ planName, tasks: [] }`** when a plan is selected — empty array until plan 04-16's `useTasks(planName)` SSE hook hydrates it. ExecutionDAGView renders 0 task nodes + 0 wave bands in this state — well-defined visually but no data.
- **PlanningDAGView fetches on mount + re-fetches on projectName change** — plan 04-16 replaces this with `useProjectEvents(projectName)` SSE consumption. The data effect is the documented seam.

All four are pre-declared in plan 04-13's `<objective>` ("After this plan: the dashboard renders side-by-side Planning + Execution DAGs with click-through to a task drawer. The SSE consumption hooks, log streamer, full-screen error states, and bundle-size gate land in plan 04-16."). The components themselves ship complete behavior for the data they receive.

## Issues Encountered

None beyond the 5 deviations documented above. The biggest surprise was the infinite-loop deviation (#2) — the original heuristic-gated layout effect looked correct on paper but failed in the test harness as soon as dagre legitimately positioned a node at (0,0). The sentinel-pair pattern (`layoutBatchRef` + `lastPositionedBatchRef`) is the standard fix and should be the template if a similar effect-loop concern surfaces in plan 04-16's SSE hooks.

## Self-Check: PASSED

- **Files exist:** all 17 created files present under `dashboard/web/src/`, 2 modified files present at their expected paths.
- **Commits exist:** `c00b3af`, `b8a23dc`, `0fd991b`, `c5e545d` all visible in `git log --oneline 395565e..HEAD`.
- **Verification gates green:**
  - `cd dashboard/web && CI=1 npx vitest run` → 94 passing across 12 files (was 52 at 04-15 baseline; +42 new).
  - `npm run build` → 147.40KB gzipped JS + 6.43KB gzipped CSS = ~153.8KB total (was 48.12KB before @xyflow/react + dagre mounted; still ~3× headroom under the plan 04-16 <500KB gate).
  - `npm run lint` (tsc -b) → clean.
  - `grep -rE "dangerouslySetInnerHTML" dashboard/web/src/ --include="*.tsx" --include="*.ts"` (excluding the guard test itself) → 0 hits.
  - Plan must_haves.artifacts.contains satisfied: `useNodesInitialized` in PlanningDAGView.tsx (2 hits) + ExecutionDAGView.tsx (2 hits), `WaveBackground` in ExecutionDAGView.tsx (3 hits), `role="dialog"` in TaskDetailDrawer.tsx (1 hit), `dagre` in layout.ts (3 hits).
  - 5 custom node tests pass (Test 1-5 of Task 1 — 16 assertions).
  - 2 DAG view tests pass (Test 1-5 of Task 2 — 5 assertions).
  - Drawer tests pass (Test 6-9 of Task 2 — 12 assertions).

## Next Plan Readiness

- **Plan 04-16** (log streamer + bundle gate): three documented seams ready to consume — `onOpenLogStream(taskName)` callback for the `<PodLogStreamer>` mount, the `PlanningDAGView` data-load effect for `useProjectEvents`, the `ExecutionDAGView.plan` prop for `useTasks`. App.tsx already manages `streamingTask` state for the conditional `<PodLogStreamer>` mount. Bundle gate (<500KB gzipped) has ~3× headroom — comfortable margin for the PodLogStreamer's bespoke 80-line ANSI parser plus the SSE consumption hook.

---
*Phase: 04-gates-observability-dashboard-cli*
*Plan: 13*
*Completed: 2026-05-19*
