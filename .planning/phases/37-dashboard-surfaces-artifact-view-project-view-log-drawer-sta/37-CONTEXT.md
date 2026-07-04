# Phase 37: Dashboard Surfaces — Artifact View, Project View, Log-Drawer States - Context

**Gathered:** 2026-07-03
**Status:** Ready for planning

<domain>
## Phase Boundary

The dashboard becomes a sufficient approve-gate review surface: operators read the planning artifacts a Planning DAG node produced, the project's outcome prompt and settings, and honest log-drawer states — without ad-hoc PVC reader pods. Requirements DASH-01..04.

**Major mid-discussion revision:** the transport for planning artifacts is **git, not ConfigMaps**. The user rejected any truncated artifact display ("Being unable to see the entire artifact is a no-go"), requested prior-art research (Argo Workflows artifact repository, Tekton Results, K8s ConfigMap guidance), and chose git-as-artifact-store. This supersedes the v1.0.7 STATE.md binding constraint "Artifact ConfigMaps are a size-capped display cache (~512 KiB, truncation markers)" and requires rewording DASH-02 and Phase-37 success criterion 2 during planning. The "dashboard stays read-only" lock is unchanged.

</domain>

<decisions>
## Implementation Decisions

### Artifact transport (supersedes DASH-02's ConfigMap wording)
- **D-01:** Planning artifacts are committed to git at reporter-materialization time — NOT at boundary push, so they exist before approve gates park. The manager fetches from the remote, caches (bounded LRU), and streams artifacts to the UI. Rationale: matches the Argo convention (artifact store + server streams to UI; never etcd) using TIDE's always-present dependency (the git remote) instead of an object store (would violate no-hidden-host-deps) or external DB (violates CRD-status-only).
- **D-02:** Artifacts land on the **run branch under a well-known `.tide/` directory** (e.g. `.tide/planning/...`) with human-readable paths. PR reviewers see planning docs beside the code; merging preserves the decision record. Accepted cost: `.tide/` lands in the user's repo unless stripped pre-merge. (Rejected: separate artifacts ref — kills PR-reviewability; envelope-mirror UID paths — unreadable.)
- **D-03:** **Full artifact visibility is a hard requirement.** No truncation, no size-capped display, anywhere in the pipeline. Any size-guard must fail loudly (condition/event), never silently trim content.
- **D-04:** Which files are artifacts: the level's planning `*.md` + `children/*.json`. `out.json` stays internal (dispatch plumbing, not a review artifact).

### Artifact viewer UX (DASH-01)
- **D-05:** Reuse the existing right-side detail panel (`TaskDetailDrawer` pattern — `fixed top-0 right-0`, slides in from right; `NodeClickContext` already routes node clicks) for all Planning DAG node types. The todo's "bottom drawer" wording was inaccurate; the panel is already a right-side surface.
- **D-06:** The detail/artifact panel and the log area become **drag-to-resize + collapsible** this phase (supersedes an interim "420px + expand toggle" answer). Full IDE-style dockable layout for ALL views is deferred (see Deferred Ideas).
- **D-07:** Markdown-render `*.md` artifacts; pretty-print `children/*.json`.
- **D-08:** Gate-parked nodes: pinned strip in the panel with gate status + the exact copyable `tide approve <target>` command via the existing `ClipboardCopyAction` pattern; artifact renders above it. Read-only lock preserved — no mutation surfaces.

### Project view (DASH-03)
- **D-09:** ProjectNode click opens the same right-side panel (no new nav, no dedicated tab).
- **D-10:** Panel content: compact live-status strip on top (Project CR lifecycle `.status.phase` string, budget spent vs cap, blocking conditions — all already on `ProjectDetail`), then curated settings cards (outcome prompt, target repo + baseRef, provider + per-level models/effort, budget cap, gate/approval policy, secret refs **by name only — never values**), then a collapsible raw-spec (rendered YAML) section.
- **D-11:** Vocabulary note (user-flagged): "phase" in the status strip means the Project CR lifecycle state string, NOT TIDE Phase CRs (which are many-at-once and already render as DAG nodes).

### Log-drawer states (DASH-04)
- **D-12:** Four-state model: **loading → streaming → pod-gone | error**. Backend already emits distinct `pod-gone` vs `error` SSE events (`cmd/dashboard/api/logs_sse.go`); the work is frontend mapping + root-causing the observed empty drawer.
- **D-13:** Pod-gone state renders an honest **message only** (e.g. "Logs no longer available — pod garbage-collected"). No what-remains pointer, no retry. Log archiving stays explicitly out of scope (REQUIREMENTS.md Out of Scope).
- **D-14:** Error state is distinct from pod-gone and carries a **reconnect** affordance — a transient stream failure must never claim the pod is gone.
- **D-15:** In-phase verification: reproduce the empty-drawer bug with (a) a running task and (b) a completed/GC'd task, per the folded todo.

### Claude's Discretion
- Outcome-prompt rendering (markdown vs pre-block), settings card layout, panel animation details.
- Resize implementation choice (e.g. `react-resizable-panels` vs hand-rolled handles) — planner picks after research.
- Manager artifact-cache sizing/eviction policy.
- Exact `.tide/` subpath layout, given human-readable paths and stable per-level locations.

