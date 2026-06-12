---
phase: 15
slug: paper-cuts
status: approved
reviewed_at: 2026-06-12
shadcn_initialized: false
preset: none
created: 2026-06-12
---

# Phase 15 — UI Design Contract (CUTS-05 Complete Chip + CUTS-06 Running-Waves View)

> Visual and interaction contract for the two UI cuts in Phase 15. CUTS-05: the `Complete` Project terminal status renders correctly instead of degrading to `Pending`. CUTS-06: the right pane's empty state is replaced by an aggregate "all running waves" view with click-through navigation. The other five cuts (CUTS-01/02/03/04/07) have no UI surface and are out of this contract's scope.

> **Scope guard:** This is NOT a phase redesign. Everything not listed under "Components in Scope" is untouched. The governing full-dashboard contract remains `.planning/milestones/v1.0.0-phases/04-gates-observability-dashboard-cli/04-UI-SPEC.md` (approved 2026-05-17), extended by `.planning/phases/14-budget-enforcement-pricing/14-UI-SPEC.md` (approved 2026-06-12 — ConditionBadge, TideNodeShell blocked border). All tokens, typography, spacing, and voice below are inherited verbatim, restated only where this phase consumes them.

> **Locked upstream (15-CONTEXT.md):**
> - **D-13** — the aggregate all-running-waves view is the RIGHT-PANE DEFAULT, replacing the "Select a plan to view its execution DAG" empty state in the existing two-column layout. No new navigation chrome.
> - **D-14** — rich wave cards: plan name, wave index, running/total count, task chips reusing the existing `<StatusBadge>` primitive.
> - **D-15** — server-side aggregate via label-selector queries over Tasks (waves derived, never stored), delivered over the EXISTING project-events SSE channel. Thin client; no client-side wave re-derivation. No new REST route.
> - **D-16** — clicking a wave card selects that plan; the right pane swaps to its `<ExecutionDAGView>`. The aggregate view doubles as a navigation surface.
> - **CUTS-05 discretion resolved here:** map Project `Complete` (api/v1alpha1/project_types.go:392 `PhaseComplete`) to success-family presentation as a first-class `StatusValue` — NOT a silent alias of `Succeeded`.
> - **Constraints:** dashboard stays read-only (no approve buttons); existing design system only (no new dependencies); Phase 16 adds a Telemetry tab to AppShell — leave AppShell/Header untouched; tide-metaphor naming for the wave view was deferred — plain prose ("running waves") wins, since "wave" is already the most-surfaced vocabulary word (04-UI-SPEC §Copywriting).

---

## Design System

| Property | Value |
|----------|-------|
| Tool | none (Tailwind v4 raw, no shadcn) — inherited, locked by 04-UI-SPEC |
| Preset | not applicable |
| Component library | none — bespoke primitives in `dashboard/web/src/components/` |
| Icon library | `lucide-react` (tree-shaken; only enumerated icons imported) |
| Font | System font stack; `--font-mono` for all badge text, resource names, wave labels (tokens in `src/index.css`) |

No shadcn gate applies: `components.json` is absent by approved prior decision (04-UI-SPEC "Rationale for no shadcn") and AUTO mode forbids re-asking. Registry safety: not applicable.

**New lucide imports this phase (enumerated):** `CircleCheckBig` (Complete badge icon), `Waves` (wave-card kind icon). Both from the already-pinned `lucide-react` dependency — no new packages; ~2KB total; bundle gates (<500KB gzipped) unaffected.

---

## Spacing Scale

Inherited from 04-UI-SPEC (8-point scale, multiples of 4; arbitrary values forbidden by lint). Values this phase consumes:

