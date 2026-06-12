# Phase 16: Telemetry Completion - Research

**Researched:** 2026-06-12
**Domain:** Go metrics emission + React dashboard wiring (Prometheus, recharts, Vitest)
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Telemetry tab navigation (TELEM-02)
- **D-01: Header view switcher.** AppShell has no tab system today (header + two-pane body). Add a small tab/segmented control in the header: "DAGs" (the existing PlanningDAGView + ExecutionDAGView/RunningWavesView two-pane body, unchanged) and "Telemetry" (full-width TelemetryView). Phase 15's right-pane selection logic (D-13 RunningWavesView default) is untouched.
- **D-02: Selected-project scope with all-projects toggle.** TelemetryView defaults to the project selected in ProjectPicker (queries filter `{project="<selected>"}`); a toggle switches to a cluster-wide all-projects view (`by (project)` aggregates).
- **D-03: Per-project budget card grid in all-projects mode.** The live budget card (from `Project.Status.Budget`, no Prometheus dependency) renders as one compact card per project in all-projects mode — its always-available value is preserved in both modes.
- **D-04: Tab follows the picker.** Opening Telemetry with a project selected → selected-project mode; no project selected → all-projects mode. The toggle is transient UI state, not persisted.

#### Panel scope + queries (TELEM-04)
- **D-05: Proper time-series charts.** Charting dependency chosen during RESEARCH.
- **D-06: MILESTONE.md query shapes for the dead panels.** Dispatch Counts → `tide_waves_dispatched_total` + `tide_tasks_completed_total`; Failure Rate → `failed / (completed + failed)`; Token Breakdown → the four locked `tide_tokens_*_total` counters stacked by token type.
- **D-07: 24h/7d/30d range selector + polling.** Panels offer the MILESTONE.md time ranges and re-fetch on a modest interval (~30–60s) while the tab is visible. No SSE plumbing for Prometheus data.

#### Metrics emission (TELEM-03)
- **D-08: Emit on ALL terminal branches.** Tokens, cost, and duration emit wherever usage rolls up — success AND failure.
- **D-09: `wave` label = owning Wave CRD name**, resolved by walking the Task owner-reference chain — same pattern as `resolveProject`.
- **D-10: Label set is `{project, phase, plan, wave}`.** Adds `plan` to the base three because existing 7 registry metrics all carry `{project, phase, plan}`, cardinality budget table approves Plan roll-up, and wave ⊂ plan.
- **D-11: Minutes-scale histogram buckets** for `tide_task_duration_seconds` — ~30s to ~2h, e.g. `{30, 60, 120, 300, 600, 1200, 1800, 3600, 7200}`.
- **D-12: Exactly-once via the usage-rollup guard.** Metrics emit at exactly the point `budget.RollUpUsage` commit succeeds.

#### Makefile gate wiring (TELEM-05)
- **D-13: Umbrella targets.** New `helm-telemetry-assert` runs both `assert-prometheus-env.py` invocations + `assert-telemetry-render.sh`; new `helm-assert` aggregates it with `helm-rbac-assert`. Fix docstring falsely claiming `helm-rbac-assert` drives telemetry scripts.
- **D-14: Per-push CI.** `make helm-assert` added to ci.yaml's `helm-lint` job.

#### Config wiring (TELEM-01)
- `internal/config/config.go` gains `PrometheusEndpoint string` (YAML key `prometheusEndpoint`), overridable via `PROM_ENDPOINT` env. `cmd/dashboard/main.go` passes it into `Dependencies.PrometheusEndpoint`. Chart is a FIXED contract — no chart edits.

### Claude's Discretion
- Charting library final choice (researcher validates; recharts is the default candidate).
- Exact PromQL expressions within D-06's shapes; polling interval within 30–60s; precise bucket boundary list within D-11's range.
- View-switcher visual treatment.
- TELEM-06 proxy hardening specifics (client timeout value, `r.Context()` propagation, `url.JoinPath`-style base-path preservation).
- Vitest test structure beyond the locked requirement (both degradation shapes: 200 `unavailable` sentinel and 502 error).

### Deferred Ideas (OUT OF SCOPE)
- Prometheus native histograms for task duration.
- `outcome`/status label on cost metrics.
- SSE-driven live Prometheus panels.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| TELEM-01 | Dashboard reads `PROM_ENDPOINT` into `Dependencies.PrometheusEndpoint` — helm-injected env drives the PromQL proxy | Config struct needs `PrometheusEndpoint string` field + env read in main.go at startup |
| TELEM-02 | TelemetryView mounted as Telemetry tab in AppShell; Vitest coverage of both degradation shapes | View switcher in App.tsx/Header; `vi.stubGlobal("fetch", ...)` pattern for Vitest fetch mocking |
| TELEM-03 | Six locked metrics emitted from TaskReconciler terminal branches with `{project, phase, plan, wave}` labels | Three `RollUpUsage` sites (:857/:883/:932); `resolveWave` needed (new, modeled on `resolveProject`); Usage struct has all four token fields + EstimatedCostCents |
| TELEM-04 | TelemetryView PromQL queries use locked metric names — fix two dead panel queries | `tide_tasks_dispatched_total` → `tide_waves_dispatched_total`; `tide_tokens_used_total{model}` → four stacked counters |
| TELEM-05 | `hack/helm` telemetry gate scripts wired into Makefile + CI | Pattern from existing `helm-rbac-assert` target at Makefile:546; ci.yaml `helm-lint` job at line 141 |
| TELEM-06 | PrometheusHandler uses bounded HTTP client (timeout + ctx propagation) + preserves base paths | `http.DefaultClient.Get` + `upstream.Path = path` are both identified bugs; standard Go fix shapes documented |
</phase_requirements>

## Summary

