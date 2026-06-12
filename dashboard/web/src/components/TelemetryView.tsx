/*
 * TelemetryView.tsx (phase-16-telemetry-completion, plan 16-04)
 *
 * The Telemetry tab view, composed of:
 *   (a) A toolbar with scope toggle (D-02/D-04) and range selector (D-07).
 *   (b) A budget surface — single card in project scope, card grid in
 *       all-projects mode (D-03) — sourced from the projects prop (no Prometheus
 *       dependency; renders in ALL states including degraded panels).
 *   (c) Four chart-panel slots backed by the same-origin chi PromQL proxy
 *       (GET /api/v1/query_range) with real recharts time-series (D-05/D-06):
 *         — Cost Over Time
 *         — Dispatch Counts
 *         — Failure Rate
 *         — Token Breakdown (stacked)
 *
 * Query names are locked to internal/metrics/registry.go (TELEM-04).
 * All panel queries use only metric names registered in internal/metrics/registry.go.
 *
 * The proxy emits THREE response shapes (MILESTONE.md §Q1=PROXY):
 *   1. HTTP 200 + {status:"success", data:{resultType:"matrix", result:[…]}}
 *      → recharts time-series rendered in the panel slot.
 *   2. HTTP 200 + {status:"unavailable"}
 *      → TelemetryUnavailableNotice (generic "not configured" wording).
 *   3. HTTP non-2xx (502 etc.) + any body
 *      → TelemetryUnavailableNotice with "unreachable" wording.
 *
 * No Prometheus call ever causes an ErrorState/EmptyState takeover — those
 * are reserved for CRD-.status failures (out of scope here). See EC-6.
 *
 * Polling: all four panels re-fetch every 60s while visible (D-07). Range or
 * scope change triggers an immediate re-fetch and resets the interval.
 * Polling never resets panels to loading — updates are in-place.
 */
