---
phase: 16-telemetry-completion
reviewed: 2026-06-12T21:32:58Z
depth: standard
files_reviewed: 19
files_reviewed_list:
  - .github/workflows/ci.yaml
  - Makefile
  - cmd/dashboard/api/prometheus.go
  - cmd/dashboard/api/telemetry_proxy_integration_test.go
  - cmd/dashboard/main.go
  - cmd/dashboard/router_test.go
  - dashboard/web/package.json
  - dashboard/web/src/App.tsx
  - dashboard/web/src/components/__tests__/TelemetryView.test.tsx
  - dashboard/web/src/components/__tests__/view-switcher.test.tsx
  - dashboard/web/src/components/Header.tsx
  - dashboard/web/src/components/TelemetryView.tsx
  - hack/helm/assert-prometheus-env.py
  - internal/config/config.go
  - internal/config/config_test.go
  - internal/controller/task_controller.go
  - internal/controller/task_controller_metrics_test.go
  - internal/metrics/registry.go
  - internal/metrics/registry_test.go
findings:
  critical: 2
  warning: 4
  info: 7
  total: 13
status: issues_found
---

# Phase 16: Code Review Report

**Reviewed:** 2026-06-12T21:32:58Z
**Depth:** standard
**Files Reviewed:** 19
**Status:** issues_found

## Summary

Phase 16 telemetry work is structurally sound in the places the key invariants name: metric label arity is consistent ({project, phase, plan, wave} = 4 across all six new metrics; registry, emit site, and tests agree), the emit happens exactly once per terminal commit point (all three `emitTaskMetrics` call sites in `handleJobCompletion` are mutually exclusive, ordered after the terminal status patch, and shielded from replay by the `Succeeded`/`Failed` short-circuit in `gateChecks`), the proxy's three-shape degradation contract is implemented and integration-tested end-to-end (200 unavailable / 502 error / pass-through, plus base-path preservation, bounded client, context propagation), no `task` or `model` labels appear, no chart files were edited (render gates added instead), polling cleanup on unmount clears the interval and the visibilitychange listener, and DASH-05 stays intact (proxy routes are GET-only and `TestZeroMutationRoutes` walks them).

However, two Critical defects undercut the phase's headline deliverable: in all-projects scope every chart panel collapses per-project Prometheus series onto a single fixed legend key (later projects silently overwrite earlier ones — wrong numbers rendered), and two of the four panels (Dispatch Counts, Failure Rate) query metric families that are registered but never incremented anywhere in production code, so they will render "No data in range" on every cluster, forever, regardless of Prometheus configuration. Four Warnings and seven Info items follow.

## Narrative Findings (AI reviewer)

### Critical Issues

#### CR-01: All-projects scope collapses per-project series onto one key — chart renders wrong data

**File:** `dashboard/web/src/components/TelemetryView.tsx:329` (with `dashboard/web/src/components/TelemetryView.tsx:127-220`, `341-352`)
**Issue:** In all-projects scope, panel queries aggregate `by (project)` (e.g. `sum(increase(tide_cost_cents_total[5m])) by (project)`), so Prometheus returns one matrix per project. `fetchPanel` keys each matrix with:

```ts
const key = sd.key ?? matrix.metric["project"] ?? Object.values(matrix.metric).join(",") ?? "value";
```

But **every** `SeriesDef` in `PANELS` sets `key` (e.g. `"cost"`, `"waves dispatched"`, `"failure rate"`), so the `matrix.metric["project"]` fallback is dead code. All projects' matrices get the same key, and `matrixToPoints` then does `existing[key] = parseFloat(v)` per timestamp — last project in the result array silently overwrites all others. With N projects, the Cost Over Time / Dispatch Counts / Failure Rate panels display one arbitrary project's values labeled as the cluster aggregate. The multi-series legend machinery (`hasMultipleSeries`, palette cycling) was clearly built for per-project series and can never activate. No test covers all-projects mode with a multi-result success payload (the scope tests stub `{status:"unavailable"}`), which is why this slipped through.
**Fix:** Derive the key from the project label when the query is project-grouped, e.g.:

```ts
const projectLabel = matrix.metric["project"];
const key =
  scope === "all" && projectLabel
    ? (panelDef.series.length > 1 ? `${sd.key} (${projectLabel})` : projectLabel)
    : sd.key ?? projectLabel ?? "value";
```