Phase 16 completes six isolated but interlocking gaps in the telemetry foundation that was merged but never wired. The research confirms every gap identified by the scout: `cmd/dashboard/main.go` does not read `PROM_ENDPOINT` (the `Dependencies` struct has the `PrometheusEndpoint` field and `router.go` wires it to `PrometheusHandler`, but `main.go` never populates it from the environment); `internal/config/config.go` exists but contains only concurrency config, not a `PrometheusEndpoint` field; the six locked metrics have no `var` declarations in `internal/metrics/registry.go` (only 7 existing metrics); `TelemetryView.tsx` exists with full panel/state machinery but is never imported in `App.tsx` or `AppShell.tsx`; two of four panel queries target nonexistent metric names; `Makefile` has `helm-rbac-assert` but neither `assert-prometheus-env.py` nor `assert-telemetry-render.sh` is wired; `prometheus.go` uses `http.DefaultClient.Get` with `//nolint:noctx` and `upstream.Path = path` overwrites any base path from the configured endpoint.

The charting library decision is confirmed: **recharts v3.8.1** is the right choice. It is DOM/SVG-based (consistent with the React-Flow-DOM-nodes philosophy), React 18 compatible, has 244 Context7 code snippets, TypeScript support, SSR-safe `ResponsiveContainer`, time-scale `XAxis`, and passed slopcheck `[OK]`. No alternative (visx, tremor, nivo) offers a better fit for this stack without adding significant complexity.

The wave label resolution (D-09) requires a new `resolveWave` helper in `task_controller.go` modeled precisely on `resolveProject` — Tasks carry `task.Spec.PlanRef` (the Plan name) but no direct wave reference. Wave CRDs are named `tide-wave-<plan-uid>-<index>` and own Tasks via OwnerRef. The `walkOwnerChainToProject` machinery already handles `Wave` as an intermediate kind; a symmetric `resolveWave` extracts the Wave name on the first OwnerRef hit of Kind `Wave`.

**Primary recommendation:** Implement in dependency order — TELEM-01 config wiring (independent), TELEM-06 proxy hardening (independent), TELEM-03 metrics registration + emission (requires `resolveWave`), TELEM-04 query fixes (depends on TELEM-03 metric names existing), TELEM-02 dashboard integration (depends on recharts install), TELEM-05 Makefile gates (independent). All six can be split across two waves: Wave 1 = backend (01/03/06), Wave 2 = frontend + gates (02/04/05).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| PROM_ENDPOINT env → config | API/Backend (Go binary) | — | main.go reads env at startup; `Dependencies.PrometheusEndpoint` already declared in router.go |
| PromQL proxy HTTP hardening | API/Backend (Go binary) | — | `prometheus.go` is server-side; client lifetime is owned by the handler |
| Six locked metrics registration | API/Backend (Go registry) | — | `internal/metrics/registry.go` init() owns all metric var declarations |
| Six locked metrics emission | API/Backend (TaskReconciler) | — | Terminal branches in `task_controller.go`; rides the existing RollUpUsage seam |
| Wave label resolution | API/Backend (TaskReconciler) | — | New `resolveWave` helper, same package, same pattern as `resolveProject` |
| Telemetry tab view switcher | Browser/Client (React) | — | Transient UI state in App.tsx; no server involvement |
| TelemetryView mounting | Browser/Client (React) | — | Component already exists; needs import + rendering path in App.tsx |
| PromQL panel real charts | Browser/Client (React) | — | Replace Sparkline renderer with recharts `AreaChart`/`BarChart` |
| Degradation shape Vitest coverage | Browser/Client (test) | — | `vi.stubGlobal("fetch", ...)` pattern; no server needed |
| Helm render gate scripts | CDN/Static (chart) | — | Pure `helm template` render gates; no cluster connection needed |
| Makefile + CI wiring | CDN/Static (build) | — | New Makefile targets + ci.yaml step in `helm-lint` job |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| recharts | 3.8.1 | DOM/SVG time-series charts in TelemetryView | MILESTONE.md sanctioned candidate; SVG (no canvas); React 18 peer dep; slopcheck [OK]; 244 Context7 snippets |
| prometheus/client_golang | v1.23 (pinned in CLAUDE.md) | Metrics counters/histograms registration | Already in use; controller-runtime's `metrics.Registry` |
| vitest + @testing-library/react | 1.6.1 / 16.3.2 (already installed) | Vitest degradation tests | Already installed; existing pattern is `vi.stubGlobal("fetch", ...)` |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `net/http` stdlib | Go 1.26 | `http.Client{Timeout}` for proxy hardening | TELEM-06; no new dependency |
| `url.JoinPath` / manual URL joining | stdlib | Preserve base path in proxy | TELEM-06; `url.URL.ResolveReference` or string join |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| recharts | visx | visx is lower-level (primitives only); more code to assemble a time-series chart panel |
| recharts | nivo | nivo uses canvas for some chart types; violates DOM-node philosophy from CLAUDE.md |
| recharts | tremor | tremor is a UI kit layered on recharts; adds weight without benefit for this focused use case |

**Installation (frontend only — recharts is the single new dependency):**
```bash
cd dashboard/web && npm install recharts@3.8.1
```

**Version verification:** `npm view recharts version` → 3.8.1 (verified 2026-06-12).

## Package Legitimacy Audit

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| recharts | npm | ~10 yrs | ~4M/wk | github.com/recharts/recharts | [OK] | Approved |

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

*slopcheck was run with `--ecosystem npm` and returned `[OK]` for recharts. The Go-side changes use only stdlib and existing dependencies — no new packages.*

## Architecture Patterns

### System Architecture Diagram

```
PROM_ENDPOINT env var
        │
        ▼
cmd/dashboard/main.go  ─── os.Getenv("PROM_ENDPOINT") ──► Dependencies.PrometheusEndpoint
        │
        ▼
RegisterRoutes(deps)  ──► PrometheusHandler{Endpoint: deps.PrometheusEndpoint}
        │
   /api/v1/query_range?query=...
        │
        ▼
PrometheusHandler.proxy()
   ├─ Endpoint == "" ──► HTTP 200 {"status":"unavailable"}  (graceful degrade)
   ├─ http.NewRequestWithContext(r.Context(), ...) ──► upstream Prometheus
   │    ├─ success ──► HTTP 200 pass-through
   │    └─ unreachable ──► HTTP 502 {"status":"error","message":"..."}
        │
        ▼
TelemetryView.tsx  (browser)
   ├─ fetchQueryRange() calls /api/v1/query_range
   │    ├─ 200 + status:"success" ──► recharts AreaChart/BarChart
   │    ├─ 200 + status:"unavailable" ──► TelemetryUnavailableNotice
   │    └─ 502 / non-2xx ──► TelemetryUnavailableNotice (unreachable msg)
   └─ BudgetCard ── Project.Status.Budget (SSE, no Prometheus dep)

TaskReconciler terminal branch (Go)
   budget.RollUpUsage succeeds
        │
        ├─ resolveWave(ctx, task) ──► Wave CRD name
        ├─ resolvePhase(ctx, task) ──► [from label or owner chain]
        └─ metrics.TokensInputTotal.WithLabelValues(project, phase, plan, wave).Add(usage.InputTokens)
           metrics.TokensOutputTotal ... .Add(usage.OutputTokens)
           metrics.TokensCacheReadTotal ... .Add(usage.CacheReadTokens)
           metrics.TokensCacheCreationTotal ... .Add(usage.CacheCreationTokens)
           metrics.CostCentsTotal ... .Add(usage.EstimatedCostCents)
           metrics.TaskDurationSeconds ... .Observe(duration.Seconds())
```