import { useEffect, useRef, useState } from "react";
import {
  Area,
  AreaChart,
  CartesianGrid,
  Legend,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import type { ProjectSummary as Project } from "../lib/api";
import TelemetryUnavailableNotice from "./TelemetryUnavailableNotice";

// ─── Prometheus wire types ────────────────────────────────────────────────────

type PromMetric = Record<string, string>;

export type PromMatrix = {
  metric: PromMetric;
  values: [number, string][];
};

type PromSuccessBody = {
  status: "success";
  data: {
    resultType: "matrix";
    result: PromMatrix[];
  };
};

type PromUnavailableBody = {
  status: "unavailable";
};

type PromErrorBody = {
  status: "error";
  message?: string;
};

type PromBody = PromSuccessBody | PromUnavailableBody | PromErrorBody;

// ─── Panel state ─────────────────────────────────────────────────────────────

type PanelState =
  | { kind: "loading" }
  | { kind: "data"; series: SeriesPoint[] }
  | { kind: "unavailable" }
  | { kind: "unreachable"; message: string };

// ─── Series types ────────────────────────────────────────────────────────────

/** A resolved series for rendering: key = legend label, data = matrix result. */
type ResolvedSeries = {
  key: string;
  matrix: PromMatrix;
};

/** A merged time-series data point keyed by series labels. */
type SeriesPoint = Record<string, number>;

// ─── Scope + range ───────────────────────────────────────────────────────────

type ScopeKind = "project" | "all";
type RangeKey = "24h" | "7d" | "30d";

const RANGE_CONFIG: Record<
  RangeKey,
  { durationSec: number; step: number; window: string }
> = {
  "24h": { durationSec: 86400, step: 300, window: "5m" },
  "7d": { durationSec: 604800, step: 1800, window: "30m" },
  "30d": { durationSec: 2592000, step: 7200, window: "2h" },
};

// ─── Panel definitions ───────────────────────────────────────────────────────

type SeriesDef = {
  key?: string; // fixed legend label; omitted → use metric.project from response
  buildQuery: (scope: ScopeKind, project: string, window: string) => string;
};

type PanelDef = {
  id: string;
  label: string;
  series: SeriesDef[];
  yFormatter: (v: number) => string;
  yDomain?: [number | string, number | string];
  stacked?: boolean;
  /** When true, single-series mode uses the failure-red color instead of palette-1 */
  failureColor?: boolean;
};

const PANELS: PanelDef[] = [
  {
    id: "cost-over-time",
    label: "Cost Over Time",
    yFormatter: (v: number) => formatCents(Math.round(v)),
    series: [
      {
        key: "cost",
        buildQuery: (scope, project, window) =>
          scope === "project"
            ? `sum(increase(tide_cost_cents_total{project="${project}"}[${window}]))`
            : `sum(increase(tide_cost_cents_total[${window}])) by (project)`,
      },
    ],
  },
  {
    id: "dispatch-counts",
    label: "Dispatch Counts",
    yFormatter: (v: number) => String(Math.round(v)),
    series: [
      {
        key: "waves dispatched",
        buildQuery: (scope, project, window) =>
          scope === "project"
            ? `sum(increase(tide_waves_dispatched_total{project="${project}"}[${window}]))`
            : `sum(increase(tide_waves_dispatched_total[${window}])) by (project)`,
      },
      {
        key: "tasks completed",
        buildQuery: (scope, project, window) =>
          scope === "project"
            ? `sum(increase(tide_tasks_completed_total{project="${project}"}[${window}]))`
            : `sum(increase(tide_tasks_completed_total[${window}])) by (project)`,
      },
    ],
  },
  {
    id: "failure-rate",
    label: "Failure Rate",
    yFormatter: (v: number) => `${(v * 100).toFixed(1)}%`,
    yDomain: [0, 1],
    failureColor: true,
    series: [
      {
        key: "failure rate",
        buildQuery: (scope, project, window) =>
          scope === "project"
            ? `sum(rate(tide_tasks_failed_total{project="${project}"}[${window}])) / (sum(rate(tide_tasks_failed_total{project="${project}"}[${window}])) + sum(rate(tide_tasks_completed_total{project="${project}"}[${window}])))`
            : `sum(rate(tide_tasks_failed_total[${window}])) by (project) / (sum(rate(tide_tasks_failed_total[${window}])) by (project) + sum(rate(tide_tasks_completed_total[${window}])) by (project))`,
      },
    ],
  },
  {
    id: "token-breakdown",
    label: "Token Breakdown",
    yFormatter: (v: number) =>
      v >= 1_000_000
        ? `${(v / 1_000_000).toFixed(1)}M`
        : v >= 1_000
          ? `${(v / 1_000).toFixed(1)}k`
          : String(Math.round(v)),
    stacked: true,
    series: [
      {
        key: "input",
        buildQuery: (scope, project, window) =>
          scope === "project"
            ? `sum(increase(tide_tokens_input_total{project="${project}"}[${window}]))`
            : `sum(increase(tide_tokens_input_total[${window}]))`,
      },
      {
        key: "output",
        buildQuery: (scope, project, window) =>
          scope === "project"
            ? `sum(increase(tide_tokens_output_total{project="${project}"}[${window}]))`
            : `sum(increase(tide_tokens_output_total[${window}]))`,
      },
      {
        key: "cache read",
        buildQuery: (scope, project, window) =>
          scope === "project"
            ? `sum(increase(tide_tokens_cache_read_total{project="${project}"}[${window}]))`
            : `sum(increase(tide_tokens_cache_read_total[${window}]))`,
      },
      {
        key: "cache creation",
        buildQuery: (scope, project, window) =>
          scope === "project"
            ? `sum(increase(tide_tokens_cache_creation_total{project="${project}"}[${window}]))`
            : `sum(increase(tide_tokens_cache_creation_total[${window}]))`,
      },
    ],
  },
];

// ─── Helpers ─────────────────────────────────────────────────────────────────

/** Format an integer cent value as a USD dollar string (e.g. 1250 → "$12.50"). */
function formatCents(cents: number): string {
  return "$" + (cents / 100).toFixed(2);
}

/**
 * Fetch a single PromQL range query from the same-origin proxy.
 * Returns a resolved PanelState — never throws (graceful degradation).
 */
async function fetchQueryRange(
  query: string,
  startSec: number,
  endSec: number,
  step: number,
): Promise<{ kind: "data"; result: PromMatrix[] } | { kind: "unavailable" } | { kind: "unreachable"; message: string }> {
  const params = new URLSearchParams({
    query,
    start: String(startSec),
    end: String(endSec),
    step: String(step),
  });

  let res: Response;
  try {
    res = await fetch(`/api/v1/query_range?${params.toString()}`);
  } catch {
    return {
      kind: "unreachable",
      message: "Prometheus endpoint is unreachable — network error",
    };
  }

  if (!res.ok) {
    let detail = "";
    try {
      const body = (await res.json()) as PromErrorBody;
      if (body.message) detail = ": " + body.message;
    } catch {
      // JSON parse failed; use generic wording.
    }
    return {
      kind: "unreachable",
      message: `Prometheus endpoint is unreachable (HTTP ${res.status})${detail}`,
    };
  }

  let body: PromBody;
  try {
    body = (await res.json()) as PromBody;
  } catch {
    return { kind: "unavailable" };
  }

  if (body.status === "success") {
    return { kind: "data", result: body.data.result };
  }

  if (body.status === "unavailable") {
    return { kind: "unavailable" };
  }

  // status === "error" on a 200 response — treat as unavailable.
  return { kind: "unavailable" };
}

/**
 * Fetch all series for a panel; merge results into a single PanelState.
 * Any unavailable/unreachable result wins for the whole panel.
 */
async function fetchPanel(
  panelDef: PanelDef,
  scope: ScopeKind,
  projectName: string,
  startSec: number,
  endSec: number,
  step: number,
  window: string,
): Promise<PanelState> {
  const seriesDefs = panelDef.series;

  const results = await Promise.all(
    seriesDefs.map((sd) =>
      fetchQueryRange(
        sd.buildQuery(scope, projectName, window),
        startSec,
        endSec,
        step,
      ),
    ),
  );

  // Any degradation wins for the whole panel.
  for (const r of results) {
    if (r.kind === "unreachable") return { kind: "unreachable", message: r.message };
  }
  for (const r of results) {
    if (r.kind === "unavailable") return { kind: "unavailable" };
  }

  // All data — merge series.
  const allSeries: ResolvedSeries[] = [];
  results.forEach((r, i) => {
    if (r.kind !== "data") return;
    const sd = seriesDefs[i];
    r.result.forEach((matrix) => {
      const key = sd.key ?? matrix.metric["project"] ?? Object.values(matrix.metric).join(",") ?? "value";
      allSeries.push({ key, matrix });
    });
  });

  return { kind: "data", series: matrixToPoints(allSeries) };
}

/**
 * Merge resolved series into [{time, <seriesKey>: number}] sorted by time.
 * Pattern 7 from 16-PATTERNS.md.
 */
function matrixToPoints(series: ResolvedSeries[]): SeriesPoint[] {
  if (series.length === 0) return [];
  const timeMap = new Map<number, SeriesPoint>();
  series.forEach(({ key, matrix }) => {
    matrix.values.forEach(([t, v]) => {
      const existing = timeMap.get(t) ?? { time: t };
      existing[key] = parseFloat(v);
      timeMap.set(t, existing);
    });
  });
  return Array.from(timeMap.values()).sort((a, b) => (a["time"] as number) - (b["time"] as number));
}

/** Get all series keys from a data PanelState's SeriesPoint array. */
function getSeriesKeys(points: SeriesPoint[]): string[] {
  const keys = new Set<string>();
  points.forEach((p) => {
    Object.keys(p).forEach((k) => {
      if (k !== "time") keys.add(k);
    });
  });
  return Array.from(keys);
}

// ─── Chart series palette (UI-SPEC §Color chart series palette) ───────────────

const SERIES_PALETTE = [
  "var(--color-status-running)",   // series-1: #06B6D4
  "var(--color-status-success)",   // series-2: #3FB950
  "var(--color-status-warning)",   // series-3: #D29922
  "var(--color-status-blocked)",   // series-4: #A371F7
  "var(--color-status-pending)",   // series-5: #8B949E
];
const FAILURE_COLOR = "var(--color-status-error)"; // #F85149

function seriesColor(index: number, failureMode: boolean): string {
  if (failureMode) return FAILURE_COLOR;
  return SERIES_PALETTE[index % SERIES_PALETTE.length];
}

// ─── Tick formatters ─────────────────────────────────────────────────────────

function tickFormatterFor(range: RangeKey) {
  return (v: number) => {
    const d = new Date(v * 1000);
    if (range === "24h") {
      return d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
    }
    return d.toLocaleDateString([], { month: "short", day: "numeric" });
  };
}

// ─── Sub-components ──────────────────────────────────────────────────────────

/**
 * TimeSeriesChart — recharts AreaChart per UI-SPEC C4.
 * Real recharts time-series per UI-SPEC C4.
 */
type TimeSeriesChartProps = {
  points: SeriesPoint[];
  panelDef: PanelDef;
  range: RangeKey;
};

function TimeSeriesChart({ points, panelDef, range }: TimeSeriesChartProps) {
  if (points.length === 0) {
    return (
      <div
        style={{
          height: "180px",
          display: "flex",
          alignItems: "center",
          justifyContent: "center",
        }}
      >
        <p
          style={{
            fontSize: "12px",
            fontFamily: "var(--font-mono)",
            color: "var(--color-text-muted)",
          }}
        >
          No data in range
        </p>
      </div>
    );
  }

  const keys = getSeriesKeys(points);
  const hasMultipleSeries = keys.length > 1;
  // Failure panels: single series → failure red; multi-series (all-projects) → palette
  const isSingleFailure = panelDef.failureColor && !hasMultipleSeries;

  const tickFmt = tickFormatterFor(range);

  return (
    <div aria-label={`${panelDef.label} chart`}>
      <ResponsiveContainer width="100%" height={180}>
        <AreaChart data={points}>
          <CartesianGrid
            strokeDasharray="3 3"
            stroke="var(--color-border-subtle)"
          />
          <XAxis
            dataKey="time"
            type="number"
            scale="time"
            domain={["dataMin", "dataMax"]}
            tickFormatter={tickFmt}
            tick={{
              fontSize: 12,
              fontFamily: "var(--font-mono)",
              fill: "var(--color-text-muted)",
            }}
            stroke="var(--color-border-subtle)"
          />
          <YAxis
            domain={panelDef.yDomain}
            tickFormatter={panelDef.yFormatter}
            tick={{
              fontSize: 12,
              fontFamily: "var(--font-mono)",
              fill: "var(--color-text-muted)",
            }}
            stroke="var(--color-border-subtle)"
            width={60}
          />
          <Tooltip
            formatter={(value: number) => panelDef.yFormatter(value)}
            labelFormatter={tickFmt}
            contentStyle={{
              background: "var(--color-surface-overlay)",
              border: "1px solid var(--color-border-subtle)",
              borderRadius: "4px",
              fontSize: "12px",
              fontFamily: "var(--font-mono)",
            }}
          />
          {hasMultipleSeries && (
            <Legend
              wrapperStyle={{
                fontSize: "12px",
                fontFamily: "var(--font-mono)",
                color: "var(--color-text-muted)",
              }}
            />
          )}
          {keys.map((key, i) => {
            const color = seriesColor(i, isSingleFailure);
            return (
              <Area
                key={key}
                type="monotone"
                dataKey={key}
                stroke={color}
                fill={color}
                fillOpacity={panelDef.stacked ? 0.35 : 0.2}
                strokeWidth={2}
                isAnimationActive={false}
                stackId={panelDef.stacked ? "stack" : undefined}
              />
            );
          })}
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}

type ChartPanelProps = {
  def: PanelDef;
  state: PanelState;
  range: RangeKey;
};

function ChartPanel({ def, state, range }: ChartPanelProps) {
  return (
    <div
      data-testid={`panel-${def.id}`}
      className="flex flex-col gap-2 rounded border p-4"
      style={{
        borderColor: "var(--color-border-subtle)",
        background: "var(--color-surface-raised)",
      }}
    >
      <h3
        style={{
          fontSize: "12px",
          fontWeight: 600,
          fontFamily: "var(--font-mono)",
          color: "var(--color-text-muted)",
          textTransform: "uppercase",
          letterSpacing: "0.05em",
          margin: 0,
        }}
      >
        {def.label}
      </h3>

      {state.kind === "loading" && (
        <div
          style={{
            minHeight: "80px",
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            color: "var(--color-text-muted)",
            fontFamily: "var(--font-mono)",
            fontSize: "12px",
          }}
        >
          Loading…
        </div>
      )}

      {state.kind === "data" && (
        <TimeSeriesChart points={state.series} panelDef={def} range={range} />
      )}

      {state.kind === "unavailable" && <TelemetryUnavailableNotice />}

      {state.kind === "unreachable" && (
        <TelemetryUnavailableNotice message={state.message} />
      )}
    </div>
  );
}

// ─── Budget card ─────────────────────────────────────────────────────────────

type BudgetCardProps = {
  project?: Project;         // full project with budget; if omitted → no-project fallback
  projectName?: string;      // name shown in all-projects grid
  showName?: boolean;        // render project name as first line
  testId?: string;           // data-testid override for grid cards
};

function BudgetCard({ project, projectName, showName = false, testId = "budget-card" }: BudgetCardProps) {
  const budget = project?.budget;
  const name = projectName ?? project?.name ?? "";

  const noBudget = !budget || budget.capCents <= 0;

  return (
    <div
      data-testid={testId}
      className="flex flex-col gap-1 rounded border p-4"
      style={{
        borderColor: "var(--color-border-subtle)",
        background: "var(--color-surface-raised)",
      }}
    >
      {showName && (
        <div
          style={{
            fontSize: "13px",
            fontFamily: "var(--font-mono)",
            color: "var(--color-text-primary)",
            marginBottom: "4px",
          }}
        >
          {name}
        </div>
      )}
      <h3
        style={{
          fontSize: "12px",
          fontWeight: 600,
          fontFamily: "var(--font-mono)",
          color: "var(--color-text-muted)",
          textTransform: "uppercase",
          letterSpacing: "0.05em",
          margin: 0,
        }}
      >
        Budget
      </h3>

      {noBudget ? (
        <p
          style={{
            fontSize: "12px",
            fontFamily: "var(--font-mono)",
            color: "var(--color-text-muted)",
          }}
        >
          No budget configured
        </p>
      ) : (
        <>
          {/* Metric figure: 20px/600/mono per UI-SPEC §Typography (was 24px/700 — compliance fix) */}
          <div
            style={{
              fontSize: "20px",
              fontWeight: 600,
              fontFamily: "var(--font-mono)",
              color: "var(--color-text-primary)",
            }}
          >
            {formatCents(budget.currentSpend)}
          </div>
          <div
            style={{
              fontSize: "13px",
              fontFamily: "var(--font-mono)",
              color: "var(--color-text-muted)",
            }}
          >
            of {formatCents(budget.capCents)} cap
          </div>
          <div
            data-testid="budget-status"
            style={{
              fontSize: "12px",
              fontWeight: 600,
              fontFamily: "var(--font-mono)",
              color: budget.withinBudget
                ? "var(--color-status-success)"
                : "var(--color-destructive)",
              marginTop: "4px",
            }}
          >
            {budget.withinBudget ? "Within budget" : "Over budget"}
          </div>
        </>
      )}
    </div>
  );
}

// ─── Segmented control ────────────────────────────────────────────────────────

type SegmentedControlProps<T extends string> = {
  options: Array<{ value: T; label: string; mono?: boolean }>;
  value: T;
  onChange: (v: T) => void;
  testId: string;
  role?: "group" | undefined;
  useAriaPressed?: boolean;
};

function SegmentedControl<T extends string>({
  options,
  value,
  onChange,
  testId,
  useAriaPressed = true,
}: SegmentedControlProps<T>) {
  return (
    <div
      data-testid={testId}
      style={{
        display: "inline-flex",
        borderRadius: "4px",
        border: "1px solid var(--color-border-subtle)",
      }}
    >
      {options.map((opt) => {
        const isActive = opt.value === value;
        return (
          <button
            key={opt.value}
            type="button"
            aria-pressed={useAriaPressed ? isActive : undefined}
            onClick={() => onChange(opt.value)}
            style={{
              padding: "4px 8px",
              fontSize: "12px",
              fontWeight: 600,
              fontFamily: opt.mono ? "var(--font-mono)" : "var(--font-sans)",
              cursor: "pointer",
              border: "none",
              borderRadius: "3px",
              background: isActive ? "var(--color-surface-overlay)" : "transparent",
              color: isActive ? "var(--color-text-primary)" : "var(--color-text-muted)",
            }}
          >
            {opt.label}
          </button>
        );
      })}
    </div>
  );
}

// ─── TelemetryView ───────────────────────────────────────────────────────────

export type TelemetryViewProps = {
  /** Full projects list (includes per-project budget) — from App.tsx's existing fetch. */
  projects: Project[];
  /** ProjectPicker selection; null → all-projects mode (D-04). */
  selectedProject: string | null;
};

export default function TelemetryView({
  projects,
  selectedProject,
}: TelemetryViewProps) {
  // D-04: scope derives from selectedProject; transient, never persisted.
  const [scope, setScope] = useState<ScopeKind>(
    selectedProject != null ? "project" : "all",
  );
  // Re-derive scope when selectedProject changes (picker change while mounted).
  useEffect(() => {
    setScope(selectedProject != null ? "project" : "all");
  }, [selectedProject]);

  // D-07: 24h/7d/30d range selector, default 24h.
  const [range, setRange] = useState<RangeKey>("24h");

  // Panel states — never reset to loading on polling (update in place).
  const [panelStates, setPanelStates] = useState<PanelState[]>(
    PANELS.map(() => ({ kind: "loading" })),
  );

  // Track the current scope/range/project in a ref so the polling callback
  // always reads the latest value without needing to re-register the interval.
  const scopeRef = useRef(scope);
  const rangeRef = useRef(range);
  const projectRef = useRef(selectedProject);
  useEffect(() => { scopeRef.current = scope; }, [scope]);
  useEffect(() => { rangeRef.current = range; }, [range]);
  useEffect(() => { projectRef.current = selectedProject; }, [selectedProject]);

  // Fetch all panels using current refs.
  const fetchAllPanels = () => {
    const nowSec = Math.floor(Date.now() / 1000);
    const cfg = RANGE_CONFIG[rangeRef.current];
    const startSec = nowSec - cfg.durationSec;
    const currentScope = scopeRef.current;
    const currentProject = projectRef.current ?? "";

    PANELS.forEach((panelDef, idx) => {
      void fetchPanel(
        panelDef,
        currentScope,
        currentProject,
        startSec,
        nowSec,
        cfg.step,
        cfg.window,
      ).then((state) => {
        setPanelStates((prev) => {
          const next = [...prev];
          next[idx] = state;
          return next;
        });
      });
    });
  };

  // D-07 polling: 60s interval, paused when document hidden, immediate on
  // scope/range change. Single effect re-registered on scope or range changes.
  useEffect(() => {
    // Immediate fetch on mount or scope/range change.
    fetchAllPanels();

    const startInterval = () =>
      setInterval(() => {
        if (document.visibilityState === "visible") {
          fetchAllPanels();
        }
      }, 60_000);

    let intervalId = startInterval();

    const onVisibilityChange = () => {
      if (document.visibilityState === "visible") {
        fetchAllPanels();
        clearInterval(intervalId);
        intervalId = startInterval();
      }
    };

    document.addEventListener("visibilitychange", onVisibilityChange);

    return () => {
      clearInterval(intervalId);
      document.removeEventListener("visibilitychange", onVisibilityChange);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scope, range]);

  const selectedProjectData = projects.find((p) => p.name === selectedProject) ?? null;

  const rangeOptions: Array<{ value: RangeKey; label: string }> = [
    { value: "24h", label: "24h" },
    { value: "7d", label: "7d" },
    { value: "30d", label: "30d" },
  ];

  const scopeOptions: Array<{ value: ScopeKind; label: string; mono?: boolean }> = [];
  if (selectedProject != null) {
    scopeOptions.push({ value: "project", label: selectedProject, mono: true });
  }
  scopeOptions.push({ value: "all", label: "All projects" });

  return (
    <div
      data-testid="telemetry-view"
      className="flex flex-col gap-4 p-4"
      style={{ height: "100%", overflow: "auto" }}
    >
      {/* Toolbar: scope toggle (left) + range selector (right) — UI-SPEC C3 */}
      <div className="flex items-center justify-between">
        <SegmentedControl<ScopeKind>
          options={scopeOptions}
          value={scope}
          onChange={setScope}
          testId="telemetry-scope-toggle"
        />
        <SegmentedControl<RangeKey>
          options={rangeOptions}
          value={range}
          onChange={setRange}
          testId="telemetry-range-selector"
        />
      </div>

      {/* Budget surface — always rendered, no Prometheus dependency (D-03) */}
      {scope === "project" ? (
        /* Single budget card for the selected project */
        selectedProjectData != null ? (
          <BudgetCard project={selectedProjectData} />
        ) : null
      ) : (
        /* All-projects budget grid — UI-SPEC C5 */
        projects.length === 0 ? (
          <div
            style={{
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              padding: "16px",
            }}
          >
            <p
              style={{
                fontSize: "12px",
                fontFamily: "var(--font-mono)",
                color: "var(--color-text-muted)",
              }}
            >
              No projects in this cluster
            </p>
          </div>
        ) : (
          <div
            data-testid="budget-card-grid"
            style={{
              display: "grid",
              gridTemplateColumns: "repeat(auto-fill, minmax(240px, 1fr))",
              gap: "16px",
            }}
          >
            {projects.map((p) => (
              <BudgetCard
                key={p.name}
                project={p}
                showName
                testId={`budget-card-${p.name}`}
              />
            ))}
          </div>
        )
      )}

      {/* Chart panels — Prometheus-backed (UI-SPEC C4) */}
      <div
        data-testid="telemetry-panels"
        className="grid grid-cols-2 gap-4"
      >
        {PANELS.map((def, idx) => (
          <ChartPanel key={def.id} def={def} state={panelStates[idx]} range={range} />
        ))}
      </div>
    </div>
  );
}