(Pass `scope` into `fetchPanel`'s merge step — it already receives it.) Add a test: all-projects scope + success payload with `result: [{metric:{project:"p1"},...},{metric:{project:"p2"},...}]` asserting two distinct series keys render.

#### CR-02: Dispatch Counts and Failure Rate panels query metrics no production code ever emits

**File:** `dashboard/web/src/components/TelemetryView.tsx:151,158,174-175` (with `internal/metrics/registry.go:46-64`, `internal/controller/task_controller.go:804-965`)
**Issue:** The Dispatch Counts panel queries `tide_waves_dispatched_total` and `tide_tasks_completed_total`; the Failure Rate panel queries `tide_tasks_failed_total` and `tide_tasks_completed_total`. Repo-wide grep confirms `WavesDispatchedTotal`, `TasksCompletedTotal`, and `TasksFailedTotal` are referenced **only** in `internal/metrics/registry.go` (registration) and test files — there is no `.Inc()`/`.Add()` call site in any controller. Registration creates the descriptor but emits no samples until a child is observed, so Prometheus will never have these series and both panels will permanently render "No data in range" on every cluster, even with Prometheus correctly configured. The TelemetryView header comment ("All panel queries use only metric names registered in internal/metrics/registry.go" — TELEM-04) conflates *registered* with *emitted*. Phase 16 wired the six new token/cost/duration metrics into `handleJobCompletion` but built half the panel surface on Phase 4 counters that were never wired up — `handleJobCompletion`'s terminal branches are exactly where `TasksCompletedTotal`/`TasksFailedTotal` should increment, alongside the `emitTaskMetrics` call that already resolves the {project, phase, plan} labels.
**Fix:** In `handleJobCompletion` (and the two early-return Failed branches), increment `TasksFailedTotal.WithLabelValues(project.Name, phase, plan, reason).Inc()` on the Failed paths and `TasksCompletedTotal.WithLabelValues(project.Name, phase, plan).Inc()` on the Succeeded path — the same once-only commit point as `emitTaskMetrics` (label resolution can be shared with it). Emit `WavesDispatchedTotal` from the wave controller's dispatch commit point. If emission is intentionally deferred to a later plan, the two panels must not ship querying dead series — either gate them or replace the queries with emitted metrics.

### Warnings

#### WR-01: Switching projects while in project scope does not trigger a re-fetch — up to 60s of another project's data shown under the new project's label

**File:** `dashboard/web/src/components/TelemetryView.tsx:744-746,794-822`
**Issue:** The fetch effect re-runs on `[scope, range]` only. When the operator changes the header ProjectPicker while the Telemetry tab is open in project scope, `selectedProject` changes, the scope-rederive effect calls `setScope("project")` — a no-op since scope is already `"project"` — so the fetch effect never re-fires. `projectRef` updates, but no immediate fetch happens; the panels keep showing the *previous* project's cost/token data while the scope toggle now labels them with the *new* project's name, until the 60s poll corrects it. This violates the component's own "scope change triggers an immediate re-fetch" contract (the scope's subject changed) and mislabels spend data in a budget UI.
**Fix:** Add `selectedProject` to the effect deps (`}, [scope, range, selectedProject]);`) — the refs already make the callback safe — or fetch inside the `selectedProject` rederive effect when scope is unchanged.

#### WR-02: No stale-response guard on scope/range change — out-of-order responses can render the wrong range's data

**File:** `dashboard/web/src/components/TelemetryView.tsx:766-790`
**Issue:** `fetchAllPanels` fires four unguarded `fetchPanel(...).then(setPanelStates)` chains. On a rapid range toggle (24h → 7d → 24h) or scope flip, an earlier in-flight response can resolve *after* the latest one and overwrite `panelStates[idx]` with data for a stale range/scope — the panel then displays 7d data under a 24h toolbar until the next poll. There is also no AbortController, so responses landing after unmount call `setPanelStates` on an unmounted component (harmless no-op in React 18, but the stale-overwrite is real while mounted).
**Fix:** Tag each fetch generation with a ref counter and drop late results:

```ts
const fetchGen = useRef(0);
const gen = ++fetchGen.current;
...).then((state) => {
  if (gen !== fetchGen.current) return; // stale — a newer fetch superseded this one
  setPanelStates(...);
});
```

#### WR-03: `Config.PrometheusEndpoint` is dead config — the documented `prometheusEndpoint` YAML key has no effect

**File:** `internal/config/config.go:48-51,116-123`
**Issue:** `Load` parses `prometheusEndpoint` from the manager's `/etc/tide/config.yaml` and applies a `PROM_ENDPOINT` env override, with three tests pinning the behavior — but no binary consumes `cfg.PrometheusEndpoint`. The dashboard (the only consumer of the endpoint) reads `os.Getenv("PROM_ENDPOINT")` directly in `cmd/dashboard/main.go:156` and never calls `config.Load`. An operator who sets `prometheusEndpoint` in the manager ConfigMap (as the field's doc comment invites) gets silently nothing; the Helm `prometheus.endpoint` value only works because the chart injects the env var into the dashboard container. This is a misleading operator surface and untested-in-anger plumbing.
**Fix:** Either wire the dashboard to load the config file (preferred if the ConfigMap is mounted there) or delete the field, the override block, and the three `TestConfigLoad_PrometheusEndpoint_*` tests, leaving the env var as the single documented mechanism.

#### WR-04: `TaskDurationSeconds` can observe negative durations — corrupts the histogram sum

