# Phase 16: Telemetry Completion - Pattern Map

**Mapped:** 2026-06-12
**Files analyzed:** 11 new/modified files
**Analogs found:** 11 / 11

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/config/config.go` | config | request-response | itself (extend) | exact |
| `cmd/dashboard/main.go` | config | request-response | itself (extend) | exact |
| `cmd/dashboard/api/prometheus.go` | middleware/proxy | request-response | itself (harden) | exact |
| `internal/metrics/registry.go` | utility | CRUD | itself (extend) | exact |
| `internal/controller/task_controller.go` | controller | event-driven | `resolveProject` + `setBudgetBlockedIfNeeded` within same file | exact |
| `dashboard/web/src/App.tsx` | component | event-driven | itself (extend) | exact |
| `dashboard/web/src/components/Header.tsx` | component | request-response | itself (extend, add slot) | exact |
| `dashboard/web/src/components/TelemetryView.tsx` | component | request-response | itself (fix + enhance) | exact |
| `dashboard/web/src/components/TelemetryView.test.tsx` (new) | test | request-response | `dashboard/web/src/lib/api.test.ts` + `dag-views.test.tsx` | exact |
| `Makefile` | config | batch | `helm-rbac-assert` target at Makefile:546 | exact |
| `.github/workflows/ci.yaml` | config | batch | existing `helm-lint` job steps at ci.yaml:141 | exact |

## Pattern Assignments

### `internal/config/config.go` (config, extend)

**Analog:** itself — existing `Load()` + `rawConfig` pattern

**Current struct pattern** (lines 44-48):
```go
type Config struct {
    PlannerConcurrency      int                     `yaml:"plannerConcurrency"`
    ExecutorConcurrency     int                     `yaml:"executorConcurrency"`
    MaxConcurrentReconciles MaxConcurrentReconciles `yaml:"maxConcurrentReconciles"`
}
```

**Add** (copy the struct + rawConfig pointer-field pattern, line 44 block):
```go
// PrometheusEndpoint is the Prometheus base URL injected by Helm's
// prometheusEndpoint value. Empty is valid — the proxy self-degrades.
// YAML key: prometheusEndpoint. Env override: PROM_ENDPOINT (manager side).
PrometheusEndpoint string `yaml:"prometheusEndpoint"`
```

**rawConfig pattern** (lines 75-88) — add a parallel `*string` field:
```go
type rawConfig struct {
    // ... existing pointer fields ...
    PrometheusEndpoint *string `yaml:"prometheusEndpoint"`
}
```

**resolveField equivalent** — strings have no "must be >= 1" constraint. After the existing `applyAndValidate` calls in `Load()`, apply directly (empty string is valid):
```go
if raw.PrometheusEndpoint != nil {
    cfg.PrometheusEndpoint = *raw.PrometheusEndpoint
}
// else cfg.PrometheusEndpoint stays "" (valid; proxy self-degrades)
```

**Critical distinction:** `cmd/dashboard/main.go` does NOT call `config.Load()`. It reads `PROM_ENDPOINT` directly from `os.Getenv`. The config.go change serves the manager binary only — both changes are required and serve different binaries.

---

### `cmd/dashboard/main.go` (config, one-line add)

**Analog:** itself — existing `RegisterRoutes(Dependencies{...})` call at lines 149-156

**Current gap** (lines 149-156):
```go
router := RegisterRoutes(Dependencies{
    Client:    mgr.GetClient(),
    Hub:       pubsubHub,
    Clientset: clientset,
    Log:       setupLog.WithName("router"),
    SPAFS:     spaFS,
    // PrometheusEndpoint is MISSING — this is the TELEM-01 gap
})
```

**Fix pattern** (add the single field; `os` is already imported):
```go
router := RegisterRoutes(Dependencies{
    Client:             mgr.GetClient(),
    Hub:                pubsubHub,
    Clientset:          clientset,
    Log:                setupLog.WithName("router"),
    SPAFS:              spaFS,
    PrometheusEndpoint: os.Getenv("PROM_ENDPOINT"),
})
```

No new imports needed — `os` is already in the import block at line 47.

---

### `cmd/dashboard/api/prometheus.go` (middleware/proxy, harden)

**Analog:** itself — two bugs to fix in the existing `proxy()` method

**Bug 1 — no timeout, no context** (line 87):
```go
// BEFORE (current):
resp, err := http.DefaultClient.Get(upstream.String()) //nolint:noctx

