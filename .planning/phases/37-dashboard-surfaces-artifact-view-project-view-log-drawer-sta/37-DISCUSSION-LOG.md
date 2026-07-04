# Phase 37: Dashboard Surfaces — Artifact View, Project View, Log-Drawer States - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-03
**Phase:** 37-Dashboard Surfaces — Artifact View, Project View, Log-Drawer States
**Areas discussed:** Artifact viewer UX, Artifact transport contract (was: ConfigMap contract), Project view, Log-drawer states

---

## Todo Cross-Reference

| Option | Description | Selected |
|--------|-------------|----------|
| Artifact-view todo | 2026-07-03-dashboard-planning-dag-artifact-view.md — envelope paths, transport options, UI suggestions | ✓ |
| Log-drawer todo | 2026-07-03-dashboard-log-stream-drawer-empty.md — empty-drawer repro, fix split | ✓ |

**User's choice:** Folded both. Seven other matcher hits excluded (tagged `resolves_phase` 34/35/36/38 or deferred).

---

## Artifact viewer UX

| Option | Description | Selected |
|--------|-------------|----------|
| Bottom drawer | Same surface as log stream per todo wording | |
| Side panel | New right-hand panel next to the DAG | |
| Full-screen view | Dedicated route/modal | |

**User's choice:** None of the presented options — user interrupted to ask "A right side panel currently exists when clicking on a task. Why can't that be reused?" Verified in code: `TaskDetailDrawer` is already a right-side panel (`fixed top-0 right-0`, 420px). Decision: reuse it. The todo's "bottom drawer" description was inaccurate.

| Option | Description | Selected |
|--------|-------------|----------|
| Wider panel variant | ~640–720px artifact panels | |
| Keep 420px + expand toggle | Familiar width, expand on demand | (✓ initially) |
| Keep 420px as-is | No width change | |
| You decide | Planner picks | |

**User's choice:** Initially "Keep 420px + expand toggle — same component, expand toggle in both views." Then at the area continue-check, the user proposed: "what if all windows (Planning DAG, Execution DAG, Task/Artifact panel, log panel, etc) were adjustable windows that could be collapsed like windows in an IDE like VS Code?"

| Option | Description | Selected |
|--------|-------------|----------|
| Resizable panel now, IDE layout later | Panel + log area get drag-to-resize + collapse this phase; full dockable layout deferred | ✓ |
| Full IDE layout this phase | Layout overhaul in-phase (scope growth) | |
| Keep expand toggle, defer all of it | Original decision stands | |

**User's choice:** Resizable panel now, IDE layout later.

| Option | Description | Selected |
|--------|-------------|----------|
| Level .md + children JSON | Matches DASH-01 text; out.json stays internal | ✓ |
| Everything in the envelope | Full envelope browser incl. out.json | |
| Level .md only | Primary doc only | |

**User's choice:** Level .md + children JSON.

| Option | Description | Selected |
|--------|-------------|----------|
| Copyable approve command | Pinned strip: gate status + `tide approve <target>` via ClipboardCopyAction | ✓ |
| Badge + artifact only | No new affordance | |
| Revisit read-only for gates | Real approve button (breaks read-only lock) | |

**User's choice:** Copyable approve command.

---

## Artifact transport contract (was: ConfigMap contract)

| Option | Description | Selected |
|--------|-------------|----------|
| One CM per CR envelope | Keys = artifact filenames | (✓ — later mooted) |
| One CM per artifact file | Finer granularity, more objects | |
| You decide | Researcher picks | |

**User's choice:** One CM per CR envelope — mooted by the transport rethink below.

| Option | Description | Selected |
|--------|-------------|----------|
| Keep head + marker | First N KiB + truncation marker | |
| Stub only | Marker-only key for oversize | |
| Keep head + tail | Splice with gap marker | |

**User's choice:** None — **"Being unable to see the entire artifact is a no-go."** All truncation rejected; this overrode the locked STATE.md truncation-marker constraint and triggered the transport rethink.

| Option | Description | Selected |
|--------|-------------|----------|
| Chunk with high ceiling | Chunked CMs, ~10 MiB pathology guard | |
| Chunk with no ceiling | Unlimited etcd mirroring | |
| Rethink transport instead | Reopen the transport decision | ✓ |

**User's choice:** Rethink transport. Then: "Search the web for established conventions and best practices for this use-case." Research performed (Argo Workflows artifact repository + UI streaming; Tekton Results external-DB offload; K8s ConfigMap 1 MiB guidance). Convention: durable artifact store + server streams to UI; etcd never holds artifacts.

| Option | Description | Selected |
|--------|-------------|----------|
| Git as artifact store | Commit artifacts to run branch at materialization; manager fetch+cache+stream | ✓ |
| ConfigMaps + chunking | K8s-native splitting workaround | |
| Have researcher validate git path first | Provisional git, fallback CMs | |

**User's choice:** Git as artifact store.

| Option | Description | Selected |
|--------|-------------|----------|
| Run branch, .tide/ dir | Human-readable paths, PR-reviewable | ✓ |
| Separate artifacts ref | Repo stays pristine, no PR review | |
| Run branch, envelope-mirror paths | UID paths, lossless mapping | |

**User's choice:** Run branch, `.tide/` dir.

---

## Project view

| Option | Description | Selected |
|--------|-------------|----------|
| ProjectNode click → panel | Same right-side panel, no new nav | ✓ |
| Dedicated view tab | Third ViewSwitcher tab | |
| Both | Panel + tab | |

**User's choice:** ProjectNode click → panel.

| Option | Description | Selected |
|--------|-------------|----------|
| Curated + raw spec toggle | Cards + collapsible rendered YAML | ✓ |
| Curated fields only | Cards only | |
| Full spec dump | Whole spec as YAML/JSON | |

**User's choice:** Curated + raw spec toggle. (Secret refs by name only — stated as a given.)

| Option | Description | Selected |
|--------|-------------|----------|
| Status header + settings | Lifecycle state, budget, blocking conditions strip on top | ✓ |
| Settings only | Pure configuration view | |

**User's choice:** Status header + settings — after a vocabulary clarification the user raised: "current phase" was ambiguous; confirmed it means the Project CR lifecycle `.status.phase` string, not TIDE Phase CRs (which are many-at-once).

---

## Log-drawer states

| Option | Description | Selected |
|--------|-------------|----------|
| Message + what-remains pointer | Honest text + pointer to task commit/artifacts | |
| Message only | Honest state text, nothing else | ✓ |
| Message + retry | Text + reconnect button | |

**User's choice:** Message only.

| Option | Description | Selected |
|--------|-------------|----------|
| Distinct error state + retry | Four states: loading → streaming → pod-gone \| error+reconnect | ✓ |
| Fold errors into pod-gone | Three states exactly as DASH-04 lists | |

**User's choice:** Distinct error state + retry.

---

## Claude's Discretion

- Outcome-prompt rendering, settings card layout, panel animation details
- Resize implementation (e.g. react-resizable-panels vs hand-rolled)
- Manager artifact-cache sizing/eviction
- Exact `.tide/` subpath layout (human-readable, stable per level)

## Deferred Ideas

- IDE-style adjustable-windows layout for all dashboard views (VS Code-like dockable/collapsible panes) — own phase, needs UI-SPEC pass.