### Recommended Project Structure

No structural changes needed. All edits are in-place modifications to existing files:

```
internal/
  config/
    config.go         ← add PrometheusEndpoint field + env read in Load()
  metrics/
    registry.go       ← add 6 new metric vars + MustRegister in init()
    registry_test.go  ← add arity + registration tests for 6 new metrics
  controller/
    task_controller.go ← add resolveWave(); emit 6 metrics at RollUpUsage seams

cmd/dashboard/
  main.go             ← read PROM_ENDPOINT env, populate Dependencies.PrometheusEndpoint
  api/
    prometheus.go     ← TELEM-06 hardening (http.Client{Timeout}, NewRequestWithContext, URL join)

dashboard/web/src/
  App.tsx             ← add activeView state + view switcher; mount TelemetryView
  components/
    TelemetryView.tsx ← fix 2 dead queries; add D-07 range selector + polling; replace Sparkline with recharts
    AppShell.tsx      ← (may not need changes; view switching lives in App.tsx)

Makefile              ← add helm-telemetry-assert + helm-assert targets; fix docstring
.github/workflows/
  ci.yaml             ← add `make helm-assert` step in helm-lint job
```

### Pattern 1: Config Field + Env Override (TELEM-01)

The existing `internal/config/config.go` uses `os.ReadFile` + `yaml.Unmarshal` into a struct. `PrometheusEndpoint` is a string with no validation required (empty is valid; the proxy self-degrades). The pattern used by the YAML key + env override is:

```go
// Source: internal/config/config.go pattern (ASSUMED — new field, no existing env-override in this file)
// The env override must be applied AFTER yaml.Unmarshal:
func Load(path string) (*Config, error) {
    data, err := os.ReadFile(path)
    // ... parse YAML into raw ...
    cfg := &Config{}
    // ... existing fields ...
    // PrometheusEndpoint: YAML key wins unless PROM_ENDPOINT env is set.
    if v := os.Getenv("PROM_ENDPOINT"); v != "" {
        cfg.PrometheusEndpoint = v
    } else {
        cfg.PrometheusEndpoint = raw.PrometheusEndpoint // from YAML, may be ""
    }
    return cfg, nil
}
```

The `cmd/dashboard/main.go` today builds `Dependencies` without `PrometheusEndpoint`:
```go
// Source: cmd/dashboard/main.go:149-156 (VERIFIED by file read)
router := RegisterRoutes(Dependencies{
    Client:    mgr.GetClient(),
    Hub:       pubsubHub,
    Clientset: clientset,
    Log:       setupLog.WithName("router"),
    SPAFS:     spaFS,
    // PrometheusEndpoint: ← MISSING; this is the TELEM-01 gap
})
```

