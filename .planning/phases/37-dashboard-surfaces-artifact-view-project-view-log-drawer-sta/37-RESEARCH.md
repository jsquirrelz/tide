# Phase 37: Dashboard Surfaces — Artifact View, Project View, Log-Drawer States - Research

**Researched:** 2026-07-04
**Domain:** K8s dashboard (Go chi backend + React SPA), git-as-artifact-store transport, SSE log streaming
**Confidence:** HIGH (codebase findings), MEDIUM (external library/API findings)

## Summary

This phase has three surfaces sharing one read-only manager-API layer: (1) an artifact viewer for Planning DAG nodes, (2) a project settings view, (3) honest log-drawer states. The transport decision (git, not ConfigMaps — CONTEXT D-01..D-04) turns out to be **far cheaper than it looks**: `tide-push` already contains a complete, tested artifact-staging pipeline (`--artifact-paths` → copy into run worktree → stage → single commit → gitleaks scan → force-with-lease push) that is **plumbed end-to-end but intentionally passed empty at every production call site** (`boundary_push.go:173`). The backend work is therefore "arm existing machinery at a new trigger point," not "build a git pipeline." Reusing `tide-push` also resolves research flags R-03 and R-05 by construction: artifact commits ride the same commit site Phase 36's identity work covers and the same deterministic-Job serialization + lease machinery Phase 34 hardens.

The empty log drawer's root cause is **verified in code**: `logs_sse.go` emits named terminal SSE events (`pod-gone`, `error`, `idle-timeout`) and closes the stream, but the frontend `useSSEStream` hook registers listeners only for unnamed `message` frames and the project-event names — the log-stream terminal events are never received. The server close then fires `EventSource.onerror` → state `reconnecting`, and `PodLogStreamer` renders **no placeholder for the `reconnecting` state** → silently empty drawer plus an infinite reconnect loop against a GC'd pod. A secondary backend gap: `resolvePodName` only returns Running/Pending pods, so a completed-but-not-yet-GC'd pod is misreported as `pod-gone` even though its logs are still servable.

The genuinely new engineering is the dashboard's git *read* path (R-02): fetching the run branch tip from the remote using per-project creds. go-git v5.19.0 is already a direct dependency; shallow-clone support exists but has documented sharp edges (never pull a shallow repo; disable tag fetching). The dashboard SA gaining `secrets get` is the phase's one real security-surface expansion and must be done via the typed clientset (not the cached controller-runtime client, which would silently start a cluster-wide Secret informer).