// AFTER — package-level bounded client + context propagation:
```

**Package-level client** (add after imports, before `PrometheusHandler` type):
```go
// proxyTimeout bounds individual upstream Prometheus requests. Prevents
// browser-disconnect from hanging a goroutine indefinitely (TELEM-06).
const proxyTimeout = 30 * time.Second

var proxyClient = &http.Client{Timeout: proxyTimeout}
```

**Context-propagating request** (replace line 87):
```go
req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream.String(), nil)
if err != nil {
    h.Log.Error(err, "failed to build upstream request", "url", upstream.String())
    w.WriteHeader(http.StatusBadGateway)
    _ = json.NewEncoder(w).Encode(map[string]string{
        "status":  "error",
        "message": fmt.Sprintf("failed to build request: %v", err),
    })
    return
}
resp, err := proxyClient.Do(req)
```

**Bug 2 — base-path clobber** (line 84):
```go
// BEFORE (current):
upstream.Path = path

// AFTER — preserve operator-configured base path:
upstream.Path = strings.TrimRight(upstream.Path, "/") + path
```

Add `"strings"` and `"time"` to the import block. Remove the `//nolint:noctx` comment (no longer needed). The three-shape degradation contract (`unavailable` / `error` / pass-through) is UNCHANGED — only the HTTP client and URL join are replaced.

**Existing integration test analog** (`cmd/dashboard/api/telemetry_proxy_integration_test.go`, lines 61-72):
```go
func buildTelemetryProxyRouter(endpoint string) http.Handler {
    handler := &PrometheusHandler{
        Endpoint: endpoint,
        Log:      logr.Discard(),
    }
    r := chi.NewRouter()
    r.Route("/api/v1", func(r chi.Router) {
        r.Get("/query", handler.Query)
        r.Get("/query_range", handler.QueryRange)
    })
    return r
}
```
Use this same helper for the new base-path test — extend the existing file, don't create a new one.

---

### `internal/metrics/registry.go` (utility, extend)

**Analog:** itself — the exact existing var + init() pattern for 7 metrics

**Existing bucket var pattern** (lines 33):
```go
var dispatchLatencyBuckets = []float64{0.1, 0.5, 1, 5, 10, 30, 60, 300, 1800}
```

**New bucket var** (add after `dispatchLatencyBuckets`):
```go
// taskDurationBuckets covers the minutes-to-hours range for agentic tasks
// (Phase 16 D-11). Prometheus default buckets top out at 10s — useless here.
var taskDurationBuckets = []float64{30, 60, 120, 300, 600, 1200, 1800, 3600, 7200}
```

**Existing CounterVec var pattern** (lines 41-45):
```go
var WavesDispatchedTotal *prometheus.CounterVec
var TasksCompletedTotal *prometheus.CounterVec
```

**New var block** (add after `BudgetOverrunsTotal`):
```go
// Six locked metrics for token, cost, and duration telemetry (Phase 16 TELEM-03).
// Label set: {project, phase, plan, wave} — D-10. "wave" label is permitted by
// the metriccardinality analyzer; only the literal "task" is forbidden (Pitfall 17).
var TokensInputTotal *prometheus.CounterVec
var TokensOutputTotal *prometheus.CounterVec
var TokensCacheReadTotal *prometheus.CounterVec
var TokensCacheCreationTotal *prometheus.CounterVec
var CostCentsTotal *prometheus.CounterVec
var TaskDurationSeconds *prometheus.HistogramVec
```

**Existing NewCounterVec init() pattern** (lines 97-103):
```go
WavesDispatchedTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "tide_waves_dispatched_total",
        Help: "Count of Waves dispatched to the executor pool, surfaced by Project, Phase, and Plan (Phase 4 D-O2).",
    },
    []string{"project", "phase", "plan"},
)
```