The fix: `main.go` reads `os.Getenv("PROM_ENDPOINT")` directly (no config file involved — dashboard binary does not call `config.Load()`; that is the manager binary's path). The `internal/config` change is for the manager binary; `main.go` reads the env directly:

```go
// Source: ASSUMED — env read pattern, straightforward
PrometheusEndpoint: os.Getenv("PROM_ENDPOINT"),
```

**IMPORTANT CLARIFICATION (verified by reading main.go):** `cmd/dashboard/main.go` does NOT call `config.Load()`. It is a separate binary from `cmd/manager`. The `internal/config/config.go` change (YAML key `prometheusEndpoint`) is for MILESTONE.md completeness and future operator-config use, but `main.go` reads `PROM_ENDPOINT` directly from `os.Getenv`. Both the config.go addition AND the main.go env read are needed (they serve different consumers). [VERIFIED: file read]

### Pattern 2: resolveWave (TELEM-03 / D-09)

The `resolveProject` function uses a label fast-path then a bounded owner-ref chain walk. Tasks do NOT carry a `tideproject.k8s/wave` label today (confirmed: only `tideproject.k8s/project` and `tideproject.k8s/wave-paused` labels exist on Tasks). The Wave CRD is named `tide-wave-<plan-uid>-<index>` and Tasks are owned by Wave CRDs via OwnerRef.

```go
// Source: internal/controller/task_controller.go:979-1002 (VERIFIED by file read)
// resolveWave is modeled on the existing walkOwnerChainToProject:
func (r *TaskReconciler) resolveWave(ctx context.Context, task *tideprojectv1alpha1.Task) (string, error) {
    // Walk OwnerRefs looking for Kind == "Wave"
    for _, ref := range task.GetOwnerReferences() {
        if ref.Kind == "Wave" {
            return ref.Name, nil  // Wave CRD name is the label value
        }
    }
    // Walk up through Plan if Wave not a direct owner
    // (in practice Wave owns Task directly per wave_controller.go)
    return "", fmt.Errorf("wave owner not found for task %s", task.Name)
}
```

Confirmed from `wave_controller.go:151-154`: wave controller lists tasks with `client.InNamespace(wave.Namespace)` — Tasks are in the same namespace as Waves. From `plan_controller.go:1298-1326`: Wave names follow `tide-wave-<plan.UID>-<i>` format. The Task's direct OwnerRef will be the Wave CRD.

The `plan` label value is available from `task.Spec.PlanRef` (confirmed: `task_types.go:70-72` has `PlanRef string`). No chain walk needed for `plan` — it is a direct spec field.

For `phase` and `project`: already resolved by `resolveProject` which returns the full `Project` CRD. The Phase name must be derived from the Project's owner chain OR from a label. Confirmed: PlanReconciler stamps `tideproject.k8s/project` on Tasks. Phase resolution requires the owner chain: Task → Plan (via Spec.PlanRef) → Phase (owner of Plan). The `walkOwnerChainToProject` already traverses this chain — `phase` can be extracted by a similar walk stopping at Kind `Phase`. [VERIFIED: file read + grep]

**Emission point pattern:**
```go
// Source: internal/controller/task_controller.go:931-935 (VERIFIED)
// Three seams for D-12 — emit AFTER the RollUpUsage commit:
if err := budget.RollUpUsage(ctx, r.Client, project, out.Usage); err != nil {
    logger.Error(err, "failed to roll up budget usage", "task", task.Name)
}
// INSERT: metrics emission here, non-fatal (same pattern as setBudgetBlockedIfNeeded)
emitTaskMetrics(ctx, r.Client, task, project, out.Usage, startedAt, completedAt)
```

### Pattern 3: Metrics Registration (TELEM-03)

Six new vars added to `internal/metrics/registry.go` following the exact pattern of existing vars:

```go
// Source: internal/metrics/registry.go (VERIFIED by file read)
// taskDurationBuckets covers the minutes-to-hours range for agentic tasks (D-11).
var taskDurationBuckets = []float64{30, 60, 120, 300, 600, 1200, 1800, 3600, 7200}

var TokensInputTotal *prometheus.CounterVec
var TokensOutputTotal *prometheus.CounterVec
var TokensCacheReadTotal *prometheus.CounterVec
var TokensCacheCreationTotal *prometheus.CounterVec
var CostCentsTotal *prometheus.CounterVec
var TaskDurationSeconds *prometheus.HistogramVec

func init() {
    // ... existing vars ...
    TokensInputTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "tide_tokens_input_total",
            Help: "...",
        },
        []string{"project", "phase", "plan", "wave"},
    )
    // ... four more CounterVecs ...
    TaskDurationSeconds = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name:    "tide_task_duration_seconds",
            Buckets: taskDurationBuckets,
        },
        []string{"project", "phase", "plan", "wave"},
    )
    metrics.Registry.MustRegister(
        // ... existing ...
        TokensInputTotal, TokensOutputTotal, TokensCacheReadTotal,
        TokensCacheCreationTotal, CostCentsTotal, TaskDurationSeconds,
    )
}
```

The `metriccardinality` analyzer only forbids the literal string `"task"` in label slices. The label `"wave"` is permitted. The `"plan"` label is already used by 5 of the 7 existing metrics. [VERIFIED: analyzer.go source read]

### Pattern 4: TELEM-06 Proxy Hardening

The two bugs in `cmd/dashboard/api/prometheus.go`:

1. **No timeout, no context:** `http.DefaultClient.Get(upstream.String())` with `//nolint:noctx`
2. **Base path clobber:** `upstream.Path = path` ignores any base path in the configured endpoint

Fix shape:
```go
// Source: Standard Go practice (ASSUMED — no project-specific pattern)
// Replace the single-call proxy with:
const proxyTimeout = 30 * time.Second

var proxyClient = &http.Client{Timeout: proxyTimeout}

func (h *PrometheusHandler) proxy(w http.ResponseWriter, r *http.Request, path string) {
    // ... graceful degrade check ...
    upstream, err := url.Parse(h.Endpoint)
    // ... error handling ...

    // Preserve base path: join with path.Join, not overwrite
    upstream.Path = strings.TrimRight(upstream.Path, "/") + path
    upstream.RawQuery = r.URL.RawQuery

    req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream.String(), nil)
    // ... error handling ...
    resp, err := proxyClient.Do(req)
    // ... rest unchanged ...
}
```

The `proxyClient` should be a package-level var or injected into `PrometheusHandler`. The existing integration tests in `telemetry_proxy_integration_test.go` cover all three degradation shapes — they must continue to pass after hardening. The context propagation means a browser disconnect cancels the upstream request. [VERIFIED: prometheus.go + integration test read]

### Pattern 5: Vitest Fetch Mocking (TELEM-02)

The established project pattern (confirmed from `dag-views.test.tsx`, `projects.test.ts`) is `vi.stubGlobal("fetch", fn)`. The TelemetryView/AppShell tests need two shapes:

```typescript
// Source: dashboard/web/src/components/__tests__/dag-views.test.tsx pattern (VERIFIED)
// Shape 1: 200 + {"status":"unavailable"} sentinel
vi.stubGlobal("fetch", vi.fn().mockResolvedValue({
    ok: true,
    json: async () => ({ status: "unavailable" }),
} as unknown as Response));
// Assert: screen.getAllByTestId("telemetry-unavailable-notice") has entries

// Shape 2: 502 error
vi.stubGlobal("fetch", vi.fn().mockResolvedValue({
    ok: false,
    status: 502,
    json: async () => ({ status: "error", message: "upstream unreachable" }),
} as unknown as Response));
// Assert: same TelemetryUnavailableNotice with unreachable wording
```

`TelemetryUnavailableNotice` already has `data-testid="telemetry-unavailable-notice"` [VERIFIED: file read]. The `fetchQueryRange` function already handles both shapes [VERIFIED: TelemetryView.tsx read].

### Pattern 6: View Switcher (D-01)

The view switcher lives in `App.tsx`, not `Header.tsx` or `AppShell.tsx`. `Header` has a `projectPicker` slot but no `viewSwitcher` slot — it needs a new slot or the switcher is placed between projectPicker and connectionStatus. The existing `App.tsx` structure (line 282-314, VERIFIED) shows `AppShell` receives the header via the `header` prop. The simplest approach: add a `viewSwitcher` slot to `HeaderProps` or embed the switcher in App.tsx's header JSX inline.

`AppShell.tsx` is a pure layout shell (VERIFIED: it only renders header + main + ToastContainer). The view switching state (`activeView: "dags" | "telemetry"`) belongs in `App.tsx` — it is sibling state to `selectedProject`, `selectedPlan`, etc. The body switching is:
```tsx
// Source: App.tsx body branch pattern (VERIFIED — lines 197-279)
// Add: const [activeView, setActiveView] = useState<"dags" | "telemetry">("dags");
// In body render:
if (activeView === "telemetry") {
    body = <TelemetryView projectName={selectedProject ?? ""} namespace={selectedNamespace ?? ""} budget={budget} />;
} else {
    // existing two-column grid
}
```

The `budget` prop needs to be plumbed from `Project.Status.Budget`. `TelemetryViewProps` already requires `budget: BudgetSummary` [VERIFIED: TelemetryView.tsx:331-335]. App.tsx currently does not fetch budget data for the selected project — the data lives on the Project CR's status, accessible via the `/api/v1/projects/{name}` endpoint that `PlansHandler.Get` or `ProjectsHandler.Get` already serves. [ASSUMED — need to verify what field the projects list endpoint returns]

### Pattern 7: recharts Time-Series Panel

```tsx
// Source: Context7 /recharts/recharts docs (CITED: github.com/recharts/recharts)
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer } from "recharts";

// Prometheus matrix → recharts data transform:
function matrixToPoints(series: PromMatrix[]): { time: number; [key: string]: number }[] {
    // Merge multiple series into [{time, seriesA, seriesB, ...}]
}

<ResponsiveContainer width="100%" height={180}>
  <AreaChart data={points}>
    <XAxis dataKey="time" type="number" scale="time"
           tickFormatter={(v) => new Date(v * 1000).toLocaleTimeString()} />
    <YAxis />
    <Tooltip />
    {series.map((s, i) => (
      <Area key={i} type="monotone" dataKey={seriesKey(s)} stroke={COLORS[i]} fill={COLORS[i]} fillOpacity={0.2} />
    ))}
  </AreaChart>
</ResponsiveContainer>
```

recharts v3.x uses SVG only (no canvas). `ResponsiveContainer` handles SSR gracefully [VERIFIED: Context7 docs]. The jsdom test environment does not implement `ResizeObserver` but the setup.ts already polyfills it [VERIFIED: setup.ts read].

### Anti-Patterns to Avoid

- **Do NOT change `charts/` files directly.** The chart is a FIXED contract; PROM_ENDPOINT injection is already in `charts/tide/templates/dashboard-deployment.yaml:58-65`. [VERIFIED: CONTEXT.md D-01 in canonical refs]
- **Do NOT add a `task` label to any metric.** The `metriccardinality` analyzer will fail the build. [VERIFIED: analyzer.go]
- **Do NOT use `http.DefaultClient` in the hardened proxy.** Create a `http.Client{Timeout: 30 * time.Second}` — either package-level var or injected field on `PrometheusHandler`.
- **Do NOT call `metrics.Registry.MustRegister` twice for the same metric.** The existing `ProviderRateLimitHitsTotal` re-export pattern shows the correct way to avoid double-registration panics. [VERIFIED: registry.go:163-169]
- **Do NOT pass config.Load() path to the dashboard binary.** `cmd/dashboard/main.go` reads `PROM_ENDPOINT` directly from the environment; it does not use `internal/config`. [VERIFIED: main.go read]
- **Do NOT merge recharts test renders into the existing vitest setup without checking ResizeObserver.** The setup.ts already polyfills it — recharts' ResponsiveContainer will work. [VERIFIED: setup.ts read]

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Time-series chart rendering | Custom SVG AreaChart | recharts ResponsiveContainer + AreaChart | Axis formatting, tooltip, responsive sizing, Prometheus matrix-to-points transform |
| HTTP client timeout | Sleep + goroutine cancel | `http.Client{Timeout: 30*time.Second}` | stdlib; handles OS-level socket timeouts, not just response timeouts |
| Context propagation to HTTP request | Custom context threading | `http.NewRequestWithContext(r.Context(), ...)` | stdlib pattern; browser disconnect cancels upstream |
| URL path joining with base paths | String concatenation | `upstream.Path = strings.TrimRight(upstream.Path, "/") + path` | Avoids overwriting operator-configured base paths (e.g. `/prometheus`) |

**Key insight:** The proxy bug (`upstream.Path = path`) is a one-line replacement; resist the urge to use `url.JoinPath` which strips trailing slashes differently than Prometheus expects. The `strings.TrimRight + concat` pattern is idiomatic for this use case.

## Common Pitfalls

### Pitfall 1: dashboard main.go does NOT call config.Load()
**What goes wrong:** Adding `PrometheusEndpoint` to `internal/config/config.go` but forgetting that `cmd/dashboard/main.go` is a separate binary that never calls `config.Load()`. The manager binary reads the config file; the dashboard binary reads env directly.
**Why it happens:** `internal/config` exists and looks like the right place; CONTEXT.md and MILESTONE.md both mention it; the seam is subtle.
**How to avoid:** The fix is two separate edits: (1) add the field to `config.go` for manager-side future use, (2) in `main.go` read `os.Getenv("PROM_ENDPOINT")` inline when constructing `Dependencies`.
**Warning signs:** `PrometheusEndpoint` is "" at runtime despite PROM_ENDPOINT being set in the Pod.

### Pitfall 2: `upstream.Path = path` clobbers Prometheus base paths
**What goes wrong:** If the operator sets `prometheusEndpoint: http://myprom:9090/prometheus`, the current code sets `upstream.Path = "/api/v1/query_range"`, discarding `/prometheus`. Requests 404 on the upstream.
**Why it happens:** The original implementation assumed Prometheus is always at the root path.
**How to avoid:** `upstream.Path = strings.TrimRight(upstream.Path, "/") + path`. The existing integration test uses `httptest.NewServer` which serves at root — the test does NOT catch this bug. A new test with a base-path upstream is needed if full coverage is desired, but the TELEM-06 requirement only requires the proxy to "preserve base paths" — the fix is the implementation change.
**Warning signs:** PromQL queries return 404 when endpoint has a path component.

### Pitfall 3: Double MustRegister panic on metrics
**What goes wrong:** Adding new metric vars to `registry.go` and calling `metrics.Registry.MustRegister` on them, but also accidentally re-registering an existing metric (e.g. if copy-paste includes the existing `BudgetOverrunsTotal`).
**Why it happens:** The existing init() has a single `metrics.Registry.MustRegister(...)` call with all 7 metrics listed. Adding to this call is correct; creating a second call is not.
**How to avoid:** Extend the single MustRegister call in init() rather than adding a second one.

### Pitfall 4: Wave label resolution falls back to empty string silently
**What goes wrong:** `resolveWave` finds no Wave OwnerRef and returns `""`. Metrics emit with `wave=""` label value — creates a cardinality anomaly and breaks PromQL `by (wave)` queries.
**Why it happens:** Not all Tasks may have a direct Wave OwnerRef (edge case during controller transitions).
**How to avoid:** Make `resolveWave` return an error on miss and emit metrics only when the wave name is non-empty. Use `"unknown"` as a safe sentinel if the wave is genuinely unresolvable.

### Pitfall 5: recharts ResponsiveContainer needs a parent with defined height
**What goes wrong:** Wrapping recharts in a div with no explicit height or `flex-1` — `ResponsiveContainer height="100%"` gets 0px.
**Why it happens:** ResponsiveContainer uses ResizeObserver to measure its parent; jsdom polyfill returns 0 dimensions.
**How to avoid:** Use a fixed pixel height (e.g. `height={180}`) on ResponsiveContainer rather than `"100%"` in production, OR ensure the parent has an explicit height. For tests, the ResizeObserver polyfill is already in setup.ts.

### Pitfall 6: Vitest timer handling for polling useEffect
**What goes wrong:** TelemetryView adds a polling interval (D-07) — tests that don't use `vi.useFakeTimers()` will have real intervals fire, causing "act() warning" flood or memory leaks.
**Why it happens:** `setInterval` in useEffect runs asynchronously in jsdom.
**How to avoid:** The degradation tests only need to verify the initial fetch shape (not polling). Use `vi.useFakeTimers()` + `vi.advanceTimersByTimeAsync(0)` pattern from the existing `dag-views.test.tsx`. Or test polling separately.

## Code Examples

### TELEM-01: main.go Dependencies wiring

```go
// Source: cmd/dashboard/main.go:149-156 (VERIFIED - current code shows the gap)
// Before:
router := RegisterRoutes(Dependencies{
    Client:    mgr.GetClient(),
    Hub:       pubsubHub,
    Clientset: clientset,
    Log:       setupLog.WithName("router"),
    SPAFS:     spaFS,
})
// After (add PrometheusEndpoint):
router := RegisterRoutes(Dependencies{
    Client:             mgr.GetClient(),
    Hub:                pubsubHub,
    Clientset:          clientset,
    Log:                setupLog.WithName("router"),
    SPAFS:              spaFS,
    PrometheusEndpoint: os.Getenv("PROM_ENDPOINT"),
})
```

### TELEM-03: Metric emission at RollUpUsage seam

```go
// Source: task_controller.go:931-944 (VERIFIED - current code, emission goes after line 935)
// After: budget.RollUpUsage(ctx, r.Client, project, out.Usage)
// Add (non-fatal, same pattern as setBudgetBlockedIfNeeded):
if emitErr := r.emitTaskMetrics(ctx, task, project, out.Usage); emitErr != nil {
    logger.Error(emitErr, "failed to emit task metrics (non-fatal)", "task", task.Name)
}
```

### TELEM-03: resolveWave implementation

```go
// Source: ASSUMED (new function, modeled on resolveProject at task_controller.go:948-968)
// resolveWave returns the name of the Wave CRD that directly owns this Task.
// Tasks in the normal execution path are owned by Wave CRDs.
// Returns ("unknown", nil) on miss so callers can emit a safe sentinel.
func (r *TaskReconciler) resolveWave(task *tideprojectv1alpha1.Task) string {
    for _, ref := range task.GetOwnerReferences() {
        if ref.Kind == "Wave" {
            return ref.Name
        }
    }
    return "unknown"
}
```

Note: `resolveWave` does NOT need a context or API call — it reads the Task's OwnerReferences, which are set when the Task is created and are in-memory on the already-fetched Task object. [ASSUMED: verified that wave_controller.go creates Tasks with Wave as OwnerRef via `controllerutil.SetControllerReference`]

### TELEM-04: Dead panel query fixes

Current TelemetryView.tsx PANELS (lines 73-95, VERIFIED):
```typescript
// WRONG (nonexistent metric names):
{ id: "dispatch-counts", query: "sum(increase(tide_tasks_dispatched_total[5m])) by (project)" }
{ id: "token-breakdown", query: "sum(tide_tokens_used_total) by (model)" }

// CORRECT (D-06 shapes):
{ id: "dispatch-counts", query: "sum(increase(tide_waves_dispatched_total[$__range])) by (project) + sum(increase(tide_tasks_completed_total[$__range])) by (project)" }
{ id: "token-breakdown", query: "sum(increase(tide_tokens_input_total[$__range])) by (project) + sum(increase(tide_tokens_output_total[$__range])) by (project) + sum(increase(tide_tokens_cache_read_total[$__range])) by (project) + sum(increase(tide_tokens_cache_creation_total[$__range])) by (project)" }
```

Also fix failure rate denominator (uses `tide_tasks_dispatched_total` which doesn't exist):
```typescript
// WRONG:
{ id: "failure-rate", query: "sum(rate(tide_tasks_failed_total[5m])) / sum(rate(tide_tasks_dispatched_total[5m]))" }
// CORRECT (D-06):
{ id: "failure-rate", query: "sum(rate(tide_tasks_failed_total[5m])) by (project) / (sum(rate(tide_tasks_failed_total[5m])) by (project) + sum(rate(tide_tasks_completed_total[5m])) by (project))" }
```

### TELEM-05: Makefile targets

```makefile
# Source: Makefile:546-555 (VERIFIED — existing helm-rbac-assert pattern)
##@ Helm telemetry gate (Phase 16 TELEM-05 — D-13/D-14)

.PHONY: helm-telemetry-assert
helm-telemetry-assert: ## Assert PROM_ENDPOINT env injection and telemetry render (TELEM-05).
	@helm template charts/tide --set dashboard.enabled=true > /tmp/tide-helm-render.yaml
	@python3 hack/helm/assert-prometheus-env.py /tmp/tide-helm-render.yaml --expect-absent
	@helm template charts/tide --set dashboard.enabled=true --set prometheus.endpoint=http://prom:9090 > /tmp/tide-helm-render-prom.yaml
	@python3 hack/helm/assert-prometheus-env.py /tmp/tide-helm-render-prom.yaml --expect-endpoint http://prom:9090
	@bash hack/helm/assert-telemetry-render.sh

.PHONY: helm-assert
helm-assert: helm-rbac-assert helm-telemetry-assert ## Run all Helm render gate assertions.
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Text Sparkline (last-value lists) | recharts AreaChart/BarChart | Phase 16 | Real time-series charts, not debug text |
| Dead metric names in PromQL | Locked metric names (tide_waves_dispatched_total, etc.) | Phase 16 | Panels actually return data |
| http.DefaultClient (no timeout) | http.Client{Timeout: 30s} + NewRequestWithContext | Phase 16 | Proxy respects browser disconnects, bounded hangs |
| PROM_ENDPOINT unread (dead config) | main.go reads env → Dependencies.PrometheusEndpoint | Phase 16 | Helm injection actually drives proxy |

**Deprecated/outdated:**
- `tide_tasks_dispatched_total`: does not exist in registry.go; replace with `tide_waves_dispatched_total` + `tide_tasks_completed_total`.
- `tide_tokens_used_total{model}`: does not exist; `model` label is illegal under the locked label set. Replace with four locked counter names.
- `Sparkline` component in TelemetryView.tsx: text-only, replace with recharts.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `resolveWave` can read Wave OwnerRef from in-memory Task object without an API call | Pattern 3 + Code Examples | Low — wave_controller.go clearly sets OwnerRef when creating Tasks; fallback to "unknown" is safe |
| A2 | Tasks are always directly owned by Wave CRDs (not by Plan directly) | Pattern 3 | Medium — if some Tasks are Plan-owned (integration Jobs?), resolveWave returns "unknown" for them |
| A3 | `Project.Status.Budget` is included in the projects list endpoint response that App.tsx already fetches | Pattern 6 | Medium — if BudgetSummary is not in the list response, TelemetryView needs a separate per-project fetch |
| A4 | The polling interval useEffect in TelemetryView does not cause test interference if `vi.useFakeTimers()` is used | Pitfall 6 | Low — standard Vitest pattern for interval-based hooks |
| A5 | `strings.TrimRight(upstream.Path, "/") + path` is the correct join for Prometheus URLs with base paths | Pattern 4 | Low — Prometheus always uses absolute paths starting with `/api/v1/`; the join is unambiguous |
| A6 | `internal/config/config.go` also needs the `PrometheusEndpoint` field even though `main.go` reads env directly | Pattern 1 | Low — MILESTONE.md explicitly names this file; both changes are needed but serve different binaries |

## Open Questions

1. **Does `Project.Status.Budget` come back in the projects list API response?**
   - What we know: `TelemetryViewProps` requires `budget: BudgetSummary`; Phase 14 added `BudgetSummary` to the Project CR status.
   - What's unclear: Does `ProjectsHandler.List` include `.Status.Budget` in its JSON response?
   - Recommendation: Check `cmd/dashboard/api/projects.go` — if the list endpoint omits budget, TelemetryView needs a separate per-project fetch or App.tsx must use the already-fetched project detail.

2. **Are integration Jobs (boundary-push) also Task-owned, and do they need wave resolution?**
   - What we know: D-08 says emit on ALL terminal branches; boundary-push Jobs are separate from Task Jobs.
   - What's unclear: Does boundary push go through `task_controller.go` terminal branch? (Likely not — it's handled by `plan_controller.go`.)
   - Recommendation: The six metrics are Task-scoped by design (Usage comes from `out.Usage` in the task reconciler). Boundary-push is not in scope.

3. **How does D-02's `{project="<selected>"}` label filter interact with the `wave` label being on the six new metrics?**
   - What we know: The four locked PromQL panel queries filter by project label. The `plan` label is also on the new metrics (D-10).
   - What's unclear: Whether the 24h range panels should also filter `by (plan)` or aggregate across all plans for the selected project.
   - Recommendation: Default to `by (project)` aggregation for the TelemetryView panels — this matches the cardinality budget table's "Project roll-up" level and keeps panel queries simple.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Node.js / npm | recharts install | ✓ | (in path) | — |
| python3 | helm gate scripts | ✓ | system | — |
| helm | Makefile helm targets | ✓ (CI installs) | v3.16.3 in CI | — |
| recharts npm package | TELEM-02 charts | ✓ (npm view confirmed) | 3.8.1 | — |

**Missing dependencies with no fallback:** none.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework (Go) | standard `testing` + `testutil.CollectAndCount` (prometheus) + Ginkgo/Gomega for integration |
| Framework (TS) | Vitest 1.6.1 + @testing-library/react 16.3.2 |
| Config file (TS) | `dashboard/web/vitest.config.ts` |
| Quick run command (TS) | `cd dashboard/web && npm test` |
| Full suite command (Go) | `make test` (unit) |
| Quick run command (Go) | `go test ./internal/metrics/... ./cmd/dashboard/api/...` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| TELEM-01 | `os.Getenv("PROM_ENDPOINT")` populates `Dependencies.PrometheusEndpoint` | unit (Go) | `go test ./cmd/dashboard/... -run TestRouterDependencies` | ❌ Wave 0 |
| TELEM-02 | TelemetryView renders `TelemetryUnavailableNotice` on 200 `unavailable` sentinel | Vitest | `cd dashboard/web && npm test -- TelemetryView` | ❌ Wave 0 |
| TELEM-02 | TelemetryView renders `TelemetryUnavailableNotice` on 502 error | Vitest | `cd dashboard/web && npm test -- TelemetryView` | ❌ Wave 0 |
| TELEM-02 | AppShell/App.tsx Telemetry tab mounts TelemetryView | Vitest | `cd dashboard/web && npm test -- AppShell` | ❌ Wave 0 |
| TELEM-03 | Six metric families registered in `crmetrics.Registry` | unit (Go) | `go test ./internal/metrics/... -run TestRegistry` | ✅ (extend registry_test.go) |
| TELEM-03 | Label arity `{project, phase, plan, wave}` = 4 for each new metric | unit (Go) | `go test ./internal/metrics/... -run TestRegistry` | ✅ (extend registry_test.go) |
| TELEM-03 | `resolveWave` returns Wave CRD name from Task OwnerRef | unit (Go) | `go test ./internal/controller/... -run TestResolveWave` | ❌ Wave 0 |
| TELEM-04 | All four panel queries use only names in registry.go | code check | grep/test | ❌ Wave 0 (or compile-time check) |
| TELEM-05 | `make helm-telemetry-assert` passes | shell | `make helm-telemetry-assert` | ❌ Wave 0 (targets don't exist) |
| TELEM-05 | `make helm-assert` passes | shell | `make helm-assert` | ❌ Wave 0 |
| TELEM-06 | Proxy uses bounded HTTP client (no DefaultClient) | unit (Go) | `go test ./cmd/dashboard/api/... -run TestPrometheusProxy` | ✅ (extend telemetry_proxy_integration_test.go) |
| TELEM-06 | Proxy preserves base path in configured endpoint | unit (Go) | `go test ./cmd/dashboard/api/... -run TestPrometheusProxyBasePath` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./internal/metrics/... ./cmd/dashboard/api/...` (Go side); `cd dashboard/web && npm test` (TS side)
- **Per wave merge:** `make test` + `cd dashboard/web && npm test`
- **Phase gate:** Full suite green + `make helm-assert` passes before `/gsd:verify-work`

### Wave 0 Gaps
- [ ] `dashboard/web/src/components/TelemetryView.test.tsx` — covers TELEM-02 (both degradation shapes)
- [ ] `dashboard/web/src/components/AppShell.test.tsx` (or App.test.tsx addition) — covers TELEM-02 tab mounting
- [ ] `internal/controller/task_controller_wave_test.go` — covers `resolveWave` unit test (TELEM-03)
- [ ] `cmd/dashboard/api/prometheus_basepath_test.go` — covers TELEM-06 base-path preservation

*(Extend existing files where possible: `internal/metrics/registry_test.go` for TELEM-03 arity; `cmd/dashboard/api/telemetry_proxy_integration_test.go` for TELEM-06 timeout.)*

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | — |
| V3 Session Management | no | — |
| V4 Access Control | no | Dashboard is read-only (DASH-05 enforced); proxy only forwards GET |
| V5 Input Validation | yes | Proxy passes PromQL query params verbatim — acceptable for internal Prometheus; operator-configured endpoint |
| V6 Cryptography | no | — |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| SSRF via configurable Prometheus endpoint | Spoofing/Tampering | Endpoint is operator-set via Helm values; not user-controlled. Low risk in this deployment model |
| Unbounded proxy hangs (TELEM-06) | Denial of Service | `http.Client{Timeout: 30s}` — this is the TELEM-06 fix |
| PromQL injection via `query` param | Tampering | Prometheus evaluates queries against its own data only; no SQL injection risk; acceptable passthrough |

## Sources

### Primary (HIGH confidence)
- `cmd/dashboard/main.go` — verified that Dependencies has no PrometheusEndpoint wired from env
- `cmd/dashboard/router.go` — verified PrometheusEndpoint field exists in Dependencies struct
- `cmd/dashboard/api/prometheus.go` — verified both TELEM-06 bugs: `http.DefaultClient.Get` + `upstream.Path = path`
- `cmd/dashboard/api/telemetry_proxy_integration_test.go` — verified three-shape degradation test coverage
- `internal/metrics/registry.go` — verified 7 existing metrics, none of the 6 new ones present
- `internal/metrics/registry_test.go` — verified test pattern for new metrics
- `internal/controller/task_controller.go:840-945` — verified three RollUpUsage call sites at :857/:883/:932
- `internal/controller/task_controller.go:948-1002` — verified resolveProject + walkOwnerChainToProject pattern
- `pkg/dispatch/envelope.go:252-282` — verified Usage struct fields: InputTokens, OutputTokens, EstimatedCostCents, CacheReadTokens, CacheCreationTokens
- `api/v1alpha1/task_types.go` — verified PlanRef field exists; StartedAt + CompletedAt *metav1.Time exist
- `dashboard/web/src/components/TelemetryView.tsx` — verified panel definitions with two dead queries; Sparkline text-only renderer
- `dashboard/web/src/components/TelemetryUnavailableNotice.tsx` — verified data-testid
- `dashboard/web/src/App.tsx` — verified current body render structure; no activeView state
- `dashboard/web/src/components/Header.tsx` — verified no tab/view-switcher slot
- `tools/analyzers/metriccardinality/analyzer.go` — verified only `"task"` literal is forbidden, not `"wave"` or `"plan"`
- `hack/helm/assert-prometheus-env.py` — verified --expect-endpoint / --expect-absent API
- `hack/helm/assert-telemetry-render.sh` — verified 4-permutation structure; no cluster needed
- `Makefile:540-555` — verified `helm-rbac-assert` pattern; no `helm-telemetry-assert` or `helm-assert` exist
- `.github/workflows/ci.yaml:141-193` — verified `helm-lint` job structure; no `make helm-assert` step
- `dashboard/web/vitest.config.ts` — verified jsdom environment + setupFiles
- `dashboard/web/src/__tests__/setup.ts` — verified ResizeObserver polyfill present
- `/recharts/recharts` Context7 docs — verified ResponsiveContainer SSR support, XAxis time scale pattern

### Secondary (MEDIUM confidence)
- `npm view recharts version` → 3.8.1 + peer deps covering React 18 [VERIFIED: npm registry]
- `slopcheck install recharts --ecosystem npm` → [OK] [VERIFIED: slopcheck run]

### Tertiary (LOW confidence)
- None — all critical claims verified from source files.

## Metadata

**Confidence breakdown:**
- Standard stack (recharts): HIGH — npm registry + slopcheck + Context7 verified
- Architecture (existing seams): HIGH — all critical files read directly
- Pitfalls: HIGH — derived from actual observed code bugs, not speculation
- Wave label resolution: MEDIUM — resolveWave pattern is ASSUMED from code structure; exact owner chain verified but the assumption that Wave is always the direct Task owner needs test confirmation

**Research date:** 2026-06-12
**Valid until:** 2026-07-12 (recharts patch versions may bump; all other findings are stable internal code)