**Primary recommendation:** Arm the existing `tide-push` staging pipeline with a cumulative envelope→`.tide/` map triggered at planner-Job completion (all four levels); add a go-git shallow-fetch + LRU artifact reader to `cmd/dashboard`; fix DASH-04 entirely in `sse.ts`/`PodLogStreamer` state mapping plus a one-line `resolvePodName` widening; render markdown with `react-markdown@10.1.0` + `remark-gfm@4.0.1` (no raw HTML); hand-roll the two drag handles rather than adopting the SUS-flagged `react-resizable-panels`.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Artifact transport (supersedes DASH-02's ConfigMap wording)**
- **D-01:** Planning artifacts are committed to git at reporter-materialization time — NOT at boundary push, so they exist before approve gates park. The manager fetches from the remote, caches (bounded LRU), and streams artifacts to the UI. Rationale: matches the Argo convention (artifact store + server streams to UI; never etcd) using TIDE's always-present dependency (the git remote) instead of an object store (would violate no-hidden-host-deps) or external DB (violates CRD-status-only).
- **D-02:** Artifacts land on the **run branch under a well-known `.tide/` directory** (e.g. `.tide/planning/...`) with human-readable paths. PR reviewers see planning docs beside the code; merging preserves the decision record. Accepted cost: `.tide/` lands in the user's repo unless stripped pre-merge. (Rejected: separate artifacts ref — kills PR-reviewability; envelope-mirror UID paths — unreadable.)
- **D-03:** **Full artifact visibility is a hard requirement.** No truncation, no size-capped display, anywhere in the pipeline. Any size-guard must fail loudly (condition/event), never silently trim content.
- **D-04:** Which files are artifacts: the level's planning `*.md` + `children/*.json`. `out.json` stays internal (dispatch plumbing, not a review artifact).

**Artifact viewer UX (DASH-01)**
- **D-05:** Reuse the existing right-side detail panel (`TaskDetailDrawer` pattern — `fixed top-0 right-0`, slides in from right; `NodeClickContext` already routes node clicks) for all Planning DAG node types. The todo's "bottom drawer" wording was inaccurate; the panel is already a right-side surface.
- **D-06:** The detail/artifact panel and the log area become **drag-to-resize + collapsible** this phase (supersedes an interim "420px + expand toggle" answer). Full IDE-style dockable layout for ALL views is deferred (see Deferred Ideas).
- **D-07:** Markdown-render `*.md` artifacts; pretty-print `children/*.json`.
- **D-08:** Gate-parked nodes: pinned strip in the panel with gate status + the exact copyable `tide approve <target>` command via the existing `ClipboardCopyAction` pattern; artifact renders above it. Read-only lock preserved — no mutation surfaces.

**Project view (DASH-03)**
- **D-09:** ProjectNode click opens the same right-side panel (no new nav, no dedicated tab).
- **D-10:** Panel content: compact live-status strip on top (Project CR lifecycle `.status.phase` string, budget spent vs cap, blocking conditions — all already on `ProjectDetail`), then curated settings cards (outcome prompt, target repo + baseRef, provider + per-level models/effort, budget cap, gate/approval policy, secret refs **by name only — never values**), then a collapsible raw-spec (rendered YAML) section.
- **D-11:** Vocabulary note (user-flagged): "phase" in the status strip means the Project CR lifecycle state string, NOT TIDE Phase CRs (which are many-at-once and already render as DAG nodes).

**Log-drawer states (DASH-04)**
- **D-12:** Four-state model: **loading → streaming → pod-gone | error**. Backend already emits distinct `pod-gone` vs `error` SSE events (`cmd/dashboard/api/logs_sse.go`); the work is frontend mapping + root-causing the observed empty drawer.
- **D-13:** Pod-gone state renders an honest **message only** (e.g. "Logs no longer available — pod garbage-collected"). No what-remains pointer, no retry. Log archiving stays explicitly out of scope (REQUIREMENTS.md Out of Scope).
- **D-14:** Error state is distinct from pod-gone and carries a **reconnect** affordance — a transient stream failure must never claim the pod is gone.
- **D-15:** In-phase verification: reproduce the empty-drawer bug with (a) a running task and (b) a completed/GC'd task, per the folded todo.

### Claude's Discretion
- Outcome-prompt rendering (markdown vs pre-block), settings card layout, panel animation details.
- Resize implementation choice (e.g. `react-resizable-panels` vs hand-rolled handles) — planner picks after research.
- Manager artifact-cache sizing/eviction policy.
- Exact `.tide/` subpath layout, given human-readable paths and stable per-level locations.

### Deferred Ideas (OUT OF SCOPE)
- **IDE-style adjustable-windows layout** — all dashboard views (Planning DAG, Execution DAG, Task/Artifact panel, log area, Telemetry) become dockable/resizable/collapsible panes like VS Code. Own phase; needs a UI-SPEC pass. This phase ships only drag-to-resize + collapse on the detail panel and log area.
- Seven other reviewed todos belong to other phases (34/35/36/38) or are explicitly deferred (`subagent.levels` rename; CACHE-F1).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| DASH-01 | Clicking a Planning DAG node shows the artifacts it produced, markdown-rendered (children JSON pretty-printed); gate-parked nodes surface the artifact beside the approve action | Panel reuse path verified (`TaskDetailDrawer` 420px fixed-right overlay, focus trap solved); `NodeClickContext` needs a kind-aware signature (today it carries name only, and only Plan nodes are `clickable` via `TideNodeShell`); `react-markdown` verified safe-by-default; `tide approve <project>` CLI syntax verified for the D-08 copy strip |
| DASH-02 | (Superseded wording per D-01/D-03) Planning artifacts committed to git at reporter-materialization time; full visibility, loud failure, PVC/git source of truth | `tide-push` `--artifact-paths` staging pipeline exists and is unused; envelope layout verified (`/workspace/envelopes/<cr-uid>/*.md`, `children/*.json`); reporter Job spawn sites at all four level controllers verified; gate-park ordering measured (R-01, below) |
| DASH-03 | Operator can read the outcome prompt and project settings in a dashboard project view | `Project.Spec` fields verified: `OutcomePrompt`, `Git` (RepoURL/BaseRef-pending-35/CredsSecretRef/GitCredentials), `ModelSelection.Levels`, `Budget`, `Gates`, `SecretRefs`, `ProviderSecretRef`; `projectDetail` API shape exists to extend. **No `Effort` field exists anywhere in v1.0.7** — D-10's "models/effort" wording is resolved in Pitfall 11 (render per-level models only; effort column activates when effort plumbing ships) |
| DASH-04 | Log drawer renders explicit loading / streaming / pod-gone states (never silently empty) | Root cause verified in code (named-event listeners missing + no `reconnecting` placeholder + infinite reconnect); backend `pod-gone`/`error`/`idle-timeout` contract verified in `logs_sse.go`; `resolvePodName` Running/Pending-only gap identified |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

- **GSD workflow enforcement:** no direct repo edits outside a GSD workflow.
- **`charts/tide/values.yaml` is a FIXED contract** — binary catches up to chart, never reverse. New chart values (if any) batch into chart version bumps deliberately.
- **Dashboard read-only invariant:** DASH-05 `TestZeroMutationRoutes` walks the chi route tree — every new endpoint MUST be GET. `make helm-rbac-assert` enforces dashboard ClusterRole verbs ⊆ {get, list, watch}.
- **CRD `.status` only, no external DB; never cache derived schedules in `.status`.** The artifact LRU is in-process memory — rederivable, compliant.
- **No hard-coded git host / LLM provider / auth model.** The dashboard's git fetch must speak generic git protocol (no GitHub raw-URL shortcuts).
- **chi router as `manager.Runnable`; zap behind logr; React 18 + @xyflow/react 12 + Tailwind v4; SSE not WebSockets.**
- **Don't vendor GSD Markdown; don't mount host `~/.claude/`** — not touched by this phase.
- **Verification discipline:** `make test-int` exit ≠ Ginkgo green — read `MAKE_EXIT` and grep `^--- FAIL`. Subagent "pre-existing" dismissals require git-archaeology before relaying.
- **Constrained-VM recipe:** one heavy kind run at a time; delete → recreate → pre-warm per run.
- **Voice for docs:** tight, declarative, em-dash-heavy.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Artifact commit to run branch | In-namespace Job (tide-push) | Level controllers (trigger) | Only in-namespace Jobs mount the PVC and hold git creds (locked security model); controller cannot read PVC |
| Artifact fetch from remote | Dashboard binary (Go) | — | Manager/dashboard cannot mount project PVCs (locked); remote git is the only cross-namespace-safe read path |
| Artifact caching | Dashboard binary (in-process LRU) | — | Rederivable from remote; CRD `.status` caching forbidden |
| Artifact rendering (markdown/JSON) | Browser (React) | — | Presentation only; backend serves raw bytes + metadata |
| Project settings read | Dashboard API (Go) | Browser (cards) | Secret-name-only redaction MUST happen server-side, never trust the client to omit values |
| Log streaming + state contract | Dashboard API (SSE) | Browser (state machine) | Backend emits typed terminal events (already done); browser maps them to UI states (the gap) |
| Gate status + approve command | Browser | Dashboard API (status fields) | Read-only display + clipboard copy; no mutation surface |
| Resize/collapse persistence | Browser (localStorage) | — | Pure UI preference |

## Research Flags Resolved (R-01..R-05)

### R-01: Gate-park ordering — VERIFIED, with a bounded race window
`handleJobCompletion` in `milestone_controller.go` spawns the reporter Job at line 594 (async — Job creation only) and parks at `AwaitingApproval` at line 688 **in the same reconcile pass, without waiting for any Job to finish**. Same shape in `phase_controller.go` (spawn at :528) and the plan/project paths. However, the artifact *files* (`*.md`, `children/*.json`) are written by the **planner subagent during the planner Job**, which is already terminal when `handleJobCompletion` runs — so the artifacts exist on the PVC before the park. What lags the park is the git **commit+push** (a Job that starts after the trigger). Consequence: there is an unavoidable eventual-consistency window of roughly pod-schedule + commit + push (seconds to ~1 min) between "node shows gate-parked" and "artifacts fetchable from the remote." **Do not restructure the park ordering** (blast radius: all four level controllers). Instead the UI must render an explicit "artifacts materializing — refresh" state and poll while a node is gate-parked with artifacts absent. This same state satisfies R-04. `[VERIFIED: internal/controller/milestone_controller.go:594,688]`

### R-02: Manager git-credential plumbing — dashboard SA needs `secrets get`, via typed clientset only
Today git creds (`Project.Spec.Git.CredsSecretRef`, same-namespace Secret) reach ONLY in-namespace Jobs via `EnvFrom` (`push_helpers.go:239`); the reporter Job explicitly carries **no** creds (`reporter_jobspec.go` — "NO EnvFrom git-creds Secret"). The dashboard binary (separate Deployment, SA `<fullname>-dashboard`, ClusterRole enforced read-only by `make helm-rbac-assert`) must gain `secrets` `get` (verb passes the ⊆{get,list,watch} assert). **Critical implementation constraint:** read the Secret through the typed `kubernetes.Interface` clientset the dashboard already holds (`cmd/dashboard/main.go:123`), NOT through the cached controller-runtime client — a cached `Get` on a Secret starts a cluster-wide Secret informer, which requires list/watch RBAC and caches every Secret in the cluster in dashboard memory. With clientset `get`-at-fetch-time, RBAC stays `get`-only and no Secret is retained. Document the expansion in the chart comment block (mirror the existing D-D2 commentary). `[VERIFIED: charts/tide/templates/dashboard-rbac.yaml, cmd/dashboard/main.go]`

### R-03: Phase 36 interaction — resolved by construction if artifact commits ride tide-push
The artifact commit author today would be `tideBotSignature()` (`cmd/tide-push/main.go:131`) — exactly one of the three commit sites Phase 36 (SIGN-01) makes configurable (`spec.git.agentName`/`agentEmail` → chart → compiled default, bot→agent rename). Phase 36 precedes 37 in the roadmap. **If the artifact commit reuses tide-push (recommended), zero new commit sites are introduced and Phase 36's work covers it automatically.** If the planner instead puts the commit in the reporter binary, it creates a FOURTH commit site that must replicate 36's identity resolution — a strong reason not to. `[VERIFIED: cmd/tide-push/main.go:131, REQUIREMENTS.md SIGN-01]`

### R-04: Pre-upgrade runs — three distinct "absent" states needed
(a) Project has no `spec.git` at all (git-less runs are legal; `triggerBoundaryPush` skips silently) → "No git remote configured — artifacts unavailable for this project." (b) Run branch exists but no `.tide/<path>` for this node (pre-upgrade run, or post-upgrade node whose artifact push hasn't landed yet) → "Artifacts not available (yet) for this run" with refetch affordance / polling while gate-parked. (c) Fetch failed (network/creds) → error state with retry, never conflated with (a)/(b). The API response must carry a typed state discriminator, not an empty file list. `[VERIFIED: boundary_push.go:75-78 git-less skip]`

### R-05: Push cadence composition with Phase 34 — compose by riding the same Job, cumulatively
Phase 34 (lands first) serializes ALL run-branch writers behind the deterministic `tide-push-<project-uid>` Job name plus a controller-side single-flight gate (34-CONTEXT D-02), makes the push Job integrate→verify→push atomically under flock (D-06), and stamps `lastPushedSHA` from the push envelope (D-14). A separate artifact-push mechanism would be a third writer class outside that gate and would break the force-with-lease anchor (any push that advances the remote without stamping `lastPushedSHA` causes the next boundary push's lease to reject). **Composition rule: artifact staging must ride the same tide-push Job.** Make staging cumulative and idempotent (mirror 34's cumulative Succeeded-branch re-merge): every push — artifact-triggered or boundary — carries the full envelope→dest map for all planner-completed levels; byte-identical restages produce a clean tree and skip the empty commit (`worktreeClean` path already handles this). A trigger that finds the Job name busy loses nothing — the next push self-heals the missing artifacts. Cadence: one push per planner completion ≈ tens per run — acceptable. `[VERIFIED: 34-CONTEXT.md D-02/D-06/D-14, cmd/tide-push/main.go clean-tree path]`

### Dependency correction: the phase description's "Depends on: Nothing (independent of Phases 34–36)" is stale
The git-transport decision (D-01/D-02) makes the DASH-02 write path **hard-depend** on Phase 34's rewritten tide-push (single-flight gate, verify-in-push, `lastPushedSHA` stamping — R-05 composes against exactly those) and on Phase 36's commit identity (R-03). Neither has landed in this planning worktree — phases 34–38 are being planned together in `gsd-plan-v107-ph34-38` — so the exact function signatures of the Phase-34-shaped `triggerBoundaryPush`/tide-push internals are unknowable at plan-authoring time. **Every plan touching the artifact-push trigger or tide-push staging must either (a) carry an explicit precondition "Phase 34 (and 36) merged to main" plus a re-verify-the-surface step at execution start (re-read the trigger/staging call sites before wiring), or (b) be written contract-level with the concrete wiring deferred to execution.** Frontend-only and dashboard-read-path plans (DASH-01 render, DASH-03 cards, DASH-04 SSE fix) do not carry this dependency.

## Standard Stack

### Core (all already in the repo — zero new Go dependencies required)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| go-git/go-git/v5 | v5.19.0 (in go.mod, direct) | Dashboard-side shallow fetch of run branch | Already the repo's git library (clone/push/integrate) `[VERIFIED: go.mod:7]` |
| hashicorp/golang-lru/v2 | v2.0.7 (in go.mod, indirect → promote to direct) | Bounded artifact cache | Already resolved in the module graph; the canonical Go LRU `[VERIFIED: go.mod:87]` |
| go-chi/chi/v5 | v5.1.0 | New GET endpoints on existing `/api/v1` router | Existing router `[VERIFIED: cmd/dashboard/router.go]` |
| react-markdown | 10.1.0 | Markdown-render `*.md` artifacts | Safe by default — builds a React vDOM, no `dangerouslySetInnerHTML` (passes the repo's `no-dangerous-html.test.ts` guard); raw HTML escaped unless `rehype-raw` added (don't) `[VERIFIED: npm registry + official README]` |
| remark-gfm | 4.0.1 | GFM tables/task-lists in planning docs | The remark-official GFM plugin; planning artifacts use GFM tables heavily `[VERIFIED: npm registry + official README]` |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `JSON.stringify(x, null, 2)` | stdlib | Pretty-print `children/*.json` | Always — no library needed (D-07) |
| `sigs.k8s.io/yaml` | (in module graph via k8s deps) | Raw-spec YAML rendering for D-10 collapsible section | Server-side marshal of Project spec to YAML string |
| lucide-react | ^1.16.0 (installed) | Icons for new panel sections | Existing icon library (UI-SPEC) |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Hand-rolled drag handles (recommended) | react-resizable-panels 4.12.x | Library is mature (2022, 35M dl/wk, bvaughn) but: (1) flagged `SUS/too-new` by the legitimacy gate (latest publish 2026-07-03 — a false-positive on a fresh release, but protocol requires a human-verify checkpoint); (2) its v4 API (`Group`/`Panel`/`Separator`) differs from the pre-v4 API most examples show; (3) it composes flex-flow layouts, while `TaskDetailDrawer` is a `fixed` overlay — adopting it forces a layout refactor that belongs to the deferred IDE-layout phase. Two independent drag handles (panel width, log-area height) + collapse toggles are ~60 LOC with a shared hook; add `role="separator"` + arrow-key handling for a11y. Revisit the library in the deferred docking phase. |
| tide-push staging reuse (recommended) | Git commit inside the reporter binary | Reporter-side commit needs: git creds EnvFrom added to the reporter Job (breaking its "no PAT" least-privilege design), its own flock + lease + lastPushedSHA stamping, its own gitleaks scan, and a fourth Phase-36 identity site. All four exist in tide-push already. |
| go-git shallow clone in dashboard | exec `git` CLI in dashboard image | go-git shallow has known inefficiency (server enumerates far more objects than native `--depth=1`) but avoids adding a git binary + shell-out surface to the dashboard image. Start with go-git; the fetcher interface should be a seam so a CLI fallback is swappable if large-repo latency is observed. |
| `.tide/planning/<kind>/<name>/` layout (recommended) | Envelope-UID mirror paths | UID paths rejected by D-02 (unreadable). CR names are namespace-unique and human-readable; both the controller (staging map) and the dashboard (path lookup from clicked node kind+name) can derive the path independently with zero coordination. |

**Installation:**
```bash
cd dashboard/web && npm install react-markdown@10.1.0 remark-gfm@4.0.1
# Go side: promote github.com/hashicorp/golang-lru/v2 to a direct require (already resolved at v2.0.7)
```

**Version verification (performed 2026-07-04):**
```bash
npm view react-markdown version          # → 10.1.0 (published 2025-03-07)
npm view remark-gfm version              # → 4.0.1  (published 2025-02-10)
npm view react-resizable-panels version  # → 4.12.1 (published 2026-07-03 — yesterday)
```

## Package Legitimacy Audit

Seam command run: `gsd-tools query package-legitimacy check --ecosystem npm react-markdown remark-gfm react-resizable-panels`

| Package | Registry | Age | Downloads | Source Repo | Verdict | Disposition |
|---------|----------|-----|-----------|-------------|---------|-------------|
| react-markdown | npm | 9+ yrs (v10 line 2025) | 24.6M/wk | github.com/remarkjs/react-markdown | OK | Approved |
| remark-gfm | npm | 5+ yrs | 23.2M/wk | github.com/remarkjs/remark-gfm | OK | Approved |
| react-resizable-panels | npm | 3.5 yrs (created 2022-12), 203 versions | 35.3M/wk | github.com/bvaughn/react-resizable-panels | SUS (`too-new`) | Flagged — NOT recommended this phase (hand-roll instead); if a planner overrides, pin an exact version and insert `checkpoint:human-verify` before install |

**Packages removed due to [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** react-resizable-panels — the `too-new` signal fires on its latest release date (2026-07-03), not package age; signals otherwise clean (huge downloads, known maintainer, no postinstall). Treated as a heuristic false-positive, but the recommendation to hand-roll makes the flag moot.
**Postinstall scripts:** none on any of the three (verified via `npm view <pkg> scripts.postinstall`).

## Architecture Patterns

### System Architecture Diagram

```
                         WRITE PATH (in-cluster, per planner completion)
┌─────────────┐   writes    ┌─────────────────────────────────┐
│ Planner Job  │──────────▶│ PVC: /workspace/envelopes/<uid>/ │
│ (subagent)   │            │   *.md, children/*.json, out.json│
└──────┬──────┘            └────────────┬────────────────────┘
       │ Job terminal                    │ mounted by in-namespace Jobs only
       ▼                                 │
┌──────────────────────────┐             │
│ Level controller          │             │
│ handleJobCompletion       │             │
│  ├─ spawnReporterIfNeeded │──creates──▶ tide-reporter Job ── materializes child CRDs (unchanged)
│  ├─ triggerArtifactPush ★ │──creates──▶ tide-push Job (deterministic tide-push-<project-uid>)
│  └─ gate-park (approve)   │             │  mounts PVC + EnvFrom credsSecretRef
└──────────────────────────┘             │  integrate → stage .tide/ from envelope map ★
                                          │  → verify (Ph34) → commit (agent identity, Ph36)
                                          │  → gitleaks scan → push --force-with-lease
                                          ▼
                              ┌─────────────────────┐
                              │ Remote git run branch│  .tide/planning/<kind>/<name>/…
                              └──────────┬──────────┘
                                         │ READ PATH (on demand)
       ┌─────────────────────────────────┼─────────────────────────────┐
       │ cmd/dashboard (read-only SA)     ▼                             │
       │  ls-remote tip SHA ─▶ LRU hit? ─▶ miss: shallow clone ★        │
       │  (creds: clientset Secret get    (Depth1, SingleBranch,        │
       │   at fetch time, never cached)    NoTags, NoCheckout, memfs)   │
       │        │                                                       │
       │        ▼                                                       │
       │  GET /api/v1/nodes/{kind}/{name}/artifacts ★  (typed states:   │
       │  GET /api/v1/projects/{name}/settings ★        available/      │
       │  GET /api/v1/tasks/{name}/log (SSE, exists)    absent/no-git)  │
       └────────┬──────────────────────────────────────────────────────┘
                ▼
       React SPA: NodeClick(kind,name) ★ → right panel (resizable ★)
         ├─ markdown (react-markdown+gfm) / JSON pretty-print
         ├─ gate-parked: approve strip (ClipboardCopyAction)
         ├─ ProjectNode → settings cards + raw-spec YAML
         └─ PodLogStreamer: loading → streaming → pod-gone | error ★
            (named SSE listeners: pod-gone / error / idle-timeout ★)

★ = new in this phase; everything unstarred exists today.
```

### Recommended Project Structure (new/changed files)
```
internal/controller/
├── boundary_push.go        # extend: cumulative artifact map on every push trigger
├── dispatch_helpers.go     # new: triggerArtifactPush beside spawnReporterIfNeeded
cmd/tide-push/
└── main.go                 # extend: --stage-envelopes=<uid>:<destPrefix>,... globbing *.md + children/*.json
cmd/dashboard/
├── api/
│   ├── artifacts.go        # GET node artifacts endpoint (typed absent states)
│   ├── settings.go         # GET project settings (secret NAMES only, raw-spec YAML)
│   └── logs_sse.go         # widen resolvePodName to Succeeded/Failed pods
├── gitfetch/               # go-git shallow fetch + golang-lru cache (seam interface for CLI fallback)
dashboard/web/src/
├── components/
│   ├── NodeDetailPanel.tsx     # generalization of TaskDetailDrawer for planning nodes (D-05)
│   ├── ArtifactViewer.tsx      # markdown/JSON tabs + absent/materializing/error states
│   ├── ProjectSettingsPanel.tsx# D-10 cards + collapsible raw spec
│   ├── ApproveStrip.tsx        # D-08 pinned gate strip (ClipboardCopyAction)
│   ├── PodLogStreamer.tsx      # four-state rendering (D-12..14)
│   └── ResizeHandle.tsx        # shared hand-rolled drag/collapse hook + handle
├── lib/
│   ├── api.ts                  # fetchNodeArtifacts, fetchProjectSettings
│   └── sse.ts                  # named-event listener support (pod-gone/error/idle-timeout)
charts/tide/templates/
└── dashboard-rbac.yaml     # + secrets get (documented expansion)
```

### Pattern 1: Cumulative envelope staging inside tide-push (write path)
**What:** The controller passes `--stage-envelopes=<crUID>:<kind>/<name>,...` for every planner-completed level CR (it knows UIDs + names from the K8s API — it never needs to read the PVC). Inside the Job (PVC mounted at `/workspace`), tide-push globs `envelopes/<uid>/*.md` and `envelopes/<uid>/children/*.json` (excluding `out.json`/`in.json` per D-04), copies to `<worktree>/.tide/planning/<destPrefix>/`, stages via the existing `pkggit.AddPath`, and rides the existing commit/scan/push flow. Byte-identical restages fall into the existing `worktreeClean` skip — cumulative maps are idempotent.
**When to use:** Every push trigger — boundary AND the new planner-completion trigger — carries the full map (mirrors Phase 34's cumulative branch set).
**Why not `--artifact-paths`:** the existing flag stages src and dst at the SAME relative path (`copyIntoWorktree(src, dst)` with identical `rel`), which would put UID-keyed `envelopes/...` paths on the branch — violating D-02's human-readable requirement. A new src→dst mapping flag is required either way.
**Failure honesty (D-03):** a missing/unreadable envelope file exits nonzero with a typed envelope reason (e.g. `artifact-stage-failed`) → surfaces through the existing #13b push-retry state machine and its conditions. Never skip-and-continue.

### Pattern 2: Trigger placement + retry-while-parked (write path)
**What:** `triggerArtifactPush` is called in `handleJobCompletion` at all four planner-completion sites, immediately after `spawnReporterIfNeeded` and BEFORE the gate-park return. If the deterministic Job name is busy (Phase 34 single-flight), the trigger is a no-op — the cumulative map self-heals on the next push. Because a parked level may have no "next push" until approval, the `AwaitingApproval` early-return arm (e.g. `milestone_controller.go:256`) must also attempt the trigger with a `RequeueAfter` until a push containing this level's envelope succeeds.
**Tracking "has this level's artifacts landed":** planner decision — options: (a) fire-and-forget + RequeueAfter ~30s while parked and artifacts-absent (simplest; the push is idempotent so re-triggering is harmless); (b) a per-level status condition (e.g. `ArtifactsCommitted`) stamped when a push envelope that included this UID succeeds. Option (a) is recommended for v1 — no new status fields, and the UI's polling state (R-04) covers the visibility gap.

### Pattern 3: Shallow fetch + LRU (read path)
**What:** On artifact request: (1) resolve Project → `Spec.Git.RepoURL` + `Status.Git.BranchName`; (2) `remote.List` (ls-remote) for the branch tip SHA — cheap, no clone; (3) LRU lookup keyed `(namespace, project, sha)`; (4) on miss, fresh shallow clone `Depth:1, SingleBranch:true, ReferenceName:refs/heads/<branch>, Tags:git.NoTags, NoCheckout:true` into in-memory storage, walk the commit tree under `.tide/`, store the parsed file map in the LRU, discard the clone. **Never pull/fetch into an existing shallow clone** — that is the documented go-git failure mode (`object not found`, go-git#305/src-d#900). Fresh-clone-per-new-SHA sidesteps it entirely.
**Creds:** `clientset.CoreV1().Secrets(ns).Get(ctx, credsSecretRef, ...)` at fetch time; extract the same env key tide-push consumes (`GIT_PAT`); use `http.BasicAuth` for https remotes; anonymous for plain http (mirror tide-push's scheme-conditional logic). Never log, never cache the credential itself.
**Cache policy (discretion):** entries ≈ parsed `.tide/` trees; planning artifacts are small (KBs–100s of KBs). A `golang-lru/v2` sized ~32 entries with an aggregate-bytes guard (evict-on-insert until under, e.g., 64 MiB) is ample; expose sizes as Prometheus gauges beside the existing dashboard metrics.

### Pattern 4: Named-SSE-event mapping (DASH-04)
**What:** `useSSEStream` gains an `eventTypes?: string[]` option (or `useTaskLog` registers directly): `addEventListener("pod-gone"|"error"|"idle-timeout", ...)` on each EventSource instance. `useTaskLog` maps them to a terminal state machine: `pod-gone` → state `pod-gone`, STOP the reconnect loop (clear timer, don't reopen); `error` (backend-emitted) or repeated transport `onerror` → state `stream-error` with a manual reconnect affordance (re-mount/re-open resets backoff); `idle-timeout` → existing `idle-closed`. `PodLogStreamer` then renders: `connecting` → "Connecting…" (loading); `connected` + 0 lines → "Waiting for output…" ; lines → streaming; `pod-gone` → "Logs no longer available — pod garbage-collected." (message only, D-13); `stream-error` → distinct copy + Reconnect button (D-14); and — the missing case — `reconnecting` → visible "Reconnecting…" placeholder so no state ever renders empty.
**Backend widening:** `resolvePodName` currently returns only Running/Pending pods; include Succeeded/Failed so a finished-but-present pod streams its retained logs (kube pods/log serves terminated containers), then EOF → `pod-gone` frame ends the stream honestly. Distinguish "task exists, no pod at all" (GC'd → pod-gone immediately) — current behavior, correct.

### Pattern 5: Kind-aware node clicks (DASH-01/03)
**What:** `NodeClickContext` today is `(name: string) => void` and `TideNodeShell` only sets `clickable` for Plan nodes in the Planning DAG. Change the context value to `(kind: TideNodeKind, name: string) => void` (TideNodeShell already receives `kind` as a prop — one-line pass-through), set `clickable` for all Planning DAG kinds, and route in App: `project` → settings panel; `milestone|phase|plan` → artifact panel (plan keeps its existing PlanDetail pane content plus artifacts). Keep the `#/plan/<name>` deep-link behavior; add hash routes for the other kinds if cheap.

### Anti-Patterns to Avoid
- **A second git-writer path outside tide-push** — breaks Phase 34's single-flight serialization and the force-with-lease anchor (see R-05).
- **Reading Secrets through the cached controller-runtime client** — silently starts a cluster-wide Secret informer; RBAC and memory blowup (see R-02).
- **`rehype-raw` / any raw-HTML markdown path** — artifacts are LLM-authored (untrusted); react-markdown's default escaping is the mitigation. The repo's `no-dangerous-html.test.ts` will catch `dangerouslySetInnerHTML` but not a rehype-raw injection — don't add it.
- **Truncating or size-capping artifact content anywhere** — D-03 hard requirement. If a pathological artifact is too large to serve, fail with a typed error state, loudly.
- **Caching artifact content in CRD `.status` or ConfigMaps** — superseded transport; also violates the PERSIST rules.
- **Silent empty states** — every panel/drawer state must render explicit copy (the entire point of DASH-04).
- **Predicting this phase "just works" against Phase 34's in-flight changes** — Phase 34 lands first and rewrites `triggerBoundaryPush` internals; plan the integration against its landed shape, not today's. Write-path plans must carry the "Phase 34 (and 36) merged to main" precondition + execution-start re-verify step (see Dependency correction after R-05).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Markdown → React | Custom parser/renderer or `marked`+innerHTML | react-markdown + remark-gfm | CommonMark+GFM edge cases; XSS-safe vDOM output; innerHTML paths are banned by the repo's XSS guard test |
| Git transport/auth | Raw smart-HTTP protocol client | go-git v5 (already vendored) | Pack negotiation, auth schemes, redirects — already the repo standard |
| LRU eviction | Map + manual bookkeeping | hashicorp/golang-lru/v2 (already in module graph) | Correct concurrent eviction is fiddly; the dep is already resolved |
| YAML rendering of specs | String templating | sigs.k8s.io/yaml marshal server-side | Field ordering/escaping correctness |
| SSE client | fetch-streaming reimplementation | Browser EventSource (existing `sse.ts`) | Reconnect/Last-Event-ID semantics already handled; the fix is listener registration, not transport |

**Exception (deliberate hand-roll):** the two drag-to-resize handles — see Alternatives table. The library alternative forces a layout refactor owned by the deferred IDE-layout phase and carries a SUS-gate checkpoint.

**Key insight:** almost everything this phase needs already exists in-repo (staging pipeline, SSE contract, panel a11y, clipboard pattern, LRU dep, git lib). The plan should be dominated by *wiring and state mapping*, not new machinery.

## Common Pitfalls

### Pitfall 1: The empty log drawer has THREE stacked causes — fix all of them
**What goes wrong:** fixing only the frontend listener still leaves silent-empty windows.
**Why:** (1) named terminal events never listened for; (2) `reconnecting` state renders no placeholder; (3) server closes the stream after terminal frames, so the browser auto-reconnects forever against a GC'd pod (each reconnect re-emits `pod-gone`, unheard).
**How to avoid:** listener registration + `reconnecting` placeholder + terminal-state reconnect suppression, together. Verify per D-15 with both a running and a GC'd task.
**Warning signs:** network tab shows repeating `/log` requests every backoff interval while the drawer is blank.

### Pitfall 2: Force-with-lease anchor breaks if any push bypasses lastPushedSHA stamping
**What goes wrong:** artifact push advances the remote; next boundary push's `--force-with-lease=<branch>:<staleSHA>` rejects; pushes park with `lease-rejected`.
**How to avoid:** ride tide-push exclusively; its envelope success arm stamps `LastPushedSHA` (Phase 34 D-14). Never add a second push binary/path.
**Warning signs:** `lease-rejected` envelope reasons after this phase's changes land.

### Pitfall 3: go-git shallow-clone sharp edges
**What goes wrong:** pulling into a shallow clone fails `object not found`; default tag-following downloads the world; server-side object enumeration is much larger than native `--depth=1`.
**How to avoid:** fresh shallow clone per new tip SHA (never pull), `Tags: git.NoTags`, `SingleBranch`, `NoCheckout`, in-memory storage discarded after tree extraction. Wrap the fetcher in an interface so an exec-git fallback is swappable.
**Warning signs:** artifact fetch latency growing with target-repo size; memory spikes on fetch.
`[VERIFIED: go-git issues #545, #305, src-d#900 — see Sources]`

### Pitfall 4: Cached-client Secret reads
**What goes wrong:** `deps.Client.Get(&corev1.Secret{})` on the dashboard's informer-backed client starts a cluster-wide Secret informer → RBAC failure (no list/watch) at best, silent full-secret caching at worst.
**How to avoid:** typed clientset `Secrets(ns).Get` only. Add a code comment forbidding the cached path.

### Pitfall 5: Gitleaks scans artifact commits
**What goes wrong:** tide-push runs `gitleaks.ScanDiff` on every push. LLM-authored planning docs occasionally contain secret-shaped strings (example tokens, key-format documentation) → push blocked.
**Why it's (mostly) right:** secrets genuinely must not land on the run branch — this is fail-closed working as designed.
**How to avoid surprise:** document the behavior; the existing `LeaksConfigRef` ConfigMap override is the operator escape hatch. Surface the typed failure reason in the artifact-absent UI state if cheap.

### Pitfall 6: Bundle-size gate (≤500 KB gzipped JS+CSS)
**Current headroom:** 261 KB js + 6.9 KB css gzipped ≈ 268 KB used → ~232 KB free `[VERIFIED: gzip of cmd/dashboard/embed/dist assets]`. react-markdown+remark-gfm adds roughly 35–45 KB gz — fits, but run `make dashboard-frontend` (which runs `bundle-size.test.ts`) before committing, and remember `verify-dashboard-freshness` requires the embedded dist to be rebuilt and committed.

### Pitfall 7: react-markdown is ESM-only
**What goes wrong:** none expected under Vite/vitest (both ESM-native), but any future CJS interop (jest-style) would break. Keep it out of any Node-side scripts.
**Also:** style it via the `components` prop mapped to UI-SPEC tokens — do NOT add `@tailwindcss/typography`'s default prose (serif-ish, wrong for the ops aesthetic) without token overrides.

### Pitfall 8: AwaitingApproval early-return swallows work
**What goes wrong:** the parked branch (`Status.Phase == "AwaitingApproval"` → return early) runs before `handleJobCompletion` on subsequent reconciles — any trigger placed only inside `handleJobCompletion` never retries after park.
**How to avoid:** the retry-while-parked pattern (Pattern 2); test the parked-retrigger path explicitly in envtest.

### Pitfall 9: Node-click semantic — whose artifacts?
**What goes wrong:** clicking node X and showing the artifacts that *created* X (its parent's planner output) instead of the artifacts X's own planner produced.
**How to avoid:** lock the semantic: envelope of CR X = artifacts X's planner produced (X's planning `*.md` + `children/*.json` describing X's children). A leaf/parked node whose planner hasn't run yet has NO artifacts of its own — the panel should then say so (and optionally link the parent's artifact that specified it — discretion).

### Pitfall 10: DASH-02/ROADMAP wording is stale — the PLAN must carry this replacement wording
REQUIREMENTS.md:43 and Phase-37 success criterion 2 still specify size-capped owner-ref'd ConfigMaps, truncation markers, and CR-deletion GC. CONTEXT supersedes all three (D-01/D-03). This researcher may not edit REQUIREMENTS/ROADMAP; the PLAN must therefore carry the replacement wording so the verifier tests git transport, not ConfigMaps:

> **DASH-02 (reworded):** The level's planning `*.md` + `children/*.json` are committed to the run branch under `.tide/planning/<kind>/<name>/` at planner completion, via the tide-push staging pipeline. No truncation or size-capping anywhere in the pipeline — size problems fail loudly (typed envelope reason / condition), never silently trim (D-03). PVC and git remain the source of truth; the dashboard LRU is a rederivable display cache.
>
> **SC2 GC sub-criterion: retired, no analogue.** The owner-ref'd-ConfigMap/CR-deletion-GC clause has no git-transport equivalent — artifacts deliberately persist on the run branch as the durable decision record (D-02). The verifier must NOT look for artifact cleanup on CR deletion.

### Pitfall 11: D-10 says "models/effort" but no Effort field exists in v1.0.7
**What goes wrong:** a plan takes D-10's "provider + per-level models/effort" literally and hunts for an effort field to render — there is none. `grep -rn 'Effort' api/ internal/subagent/ charts/tide/values.yaml` returns zero matches; CLAUDE.md documents `--effort` as a known-unwired lever requiring a chart + provider-schema + subagent change owned by a later phase. `[VERIFIED: grep 2026-07-04 — zero matches]`
**How to avoid:** the plan must state explicitly: the settings card renders per-level **models only** (optionally an explicit "effort: not yet configurable" placeholder); D-10's effort column activates when effort plumbing ships. This is the conservative reading of the locked decision — D-10 curates existing spec fields, it does not mandate inventing one.
**Warning signs:** a plan task that adds an `Effort` field to the Project CRD or chart — that is out-of-scope CRD surface expansion, not a dashboard card.

## Code Examples

### Named SSE terminal-event listeners (the DASH-04 core fix)
```typescript
// dashboard/web/src/lib/sse.ts — inside open(), after project-event registration
// Source: existing sse.ts pattern + WHATWG EventSource (named events require addEventListener)
for (const type of ["pod-gone", "error", "idle-timeout"] as const) {
  es.addEventListener(type, ((e: MessageEvent) => {
    if (unmountedRef.current) return;
    onNamedRef.current?.(type, e);      // new callback option, mirrors onMessage
  }) as EventListener);
}
// useTaskLog: pod-gone → set terminal state AND suppress reconnect
// (clear pending timer; do not reopen — the pod will not come back)
```

### Safe markdown rendering (artifact viewer)
```tsx
// Source: github.com/remarkjs/react-markdown README (v10)
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";

<Markdown
  remarkPlugins={[remarkGfm]}
  // NO rehypePlugins — raw HTML in LLM output stays escaped (XSS-safe default).
  components={{
    // map to UI-SPEC tokens; e.g.
    code: (props) => <code style={{ fontFamily: "var(--font-mono)" }} {...props} />,
    a: (props) => <a target="_blank" rel="noreferrer" {...props} />, // urls sanitized by defaultUrlTransform
  }}
>
  {artifact.content}
</Markdown>
```

### Dashboard-side shallow fetch (read path skeleton)
```go
// Source: pkg.go.dev/github.com/go-git/go-git/v5 CloneOptions; repo precedent pkg/git/*.go
storer := memory.NewStorage()
repo, err := gogit.CloneContext(ctx, storer, nil, &gogit.CloneOptions{
    URL:           repoURL,
    Auth:          auth,                    // BasicAuth{Password: pat} for https; nil for http
    ReferenceName: plumbing.NewBranchReferenceName(branch),
    SingleBranch:  true,
    Depth:         1,
    Tags:          gogit.NoTags,            // Pitfall 3: default follows tags
    NoCheckout:    true,
})
// walk: repo.Head() → CommitObject → Tree → tree.Tree(".tide") → files → LRU
// NEVER fetch/pull into this repo later — new tip SHA ⇒ fresh clone.
```

### tide-push envelope staging (write path sketch)
```go
// cmd/tide-push/main.go — new flag, parsed like --artifact-paths (CSV of uid:destPrefix)
// inside runPush, before the existing ArtifactPaths loop:
for _, m := range cfg.StageEnvelopes { // {UID, DestPrefix}
    srcDir := filepath.Join(cfg.Workspace, "envelopes", m.UID)
    // D-04: *.md at top level + children/*.json; out.json/in.json excluded.
    // copy to filepath.Join(worktreeDir, ".tide", "planning", m.DestPrefix, rel)
    // then pkggit.AddPath(wt, tideRel) — same staging path as ArtifactPaths.
    // Missing dir ⇒ exit nonzero, envelope reason "artifact-stage-failed" (D-03: loud).
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| DASH-02 as size-capped ConfigMap display cache | Git run branch as artifact store; manager streams from remote | 2026-07-03 (phase discussion, D-01) | Supersedes STATE.md v1.0.7 constraint line; REQUIREMENTS/ROADMAP wording needs revision at planning |
| Ad-hoc PVC reader pods / `tide artifact-get` inspector pod | Dashboard artifact panel over the same content | this phase | `tide artifact-get` (busybox inspector pod) remains a CLI fallback; do not remove |
| `tideBotSignature()` hardcoded committer | Phase 36 agent-identity resolution at all commit sites | Phase 36 (lands before 37) | Artifact commits inherit automatically via tide-push reuse |
| react-resizable-panels `PanelGroup/PanelResizeHandle` API | v4 `Group/Panel/Separator` API | v4 (2026) | Training-data examples are stale — relevant only if the deferred docking phase adopts it |
| Plain `boundary_push` trigger internals | Phase 34 single-flight gate + verify-inside-push + lastPushedSHA stamp | Phase 34 (lands before 37) | This phase's trigger code must be written against Phase 34's landed shape |

**Deprecated/outdated:** none removed this phase; ConfigMap transport was never built (rejected before implementation).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | react-markdown+remark-gfm adds ~35–45 KB gzipped to the bundle | Pitfall 6 | Bundle gate failure at build; measured headroom (~232 KB) makes overflow very unlikely; the gate test catches it deterministically `[ASSUMED]` |
| A2 | react-markdown v10's `defaultUrlTransform` strips `javascript:` URLs by default | Code Examples / Security | If wrong, an LLM-authored artifact could carry a javascript: link; mitigation: verify in the component test (assert anchor href sanitization) during Wave 0 `[ASSUMED]` |
| A3 | Planner-completion push cadence (tens/run) won't contend meaningfully with Phase 34's single-flight gate | R-05 | Worst case: artifact visibility lags to the next push; self-heals by cumulative design `[ASSUMED]` |
| A4 | go-git `remote.List` (ls-remote) works against all three git hosts with BasicAuth PAT | Pattern 3 | Fallback: skip tip-check optimization and clone unconditionally per request with short TTL cache `[ASSUMED]` |
| A5 | Planning artifacts per level stay ≪ tens of MB (no need for streaming responses) | Pattern 3 | D-03 forbids truncation; if a pathological artifact appears, serve it anyway (HTTP handles large bodies) — only UI render perf suffers `[ASSUMED]` |
| A6 | `GIT_PAT` is the credential key the creds Secret carries (as consumed by tide-push) | Pattern 3 | If installs use other keys, fetch fails loudly → error state; mirror tide-push's exact env contract to stay consistent `[VERIFIED: cmd/tide-push/main.go GIT_PAT]` — assumption is only that dashboards see the same Secret shape |

## Open Questions

1. **Where exactly does the plan-level planner's envelope live vs the plan's task dispatches?**
   - What we know: every level controller spawns a reporter keyed on the parent CR UID; envelopes are per-CR-UID.
   - What's unclear: whether plan-level artifacts (PLAN.md + task children) have any naming quirks vs milestone/phase.
   - Recommendation: planner reads `plan_controller.go:~511` completion path during plan authoring; the staging map is UID-keyed so quirks are contained.
2. **Should the artifact panel for a gate-parked node auto-poll, and at what interval?**
   - What we know: R-01 gives a seconds-to-a-minute materialization window; SSE project events already push level status changes.
   - Recommendation: poll the artifact endpoint every ~10 s only while the panel is open AND the node is gate-parked AND state is `absent`; stop on `available`. Cheap and bounded.
3. **Hash-route deep-links for milestone/phase/project panels** — nice-to-have; existing `#/plan/<name>` precedent. Planner decides scope.
4. **Does `Status.Git.BranchName` exist before the first boundary push?**
   - What we know: `EnsureRunBranch` runs at clone time (clone-mode `--run-branch`), and `triggerBoundaryPush` reads `project.Status.Git.BranchName`.
   - What's unclear: exact reconcile point where BranchName is stamped vs the first planner completion.
   - Recommendation: planner greps `BranchName` stamp site; if it can be empty at first planner completion, the artifact trigger must skip-and-retry (same retry-while-parked machinery).

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | all Go builds/tests (`make test`, `make build`) | ✗ (not on PATH; not in asdf/homebrew) | — | Dev Docker VM per CLAUDE.md constrained-VM recipe — Go verification must run there |
| Node.js | frontend build/tests | ✓ | v22.22.3 (asdf) | — |
| npm | frontend deps | ✓ | bundled with node 22 | — |
| Docker daemon | image builds, kind loads | ✓ | server 29.5.3 | — |
| kind | `make test-int` Layer B; manual UAT | ✓ | v0.32.0 (`/opt/homebrew/bin/kind`, Docker 29.5.3 running) | — (`make test-int` still routes to the dev Docker VM — the Go toolchain, not kind, is the missing dependency; kind is usable locally for manual UAT alongside minikube) |
| minikube | manual UAT cluster | ✓ (running) | control plane Running | — |
| kubectl | cluster ops | ✓ | client v1.36.1 | — |
| helm | chart render / rbac assert | ✓ | v4.2.0 | — |
| git | everything | ✓ | 2.54.0 | — |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** Go toolchain only — all Go-side verification (unit envtest, integration, helm-rbac-assert via make) must run in the dev Docker VM because Go, not kind, is the missing dependency; frontend waves (vitest) can verify on this host directly. kind v0.32.0 + Docker 29.5.3 are present locally, so kind is usable for manual UAT alongside minikube. **Plans should split verification commands accordingly.**

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework (Go) | Ginkgo v2.28 + Gomega + envtest; plain `go test` for cmd binaries |
| Framework (frontend) | Vitest 1.6.1 + @testing-library/react + jsdom |
| Config files | `dashboard/web/vitest.config.ts`; Makefile targets for Go tiers |
| Quick run (frontend) | `cd dashboard/web && npx vitest run <file>` |
| Quick run (Go) | `go test ./cmd/dashboard/... ./cmd/tide-push/... -run <Test> ` (in Go-capable env) |
| Full suite | `make test` (unit tier) + `cd dashboard/web && npm run test`; `make test-int` for kind Layer B (VM only) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| DASH-01 | Artifact endpoint serves typed states + content | unit (Go) | `go test ./cmd/dashboard/api/ -run TestArtifacts` | ❌ Wave 0 (`cmd/dashboard/api/artifacts_test.go`) |
| DASH-01 | Panel renders markdown / JSON / absent states; approve strip on parked | component | `npx vitest run src/components/ArtifactViewer.test.tsx` | ❌ Wave 0 |
| DASH-01 | All-GET route invariant holds with new routes | unit (Go) | `go test ./cmd/dashboard/ -run TestZeroMutationRoutes` | ✅ exists — must stay green |
| DASH-02 | tide-push stages envelope map into `.tide/`, loud failure on missing file | unit (Go) | `go test ./cmd/tide-push/ -run TestStageEnvelopes` | ❌ Wave 0 (extend `main_test.go`) |
| DASH-02 | Controller triggers artifact push at planner completion + retries while parked | envtest | `go test ./internal/controller/ -run <spec>` (Ginkgo filter) | ❌ Wave 0 |
| DASH-02 | Reworded SC2 (Pitfall 10): planning `*.md` + `children/*.json` land on the run branch under `.tide/planning/<kind>/<name>/` at planner completion; no truncation; GC clause retired — artifacts persist on the run branch by design | kind (Layer B) | `make test-int` (VM) | ❌ Wave 0 addition to kind suite |
| DASH-03 | Settings endpoint redacts secret values (names only), includes outcome prompt + raw YAML | unit (Go) | `go test ./cmd/dashboard/api/ -run TestSettings` | ❌ Wave 0 |
| DASH-03 | Settings panel cards render; status strip uses Project lifecycle phase (D-11) | component | `npx vitest run src/components/ProjectSettingsPanel.test.tsx` | ❌ Wave 0 |
| DASH-04 | sse.ts receives named terminal events; pod-gone stops reconnect; error exposes reconnect | unit (TS) | `npx vitest run src/lib/sse.test.ts` | ✅ exists — extend |
| DASH-04 | PodLogStreamer renders all states incl. `reconnecting` (no empty render) | component | `npx vitest run src/components/PodLogStreamer.test.tsx` | ✅ exists — extend |
| DASH-04 | resolvePodName serves terminated-but-present pods | unit (Go) | `go test ./cmd/dashboard/api/ -run TestLogs` | ✅ `logs_sse_test.go` — extend |
| DASH-04 | Live repro per D-15: running task + GC'd task | manual-only | minikube/kind manual UAT — justification: requires real pod GC timing | — |

### Sampling Rate
- **Per task commit:** the targeted `vitest run <file>` or `go test -run <Test>` for the touched surface.
- **Per wave merge:** `cd dashboard/web && npm run test` (includes bundle-size + no-dangerous-html gates, needs `npm run build` first) AND `make test` in the Go-capable environment.
- **Phase gate:** full `make test` + `make test-int` (VM, clean-cluster recipe) + `make verify-dashboard-freshness` green before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `cmd/dashboard/api/artifacts_test.go` — DASH-01 endpoint states (available/absent/no-git/error)
- [ ] `cmd/dashboard/api/settings_test.go` — DASH-03 redaction contract
- [ ] `cmd/tide-push/main_test.go` extension — envelope staging + loud-failure paths
- [ ] envtest spec — artifact-push trigger + retry-while-parked
- [ ] `src/components/ArtifactViewer.test.tsx`, `ProjectSettingsPanel.test.tsx`, `ResizeHandle.test.tsx`
- [ ] Framework install: `npm install react-markdown@10.1.0 remark-gfm@4.0.1` (no new Go modules; promote golang-lru to direct)

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no (dashboard is unauthenticated-by-design inside trusted perimeter; mutation-auth seed not fired) | — |
| V3 Session Management | no | — |
| V4 Access Control | **yes** | Dashboard SA RBAC expansion limited to `secrets get` (no list/watch); `helm-rbac-assert` + `TestZeroMutationRoutes` remain the enforced invariants; reporter Job keeps zero git creds |
| V5 Input Validation | **yes** | react-markdown default escaping (no rehype-raw); URL param validation on new endpoints (kind allowlist: project/milestone/phase/plan; name/namespace passed to typed K8s Gets — no path concatenation into git paths without cleaning) |
| V6 Cryptography | no new crypto | git-over-HTTPS via go-git; PAT handling mirrors tide-push (env/memory only, never logged) |
| V12 Files & Resources | **yes** | `.tide/` path construction server-side from CR kind+name (K8s DNS-1123 names — no traversal chars); artifact file paths from git tree walk, never from client input |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| XSS via LLM-authored markdown artifacts | Tampering/Elevation | react-markdown vDOM rendering, raw HTML escaped, no rehype-raw; verify `javascript:` href sanitization in Wave 0 test (Assumption A2); `no-dangerous-html.test.ts` build gate |
| Secret value disclosure via settings endpoint | Information Disclosure | D-10 hard rule: secret NAMES only; redaction implemented server-side in the settings handler; unit test asserts no value-shaped fields in response |
| Git credential exposure in dashboard | Information Disclosure | Fetch-time clientset read; credential held only in the fetch call frame; never in LRU, logs, or responses |
| Dashboard SA privilege creep | Elevation | `get`-only Secret verb; `make helm-rbac-assert` gate; chart comment documenting the expansion and its rationale (Argo-Server-artifact-creds analogue) |
| Secret-shaped strings landing on the run branch via artifacts | Information Disclosure | Existing gitleaks ScanDiff in tide-push covers artifact commits automatically (Pitfall 5) |
| DoS via oversized artifact fetches | Denial of Service | Bounded LRU with byte guard; per-request context timeout on clone; D-03 forbids truncation, so the guard evicts cache — never trims content |

## Sources

### Primary (HIGH confidence — codebase, tool-verified)
- `cmd/tide-push/main.go`, `internal/controller/push_helpers.go`, `boundary_push.go` — artifact staging pipeline, lease, creds pattern
- `internal/controller/milestone_controller.go` / `phase_controller.go` / `dispatch_helpers.go` — gate-park ordering, reporter spawn, approval machinery
- `internal/controller/reporter_jobspec.go`, `cmd/tide-reporter/main.go`, `internal/reporter/materialize.go` — reporter Job contract (no creds, PVC subPath, envelope layout)
- `cmd/dashboard/api/logs_sse.go`, `dashboard/web/src/lib/sse.ts`, `components/PodLogStreamer.tsx` — DASH-04 root cause
- `cmd/dashboard/router.go`, `main.go`, `charts/tide/templates/dashboard-rbac.yaml` — read path + RBAC invariants
- `dashboard/web/src/__tests__/bundle-size.test.ts`, `no-dangerous-html.test.ts` — build gates; gzip measurement of embedded dist
- `.planning/phases/34-.../34-CONTEXT.md` — Phase 34 locked decisions (single-flight, verify-in-push, lastPushedSHA)
- npm registry (`npm view`) + `gsd-tools query package-legitimacy` — package verification 2026-07-04

### Secondary (MEDIUM confidence — official docs via WebFetch)
- github.com/remarkjs/react-markdown README — v10 API, XSS defaults, ESM-only, size
- github.com/bvaughn/react-resizable-panels README — v4 API shape (Group/Panel/Separator), collapsible, a11y

### Tertiary (LOW confidence — WebSearch, flagged)
- go-git shallow-clone limitations: [go-git#545 inefficient shallow clone](https://github.com/go-git/go-git/issues/545), [go-git#305 object-not-found on pull after Depth:1](https://github.com/go-git/go-git/issues/305), [src-d/go-git#900 Depth:1 pull issues](https://github.com/src-d/go-git/issues/900), [go-git discussion #1146](https://github.com/go-git/go-git/discussions/1146)

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — all Go deps already vendored; npm packages registry+legitimacy verified with official-README cross-check
- Architecture (write path): HIGH — every reused mechanism located and read in source; Phase 34/36 interplay verified against their CONTEXT/REQUIREMENTS
- Architecture (read path): MEDIUM — go-git shallow behavior verified only via issue reports (LOW-tier sources), mitigated by the fresh-clone-per-SHA design and a swappable fetcher seam
- Pitfalls: HIGH for the DASH-04 root cause (read directly in code); MEDIUM elsewhere
- Validation: HIGH — frameworks and gates observed; environment gap (no Go on host) probed directly

**Research date:** 2026-07-04
**Valid until:** 2026-08-03 (stable domain; re-check only if Phase 34/36 land with shapes diverging from their CONTEXT decisions)