### Research flags (for gsd-phase-researcher — verify before planning)
- **R-01:** Gate-park ordering — confirm artifacts are committed + pushed BEFORE `approve-milestone`/`approve-phase` gates park. If the reporter's materialization runs after gate-park for any level, the commit site must move earlier.
- **R-02:** Manager git-credential plumbing — today only in-namespace Jobs touch project git creds; the manager gaining read-use is a deliberate security-surface expansion (analogous to Argo Server's artifact-repo creds). Find the cleanest mechanism (read Secret via API at fetch time; scope-limited).
- **R-03:** Phase 36 interaction — artifact commits are a NEW commit site; Phase 36's signing + TIDE Bot identity work must cover it (or explicitly note it lands after 36 and adopts its config).
- **R-04:** Pre-upgrade runs have no artifacts in git — the UI needs an explicit "artifacts not available for this run" state, never a silent empty panel.
- **R-05:** Push cadence — a commit+push per materialization event: confirm it composes with Phase 34's serialized run-branch merges and the boundary-push gate.

### Folded Todos
- **Dashboard — clicking a Planning DAG node shows the artifacts it produced** (`.planning/todos/pending/2026-07-03-dashboard-planning-dag-artifact-view.md`) — the origin document for DASH-01/02. Carries the observed envelope layout on the PVC (`/workspace/<project-uid>/workspace/envelopes/<cr-uid>/` with `*.md`, `children/*.json`, `out.json`) and the three-reader-pods-in-one-run pain that motivated the phase. Its transport options were superseded by D-01 (git), which the todo itself listed as option 3 and rejected only for "lands late" — resolved by committing at materialization instead of boundary push.
- **Dashboard "Open log stream" drawer renders empty** (`.planning/todos/pending/2026-07-03-dashboard-log-stream-drawer-empty.md`) — the origin document for DASH-04, with the first-run repro and backend/frontend fix split.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap
- `.planning/REQUIREMENTS.md` — DASH-01..04 (note: DASH-02 wording must be revised per D-01/D-03; "ConfigMap + truncation markers" is superseded)
- `.planning/ROADMAP.md` — Phase 37 entry (success criterion 2 likewise needs rewording to the git transport)
- `.planning/STATE.md` — v1.0.7 binding constraints; the "Artifact ConfigMaps display cache" line is superseded by this discussion; "dashboard stays read-only" holds

### Origin todos (folded)
- `.planning/todos/pending/2026-07-03-dashboard-planning-dag-artifact-view.md` — envelope layout on PVC, approve-gate pain, transport option analysis
- `.planning/todos/pending/2026-07-03-dashboard-log-stream-drawer-empty.md` — empty-drawer repro, backend/frontend fix split

### Existing design contracts
- `.planning/milestones/v1.0.0-phases/04-gates-observability-dashboard-cli/04-UI-SPEC.md` — the dashboard's existing UI contract (drawer spec, copywriting contract for connection states)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `dashboard/web/src/components/TaskDetailDrawer.tsx` — the right-side detail panel (420px, `fixed top-0 right-0`, focus trap, a11y solved); extend for Planning-DAG node content
- `dashboard/web/src/components/ClipboardCopyAction.tsx` — copyable-CLI-command pattern for the approve strip (already used for "Tail logs (CLI)")
- `dashboard/web/src/components/PodLogStreamer.tsx` — has connecting/connected/offline/idle-closed placeholders; needs the pod-gone/error mapping (D-12..14)
- `dashboard/web/src/components/EmptyState.tsx`, `ErrorState.tsx`, `LoadingState.tsx` — state-rendering components for the four-state model
- `dashboard/web/src/components/NodeClickContext.tsx` — node-click routing for all Planning DAG node types

### Established Patterns
- `cmd/dashboard/router.go` — chi `/api/v1` read-only surface; new artifact + project-settings endpoints extend here
- `cmd/dashboard/api/logs_sse.go` — already emits `pod-gone` and `error` SSE events with a comment citing the DASH-04 contract; backend contract is largely in place
- `dashboard/web/src/lib/api.ts` — typed fetch layer (`fetchProject`, `fetchPlan`, `fetchTask`); artifact fetchers follow this shape
- `internal/reporter/materialize.go` — the reporter materialization site where the artifact git-commit hook lands (import-safe from cmd binaries; no controller back-edge)
- App views: `dags` | `telemetry` via ViewSwitcher — project view deliberately does NOT add a third tab (D-09)

### Integration Points
- Reporter Job (in-namespace, PVC access, envelope reader) → new: commit `.tide/` artifacts to run branch at materialization
- Manager/dashboard binary → new: git fetch + LRU cache + artifact-serving endpoints
- Phase 34 (serialized run-branch merges, boundary-push gate) and Phase 36 (signing, bot identity) both touch the same git surfaces — see R-03/R-05

</code_context>

<specifics>
## Specific Ideas

- "Adjustable windows that can be collapsed like windows in an IDE like VS Code" — the user's framing for the resize/collapse direction; this phase ships the panel + log-area subset (D-06), the full vision is deferred.
- The user requested and consumed web research before locking the transport decision (Argo artifact repository, Tekton Results, ConfigMap-chunking guidance); the git choice is convention-backed, not taste.

</specifics>

<deferred>
## Deferred Ideas

- **IDE-style adjustable-windows layout** — all dashboard views (Planning DAG, Execution DAG, Task/Artifact panel, log area, Telemetry) become dockable/resizable/collapsible panes like VS Code. Own phase; needs a UI-SPEC pass. This phase ships only drag-to-resize + collapse on the detail panel and log area.

### Reviewed Todos (not folded)
- Seven other todo matches were reviewed and excluded — each carries a `resolves_phase` tag for a different phase (34: wave-parallel integration miss; 35: git baseRef; 36: signed commits; 38: pricing table, Prometheus setup) or is explicitly deferred (`subagent.levels` rename — own milestone; CACHE-F1 — vNext).

</deferred>

---

*Phase: 37-Dashboard Surfaces — Artifact View, Project View, Log-Drawer States*
*Context gathered: 2026-07-03*