| Token | Value | Usage here |
|-------|-------|------------|
| `space-1` | 4px | Badge icon-to-label gap (`gap-1`), badge vertical padding (`p-1`) |
| `space-2` | 8px | Gap between wave cards in the list (`gap-2`); chip-row wrap gap (`gap-2`, both axes); card header icon-to-name gap (`gap-2`); badge horizontal padding (`px-2`) |
| `space-3` | 12px | Wave-card internal horizontal padding (`px-3`) — matches TideNodeShell rows |
| `space-4` | 16px | RunningWavesView container padding (`p-4`) |

Exceptions (inherited, unchanged): 14px badge/kind icon square. No new exceptions introduced.

---

## Typography

Inherited from 04-UI-SPEC. Roles this phase consumes:

| Role | Size | Weight | Line Height | Family | Usage here |
|------|------|--------|-------------|--------|------------|
| Mono badge | 12px | 600 | 1.4 | mono | StatusBadge labels (incl. new `Complete` row) — identical to existing badge formula |
| Mono name | 13px | 600 | 1.4 | mono | Wave-card plan name (matches TideNodeShell header label) |
| Mono label | 12px | 600 | — | mono | Wave-card `WAVE N · x/y running` label, view header `ALL RUNNING WAVES`, `All waves` pane-header button — matches the WaveBackground/PaneHeader label idiom |
| Heading | 18px | 600 | 1.3 | sans | Empty-state heading (`No running waves`) — matches EmptyState E2/E3 anatomy |
| Body | 14px | 400 | 1.5 | sans | Empty-state body copy |

No new sizes or weights. Bold (700+) and italics remain forbidden.

---

## Color

Inherited 60/30/10 tokens (dark default + light variant, both defined in `src/index.css`). **This phase introduces zero new color values.**

