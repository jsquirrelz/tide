---
phase: 26-multi-milestone-drive-spec-conformance
plan: "04"
subsystem: dashboard
tags: [dashboard, api, react, global-dag, spec-conformance]
dependency_graph:
  requires: ["26-03"]
  provides: ["SPEC-01 visual conformance endpoint + component (T1+T2)", "README mermaid replaced with live dashboard screenshots (T3)", "planning DAG LR horizontal-handle fix"]
  affects: ["cmd/dashboard", "dashboard/web/src", "README.md", "docs/screenshots"]
tech_stack:
  added: []
  patterns: ["ExecutionDAGView project-scope variant", "project-label MatchingLabels filter"]
key_files:
  created:
    - cmd/dashboard/api/execution_dag.go
    - dashboard/web/src/components/GlobalExecutionDAGView.tsx
  modified:
    - cmd/dashboard/router.go
    - dashboard/web/src/components/EmptyState.tsx
    - dashboard/web/src/App.tsx
    - cmd/dashboard/embed/dist (regenerated)
decisions:
  - "Kept labelProject as a local const string in execution_dag.go (matches waves.go pattern) to avoid import cycle with internal/owner"
  - "Added fetchError prop to GlobalExecutionDAGView to distinguish network error from legitimately empty task list"
  - "T3 (screenshot capture + README mermaid replacement) is a human-verify checkpoint; not executed autonomously"
metrics:
  duration: "~35m"
  completed_date: "2026-06-17"
  tasks_completed: 3
  tasks_total: 3
  files_modified: 12
status: complete
---

# Phase 26 Plan 04: Global Execution DAG Dashboard View — Summary

One-liner: GET /api/v1/projects/{name}/execution-dag handler + GlobalExecutionDAGView (dagre LR, project-scoped) added; embed regenerated; both README mermaid diagrams replaced with live dashboard screenshots of the SPEC-01 fixture (T3 completed by orchestrator-driven live capture on a throwaway kind cluster).

## Tasks Completed

| Task | Name | Commit | Status |
|------|------|--------|--------|
| T1 | Add GET /api/v1/projects/{name}/execution-dag handler + router wiring | 1f606f9 | Done |
| T2 | Add GlobalExecutionDAGView + EmptyState variants + App.tsx wiring; regenerate embed | 0bebcc5 | Done |
| T3 | Capture SPEC-01 screenshots + replace README mermaid | c966a51, 0b06c58 | Done |

## T3: SPEC-01 live screenshots + README mermaid replacement (orchestrator-completed)

Captured on a throwaway kind cluster (`tide-spec-shot`, pinned `kindest/node:v1.33.7`) — chosen over the durable `kind-tide-dogfood` cluster, whose CRDs are still `v1alpha1`-only (pre-Spring-Tide) and whose stored `dogfood-codex-runtime` Project would be orphaned by a no-conversion CRD upgrade. Installed v1alpha2 CRDs (`make install`), applied the README α…θ 2-milestone fixture as real CRDs (manager scaled out of the path so no dispatch fired), patched Wave `status.taskRefs` to the envtest-proven schedule, ran the Phase-26 dashboard binary against it, and screenshotted PlanningDAGView + GlobalExecutionDAGView. Cluster deleted after capture; dogfood restored untouched.

**Edge-rendering fix (commit c966a51):** capture surfaced that PlanningDAGView's four node shells (Project/Milestone/Phase/Plan) hardcoded `handleAxis="vertical"`, so edges attached top↔bottom while dagre laid the graph out LR. Switched them to `handleAxis="horizontal"` (matching TaskNode), so planning edges now attach right→left. Embedded SPA regenerated; `verify-dashboard-freshness` green.

**README (commit 0b06c58):** both "Abstract visualization" mermaid blocks replaced with `docs/screenshots/planning-dag.png` + `docs/screenshots/execution-dag.png` and captions; caption notes the dashboard's 0-indexed wave labels map to the worked example's 1-indexed waves (identical schedule). No mermaid block remains in the section.

## T1: GET /api/v1/projects/{name}/execution-dag

Created `cmd/dashboard/api/execution_dag.go` in the `api` package:

- `ExecutionDAGHandler{Client, Log}` struct following `PlansHandler` shape exactly
- `projectExecutionDAGResponse{projectName, tasks}` wrapper reusing existing `planTaskCard` type
- Lists all Tasks via `MatchingLabels{"tideproject.k8s/project": name}` + `InNamespace(namespace)`
- Builds `waveByTask` map from `Wave.Status.TaskRefs` (same pattern as plans.go:121-127, but scoped to project label not plan label)
- Sorts output by `(waveIndex ASC, name ASC)` for deterministic rendering
- Apache license header + DASH-05 GET-only doc comment

Registered in `router.go` as `r.Get("/projects/{name}/execution-dag", execDagHandler.Get)` inside the `/api/v1` group, route table comment updated.

Verification: `go build ./cmd/dashboard/...` clean; `go test ./cmd/dashboard/... -count=1` passed (all 3 packages including DASH-05 TestZeroMutationRoutes); `make lint` exit 0.

## T2: GlobalExecutionDAGView + EmptyState + App.tsx + embed

