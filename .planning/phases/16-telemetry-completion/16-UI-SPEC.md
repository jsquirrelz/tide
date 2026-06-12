---
phase: 16
slug: telemetry-completion
status: draft
shadcn_initialized: false
preset: none
created: 2026-06-12
---

# Phase 16 — UI Design Contract (Telemetry Completion)

> Visual and interaction contract for the dashboard half of Phase 16: the D-01 header view switcher (TELEM-02) and the finished TelemetryView (TELEM-02/04 — scope toggle, range selector, polling, real recharts time-series panels, per-project budget card grid). The Go-side requirements (TELEM-01/03/05/06) have no UI surface and are out of this contract's scope.

> **Scope guard:** This is NOT a dashboard redesign. The governing full-dashboard contract remains `.planning/milestones/v1.0.0-phases/04-gates-observability-dashboard-cli/04-UI-SPEC.md` (approved 2026-05-17) — all tokens, typography, spacing, voice, and accessibility rules below are inherited from it verbatim, restated only where this phase consumes them. The DAGs view (PlanningDAGView + ExecutionDAGView/RunningWavesView two-pane body, including Phase 15's D-13 RunningWavesView right-pane default) is passed through **unchanged** behind the new switcher.

> **Locked upstream (16-CONTEXT.md):** D-01 header view switcher ("DAGs" / "Telemetry"), D-02 selected-project scope + all-projects toggle, D-03 per-project budget card grid in all-projects mode, D-04 follow-the-picker default (toggle is transient, not persisted), D-05 real time-series charts (recharts 3.8.1 confirmed by 16-RESEARCH.md — SVG/DOM, no canvas), D-06 panel query shapes (exact PromQL = planner discretion within the shapes), D-07 24h/7d/30d range selector + 30–60s polling (no SSE for Prometheus data).

---

## Design System

| Property | Value |
|----------|-------|
| Tool | none (Tailwind v4 raw, no shadcn) — inherited, locked by 04-UI-SPEC |
| Preset | not applicable |
| Component library | none — bespoke primitives in `dashboard/web/src/components/` |
| Icon library | `lucide-react` (tree-shaken) — this phase adds NO new icons |
| Charting | `recharts@3.8.1` — the single new npm dependency (slopcheck `[OK]`, 16-RESEARCH.md Package Legitimacy Audit) |
| Font | System font stack; `--font-mono` for resource names, figures, panel titles (tokens in `src/index.css`) |

No shadcn gate applies: `components.json` is absent by approved prior decision (04-UI-SPEC "Rationale for no shadcn"); AUTO mode forbids re-asking. Registry safety: see final section.

---

## Spacing Scale

Inherited from 04-UI-SPEC (8-point scale, multiples of 4; arbitrary Tailwind values forbidden by `tailwindcss/no-arbitrary-value`). Values this phase consumes:

| Token | Value | Usage here |
|-------|-------|------------|
| `space-1` | 4px | Segmented-control internal button padding (`px-2 py-1` vertical), gap inside legend entries |
| `space-2` | 8px | Segment horizontal padding (`px-2`), gap between toolbar controls and their labels |
| `space-3` | 12px | Gap between connectionStatus-group items (header, existing `gap-3`) |
| `space-4` | 16px | Panel internal padding (`p-4`, existing), grid gaps (`gap-4`), TelemetryView outer padding, header left-group gap (`gap-4`) |

**Component dimensions (not spacing-scale values — declared so lint/checker don't flag them):**

- Chart plot area: fixed `height={180}` on `ResponsiveContainer` (recharts needs a definite height; `"100%"` collapses to 0 — 16-RESEARCH Pitfall 5). Set via recharts prop, not a Tailwind class.
- Budget card grid min column width: 240px via inline style `gridTemplateColumns: "repeat(auto-fill, minmax(240px, 1fr))"` (inline styles are the established pattern in TelemetryView.tsx; arbitrary Tailwind values stay forbidden).
- Header height stays 48px (`h-12`) — the view switcher must fit inside it (segments are 12px text + `py-1`, ~26px tall).

No new spacing exceptions introduced.

---

## Typography

Inherited from 04-UI-SPEC (3 sizes + 1 mono variant, 2 weights). Roles this phase consumes, plus ONE new phase exception:

| Role | Size | Weight | Line Height | Family | Usage here |
|------|------|--------|-------------|--------|------------|
| Body | 14px | 400 | 1.5 | sans | (rarely used in this view — degradation notice body stays mono per existing component) |
| Label | 12px | 600 | 1.4 | sans | View-switcher tab labels, scope-toggle and range-selector segment labels (matches the theme-toggle button: `fontSize: var(--text-label)`, `fontWeight: 600`) |
| Mono | 13px | 400 | 1.4 | mono | Project names on budget cards, "of $X cap" line, axis tick labels may use 12px (see chart text below) |
| Panel title (existing) | 12px | 600 | 1.4 | mono, uppercase, `letterSpacing: 0.05em` | ChartPanel `<h3>` — already in TelemetryView.tsx; keep verbatim |
| **Metric figure (NEW — phase exception)** | **20px** | **600 (semibold)** | **1.2** | **mono** | Budget card spend figure ONLY |

**Compliance fix required:** the existing `BudgetCard` renders the spend figure at `24px / 700` — weight 700 is **forbidden** by the inherited typography contract. The reworked card uses the Metric figure role above (20px / 600 / mono). This is the only new size this phase introduces; nothing else may use it.

**Chart text:** recharts axis ticks, tooltip text, and legend labels render at 12px, `--font-mono`, `--color-text-muted` (ticks/legend) or `--color-text-primary` (tooltip values). No other in-chart text sizes.

Bold (700+), italics remain forbidden.

---

## Color

Inherited 60/30/10 tokens (dark default + `.light-theme` overrides — both already in `src/index.css`). **This phase introduces ZERO new color values.** Chart series colors are drawn from the existing status-family tokens, used here as a data-visualization palette (a distinct use from status badges; both themes already define every value).

| Role | Token | Dark | Usage here |
|------|-------|------|------------|
| Dominant (60%) | `--color-surface-base` | `#0B0F14` | TelemetryView background (inherited from AppShell body) |
| Secondary (30%) | `--color-surface-raised` | `#161B22` | ChartPanel + BudgetCard backgrounds (existing) |
| Secondary (sub-tier) | `--color-surface-overlay` | `#1F2937` | ACTIVE segment background in all segmented controls; tooltip background |
| Accent (10%) | `--color-accent` | `#06B6D4` | NOT used by anything new in this phase except inherited focus rings (`:focus-visible`, global) |
| Border subtle | `--color-border-subtle` | `#30363D` | Panel borders, segmented-control borders, chart grid lines (dashed), tooltip border |
| Text muted | `--color-text-muted` | `#8B949E` | Inactive segment labels, axis ticks, legends, empty/degraded copy |

**Chart series palette (ordered; cycle on overflow in all-projects mode):**

| Slot | Token | Dark value | Assigned to |
|------|-------|-----------|-------------|
| series-1 | `--color-status-running` | `#06B6D4` | Cost Over Time (single series); `input` tokens; `waves dispatched`; 1st project in all-projects mode |
| series-2 | `--color-status-success` | `#3FB950` | `output` tokens; `tasks completed`; 2nd project |
| series-3 | `--color-status-warning` | `#D29922` | `cache read` tokens; 3rd project |
| series-4 | `--color-status-blocked` | `#A371F7` | `cache creation` tokens; 4th project |
| series-5 | `--color-status-pending` | `#8B949E` | 5th project (then cycle) |
| failure | `--color-status-error` | `#F85149` | Failure Rate series ONLY (semantic: it plots failures) |

Series construction: `stroke` = token at full value, `strokeWidth` 2; `fill` = same token, `fillOpacity` 0.2 (stacked Token Breakdown uses `fillOpacity` 0.35 so layers stay distinguishable). Reference tokens via `var(--color-status-*)` so the light theme switches automatically.

**Accent reserved-for list: unchanged from 04-UI-SPEC (6 surfaces).** The view switcher's active segment, the scope toggle, and the range selector do NOT use accent — active state is `--color-surface-overlay` background + `--color-text-primary` text (the established hover/selection-without-action treatment). Destructive reserved-for list: unchanged; this phase has no destructive surfaces. Budget card status line keeps its existing `--color-status-success` / `--color-destructive` "Within budget"/"Over budget" treatment — never color-only (the text label carries the meaning).

---

## Components in Scope

Five touch points. Match surrounding file conventions exactly (doc-comment style, `data-testid` patterns, inline-style token usage, UI-SPEC § references in comments). Everything not listed is untouched — explicitly including `AppShell.tsx`'s layout contract, `RunningWavesView`, `PlanningDAGView`/`ExecutionDAGView`, `ProjectPicker`, and `TelemetryUnavailableNotice.tsx`.

### C1. Segmented control pattern (shared visual contract)

The view switcher (C2), scope toggle, and range selector (both C3) all use ONE visual construction — planner's discretion whether it's a shared `Segmented` component or repeated inline, but the rendered contract is identical:

- Container: `inline-flex rounded border` with `borderColor: var(--color-border-subtle)`; background transparent.
- Segment: `<button type="button">`, `px-2 py-1`, 12px / 600 sans (`var(--text-label)`), no icon.
- Active segment: `background: var(--color-surface-overlay)`, `color: var(--color-text-primary)`.
- Inactive segment: transparent background, `color: var(--color-text-muted)`; hover → `background: var(--color-surface-overlay)` (text stays muted until active).
- No accent anywhere; focus ring comes from the global `:focus-visible` rule (do not suppress).
- Disabled segment (scope toggle with no project selected): `opacity: 0.5`, `cursor: default`, `aria-disabled="true"`.

### C2. `<Header>` view switcher slot + App.tsx view state (D-01)

- `HeaderProps` gains `viewSwitcher?: ReactNode` (optional slot, same pattern as `projectPicker`). Rendered in the LEFT group, immediately after `{projectPicker}`, inside the existing `gap-4` flex row. Header height stays 48px; right group (connectionStatus + theme toggle) untouched.
- The switcher itself is built in `App.tsx` (state owner): `const [activeView, setActiveView] = useState<"dags" | "telemetry">("dags")`. Default view is **DAGs** — the existing two-pane body renders exactly as today (Phase 15 right-pane logic passed through unchanged). Transient state — not persisted to localStorage.
- Segments: `DAGs` then `Telemetry` (this order — primary surface first).
- Semantics: container `role="tablist"` `aria-label="Dashboard view"`; each segment `role="tab"` with `aria-selected`; ArrowLeft/ArrowRight move selection between the two tabs (roving focus). `data-testid="view-switcher"`, segments `data-testid="view-tab-dags"` / `data-testid="view-tab-telemetry"`.
- Body switch in App.tsx: `activeView === "telemetry"` → full-width `<TelemetryView …>` fills the `<main>` slot (no two-pane grid, no split-ratio involvement); otherwise the existing two-pane body. The Telemetry body does not mount the DAG views (and vice versa) — simple conditional render, no display:none preservation required.

### C3. `<TelemetryView>` toolbar — scope toggle + range selector (D-02, D-04, D-07)

A toolbar row at the top of TelemetryView (above the budget surface), `flex items-center justify-between`:

- **Left — scope toggle** (segmented control): segment 1 = the selected project's name (mono is acceptable here for the resource name — render the label at 12px/600 but `--font-mono` for this segment only, matching the resource-names-are-mono rule); segment 2 = `All projects`.
  - D-04 defaults: opening Telemetry with a project selected → project segment active; no project selected → `All projects` active and the project segment is **not rendered** (single-segment control, effectively a static scope indicator).
  - Changing the ProjectPicker selection while on the Telemetry tab re-derives the scope: new project → selected-project mode for it; cleared selection → all-projects mode. The toggle is transient (never persisted).
  - `data-testid="telemetry-scope-toggle"`.
- **Right — range selector** (segmented control): segments `24h` / `7d` / `30d`, default `24h`. `data-testid="telemetry-range-selector"`.
  - Range → query window + step mapping (prescriptive defaults; planner may adjust step only to keep point counts in the 250–400 range): 24h → step 300s; 7d → step 1800s; 30d → step 7200s.
- **Polling (D-07):** all four panels re-fetch every **60s** while the Telemetry tab is mounted AND `document.visibilityState === "visible"` (pause on `visibilitychange`, resume with an immediate fetch). Range or scope change triggers an immediate re-fetch and resets the interval. No SSE.
- Scope drives the queries: selected-project mode adds `{project="<selected>"}` to every panel query; all-projects mode aggregates `by (project)` (one series per project). Exact PromQL stays planner discretion within the D-06 shapes documented in 16-RESEARCH.md.

### C4. Chart panels — recharts time-series (D-05, D-06)

Replace the text-only `Sparkline` with recharts. The existing `ChartPanel` chrome (panel `div`, `data-testid="panel-<id>"`, uppercase mono title, `PanelState` machine, degradation rendering) is **preserved** — only the `state.kind === "data"` branch changes.

| Panel | Chart | Series (selected-project mode) | Series (all-projects mode) | Y axis |
|-------|-------|-------------------------------|---------------------------|--------|
| Cost Over Time | `AreaChart` | 1 (cost) — series-1 | 1 per project, palette cycle | dollars — format cents via the existing `formatCents` (ticks may abbreviate, e.g. `$12.50`) |
| Dispatch Counts | `AreaChart` | 2: `waves dispatched` (series-1), `tasks completed` (series-2) | per-project totals, palette cycle (planner picks the D-06-conformant aggregate) | count, integer ticks |
| Failure Rate | `AreaChart` | 1 (ratio) — failure red | 1 per project, ALL in failure red is forbidden — use palette cycle, red reserved for the single-series mode | percent, domain `[0, 1]`, ticks formatted `0%`–`100%` |
| Token Breakdown | `AreaChart`, **stacked** (`stackId`) | 4: `input`, `output`, `cache read`, `cache creation` — series-1..4 | same 4 token-type series aggregated cluster-wide (token types stay the stack dimension; do NOT stack by project) | tokens, abbreviated ticks (e.g. `1.2M`) |

Construction rules (all panels):

- `<ResponsiveContainer width="100%" height={180}>` — fixed pixel height (Pitfall 5).
- `<CartesianGrid strokeDasharray="3 3" stroke="var(--color-border-subtle)" />`.
- `<XAxis dataKey="time" type="number" scale="time" domain={["dataMin", "dataMax"]}>` with tick formatter: 24h range → `HH:MM`; 7d/30d → `MMM D`. Ticks 12px mono muted; axis lines `--color-border-subtle`.
- `<Tooltip>` styled: background `var(--color-surface-overlay)`, border `1px solid var(--color-border-subtle)`, border-radius 4px, 12px mono; values formatted with the same formatter as the Y axis.
- `<Legend>` below the chart, 12px mono muted — rendered only when a panel has >1 series (Token Breakdown and Dispatch Counts always; others in all-projects mode only).
- recharts v3 ships `accessibilityLayer` enabled by default — do not disable it. Additionally each chart's wrapping element carries `aria-label="<panel title> chart"`.
- Prometheus matrix → points transform: merge series into `[{ time, <seriesKey>: number }]` keyed on the step timestamps (16-RESEARCH Pattern 7). Series key = token type label (Token Breakdown), fixed label (Dispatch Counts), or `metric.project` (all-projects mode).
- Empty result (`series.length === 0` or all series empty): render the `No data in range` copy (12px mono muted) centered in the 180px chart area — NOT the unavailable notice (Prometheus answered; there is simply no data).
- Degradation: `unavailable`/`unreachable` PanelStates keep rendering `<TelemetryUnavailableNotice>` exactly as today — the three-shape contract and Vitest assertions hang off it (TELEM-02).

### C5. Budget surface — single card + all-projects grid (D-03)

- **Selected-project mode:** one `BudgetCard` (existing component, reworked for typography compliance): title `BUDGET` (existing panel-title treatment), spend figure 20px/600 mono (was 24px/700 — fix), `of $X cap` 13px mono muted, status line `Within budget` (`--color-status-success`) / `Over budget` (`--color-destructive`) 12px/600 mono. `data-testid="budget-card"` preserved. No Prometheus dependency — renders even when every panel is degraded.
- **All-projects mode:** a grid of compact per-project cards, `data-testid="budget-card-grid"`, inline style `display: grid; gridTemplateColumns: "repeat(auto-fill, minmax(240px, 1fr))"; gap: 16px`. Each card = the BudgetCard anatomy with the project name added as the first line (13px mono, `--color-text-primary`); `data-testid="budget-card-<project>"`.
  - Project with no budget data (no cap configured / field absent in the API payload): the card renders the project name + `No budget configured` (12px mono muted) in place of the figures — never `NaN`, never `$0.00 of $0.00`.
  - Zero projects in the cluster: the grid area renders `No projects in this cluster` (12px mono muted, centered) — reuses the E1 empty-state heading verbatim; no CTA (the DAGs view owns onboarding).
  - Data source: `Project.Status.Budget` via the projects API (16-RESEARCH Open Question 1: if the list endpoint omits `.status.budget`, the planner adds per-project detail fetches or extends the list payload — backend shape is planner discretion; the card contract above is fixed).

**Out of scope (do NOT build):** any change to the DAGs view, RunningWavesView, TaskDetailDrawer, ProjectPicker internals, or TelemetryUnavailableNotice; persisting view/scope/range to localStorage; per-plan or per-wave drill-down panels; SSE for Prometheus data; in-dashboard budget editing (read-only contract, D-D6); new icons.

---

## Copywriting Contract

Voice inherited: terse, declarative, operator-focused, em dashes, no exclamation points. "Wave" remains the surfaced vocabulary word — `waves dispatched` extends it naturally; no new metaphor coinage.

| Element | Copy |
|---------|------|
| Primary CTA | none — read-only view; segmented controls are not CTAs |
| View switcher tabs | `DAGs` · `Telemetry` |
| Scope toggle segments | `<project name>` (verbatim resource name, mono) · `All projects` |
| Range selector segments | `24h` · `7d` · `30d` |
| Panel titles (existing, keep verbatim) | `Cost Over Time` · `Dispatch Counts` · `Failure Rate` · `Token Breakdown` |
| Dispatch Counts legend | `waves dispatched` · `tasks completed` |
| Token Breakdown legend | `input` · `output` · `cache read` · `cache creation` |
| Chart empty state | `No data in range` |
| Panel loading state (existing) | `Loading…` |
| Degradation — not configured (existing, verbatim) | `Telemetry unavailable — Prometheus not configured` |
| Degradation — unreachable (existing wording shape, verbatim) | `Prometheus endpoint is unreachable (HTTP <status>)<: detail>` / `Prometheus endpoint is unreachable — network error` |
| Budget card title (existing) | `Budget` (rendered uppercase by CSS) |
| Budget cap line (existing) | `of <$cap> cap` |
| Budget status (existing) | `Within budget` / `Over budget` |
| Budget card — no data | `No budget configured` |
| Budget grid — empty cluster | `No projects in this cluster` |
| Error state | none new — every Prometheus failure path resolves to the degradation notice (no ErrorState takeover, per the MILESTONE.md EC-6 contract preserved in TelemetryView's header comment) |
| Destructive confirmation | none — no destructive actions in this phase |

---

## Accessibility

Inherited rules (focus rings never removed, WCAG AA contrast, `prefers-reduced-motion`) apply globally. Phase-specific:

- View switcher: `role="tablist"` / `role="tab"` / `aria-selected`, ArrowLeft/ArrowRight roving focus (C2).
- Scope toggle + range selector: buttons with `aria-pressed` (they are not tabs — they don't switch the rendered surface class, they parameterize it).
- Charts: recharts `accessibilityLayer` left enabled; wrapper `aria-label="<panel title> chart"`. Series are never color-only-distinguished in legends — the legend text labels carry meaning; tooltip exposes exact values.
- Polling never steals focus or scrolls — state updates only.
- All muted-on-raised text combinations used here are already AA-verified in 04-UI-SPEC.

---

## Validation Contract

| Surface | Test | Framework |
|---------|------|-----------|
| View switcher | renders both tabs; default DAGs; clicking Telemetry mounts `telemetry-view`; DAGs view body unchanged when active (Phase 15 right-pane default intact) | Vitest (App-level, extend existing app/dag-views specs) |
| TelemetryView degradation | 200 `{"status":"unavailable"}` → `telemetry-unavailable-notice` in every panel; 502 → notice with `unreachable` wording (BOTH shapes — TELEM-02 locked requirement) | Vitest (`vi.stubGlobal("fetch", …)` pattern; `vi.useFakeTimers()` for the polling interval per 16-RESEARCH Pitfall 6) |
| Range selector | switching 24h→7d issues new fetches with the 7d window/step | Vitest |
| Scope toggle | selected project → query contains `project="<name>"`; All projects → `by (project)`; no project selected → all-projects mode (D-04) | Vitest |
| Budget grid | all-projects mode renders one card per project; missing budget → `No budget configured`; zero projects → empty copy | Vitest |
| Chart render | data PanelState renders an SVG (recharts) — not the Sparkline list; empty series → `No data in range` | Vitest (jsdom; ResizeObserver polyfill already in setup.ts) |

Go-side validation (proxy, metrics, helm gates) is owned by 16-RESEARCH.md's Validation Architecture, not this contract.

---

## Registry Safety

| Registry | Blocks Used | Safety Gate |
|----------|-------------|-------------|
| shadcn official | none — not initialized | not applicable |
| Third-party registries | none | not applicable |

No registry vetting gate applies — no shadcn, no registry blocks. The single new dependency is the npm package `recharts@3.8.1`: pinned, SVG-only (honors the no-canvas constraint), slopcheck `[OK]`, legitimacy-audited in 16-RESEARCH.md (2026-06-12). No new icons; no other packages.

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

*UI Design Contract for: Phase 16 — Telemetry Completion (TELEM-02/04 dashboard surfaces)*
*Authored: 2026-06-12 via gsd-ui-researcher (AUTO mode — zero user questions; all decisions derived from 16-CONTEXT.md D-01..D-07, 16-RESEARCH.md, 04-UI-SPEC.md tokens, 14-UI-SPEC.md patterns, and codebase inspection)*
*Inherits: 04-UI-SPEC.md tokens, typography, spacing, voice, accessibility (approved 2026-05-17)*