**File:** `internal/controller/task_controller.go:1040-1043`
**Issue:** `duration := completedAt.Sub(task.Status.StartedAt.Time)` guards `completedAt.IsZero()` but not `duration < 0`. `StartedAt` is re-stamped on every dispatch attempt (`createDispatchJob` step 10), while `out.CompletedAt` comes from the envelope on the PVC. A stale envelope from a prior attempt (attempt N+1's Job terminal, but `out.json` written by attempt N), or manager↔pod clock skew, yields `completedAt < StartedAt`. `Observe(negative)` lands in no bucket but permanently drags `_sum` negative, corrupting any `rate(tide_task_duration_seconds_sum)`-based average in Grafana.
**Fix:**

```go
if d := completedAt.Sub(task.Status.StartedAt.Time); d >= 0 {
    tidemetrics.TaskDurationSeconds.WithLabelValues(projectName, phase, plan, wave).Observe(d.Seconds())
}
```

(optionally log the negative case at V(1) as a stale-envelope signal).

### Info

#### IN-01: `emitTaskMetrics` always returns nil — the error-handling branches at all three call sites are dead

**File:** `internal/controller/task_controller.go:1011-1045` (call sites: 864, 896, 952)
**Issue:** The function has no failure path (`r.Get` miss falls back to a sentinel), yet returns `error`, and all three call sites carry an `if emitErr != nil { logger.Error(...) }` branch that can never execute. The signature implies failures are surfaced when none can occur.
**Fix:** Drop the `error` return and the dead branches, or keep the signature only if a fallible step is imminent.

#### IN-02: `SegmentedControl` is dead code; the scope and range toolbars duplicate its markup inline

**File:** `dashboard/web/src/components/TelemetryView.tsx:671-724,845-914`
**Issue:** The generic `SegmentedControl` component (with its `role` prop, itself never read) is defined but never rendered; the scope toggle and range selector re-implement identical button-group markup inline, twice. Three copies of the same segmented-control styling now drift independently.
**Fix:** Render `<SegmentedControl options={scopeOptions} ... />` and `<SegmentedControl options={rangeOptions} ... />`, or delete the component.

#### IN-03: `parseFloat("NaN")` from Prometheus propagates NaN into recharts (Failure Rate 0/0 case)

**File:** `dashboard/web/src/components/TelemetryView.tsx:347`
**Issue:** The failure-rate query divides by `failed + completed`; when both rates are 0 Prometheus emits `"NaN"` sample values, which `parseFloat` passes through to recharts as NaN data points (rendered as gaps or axis glitches depending on version).
**Fix:** Skip non-finite values: `const n = parseFloat(v); if (Number.isFinite(n)) existing[key] = n;`

#### IN-04: `helm-rbac-assert` and `helm-telemetry-assert` share `/tmp/tide-helm-render.yaml` — race under `make -j`

**File:** `Makefile:554,559` 
**Issue:** Both prerequisite targets of `helm-assert` write the same temp file; a parallel `make -j helm-assert` interleaves the two `helm template` writes with the python reads. CI invokes make serially today, so this is latent.
**Fix:** Use distinct file names (the second telemetry render already does: `/tmp/tide-helm-render-prom.yaml`).

#### IN-05: `assert-prometheus-env.py` — stale docstring and brittle input handling

**File:** `hack/helm/assert-prometheus-env.py:5,54,80`
**Issue:** (a) Docstring says "Phase 04 helm-telemetry-config render gate" — this is Phase 16 work. (b) A missing/unreadable chart file or a non-dict entry in the env list raises an uncaught traceback (exit 1 via exception, but with noise instead of the script's FAIL convention). (c) Only `value` is checked; a `valueFrom`-sourced PROM_ENDPOINT would report a confusing `got None`.
**Fix:** Correct the docstring; wrap the file open in try/except with a FAIL message; filter `e for e in dashboard_env if isinstance(e, dict)`.

#### IN-06: ViewSwitcher tablist lacks roving tabIndex — both tabs remain in the Tab order

**File:** `dashboard/web/src/App.tsx:133-170`
**Issue:** The WAI-ARIA tabs pattern the comment cites (roving focus) requires `tabIndex={-1}` on the unselected tab so Tab moves past the tablist and arrows move within it. Both buttons are currently tabbable, and `outline: "none"` removes the focus ring with no visible replacement — a keyboard-focus visibility regression.
**Fix:** `tabIndex={activeView === "dags" ? 0 : -1}` (and mirror for telemetry); restore a `:focus-visible` style instead of `outline: "none"`.

#### IN-07: `PushJobsTotal` Help string omits two outcomes its own doc comment and call sites use

**File:** `internal/metrics/registry.go:81-85,170`
**Issue:** The Help text says `outcome ∈ {success, leak, lease, auth, internal}` but the doc comment (line 82) and production call sites (`project_controller.go:612,777`, `boundary_push.go:132`) also emit `dispatched` and `exhausted`. Operators reading `/metrics` help text get a stale enum.
**Fix:** Update the Help string to the seven-value set.

---

_Reviewed: 2026-06-12T21:32:58Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