**New init() registrations** (add six vars following identical pattern, then extend the single MustRegister call):
```go
TokensInputTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "tide_tokens_input_total",
        Help: "Input tokens consumed by Tasks, surfaced by Project, Phase, Plan, and Wave (Phase 16 TELEM-03).",
    },
    []string{"project", "phase", "plan", "wave"},
)
TokensOutputTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "tide_tokens_output_total",
        Help: "Output tokens produced by Tasks (Phase 16 TELEM-03).",
    },
    []string{"project", "phase", "plan", "wave"},
)
TokensCacheReadTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "tide_tokens_cache_read_total",
        Help: "Cache-read tokens consumed by Tasks (Phase 16 TELEM-03).",
    },
    []string{"project", "phase", "plan", "wave"},
)
TokensCacheCreationTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "tide_tokens_cache_creation_total",
        Help: "Cache-creation tokens consumed by Tasks (Phase 16 TELEM-03).",
    },
    []string{"project", "phase", "plan", "wave"},
)
CostCentsTotal = prometheus.NewCounterVec(
    prometheus.CounterOpts{
        Name: "tide_cost_cents_total",
        Help: "Estimated cost in US cents consumed by Tasks (Phase 16 TELEM-03).",
    },
    []string{"project", "phase", "plan", "wave"},
)
TaskDurationSeconds = prometheus.NewHistogramVec(
    prometheus.HistogramOpts{
        Name:    "tide_task_duration_seconds",
        Help:    "Wall-clock duration of Tasks from dispatch to terminal state. Buckets sized for agentic tasks (Phase 16 D-11).",
        Buckets: taskDurationBuckets,
    },
    []string{"project", "phase", "plan", "wave"},
)
```

**Extend the single MustRegister call** (lines 154-162 — DO NOT add a second call; that panics):
```go
metrics.Registry.MustRegister(
    WavesDispatchedTotal,
    TasksCompletedTotal,
    TasksFailedTotal,
    DispatchLatency,
    SecretLeakBlockedTotal,
    PushJobsTotal,
    BudgetOverrunsTotal,
    // Phase 16 TELEM-03:
    TokensInputTotal,
    TokensOutputTotal,
    TokensCacheReadTotal,
    TokensCacheCreationTotal,
    CostCentsTotal,
    TaskDurationSeconds,
)
```

---

### `internal/controller/task_controller.go` (controller, extend)

**Analog 1 — resolveWave helper:** `resolveProject` at lines 948-968 + `walkOwnerChainToProject` at lines 970-1002

**resolveProject fast-path pattern** (lines 956-962):
```go
func (r *TaskReconciler) resolveProject(ctx context.Context, task *tideprojectv1alpha1.Task) (*tideprojectv1alpha1.Project, error) {
    if projectName, ok := task.Labels["tideproject.k8s/project"]; ok && projectName != "" {
        var project tideprojectv1alpha1.Project
        if err := r.Get(ctx, client.ObjectKey{Namespace: task.Namespace, Name: projectName}, &project); err == nil {
            return &project, nil
        }
    }
    // ...owner chain walk...
}
```

**New resolveWave** (no context/API call needed — reads in-memory OwnerReferences; add after `resolveProject`):
```go
// resolveWave returns the name of the Wave CRD that directly owns this Task.
// Tasks in the normal execution path are created by the wave controller with
// Wave as their controller OwnerRef (wave_controller.go SetControllerReference).
// Returns "unknown" on miss so callers can emit a safe sentinel label value.
// No API call needed — OwnerReferences are part of the in-memory Task object.
func (r *TaskReconciler) resolveWave(task *tideprojectv1alpha1.Task) string {
    for _, ref := range task.GetOwnerReferences() {
        if ref.Kind == "Wave" {
            return ref.Name
        }
    }
    return "unknown"
}
```

**Analog 2 — emission call site:** `setBudgetBlockedIfNeeded` non-fatal pattern at lines 941-943

**Non-fatal helper call pattern** (lines 941-943):
```go
if err := setBudgetBlockedIfNeeded(ctx, r.Client, project, r.Deps.Reservations.TotalReserved()); err != nil {
    logger.Error(err, "setBudgetBlockedIfNeeded failed (non-fatal)", "task", task.Name)
}
```

