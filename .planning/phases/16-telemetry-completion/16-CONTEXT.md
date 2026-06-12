# Phase 16: Telemetry Completion - Context

**Gathered:** 2026-06-12
**Status:** Ready for planning

<domain>
## Phase Boundary

The salvaged telemetry foundation (merge `49e93cb`, dogfood run-1 analytics milestone) becomes functional end-to-end: `PROM_ENDPOINT` actually drives the PromQL proxy (TELEM-01), TelemetryView mounts in the dashboard with Vitest degradation coverage (TELEM-02), the six locked metrics emit from the TaskReconciler terminal branches (TELEM-03), all four PromQL panels query real metric names (TELEM-04), the `hack/helm` telemetry gate scripts wire into the Makefile and CI (TELEM-05), and the proxy client is hardened with timeout + context propagation + base-path preservation (TELEM-06).

Scout confirmed all six gaps are real: `cmd/dashboard/main.go` never reads `PROM_ENDPOINT` (dead config); nothing mounts `TelemetryView.tsx`; `internal/metrics/registry.go` lacks all six locked metrics and the TaskReconciler emits nothing; two of four panels query nonexistent names; `assert-prometheus-env.py` and `assert-telemetry-render.sh` are unwired (only `helm-rbac-assert` exists, Makefile:546, and even it doesn't run in CI); the proxy uses `http.DefaultClient.Get` with no timeout and `upstream.Path = path` clobbers base paths.

</domain>

<decisions>
## Implementation Decisions

### Telemetry tab navigation (TELEM-02)
- **D-01: Header view switcher.** AppShell has no tab system today (header + two-pane body). Add a small tab/segmented control in the header: "DAGs" (the existing PlanningDAGView + ExecutionDAGView/RunningWavesView two-pane body, unchanged) and "Telemetry" (full-width TelemetryView). Phase 15's right-pane selection logic (D-13 RunningWavesView default) is untouched.
- **D-02: Selected-project scope with all-projects toggle.** TelemetryView defaults to the project selected in ProjectPicker (queries filter `{project="<selected>"}`); a toggle switches to a cluster-wide all-projects view (`by (project)` aggregates).
- **D-03: Per-project budget card grid in all-projects mode.** The live budget card (from `Project.Status.Budget`, no Prometheus dependency) renders as one compact card per project in all-projects mode — its always-available value is preserved in both modes.
- **D-04: Tab follows the picker.** Opening Telemetry with a project selected → selected-project mode; no project selected → all-projects mode. The toggle is transient UI state, not persisted.

### Panel scope + queries (TELEM-04)
- **D-05: Proper time-series charts.** The current text-only "Sparkline" (last-value lists) is replaced with real charts. The charting dependency is chosen during RESEARCH — recharts is MILESTONE.md's sanctioned candidate, but the researcher validates the best current choice. Constraint: DOM/SVG-based, consistent with the React-Flow-DOM-nodes philosophy (no canvas libraries).
- **D-06: MILESTONE.md query shapes for the dead panels.** Dispatch Counts → `tide_waves_dispatched_total` + `tide_tasks_completed_total`; Failure Rate → `failed / (completed + failed)`; Token Breakdown → the four locked `tide_tokens_*_total` counters stacked by token type (input/output/cache-read/cache-creation). The `{model}` dimension is illegal under the locked label set and is gone.
- **D-07: 24h/7d/30d range selector + polling.** Panels offer the MILESTONE.md time ranges and re-fetch on a modest interval (~30–60s) while the tab is visible. No SSE plumbing for Prometheus data.

### Metrics emission (TELEM-03)
- **D-08: Emit on ALL terminal branches.** Tokens, cost, and duration emit wherever usage rolls up — success AND failure (failed tasks burn real tokens; `budget.RollUpUsage` is already called on failure paths at task_controller.go:857/:883/:932). Prometheus cost totals must match Budget accounting. The locked table's "terminal-success" wording is read as where-it-was-sketched, not an exclusion.
- **D-09: `wave` label = owning Wave CRD name**, resolved by walking the Task owner-reference chain — per the locked table, same pattern as `resolveProject`. (The cheaper `tideproject.k8s/wave-index` label value was considered and rejected to honor the table.)
- **D-10: Label set is `{project, phase, plan, wave}`.** The locked three labels are all present; `plan` is ADDED (not a re-derivation) because the existing 7 registry metrics all carry the canonical `{project, phase, plan}` set, the cardinality budget table explicitly approves Plan roll-up, and series count is identical (wave ⊂ plan). Per-task labels remain forbidden (the `metriccardinality` analyzer enforces this).
- **D-11: Minutes-scale histogram buckets** for `tide_task_duration_seconds` — hand-picked boundaries spanning ~30s to ~2h (e.g. 30, 60, 120, 300, 600, 1200, 1800, 3600, 7200). Prometheus default buckets top out at 10s and would be useless for agentic tasks.
- **D-12: Exactly-once via the usage-rollup guard.** Metrics emit at exactly the point the `budget.RollUpUsage` commit succeeds for that task — same transition, same once-only guard, so Prometheus and Budget status can never diverge. Manager-restart counter resets are acceptable (`rate()`/`increase()` handle them).

### Makefile gate wiring (TELEM-05)
- **D-13: Umbrella targets.** New `helm-telemetry-assert` target runs `assert-telemetry-render.sh` plus both `assert-prometheus-env.py` invocations (`--expect-endpoint` and `--expect-absent`); a new aggregate `helm-assert` runs it together with the existing `helm-rbac-assert`. Docstrings that falsely claim `helm-rbac-assert` drives the telemetry scripts are corrected.
- **D-14: Per-push CI.** `make helm-assert` is added as a step in ci.yaml's existing `helm-lint` job (helm + python3 already installed there; the gates are pure render gates that run in seconds). Note: the ROADMAP criterion's "pass on a running cluster" wording is satisfied without a cluster — both scripts are `helm template`-based render gates and `assert-telemetry-render.sh` states "No cluster connection needed".

### Config wiring (TELEM-01 — locked by MILESTONE.md, not re-discussed)
- `internal/config/config.go` gains `PrometheusEndpoint string` (YAML key `prometheusEndpoint`), overridable via `PROM_ENDPOINT` env — same pattern as existing config fields; `cmd/dashboard/main.go` passes it into `Dependencies.PrometheusEndpoint`. Chart already injects the env (fixed contract — no chart changes).

### Claude's Discretion
- Charting library final choice (researcher validates; recharts is the default candidate).
- Exact PromQL expressions within D-06's shapes; polling interval within 30–60s; precise bucket boundary list within D-11's range.
- View-switcher visual treatment (UI-SPEC/ui-phase territory — ROADMAP flags `UI hint: yes`).
- TELEM-06 proxy hardening specifics (client timeout value, `r.Context()` propagation, `url.JoinPath`-style base-path preservation) — mechanical, follow standard Go practice.
- Vitest test structure beyond the locked requirement (both degradation shapes: 200 `unavailable` sentinel and 502 error).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Locked telemetry design (the contract this phase completes)
- `MILESTONE.md` (repo root, from merge `49e93cb`) — the salvaged analytics-milestone design: locked metrics table, panel inventory, config-wiring shape, graceful-degradation contract. STATE.md rule: do NOT re-derive the metric names.
- `docs/observability.md` §"Token, cost, and duration metrics" — locked metrics table + cardinality budget table (Plan roll-up approved; per-task labels forbidden) + the `metriccardinality` analyzer mandate.

### Metrics emission (TELEM-03)
- `internal/metrics/registry.go` — the existing 7 metrics with canonical `{project, phase, plan}` labels; the six new metrics register here.
- `internal/controller/task_controller.go` (:857, :883, :932) — the three `budget.RollUpUsage` terminal call sites; D-12 piggybacks their once-only commit point.
- `tools/analyzers/metriccardinality/` — the analyzer forbidding `task` metric labels; new metrics must pass it.

### Dashboard (TELEM-01, TELEM-02, TELEM-04, TELEM-06)
- `cmd/dashboard/main.go` + `cmd/dashboard/router.go` (:73-:80, :163) — `Dependencies.PrometheusEndpoint` exists and is wired to the handler; main.go must read `PROM_ENDPOINT`.
- `cmd/dashboard/api/prometheus.go` — the PromQL proxy: `http.DefaultClient.Get` (no timeout, `//nolint:noctx`) and `upstream.Path = path` base-path clobber are the TELEM-06 targets; the three-shape degradation contract must be preserved.
- `cmd/dashboard/api/telemetry_proxy_integration_test.go` — existing proxy integration tests against the production handler.
- `dashboard/web/src/components/TelemetryView.tsx` — the 4 panel defs (two dead query names), `PanelState` degradation handling, text-only Sparkline to replace.
- `dashboard/web/src/components/TelemetryUnavailableNotice.tsx` — the degradation notice both Vitest shapes assert on.
- `dashboard/web/src/components/AppShell.tsx` + `dashboard/web/src/App.tsx` (~:196-:313) — current header/two-pane structure the D-01 view switcher extends; Phase 15's RunningWavesView right-pane default must survive.

### Helm gates (TELEM-05)
- `hack/helm/assert-prometheus-env.py` — parameterized render gate (`--expect-endpoint` / `--expect-absent`); docstring claims `make helm-rbac-assert` drives it (false today — fix the docstring).
- `hack/helm/assert-telemetry-render.sh` — EC-7 four-permutation render gate; self-contained, no cluster needed.
- `Makefile` (:546 `helm-rbac-assert`, :540 `helm-lint-validate`) — existing gate target style to match.
- `.github/workflows/ci.yaml` (`helm-lint` job, ~:141-:185) — where D-14's `make helm-assert` step lands.

### Chart (fixed contract — read, don't change)
- `charts/tide/templates/dashboard-deployment.yaml` (:58-:65) — the guarded `PROM_ENDPOINT` env injection.
- `charts/tide/values.yaml` (:318) / `hack/helm/tide-values.yaml` (:297) — `prometheus.endpoint` value documentation.

### Background
- `.planning/REQUIREMENTS.md` — TELEM-01..06.
- `.planning/phases/15-paper-cuts/15-CONTEXT.md` — D-13 right-pane default and the "clean seams for the Telemetry tab" integration note.
- `.planning/phases/14-budget-enforcement-pricing/14-UI-SPEC.md` — Phase 14's approved UI contract (ConditionBadge/TideNodeShell patterns; the budget card grid should stay consistent).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `PrometheusHandler` proxy + three-shape degradation contract — already implemented and integration-tested; TELEM-06 hardens, doesn't rewrite.
- `TelemetryView` panel/state machinery (`PanelState`, `fetchQueryRange`, `ChartPanel`) — the fetch/degradation skeleton survives; the Sparkline renderer and dead queries are replaced.
- `BudgetSummary` type + `Project.Status.Budget` plumbing (Phase 14) — feeds the budget card(s) with zero Prometheus dependency.
- `internal/metrics` registry pattern (promauto-style vars + init registration) — the six new metrics follow the existing 7.
- `budget.RollUpUsage` call sites — the once-only terminal commit points D-12 piggybacks.
- `resolveProject` OwnerRef-walk pattern — template for D-09's wave-name resolution.
- `assert-dashboard-rbac.py` invocation style in `helm-rbac-assert` — the Makefile pattern D-13's targets copy.
- ProjectPicker + header (Phase 4/15) — the D-01 view switcher and D-02 scoping hang off existing header state.

### Established Patterns
- Canonical label set `{project, phase, plan}` across logs/metrics/traces (docs/observability.md D-X1) — D-10 keeps the new metrics consistent with it.
- Per-task metric labels forbidden (cardinality Pitfall 17) — enforced by `tools/analyzers/metriccardinality`.
- Chart is a FIXED contract — the binary catches up to the chart's `PROM_ENDPOINT` injection; no chart edits.
- Regression tests reproduce the symptom (milestone-wide rule) — each TELEM fix carries a test (Vitest for dashboard, Go for proxy/metrics, render-gate for chart).
- Vitest + Testing Library for dashboard components; envtest/Ginkgo for controller behavior — follow the established split.

### Integration Points
- `cmd/dashboard/main.go` env→`Dependencies` wiring (TELEM-01) — one seam, matches MILESTONE.md's stated config shape.
- TaskReconciler terminal branches — metrics emission joins the existing budget-rollup transition (no new reconcile paths).
- ci.yaml `helm-lint` job — the render-gate step lands beside the existing lint/template/reproducibility steps.
- AppShell header — the only place the view switcher touches; the two-pane body and Phase 15 pane logic are passed through unchanged in the DAGs view.

</code_context>

<specifics>
## Specific Ideas

- Prometheus cost totals and `Project.Status.Budget` accounting must never diverge — same commit point, same branches (the user chose emission semantics specifically to guarantee this).
- The phase is named "completion": the Telemetry tab should ship looking finished (real charts), not as a debug page of text values.
- TELEM-04 acceptance shape: all four panels query only names that exist in `internal/metrics/registry.go` after TELEM-03 lands.
- TELEM-05 acceptance shape: `make helm-assert` passes locally and the same target runs green in ci.yaml's helm-lint job.

</specifics>

<deferred>
## Deferred Ideas

- Prometheus native histograms for task duration — rejected for now (requires Prometheus ≥2.40 + feature flag; too risky for arbitrary operator clusters).
- An `outcome`/status label on the cost metrics (distinguishing success vs failure spend) — rejected for v1.0.1; would change the locked label set. Revisit if per-outcome cost breakdown is wanted later.
- SSE-driven live Prometheus panels — polling is sufficient; revisit if staleness bites.

</deferred>

---

*Phase: 16-Telemetry Completion*
*Context gathered: 2026-06-12*