**GlobalExecutionDAGView.tsx** (new):
- Mirrors `ExecutionDAGView.tsx` with project scope: `ProjectExecutionDAGData{projectName, tasks, activeDispatchWave?}` + `GlobalExecutionDAGViewProps{projectName, project, onTaskClick, fetchError?}`
- Imports `ExecutionTaskData` type from `./ExecutionDAGView` (reuse, no duplication)
- Identical `buildExecutionGraph` / `annotateEdges` / `computeBands` / constants (PADDING/TASK_WIDTH/TASK_HEIGHT/EDGE_*) per UI-SPEC
- Calls `applyDagreLayout(nodes, edges, "LR")` — left-to-right wave layout
- Four view states per UI-SPEC: `fetchError → global-dag-fetch-error EmptyState`; `project===null → Loader2 spinner`; `tasks.length===0 → global-dag-no-tasks EmptyState`; populated → ReactFlow canvas + WaveBackground bands
- `data-testid="global-execution-dag-view"`, wrapped in `ReactFlowProvider`

**EmptyState.tsx**: Added `"global-dag-no-tasks"` and `"global-dag-fetch-error"` variants to the union and switch, following existing CenteredCard+h2+p pattern with exact copywriting from UI-SPEC §Copywriting Contract. No existing variants modified.

**App.tsx**: 
- Added `showGlobalDAG`, `globalExecutionDAG`, `globalDAGError` state
- `useEffect` fetching `/api/v1/projects/{name}/execution-dag?namespace=...` when `showGlobalDAG && selectedProject`; maps `task.phase → StatusValue` via `STATUS_TABLE` coercion
- Three-way right-pane conditional: `selectedPlan → ExecutionDAGView`; `showGlobalDAG → GlobalExecutionDAGView`; else → `RunningWavesView`
- "Global DAG" button in EXECUTION pane header action slot (same inline style as "All waves"); "All waves" returns from global DAG view; pane label changes to `"GLOBAL EXECUTION DAG"` when `showGlobalDAG === true`
- `STATUS_TABLE` imported from `./components/StatusBadge` for phase coercion

**Embed**: `make dashboard-frontend` regenerated `cmd/dashboard/embed/dist`; `make verify-dashboard-freshness` PASSED:
- `PASS: cmd/dashboard/embed/dist/ matches a fresh SPA build (added/removed/changed files all checked)`
- `PASS: embedded bundle contains telemetry marker (panel-cache-efficiency)`

Vitest suite: 204/204 tests pass (26 test files, no regressions in existing EmptyState/DAG/App tests).

## Verification Results

| Check | Result |
|-------|--------|
| `go build ./cmd/dashboard/...` | PASS |
| `go test ./cmd/dashboard/... -count=1` | PASS (3 packages) |
| DASH-05 TestZeroMutationRoutes | PASS (new route is GET-only) |
| `make lint` | PASS (exit 0) |
| `make dashboard-frontend` | PASS |
| `make verify-dashboard-freshness` | PASS (embed + telemetry marker) |
| vitest suite (204 tests) | PASS |

## Deviations from Plan

### Auto-fixed Issues

None — plan executed as written with one minor addition:

**1. [Rule 2 - Missing critical functionality] Added `fetchError` prop to GlobalExecutionDAGView**
- **Found during:** T2 implementation
- **Issue:** The UI-SPEC specified an "error" view state (`EmptyState variant="global-dag-fetch-error"`) triggered by fetch failure, but the component interface had no way to receive a fetch-failed signal — App.tsx owns the fetch, not the component. Without this prop, the component could not distinguish "fetch failed" from "legitimately 0 tasks".
- **Fix:** Added optional `fetchError?: boolean` prop to `GlobalExecutionDAGViewProps`; checked before the `project===null` spinner guard so it always renders the error state when set. App.tsx passes `fetchError={globalDAGError}`.
- **Files modified:** `GlobalExecutionDAGView.tsx`

## T3 Checkpoint (Pending)

Task 3 (screenshot capture + README mermaid replacement) is a `checkpoint:human-verify` task requiring:
1. Running `kind-tide-dogfood` cluster with SPEC-01 fixture applied
2. Browser screenshots of PlanningDAGView + GlobalExecutionDAGView
3. Committing `docs/screenshots/planning-dag.png` + `docs/screenshots/execution-dag.png`
4. README.md mermaid block replacement

This was NOT executed autonomously — see CHECKPOINT REACHED message.

## Known Stubs

None in T1/T2 deliverables. The `GlobalExecutionDAGView` uses real data from the new endpoint; no hardcoded empty values flow to UI rendering.

## Threat Flags

No new threat surfaces beyond the plan's threat model. The new endpoint is project-label-scoped GET-only, matching the existing `waves.go` and `projects.go` scoping model (T-26-06, T-26-07, T-26-08 dispositions confirmed in implementation).

## Self-Check

Files exist:
- `cmd/dashboard/api/execution_dag.go` — FOUND
- `dashboard/web/src/components/GlobalExecutionDAGView.tsx` — FOUND

Commits exist:
- `1f606f9` (T1: Go endpoint) — FOUND
- `0bebcc5` (T2: frontend + embed) — FOUND

## Self-Check: PASSED