**Emission follows immediately after the third RollUpUsage** (line 935). Insert after each of the three RollUpUsage blocks (:857, :883, :932):
```go
// Emit six locked metrics at the same once-only terminal commit point as
// budget.RollUpUsage — guarantees Prometheus cost totals never diverge from
// Budget accounting (Phase 16 D-12). Non-fatal: task is already terminal.
if emitErr := r.emitTaskMetrics(task, project, out.Usage, out.CompletedAt); emitErr != nil {
    logger.Error(emitErr, "failed to emit task metrics (non-fatal)", "task", task.Name)
}
```

**New emitTaskMetrics helper** (add after `resolveWave`):
```go
// emitTaskMetrics emits the six locked Phase 16 telemetry metrics at the
// exact terminal commit point shared with budget.RollUpUsage (D-12).
// Failures are non-fatal — the task is already in terminal state.
func (r *TaskReconciler) emitTaskMetrics(
    task *tideprojectv1alpha1.Task,
    project *tideprojectv1alpha1.Project,
    usage dispatch.Usage,
    completedAt time.Time,
) error {
    wave := r.resolveWave(task)
    projectName := project.Name
    phase := task.Labels["tideproject.k8s/phase"] // set by PlanReconciler; "" if absent
    plan := task.Spec.PlanRef

    labels := []string{projectName, phase, plan, wave}

    tidemetrics.TokensInputTotal.WithLabelValues(labels...).Add(float64(usage.InputTokens))
    tidemetrics.TokensOutputTotal.WithLabelValues(labels...).Add(float64(usage.OutputTokens))
    tidemetrics.TokensCacheReadTotal.WithLabelValues(labels...).Add(float64(usage.CacheReadTokens))
    tidemetrics.TokensCacheCreationTotal.WithLabelValues(labels...).Add(float64(usage.CacheCreationTokens))
    tidemetrics.CostCentsTotal.WithLabelValues(labels...).Add(float64(usage.EstimatedCostCents))

    if task.Status.StartedAt != nil && !completedAt.IsZero() {
        duration := completedAt.Sub(task.Status.StartedAt.Time)
        tidemetrics.TaskDurationSeconds.WithLabelValues(labels...).Observe(duration.Seconds())
    }
    return nil
}
```

**Usage struct fields** (from `pkg/dispatch/envelope.go:252-282`, verified):
- `usage.InputTokens` (int64)
- `usage.OutputTokens` (int64)
- `usage.CacheReadTokens` (int64)
- `usage.CacheCreationTokens` (int64)
- `usage.EstimatedCostCents` (int64)

**Import to add:** `tidemetrics "github.com/jsquirrelz/tide/internal/metrics"`

---

### `dashboard/web/src/App.tsx` (component, extend)

**Analog:** itself — existing `useState` + body branch pattern at lines 89-100 and 197-280

**Existing state pattern** (lines 93-100):
```tsx
const [selectedProject, setSelectedProject] = useState<string | null>(null);
const [selectedPlan, setSelectedPlan] = useState<string | null>(null);
const [selectedTask, setSelectedTask] = useState<string | null>(null);
const [streamingTask, setStreamingTask] = useState<string | null>(null);
const [connState, setConnState] = useState<SSEState>("connecting");
```

**New state** (add after existing state block):
```tsx
// D-01 view switcher. Transient — not persisted (D-04).
const [activeView, setActiveView] = useState<"dags" | "telemetry">("dags");
```

**Existing body branch pattern** (lines 197-204):
```tsx
let body: React.ReactNode;
if (projectsError) {
    body = <ErrorState variant="backend-unreachable" />;
} else if (projectsLoading && projects.length === 0) {
    body = <LoadingState variant="initial" />;
} else if (projects.length === 0) {
    body = <EmptyState variant="no-projects" />;
} else {
    body = ( /* two-column grid */ );
}
```

**Extend the `else` branch** to switch on `activeView`:
```tsx
} else if (activeView === "telemetry") {
    // D-02: use selectedProject as filter; empty string → all-projects mode in TelemetryView
    const selectedProjectData = projects.find(p => p.name === selectedProject);
    const budget = selectedProjectData?.budget ?? { capCents: 0, currentSpend: 0, withinBudget: true };
    body = (
        <TelemetryView
            projectName={selectedProject ?? ""}
            namespace={selectedNamespace ?? "default"}
            budget={budget}
        />
    );
} else {
    body = ( /* existing two-column grid unchanged */ );
}
```