| Role | Token | Dark | Light | Usage here |
|------|-------|------|-------|------------|
| Success | `--color-status-success` | `#3FB950` | `#16A34A` | `Complete` badge (same family as `Succeeded` — terminal success) |
| Surface raised | `--color-surface-raised` | `#161B22` | `#F6F8FA` | Wave-card body (matches node shells) |
| Surface overlay | `--color-surface-overlay` | `#1F2937` | `#E5E7EB` | Wave-card hover state |
| Border subtle | `--color-border-subtle` | `#30363D` | `#D1D5DB` | Wave-card border, card internal divider |
| Text muted | `--color-text-muted` | `#8B949E` | `#4B5563` | Wave labels, counts, kind icons, empty-state body |
| Accent | `--color-accent` | `#06B6D4` | `#0891B2` | Focus ring on keyboard-focused wave cards and the `All waves` button ONLY (already #5 on the inherited accent reserved-for list) |

**Why `Complete` is green, not a new hue:** `Complete` is the Project CRD's terminal-success value (`PhaseComplete`, project_types.go:392) — the project-level sibling of the child CRDs' `Succeeded`. Same semantic family → same `--color-status-success` token. Disambiguation from `Succeeded` is by icon + label (`CircleCheckBig` vs `CircleCheck`), honoring the inherited rule that statuses sharing a color hue carry distinct icons and are never color-only.

**Accent reserved-for list: unchanged** from 04-UI-SPEC (6 surfaces). Wave cards do NOT use accent for selection, hover, active-dispatch, or borders — hover is `--color-surface-overlay`, exactly like node shells. Destructive reserved-for list: unchanged; nothing destructive ships this phase.

---

## Components in Scope

Exactly five touch points. Match surrounding file conventions exactly (doc-comment style, `data-testid` patterns, UI-SPEC § references in comments).

### C1. `<StatusBadge>` — `Complete` vocabulary row (CUTS-05)

`StatusValue` union in `dashboard/web/src/components/StatusBadge.tsx` grows from 10 to 11 values: insert `"Complete"` immediately after `"Succeeded"` (terminal-success family grouping). Add the matching `STATUS_TABLE` row:

**Status Vocabulary addition (verbatim — extends the 04-UI-SPEC §Status Vocabulary table):**

| CRD Status | Color | lucide Icon | Label | Animation | SR Description |
|------------|-------|-------------|-------|-----------|----------------|
| `Complete` | `--color-status-success` | `CircleCheckBig` | `Complete` | none | "Complete — all milestones succeeded" |

Badge construction is the existing verbatim formula (15% tint background, 40% alpha border, full-saturation foreground, 4px radius, 12px/600 mono) — the new row inherits it automatically through `STATUS_TABLE`. `data-testid="status-badge-Complete"`, `data-status="Complete"` follow from the existing render path with zero component changes.

**Icon rationale (`CircleCheckBig`, not `CircleCheck`):** `Complete` (Project terminal) and `Succeeded` (child-level terminal) share green and can appear side by side in the Planning DAG (Project node beside Milestone nodes). The inherited color-blindness rule — "statuses that share a color hue have distinct icons" — requires a distinct glyph. `CircleCheckBig` is the visually-adjacent lucide sibling: same checkmark semantics, distinguishable silhouette. (RESEARCH suggested reusing `CircleCheck`; this contract overrides to preserve the distinct-icon rule. Claude's discretion per 15-CONTEXT.)

### C2. Coerce-guard synchronization (CUTS-05)

Both runtime guards that degrade unknown phases to `Pending` must accept `Complete`:

1. `KNOWN` array in `dashboard/web/src/components/PlanningDAGView.tsx` (:61-72) — add `"Complete"`.
2. `KNOWN_STATUSES` array in `dashboard/web/src/components/ProjectPicker.tsx` (:30-41) — add `"Complete"`.

These are the only two coerce sites (verified in 15-RESEARCH A2). **Recommended (planner discretion, not required):** consolidate by exporting a single `KNOWN_STATUS_VALUES` list from `StatusBadge.tsx` derived from `Object.keys(STATUS_TABLE)` and consuming it at both sites — this kills the silent-drift bug class (a Go-API status value added without a `KNOWN` update reproduces run-1 finding 9b) permanently. The required outcome either way: `coerce("Complete")` and `coerceStatus("Complete")` return `"Complete"`, and every other unknown string still coerces to `"Pending"`.

**Where `Complete` then surfaces (no further changes needed):** the ProjectNode status chip in the Planning DAG and the ProjectPicker row badge. `Complete` is NOT added to `FAILED_STATUSES` (TideNodeShell/ExecutionDAGView) — no border treatment; it is a plain success badge.

### C3. `<RunningWavesView>` — new component (CUTS-06, D-13/D-14/D-16)

New file `dashboard/web/src/components/RunningWavesView.tsx`. Mounted as the right pane's default content (see C4). Vertically-scrolling list of wave cards, one per currently-running wave across ALL Plans of the selected project.

**Semantics (contract for both server and client):** a wave is "running" iff ≥ 1 member Task has phase ∈ {`Running`, `Dispatching`}. The card's running count = member Tasks in {`Running`, `Dispatching`}; total = all member Tasks in that (plan, wave-index) group. Waves ordered by plan name asc, then wave index asc; tasks within a wave ordered by name asc. Client renders payload order — it never re-derives or re-sorts waves (D-15; spec §derived-waves).

**Props:**
```ts
export type RunningWaveTask = { name: string; status: string }; // status coerced via the shared KNOWN guard
export type RunningWave = {
  planName: string;   // click-through arg — matches PlanNode click semantics
  waveIndex: number;  // 0-based, same as WaveBackground labels
  tasks: RunningWaveTask[];
};
type Props = {
  projectName: string;
  onPlanClick: (planName: string) => void;       // App.tsx's existing callback (sets hash + selectedPlan)
  initialSnapshot?: RunningWave[];               // tests bypass SSE, mirroring PlanningDAGView.initialData
};
```

**Layout:** container `p-4`, `flex flex-col gap-2`, vertical scroll (`overflow-y-auto`, fills pane). Header line above the cards: mono 12px/600 in `--color-text-muted`, text `ALL RUNNING WAVES` (static — the cards carry their own counts). `data-testid="running-waves-view"`.

**Wave card anatomy (mirrors the TideNodeShell two-row shell — header / divider / body):**

```
┌──────────────────────────────────────────────────────────┐
│ ≈ 15-02-running-waves            WAVE 2 · 3/8 running    │ ← header: Waves icon 14px muted + plan name (mono 13/600, truncate, title tooltip) + wave label (mono 12/600 muted, shrink-0)
│ ─────────────────────────────────────────────────────────│ ← 1px --color-border-subtle divider
│ [◌ Pending] [◐ Running] [◐ Running] [✓ Succeeded] …      │ ← task chips: one <StatusBadge> per task, flex-wrap, gap-2 both axes
└──────────────────────────────────────────────────────────┘
```

- **Card chrome:** full pane width; `rounded border border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)]`; rows `px-3 py-2`; hover `bg-[var(--color-surface-overlay)]`; `cursor-pointer`. NO accent ring, NO border-left treatments — cards are a navigation list, not status nodes.
- **Header row:** `Waves` lucide icon (14px, `--color-text-muted`, `aria-hidden`, `data-icon="Waves"`), plan name `min-w-0 flex-1 truncate` with native `title={planName}`, wave label `shrink-0` with locked format `WAVE <N> · <running>/<total> running` (extends the existing `WAVE N · X tasks` WaveBackground idiom).
- **Chip row:** each task renders `<StatusBadge status={coerced} />` wrapped in a `<span title={task.name} data-testid="wave-card-chip">` — hover identifies the task; the chips' visual job is the at-a-glance running/done ratio. The chip row is `aria-hidden="true"` (decorative density read — counts live in the card's aria-label, same doctrine as the DAG's decorative edges). Task statuses pass through the SAME coerce guard as C2 (unknown → `Pending`). No chip cap in v1: rows wrap and the card grows (operator density tolerance is high; cap is a v1.x concern).
- **Interaction (D-16):** entire card is the click target. `role="button"`, `tabIndex={0}`, click or Enter/Space fires `onPlanClick(planName)` — reusing App.tsx's existing callback so the URL hash updates and the selection is deep-linkable, identical to clicking a PlanNode. Focus ring: inherited global `:focus-visible` 2px accent.
- **Identity/aria:** `data-testid="wave-card-<planName>-<waveIndex>"`, `data-plan={planName}`, `data-wave-index={waveIndex}`. `aria-label="plan <planName>, wave <N>, <running> of <total> tasks running"`.

**View states:**

| State | Render |
|-------|--------|
| No snapshot received yet | L2 pane-loading pattern (04-UI-SPEC §15): centered `Loader2` spinner in pane bounds |
| Snapshot with `waves: []` | Empty state — new `no-running-waves` variant added to `EmptyState.tsx`, following the E2/E3 anatomy (centered; h2 18px/600 heading + 14px muted body). Copy locked in the Copywriting Contract below |
| Snapshot with ≥ 1 wave | Header line + card list |
| Malformed/unparseable snapshot event | Ignore the event, keep last good state (defensive parse, mirroring PlanningDAGView's `PLANNING_KINDS` JSON guard). No error UI — stream-level failures are already covered by the connection pill + SSE toasts |

**Data path (D-15):** the component subscribes to the EXISTING project-events SSE channel (`projectEventsURL(projectName)` via `useSSEStream`) — consistent with the established multiple-subscriber pattern (PlanningDAGView, useTasks, useTaskDetail each hold their own subscription to the same URL). It consumes a new **named SSE event type `waves.snapshot`** carrying the full current aggregate:

```json
{ "waves": [ { "planName": "…", "waveIndex": 1, "tasks": [ { "name": "…", "status": "Running" } ] } ] }
```

Snapshot-replace semantics: each event replaces the whole view state (no client-side merging/diffing — thin client). The server emits a snapshot on SSE subscribe (so the default pane populates without waiting for a Task transition) and on Task changes (debounce/coalescing is the planner's backend concern). Zero running waves serializes as `"waves": []`, never `null` (inherited empty-array contract).

**Integration pitfall (build-blocking if missed):** `SSE_PROJECT_EVENT_TYPES` in `dashboard/web/src/lib/sse.ts` registers per-name `addEventListener`s — named SSE events NOT in that list never reach `onMessage`. `waves.snapshot` must be added to the listener registration (the name deliberately follows the existing `<kind>.<action>` wire convention; plural `waves` keeps it distinct from the Wave-CRD `wave.create/update/delete` events).

### C4. `App.tsx` — right-pane default swap + return affordance (D-13)

In the right-pane branch (App.tsx ~:217-229), replace the `selectedPlan === null` empty-state `div` ("Select a plan to view its execution DAG") with:

```tsx
<RunningWavesView
  projectName={selectedProject ?? ""}
  onPlanClick={onPlanClick}
/>
```

The `PaneHeader label="EXECUTION"` strip and the two-column grid are unchanged. The `selectedPlan !== null` branch (ExecutionDAGView) is unchanged.

**Return affordance (researcher discretion — required for the aggregate to stay reachable):** when `selectedPlan !== null`, the EXECUTION pane header gains a right-aligned text button labeled `All waves` — mono 12px/600, `--color-text-muted`, hover `--color-text-primary`, no accent, no border. Click (or Enter) sets `selectedPlan` to `null` and clears the `#/plan/...` URL hash, returning the right pane to the aggregate. Without it, the default view is unreachable after the first plan selection except by reload. `data-testid="execution-pane-all-waves"`, `aria-label="Show all running waves"`. Implementation note: `PaneHeader` may grow an optional `action?: ReactNode` slot — keep the PLANNING pane's call untouched.

**Phase 16 seam (do not foreclose):** all changes live inside App.tsx's `body` branching and the new component. `AppShell`, `Header`, and the grid skeleton are untouched — the Telemetry tab (TELEM-02) lands in AppShell next phase against a clean diff surface.

### C5. Backend — `waves.snapshot` aggregate (server half of D-15)

UI-visible contract only (implementation seating is the planner's):

- Aggregate derived per-request/per-event from **label-selector queries over Tasks** (`tideproject.k8s/project` + the already-stamped `tideproject.k8s/wave-index` label), grouped by (plan ref, wave index). Waves are NEVER stored in CRD status (`make verify-no-aggregates` must stay green); reuse of `inspectWaveRun`'s grouping (cmd/tide/inspect_wave_run.go:77-101) via a shared helper is the recommended shape.
- Delivered as SSE event type `waves.snapshot` on the existing `/api/v1/projects/{name}/events` stream — **no new REST route** (D-15; 15-RESEARCH Pitfall 6).
- Running-wave filter, counts, and sort order exactly as defined in C3 §Semantics. Payload bounded to running waves only (terminal waves excluded by construction).
- Read-only GET/SSE surface — `TestZeroMutationRoutes` invariant untouched.

**Out of scope (do NOT build):** per-chip click-through to TaskDetailDrawer (chips are decorative; the operator clicks through to the plan's ExecutionDAGView first); wave-card popovers or expanded task lists; cross-PROJECT aggregation (the SSE channel is project-scoped — the aggregate spans plans within the selected project, per the requirement); a `status.runningWaves` CRD field or any cached schedule (CLAUDE.md anti-pattern); approve/resume buttons anywhere (read-only contract); Telemetry tab work (Phase 16); "currents"/metaphor naming for the view (deferred — plain prose locked); `Complete` chips on Milestone/Phase/Plan/Task nodes (those CRDs use `Succeeded`; `Complete` is Project-only).

---

## Copywriting Contract

Voice inherited: terse, declarative, operator-focused, em dashes, no exclamation points.

| Element | Copy |
|---------|------|
| Primary CTA | none — read-only surface; wave cards are navigation, not actions |
| View header line | `ALL RUNNING WAVES` (mono, muted — sibling of the `PLANNING`/`EXECUTION` pane labels) |
| Wave-card label | `WAVE <N> · <running>/<total> running` (e.g. `WAVE 2 · 3/8 running`) — extends the locked `WAVE N · X tasks` idiom |
| Card aria-label | `plan <planName>, wave <N>, <running> of <total> tasks running` |
| Empty state heading (no running waves) | `No running waves` |
| Empty state body | `Wave cards appear here while task Jobs run — select a plan to view its execution DAG.` |
| Loading state | none (spinner only — L2 pane-loading pattern) |
| `All waves` return button | `All waves` (aria-label: `Show all running waves`) |
| `Complete` badge label | `Complete` |
| `Complete` SR description | `Complete — all milestones succeeded` |
| Error state | none new — malformed snapshot events are ignored (last good state persists); stream failures surface through the existing connection pill + SSE toast vocabulary |
| Destructive confirmation | none — no destructive actions in this phase |

---

## Validation Contract

| Surface | Test | Framework |
|---------|------|-----------|
| StatusBadge | `Complete` row renders icon `CircleCheckBig` + label + success color per vocabulary table; `STATUS_TABLE` has 11 entries | Vitest (extend `StatusBadge` spec's table iteration) |
| PlanningDAGView | Project node with `phase: "Complete"` renders `status-badge-Complete`, NOT `status-badge-Pending` (run-1 finding-9b regression — assert the symptom, not just the green path); unknown phase still coerces to `Pending` | Vitest |
| ProjectPicker | row with `phase: "Complete"` renders the Complete badge (second coerce site, RESEARCH A2) | Vitest |
| RunningWavesView | snapshot → cards with plan name, `WAVE N · x/y running` label, one chip per task; chip-row `aria-hidden`; card click + Enter fire `onPlanClick(planName)`; `waves: []` → `No running waves` empty state; pre-snapshot → spinner; malformed event ignored | Vitest (`RunningWavesView.test.tsx`, new) |
| App | right pane defaults to RunningWavesView when `selectedPlan === null` (the old empty-state string is GONE); card click swaps to ExecutionDAGView + sets `#/plan/<name>`; `All waves` button returns to the aggregate and clears the hash | Vitest (extend App spec) |
| Backend aggregate | label-selector grouping by (plan, wave-index); running-wave filter ({Running, Dispatching} membership); deterministic sort; snapshot emitted on SSE subscribe and on Task change; `[]`-not-null | Go (`cmd/dashboard/api` tests, new) |
| Invariants | `make verify-no-aggregates` green (no waves in CRD status); `TestZeroMutationRoutes` green; bundle gates green | Go / CI |

---

## Registry Safety

| Registry | Blocks Used | Safety Gate |
|----------|-------------|-------------|
| shadcn official | none — not initialized | not applicable |
| Third-party registries | none | not applicable |

No new packages. New lucide imports (`CircleCheckBig`, `Waves`) come from the already-pinned `lucide-react` dependency (~2KB, tree-shaken).

---

## Checker Sign-Off

- [ ] Dimension 1 Copywriting: PASS
- [ ] Dimension 2 Visuals: PASS
- [ ] Dimension 3 Color: PASS
- [ ] Dimension 4 Typography: PASS
- [ ] Dimension 5 Spacing: PASS
- [ ] Dimension 6 Registry Safety: PASS

**Approval:** pending

---

*UI Design Contract for: Phase 15 — Paper Cuts (CUTS-05, CUTS-06)*
*Authored: 2026-06-12 via gsd-ui-researcher (AUTO mode — zero user questions; all decisions derived from 04-UI-SPEC + 14-UI-SPEC + 15-CONTEXT.md D-13..D-16 + codebase inspection)*
*Inherits: 04-UI-SPEC.md tokens, typography, spacing, voice (approved 2026-05-17); 14-UI-SPEC.md ConditionBadge/TideNodeShell extensions (approved 2026-06-12)*
