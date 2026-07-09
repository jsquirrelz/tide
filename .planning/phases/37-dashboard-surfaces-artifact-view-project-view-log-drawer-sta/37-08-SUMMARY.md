---
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
plan: 08
subsystem: ui
tags: [react, typescript, vitest, dashboard, resize, localStorage, artifacts, settings]

# Dependency graph
requires:
  - phase: 37 (plan 37-04)
    provides: NodeDetailPanel shell + ResizeHandle/usePersistedSize + PlanningNodeKind export
  - phase: 37 (plan 37-05)
    provides: ArtifactViewer + ApproveStrip content components + fetchProjectSettings/fetchNodeArtifacts + ProjectSettings type
provides:
  - ProjectSettingsPanel — DASH-03 content component (status strip + D-10 cards + raw-spec disclosure)
  - Kind-aware NodeClickContext ((kind, name) signature) routing every Planning-DAG node kind
  - App-level surface assembly — NodeDetailPanel content routing per kind + gate-parked ApproveStrip + second D-06 resize instance (log area)
affects: [37-10 (checkpoint visual pass consumes the assembled surfaces)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Kind-aware click context: (kind, name) callback lets a single provider route all node kinds without a per-kind provider tree"
    - "Backward-compatible prop migration: PlanningDAGView gained an OPTIONAL onNodeClick so the context signature could change with tsc green per-commit (no forced App edit inside the node-wiring commit)"
    - "Status-strip data from the already-held ProjectSummary; only the curated cards refetch (fetchProjectSettings) — no duplicate project-level fetch for the strip"
    - "Collapse ≠ close for the log area: PodLogStreamer stays mounted (display:none) so the SSE stream survives collapse/expand"

key-files:
  created:
    - dashboard/web/src/components/ProjectSettingsPanel.tsx
    - dashboard/web/src/components/ProjectSettingsPanel.test.tsx
    - dashboard/web/src/components/__tests__/node-panel-integration.test.tsx
  modified:
    - dashboard/web/src/components/NodeClickContext.tsx
    - dashboard/web/src/components/TideNodeShell.tsx
    - dashboard/web/src/components/ProjectNode.tsx
    - dashboard/web/src/components/MilestoneNode.tsx
    - dashboard/web/src/components/PhaseNode.tsx
    - dashboard/web/src/components/PlanningDAGView.tsx
    - dashboard/web/src/components/ExecutionDAGView.tsx
    - dashboard/web/src/components/GlobalExecutionDAGView.tsx
    - dashboard/web/src/components/__tests__/nodes.test.tsx
    - dashboard/web/src/App.tsx

key-decisions:
  - "Made Project/Milestone/Phase nodes clickable by removing clickable={false} in the node components (not in the plan's declared file set) — Task 2 acceptance requires role=button on those kinds; the old CR-04 pollution guard is obsolete now that clicks route by kind"
  - "Plan-node artifacts render in the shared NodeDetailPanel (option a), NOT the execution pane — the panel is the uniform artifact home across all kinds; the execution-pane swap is preserved underneath and revealed on close. RunningWavesView wave-card clicks deliberately do NOT open the panel, keeping that navigation flow byte-identical and avoiding App.test regressions"
  - "App fetches the full ProjectDetail locally (a duplicate of PlanningDAGView's fetch) to derive a milestone/phase node's lifecycle string for gate-parked state — the locked (kind, name) callback carries no status, and Task 3's file scope is App.tsx only, so a status-reporting prop on PlanningDAGView was out of scope"
  - "PlanningDAGView's new onNodeClick prop is optional so the (kind, name) context migration kept tsc green in the node-wiring commit without touching App"

patterns-established:
  - "Pattern: leaf DAG nodes emit (kind, name); the App routes per kind to its own surface — no per-kind provider tree"

requirements-completed: [DASH-01, DASH-03]

coverage:
  - id: T1
    description: "ProjectSettingsPanel — status strip (Status label D-11, budget line, conditions), D-10 cards in order (verbatim outcome pre-wrap, HEAD (default) baseRef, effort note, budget cap/spent/remaining, gates + pause, names-only secrets), raw-spec disclosure, fetch-failure retry"
    requirement: DASH-03
    verification:
      - kind: unit
        ref: "dashboard/web/src/components/ProjectSettingsPanel.test.tsx (11 tests)"
        status: pass
    human_judgment: false
  - id: T2
    description: "Kind-aware clicks — NodeClickContext (kind, name); TideNodeShell forwards kind; project/milestone/phase clickable (role=button) routing (\"kind\", name); Execution-DAG task behavior unchanged; tsc enforces total consumer migration"
    requirement: DASH-01
    verification:
      - kind: unit
        ref: "dashboard/web/src/components/__tests__/nodes.test.tsx (26 tests)"
        status: pass
    human_judgment: false
  - id: T3
    description: "App assembly — content routing per kind, gate-parked ApproveStrip (D-08), resizable + collapsible log area (tide.dashboard.log-height, [120,70vh]), preserved #/plan deep link + plan/task flows"
    requirement: DASH-01
    verification:
      - kind: unit
        ref: "dashboard/web/src/components/__tests__/node-panel-integration.test.tsx (2 composition tests) + full suite tsc + build"
        status: pass
  - id: D9
    description: "Visual/motion fidelity of the mounted chrome (panel slide, resize cursor/track, gate-strip layout) — rendered appearance not asserted by jsdom"
    requirement: DASH-01
    verification:
      - kind: integration
        ref: "dashboard/web/src/components/__tests__/node-panel-integration.test.tsx (panel + real content mounts and renders)"
        status: pass
    human_judgment: true
    rationale: "jsdom verifies the composition mounts + renders the right structure with real content; true visual/motion fidelity (hover colors, slide easing, cursor) still needs the 37-10 checkpoint human pass"

# Metrics
duration: 30min
completed: 2026-07-08
status: complete
---

# Phase 37 Plan 08: Dashboard Surfaces Integration Summary

**Wired the three Phase-37 frontend surfaces together — ProjectSettingsPanel (DASH-03), kind-aware Planning-DAG click routing, and the App-level assembly that mounts ArtifactViewer/ApproveStrip/ProjectSettingsPanel inside NodeDetailPanel — plus the second D-06 resize instance on a now-collapsible log area.**

## Performance

- **Duration:** ~30 min
- **Completed:** 2026-07-08
- **Tasks:** 3 (Task 1 TDD)
- **Files created:** 3 · **Files modified:** 10

## Accomplishments

- **Task 1 — ProjectSettingsPanel (DASH-03):** live-status strip from props (D-11: labeled `Status`, never `Phase`; budget line `spent $X.XX of $Y.YY cap`; ConditionBadges); D-10 cards in order — outcome prompt in a verbatim mono `pre-wrap` block (never markdown, T-37-08-02), repository (`HEAD (default)` baseRef fallback + run branch when stamped), models per level + the locked `effort: not yet configurable` note, budget (cap/spent/remaining), gates per level + `pauseBetweenWaves`, secrets as NAMES only each suffixed `(name only — value not shown)` (T-37-08-01); collapsible raw-spec YAML disclosure with locked toggle labels; fetch-failure retry surface (never a silent empty panel). 11 unit tests.
- **Task 2 — kind-aware clicks:** `NodeClickContext` callback signature became `(kind, name)`; `TideNodeShell` forwards its `kind` prop; Project/Milestone/Phase nodes are now clickable (removed the obsolete `clickable={false}` guards); `PlanningDAGView` gained an optional `onNodeClick` routed to the context (falls back to plan-only `onPlanClick` when absent); the Execution/Global DAG views adapt the two-arg signature to their task-only consumers with unchanged behavior. `nodes.test.tsx` migrated to `(kind, name)` and gained milestone/phase/project clickable + routing coverage (26 tests). `tsc -b` clean — types enforce total consumer migration.
- **Task 3 — App assembly:** kind-aware `onNodeClick` routes project → `ProjectSettingsPanel` (status-strip props from the already-fetched `ProjectSummary`), milestone/phase/plan → `ArtifactViewer`, with `ApproveStrip` pinned below when the node is gate-parked (`AwaitingApproval`, derived from a locally-fetched `ProjectDetail`); plan clicks additionally preserve the existing execution-pane swap + `#/plan/<name>` deep link. The hardcoded 240px log panel became `usePersistedSize("tide.dashboard.log-height", 240, 120, 70vh)` + a top-edge horizontal `ResizeHandle` + a collapse toggle that keeps `PodLogStreamer` mounted (collapse ≠ close). Added a deterministic panel+content composition smoke (2 tests).

## Deviations from Plan

### Auto-fixed / scope adjustments

**1. [Rule 3 - Blocking] Edited the node components to flip clickable**
- **Found during:** Task 2
- **Issue:** `clickable={false}` lives in `ProjectNode.tsx`/`MilestoneNode.tsx`/`PhaseNode.tsx`, not `TideNodeShell.tsx`. The plan's Task 2 acceptance requires milestone/phase/project nodes to render with the clickable affordance (`role="button"`), but those three files are not in the plan's `files_modified` set.
- **Fix:** Removed the `clickable={false}` prop from all three node components so they inherit the default `clickable=true`. The old CR-04 pollution guard is obsolete — clicks now route by kind, so a project click opens the settings panel instead of polluting `setSelectedPlan`.
- **Files modified:** ProjectNode.tsx, MilestoneNode.tsx, PhaseNode.tsx
- **Commit:** b76ceff

**2. [Rule 2 - Missing verification] Added an App-level composition smoke**
- **Found during:** Task 3
- **Issue:** The success criteria require "App-level integration verified renders (not just unit tests)", and 37-04 flagged D9 as needing a rendered pass "when the panel is mounted with real content". No test exercised the assembled `NodeDetailPanel` + content composition.
- **Fix:** Added `node-panel-integration.test.tsx` — mounts the exact compositions App produces (project → `ProjectSettingsPanel`; gate-parked milestone → `ArtifactViewer` + pinned `ApproveStrip`) and asserts they render together with real content. Deterministic (no ReactFlow layout), so it complements the 37-10 checkpoint visual pass rather than flaking.
- **Files modified:** node-panel-integration.test.tsx (new)
- **Commit:** 9e78bfd

**3. Design choice — optional onNodeClick prop on PlanningDAGView**
- Made `PlanningDAGView.onNodeClick` optional so the `(kind, name)` context signature change kept `tsc -b` green in the node-wiring commit without a forced App edit inside it. App passes it in Task 3.

## Design Decisions Recorded (per plan directive)

- **Plan-node artifacts render in the shared NodeDetailPanel (option a), not the execution pane.** The panel is the uniform artifact home across all kinds; the execution-pane swap is preserved underneath (`setSelectedPlan` + `#/plan` hash) and revealed on close, so the panel does not truly "fight" the pane. RunningWavesView wave-card clicks deliberately keep their byte-identical plan-only behavior (they do NOT open the artifact panel), which also keeps the existing App.test navigation specs green.
- **gateParked derivation:** App fetches the full `ProjectDetail` locally to read a milestone/phase/plan node's lifecycle string, because the locked `(kind, name)` callback carries no status and Task 3's scope is `App.tsx` only. This duplicates PlanningDAGView's fetch — an accepted trade-off for honest gate-parked review (materializing state + ApproveStrip).

## Security — Threat Mitigations Applied

- **T-37-08-01 (Information Disclosure — secret values in the settings UI, high, mitigate):** the settings panel renders only the redacted `secrets: {purpose, name}[]` array (names only), each suffixed with the locked `(name only — value not shown)` string. No client-side Secret access exists; the server redacts in 37-07.
- **T-37-08-02 (Tampering — outcome prompt as active content, medium, mitigate):** the outcome prompt renders in a verbatim mono `pre-wrap` block, never through a markdown/HTML renderer. Acceptance-gated: `grep -c react-markdown ProjectSettingsPanel.tsx` returns 0.
- **T-37-SC (package installs, low, accept):** no packages installed in this plan.

## Verification

- `npx tsc -b` → exit 0
- `npx vitest run` → 32 files, **260 tests green** (exit 0)
- `npm run build` (tsc -b + vite build) → exit 0 (assembled App bundles; chunk-size warning is pre-existing/informational)
- Acceptance greps (Task 1): `HEAD (default)`=1, `effort: not yet configurable`=1, `name only — value not shown`=1, `Show raw spec (YAML)`=1, `react-markdown`=0
- Acceptance greps (Task 2): `kind` in NodeClickContext=6 (≥2); nodes.test asserts `role="button"` on milestone/phase/project
- Acceptance greps (Task 3): `tide.dashboard.log-height`=2 (≥1), `"240px"`=0, `ApproveStrip`≥1, `ProjectSettingsPanel`≥1, `#/plan/`=5 (≥2)
- Note: one full-suite run showed a transient failure in 37-05's `ArtifactViewer` fake-timer polling test under parallel load; it passed in isolation and on the next two full runs — a known frontend timing flake, not a regression from this plan.

## Known Stubs

None. All three surfaces are wired to real fetchers (37-05/37-07 contract) and real props. `effort: not yet configurable` is an intentional locked note (no effort field exists in v1.0.7, UI-SPEC Pitfall 11), not a stub.

## Next Phase Readiness

- 37-10's checkpoint can perform the D9 human visual/motion pass against the assembled surfaces (panel slide easing, resize track hover/cursor, gate-strip layout) — the mounted chrome now renders with real content.
- Both D-06 resize instances are live and persisted (panel width from 37-04; log height here).

## Self-Check: PASSED

- Files created — all present:
  - FOUND: dashboard/web/src/components/ProjectSettingsPanel.tsx
  - FOUND: dashboard/web/src/components/ProjectSettingsPanel.test.tsx
  - FOUND: dashboard/web/src/components/__tests__/node-panel-integration.test.tsx
- Commits — all present: d12b5b3 (Task 1), b76ceff (Task 2), 9e78bfd (Task 3)
- tsc -b exit 0; full vitest suite 260/260 green; production build exit 0.

---
*Phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta*
*Completed: 2026-07-08*