**Existing Header slot pattern** (lines 286-292):
```tsx
<Header
    connectionStatus={<ConnectionStatusIndicator state={connectionState} />}
    projectPicker={
        <ProjectPicker ... />
    }
/>
```

**Add viewSwitcher slot** to Header call:
```tsx
<Header
    connectionStatus={<ConnectionStatusIndicator state={connectionState} />}
    projectPicker={<ProjectPicker ... />}
    viewSwitcher={
        <ViewSwitcher activeView={activeView} onChange={setActiveView} />
    }
/>
```

**ViewSwitcher inline component** (define above App, following PaneHeader pattern at lines 73-87):
```tsx
function ViewSwitcher({ activeView, onChange }: { activeView: "dags" | "telemetry"; onChange: (v: "dags" | "telemetry") => void }) {
    return (
        <div
            role="tablist"
            aria-label="Dashboard view"
            style={{ display: "flex", gap: "4px", fontFamily: "var(--font-mono)", fontSize: "12px", fontWeight: 600 }}
        >
            {(["dags", "telemetry"] as const).map((v) => (
                <button
                    key={v}
                    role="tab"
                    aria-selected={activeView === v}
                    data-testid={`view-tab-${v}`}
                    onClick={() => onChange(v)}
                    style={{
                        background: activeView === v ? "var(--color-surface-overlay)" : "none",
                        border: "1px solid var(--color-border-subtle)",
                        borderRadius: "4px",
                        padding: "2px 10px",
                        cursor: "pointer",
                        color: activeView === v ? "var(--color-text-primary)" : "var(--color-text-muted)",
                        textTransform: "uppercase",
                    }}
                >
                    {v === "dags" ? "DAGs" : "Telemetry"}
                </button>
            ))}
        </div>
    );
}
```

**New imports to add:** `TelemetryView` from `"./components/TelemetryView"`

---

### `dashboard/web/src/components/Header.tsx` (component, extend)

**Analog:** itself — existing `HeaderProps` interface + JSX slot pattern at lines 15-20 and 79-88

**Existing slot pattern** (lines 15-20):
```tsx
export type HeaderProps = {
    connectionStatus: ReactNode;
    projectPicker?: ReactNode;
};
```

**Add viewSwitcher slot** (matching the optional-slot pattern of `projectPicker`):
```tsx
export type HeaderProps = {
    connectionStatus: ReactNode;
    projectPicker?: ReactNode;
    viewSwitcher?: ReactNode;   // D-01 — populated by App.tsx
};
```

**Existing left-cluster JSX** (lines 79-88):
```tsx
<div className="flex items-center gap-4">
    <span ... aria-label="TIDE dashboard">TIDE</span>
    {projectPicker}
</div>
```

**Add viewSwitcher after projectPicker**:
```tsx
<div className="flex items-center gap-4">
    <span ... aria-label="TIDE dashboard">TIDE</span>
    {projectPicker}
    {viewSwitcher}
</div>
```

Destructure `viewSwitcher` from props in the function signature alongside existing props.

---

### `dashboard/web/src/components/TelemetryView.tsx` (component, fix + enhance)

**Analog:** itself — fix dead queries and replace Sparkline

**Fix 1 — dead PromQL query names** (lines 73-95, current PANELS array):
```tsx
// WRONG (two nonexistent metric names):
{ id: "dispatch-counts", query: "sum(increase(tide_tasks_dispatched_total[5m])) by (project)" }
{ id: "failure-rate",    query: "sum(rate(tide_tasks_failed_total[5m])) / sum(rate(tide_tasks_dispatched_total[5m]))" }
{ id: "token-breakdown", query: "sum(tide_tokens_used_total) by (model)" }

// CORRECT (D-06 shapes; $__range is substituted by the range picker):
```

