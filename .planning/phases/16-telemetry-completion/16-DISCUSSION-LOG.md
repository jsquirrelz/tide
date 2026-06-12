# Phase 16: Telemetry Completion - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-12
**Phase:** 16-telemetry-completion
**Areas discussed:** Telemetry tab navigation, Panel scope + broken queries, Metrics emission semantics, Makefile gate wiring

---

## Telemetry tab navigation

| Option | Description | Selected |
|--------|-------------|----------|
| Header view switcher | Tab/segmented control in header: 'DAGs' (existing two-pane body) vs 'Telemetry' (full-width); Phase 15 right-pane logic untouched | ✓ |
| Right-pane tab | Telemetry as a third right-pane state; half-width charts, entangles Phase 15 pane logic | |
| Client-side routes | React Router /dags + /telemetry; deep-linkable but new dependency | |

**User's choice:** Header view switcher (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Selected project | Queries filter on selected project; consistent with the rest of the dashboard | |
| Cluster-wide, grouped by project | Keep current by(project) aggregates; operator overview | |
| Selected project + all-projects toggle | Default to selected project with a toggle to see everything | ✓ |

**User's choice:** Selected project + all-projects toggle

| Option | Description | Selected |
|--------|-------------|----------|
| One card per project | Compact budget card row/grid in all-projects mode | ✓ |
| Hide it | Budget card only in selected-project mode | |
| You decide | Planner/UI-spec picks | |

**User's choice:** One card per project (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Follow the picker | Project selected → selected-project mode; none → all-projects; toggle transient | ✓ |
| Always all-projects | Tab always opens cluster-wide | |
| You decide | Planner picks defaults including localStorage persistence | |

**User's choice:** Follow the picker (recommended)

---

## Panel scope + broken queries

| Option | Description | Selected |
|--------|-------------|----------|
| Real charts via recharts | Add recharts per MILESTONE.md, proper time-series charts + range selector | |
| Keep text sparklines | Mount + fix names + Vitest only | |
| Hand-rolled SVG sparklines | Real visuals without a new dependency | |

**User's choice:** Other — "Research the best charting dependency during research, but we want to render proper time-series charts." (Library choice delegated to the research phase; recharts is the MILESTONE.md candidate; DOM/SVG-over-canvas constraint applies.)

| Option | Description | Selected |
|--------|-------------|----------|
| MILESTONE.md shapes | Dispatch → waves+tasks counters; Failure → failed/(completed+failed); Tokens → 4 locked counters stacked by type | ✓ |
| Tasks-only dispatch panel | Drop waves from dispatch counts | |
| You decide | Planner picks exact PromQL | |

**User's choice:** MILESTONE.md shapes (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Poll + 24h/7d/30d | MILESTONE.md range selector + ~30–60s polling while visible | ✓ |
| Static per tab-open | Fetch once per open/range change | |
| You decide | Planner picks | |

**User's choice:** Poll + 24h/7d/30d (recommended)

---

## Metrics emission semantics

| Option | Description | Selected |
|--------|-------------|----------|
| All terminal branches | Emit wherever usage rolls up — success AND failure; Prometheus matches Budget accounting | ✓ |
| Success only, failure later | Literal table reading; Prometheus undercounts vs Budget on failures | |
| All branches + status label | Would change the locked label set | |

**User's choice:** All terminal branches (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Wave CRD name per table | OwnerRef walk to owning Wave's name, per the locked table | ✓ |
| wave-index label value | Integer already on the Task; cheaper but deviates from table | |
| You decide | Researcher picks | |

**User's choice:** Wave CRD name per table (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Minutes-scale exponential | Hand-picked ~30s–2h boundaries | ✓ |
| Native histogram | No bucket choice but needs Prometheus ≥2.40 + feature flag | |
| You decide | Planner picks from run-1 observed durations | |

**User's choice:** Minutes-scale exponential (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| {project,phase,plan,wave} | Add plan alongside locked three; same series count; restores Plan roll-up; matches canonical set | ✓ |
| {project,phase,wave} exactly | Honor table literally; plan roll-up impossible | |
| You decide | Researcher checks panel PromQL needs | |

**User's choice:** {project,phase,plan,wave} (recommended) — surfaced from the scout finding that the existing 7 registry metrics all carry `{project, phase, plan}` and the cardinality budget approves Plan roll-up.

| Option | Description | Selected |
|--------|-------------|----------|
| Piggyback usage-rollup guard | Emit at the budget RollUpUsage commit point; Prometheus and Budget never diverge | ✓ |
| Separate emitted-flag | Dedicated per-task marker; second source of truth | |
| You decide | Planner picks the guard | |

**User's choice:** Piggyback usage-rollup guard (recommended)

---

## Makefile gate wiring

| Option | Description | Selected |
|--------|-------------|----------|
| Umbrella helm-assert | New helm-telemetry-assert + aggregate helm-assert (incl. helm-rbac-assert); docstrings corrected | ✓ |
| Extend helm-rbac-assert | Fold telemetry scripts into the existing target | |
| One target per script | Three sibling targets | |

**User's choice:** Umbrella helm-assert (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Per-push helm-lint job | make helm-assert step in ci.yaml's existing helm-lint job | ✓ |
| Nightly only | nightly-integration.yml | |
| No CI hook | Manual documented targets only | |

**User's choice:** Per-push helm-lint job (recommended)

---

## Claude's Discretion

- Charting library final choice (research validates; recharts default candidate)
- Exact PromQL within the decided shapes; polling interval within 30–60s; precise bucket list within ~30s–2h
- View-switcher visual treatment (ui-phase territory — ROADMAP `UI hint: yes`)
- TELEM-06 proxy hardening specifics (timeout value, context propagation, base-path join)
- Vitest structure beyond the locked degradation-shape coverage

## Deferred Ideas

- Prometheus native histograms (needs ≥2.40 + feature flag — too risky for arbitrary clusters)
- `outcome` label on cost metrics for per-outcome spend breakdown (changes locked label set)
- SSE-driven live Prometheus panels (polling sufficient for now)