Replace the PANELS const with (D-06 locked shapes):
```tsx
const PANELS: PanelDef[] = [
  {
    id: "cost-over-time",
    label: "Cost Over Time",
    query: "sum(rate(tide_cost_cents_total[5m])) by (project)",
  },
  {
    id: "dispatch-counts",
    label: "Dispatch Counts",
    query: "sum(increase(tide_waves_dispatched_total[1h])) by (project) + sum(increase(tide_tasks_completed_total[1h])) by (project)",
  },
  {
    id: "failure-rate",
    label: "Failure Rate",
    query: "sum(rate(tide_tasks_failed_total[5m])) by (project) / (sum(rate(tide_tasks_failed_total[5m])) by (project) + sum(rate(tide_tasks_completed_total[5m])) by (project))",
  },
  {
    id: "token-breakdown",
    label: "Token Breakdown",
    query: "sum(increase(tide_tokens_input_total[1h])) by (project) + sum(increase(tide_tokens_output_total[1h])) by (project) + sum(increase(tide_tokens_cache_read_total[1h])) by (project) + sum(increase(tide_tokens_cache_creation_total[1h])) by (project)",
  },
];
```

**Fix 2 — replace Sparkline with recharts** (lines 167-205, current `Sparkline` component):

Replace `Sparkline` with a recharts `AreaChart` component. Import pattern:
```tsx
import { AreaChart, Area, XAxis, YAxis, Tooltip, ResponsiveContainer } from "recharts";
```

Prometheus matrix → recharts data transform (new helper):
```tsx
function matrixToPoints(series: PromMatrix[]): Record<string, number>[] {
    if (series.length === 0) return [];
    // Merge all series into [{time, <seriesLabel>: value, ...}] keyed by time
    const timeMap = new Map<number, Record<string, number>>();
    series.forEach((s) => {
        const label = Object.values(s.metric).join(",") || "value";
        s.values.forEach(([t, v]) => {
            const existing = timeMap.get(t) ?? { time: t };
            existing[label] = parseFloat(v);
            timeMap.set(t, existing);
        });
    });
    return Array.from(timeMap.values()).sort((a, b) => a.time - b.time);
}
```

Recharts component replacing `Sparkline` (use `height={180}` not `"100%"` — Pitfall 5):
```tsx
function TimeSeriesChart({ series }: { series: PromMatrix[] }) {
    const data = matrixToPoints(series);
    if (data.length === 0) {
        return (
            <p style={{ fontSize: "12px", fontFamily: "var(--font-mono)", color: "var(--color-text-muted)" }}>
                No data in range
            </p>
        );
    }
    const seriesKeys = series.map((s) => Object.values(s.metric).join(",") || "value");
    const COLORS = ["#6366f1", "#22c55e", "#f59e0b", "#ef4444"];
    return (
        <ResponsiveContainer width="100%" height={180}>
            <AreaChart data={data}>
                <XAxis
                    dataKey="time"
                    type="number"
                    domain={["dataMin", "dataMax"]}
                    scale="time"
                    tickFormatter={(v: number) => new Date(v * 1000).toLocaleTimeString()}
                    tick={{ fontSize: 10, fontFamily: "var(--font-mono)" }}
                />
                <YAxis tick={{ fontSize: 10, fontFamily: "var(--font-mono)" }} />
                <Tooltip
                    labelFormatter={(v: number) => new Date(v * 1000).toLocaleString()}
                />
                {seriesKeys.map((key, i) => (
                    <Area
                        key={key}
                        type="monotone"
                        dataKey={key}
                        stroke={COLORS[i % COLORS.length]}
                        fill={COLORS[i % COLORS.length]}
                        fillOpacity={0.15}
                        isAnimationActive={false}
                    />
                ))}
            </AreaChart>
        </ResponsiveContainer>
    );
}
```

In `ChartPanel`, replace `<Sparkline series={state.series} />` with `<TimeSeriesChart series={state.series} />`.

**Fix 3 — D-07 range selector + polling** (extend `TelemetryView` component):

Add `rangeHours` state and a selector above the budget card. Polling useEffect (wrap in `vi.useFakeTimers()` in tests — Pitfall 6):
```tsx
const [rangeHours, setRangeHours] = useState<24 | 168 | 720>(24);
// polling via a separate useEffect with setInterval, cleanup on unmount
```

---

### `dashboard/web/src/components/TelemetryView.test.tsx` (new test file)

**Analog:** `dashboard/web/src/lib/api.test.ts` (fetch stub pattern) + `dashboard/web/src/components/__tests__/dag-views.test.tsx` (render + waitFor pattern)

**File header pattern** (from api.test.ts):
```tsx
import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import TelemetryView from "./TelemetryView";
import type { BudgetSummary } from "../lib/api";

afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
});
```

**Fetch stub helpers** (exact pattern from api.test.ts lines 8-28):
```tsx
function stubFetchOK<T>(payload: T) {
    vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
            ok: true,
            status: 200,
            json: async () => payload,
        }) as unknown as typeof fetch,
    );
}

function stubFetchError(status = 502, body: unknown = { status: "error", message: "upstream unreachable" }) {
    vi.stubGlobal(
        "fetch",
        vi.fn().mockResolvedValue({
            ok: false,
            status,
            json: async () => body,
        }) as unknown as typeof fetch,
    );
}
```

**Default budget fixture**:
```tsx
const BUDGET: BudgetSummary = { capCents: 10000, currentSpend: 500, withinBudget: true };
```

**Shape 1 — 200 `unavailable` sentinel** (locked TELEM-02 requirement):
```tsx
describe("TelemetryView — degradation: 200 unavailable sentinel", () => {
    it("renders TelemetryUnavailableNotice in each panel slot", async () => {
        stubFetchOK({ status: "unavailable" });
        render(<TelemetryView projectName="p1" namespace="default" budget={BUDGET} />);
        await waitFor(() => {
            // All four panels should show the unavailable notice
            const notices = screen.getAllByTestId("telemetry-unavailable-notice");
            expect(notices.length).toBeGreaterThanOrEqual(1);
        });
    });
});
```

**Shape 2 — 502 error** (locked TELEM-02 requirement):
```tsx
describe("TelemetryView — degradation: 502 error", () => {
    it("renders TelemetryUnavailableNotice with unreachable wording", async () => {
        stubFetchError(502, { status: "error", message: "upstream unreachable" });
        render(<TelemetryView projectName="p1" namespace="default" budget={BUDGET} />);
        await waitFor(() => {
            const notices = screen.getAllByTestId("telemetry-unavailable-notice");
            expect(notices.length).toBeGreaterThanOrEqual(1);
        });
    });
});
```

**Timer handling for polling** — use `vi.useFakeTimers()` in beforeEach if the polling useEffect is present (dag-views.test.tsx lines 148-157 pattern):
```tsx
beforeEach(() => {
    vi.useFakeTimers();
});
afterEach(() => {
    vi.useRealTimers();
    // ... cleanup ...
});
// In tests: await act(async () => { await vi.advanceTimersByTimeAsync(0); });
```

---

### `Makefile` (config, new targets)

**Analog:** `helm-rbac-assert` target at lines 546-555

**Existing pattern** (lines 546-555):
```makefile
##@ Helm Chart Validation (Phase 4 plan 04-14 — D-X3 / T-04-D2)

.PHONY: helm-rbac-assert
helm-rbac-assert: ## Assert dashboard ClusterRole verbs are read-only {get, list, watch} (T-04-D2 mitigation).
	@helm template charts/tide --set dashboard.enabled=true > /tmp/tide-helm-render.yaml
	@python3 hack/helm/assert-dashboard-rbac.py /tmp/tide-helm-render.yaml
```

**New targets** (add immediately after the `helm-rbac-assert` block, before the `##@ Legal compliance gates` section at line 557):
```makefile
.PHONY: helm-telemetry-assert
helm-telemetry-assert: ## Assert PROM_ENDPOINT env injection and telemetry render gates (Phase 16 TELEM-05 D-13).
	@helm template charts/tide --set dashboard.enabled=true > /tmp/tide-helm-render.yaml
	@python3 hack/helm/assert-prometheus-env.py /tmp/tide-helm-render.yaml --expect-absent
	@helm template charts/tide --set dashboard.enabled=true --set prometheus.endpoint=http://prom:9090 > /tmp/tide-helm-render-prom.yaml
	@python3 hack/helm/assert-prometheus-env.py /tmp/tide-helm-render-prom.yaml --expect-endpoint http://prom:9090
	@bash hack/helm/assert-telemetry-render.sh

.PHONY: helm-assert
helm-assert: helm-rbac-assert helm-telemetry-assert ## Run all Helm render gate assertions (Phase 16 TELEM-05 D-13).
```

**Docstring fix** — `helm-rbac-assert` docstring currently claims it drives telemetry scripts (false). Update its comment to describe only what it actually does (RBAC check only), and credit `helm-assert` for the aggregate.

---

### `.github/workflows/ci.yaml` (config, add step)

**Analog:** existing `helm-lint` job steps at lines 141-193

**Existing step pattern** (lines 170-179):
```yaml
      - name: helm lint charts/tide
        run: helm lint charts/tide
```

**New step** (add after the "Verify chart tree is reproducible" step at line 192, before the job ends):
```yaml
      - name: Helm render gate assertions (TELEM-05 D-14)
        run: make helm-assert
```

Note: `helm` is already installed in the job (azure/setup-helm@v4 at line 159) and python3 is present on `ubuntu-latest`. No new setup steps are needed.

---

## Shared Patterns

### Non-Fatal Error Logging (applies to: task_controller.go emission)
**Source:** `internal/controller/task_controller.go` lines 941-943
```go
if err := setBudgetBlockedIfNeeded(ctx, r.Client, project, r.Deps.Reservations.TotalReserved()); err != nil {
    logger.Error(err, "setBudgetBlockedIfNeeded failed (non-fatal)", "task", task.Name)
}
```
Pattern: always log + always proceed. Never return the error up — task is already terminal.

### Metric Label Sentinel (applies to: resolveWave, emitTaskMetrics)
**Source:** RESEARCH.md Pitfall 4; `internal/metrics/registry_test.go` line 57
When a label cannot be resolved, use `"unknown"` as the sentinel value rather than returning an error that would skip emission entirely. The `__seed__` pattern from tests uses double-underscore prefix for test-only label values — never emit `"__seed__"` in production.

### Fetch Stub in Vitest (applies to: TelemetryView.test.tsx)
**Source:** `dashboard/web/src/lib/api.test.ts` lines 8-28
```tsx
vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
        ok: true,
        json: async () => payload,
    }) as unknown as typeof fetch,
);
```
Always pair with `vi.unstubAllGlobals()` in `afterEach`. The cast `as unknown as typeof fetch` is the established project idiom — never widen to `any`.

### data-testid Convention (applies to: new UI components)
**Source:** `TelemetryUnavailableNotice.tsx` line 28, `TelemetryView.tsx` lines 215 and 377
Format is `data-testid="<component-kebab-case>"` or `data-testid="panel-<id>"`. Tests use `getAllByTestId` for repeating elements (multiple panel notices) and `getByTestId` for singletons.

### CSS Variable Style Props (applies to: new UI components)
**Source:** `TelemetryView.tsx` lines 219-229, `TelemetryUnavailableNotice.tsx` lines 29-33
All colors reference CSS variables: `var(--color-border-subtle)`, `var(--color-surface-raised)`, `var(--color-text-muted)`, `var(--color-text-primary)`. Never hardcode hex colors for themed surfaces.

### Prometheus Registry Test Pattern (applies to: registry_test.go extension)
**Source:** `internal/metrics/registry_test.go` lines 54-97 + 130-141

For each new metric:
1. Seed with sentinel label `"__seed__"` for the `AllMetricFamiliesPresent` test
2. Add an individual arity test calling `WithLabelValues(project, phase, plan, wave)` — 4 args for all six new metrics
3. Extend `TestRegistry_DispatchLatencyBuckets`-style source-file check for the `taskDurationBuckets` slice

For the `NoTaskLabel` test (line 172): it does substring search for `"task"` in the file. Since `"wave"` will appear as a label name and `"task"` appears in the existing docs comments, verify the test passes — it looks for the literal label-slot string `"task"`, not the word in comments. New labels `"wave"` and `"plan"` are safe.

---

## No Analog Found

All files have close analogs. No entries in this section.

---

## Metadata

**Analog search scope:** `internal/metrics/`, `internal/controller/`, `cmd/dashboard/`, `dashboard/web/src/`, `Makefile`, `.github/workflows/`
**Files scanned:** 15 source files read directly
**Pattern extraction date:** 2026-06-12
