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
import TelemetryDisabledBanner from "./TelemetryDisabledBanner";
import type { TelemetryDisabledBannerState } from "./TelemetryDisabledBanner";
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
type BreakdownKind = "none" | "phase" | "plan" | "wave";

const RANGE_CONFIG: Record<
  RangeKey,
  { durationSec: number; step: number; window: string }
> = {
  "24h": { durationSec: 86400, step: 300, window: "5m" },
  "7d": { durationSec: 604800, step: 1800, window: "30m" },
  "30d": { durationSec: 2592000, step: 7200, window: "2h" },
};

// ─── Panel definitions ───────────────────────────────────────────────────────
//
// Breakdown dimension support (D-06): when breakdown !== "none", all buildQuery
// functions switch to `sum by(<dim>)(...)` aggregation. Valid dimensions:
//   by(phase)  · by(plan)  · by(wave)
// These are TypeScript literal strings, not user input (T-21-02-01).

type SeriesDef = {
  /**
   * Fixed legend label used in project scope and for non-grouped queries
   * (e.g. Token Breakdown cluster sums that carry no `metric.project` label).
   * In all-projects scope on `by (project)` queries the merge step derives
   * per-project keys from `matrix.metric["project"]`, suffixing with this
   * label when the panel has multiple SeriesDefs to prevent key collisions.
   */
  key?: string;
  buildQuery: (scope: ScopeKind, project: string, window: string, breakdown?: BreakdownKind) => string;
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
        buildQuery: (scope, project, window, breakdown = "none") =>
          breakdown !== "none"
            ? `sum by(${breakdown})(increase(tide_cost_cents_total{project="${project}"}[${window}]))`
            : scope === "project"
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
        buildQuery: (scope, project, window, breakdown = "none") =>
          breakdown !== "none"
            ? `sum by(${breakdown})(increase(tide_waves_dispatched_total{project="${project}"}[${window}]))`
            : scope === "project"
              ? `sum(increase(tide_waves_dispatched_total{project="${project}"}[${window}]))`
              : `sum(increase(tide_waves_dispatched_total[${window}])) by (project)`,
      },
      {
        key: "tasks completed",
        buildQuery: (scope, project, window, breakdown = "none") =>
          breakdown !== "none"
            ? `sum by(${breakdown})(increase(tide_tasks_completed_total{project="${project}"}[${window}]))`
            : scope === "project"
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
        buildQuery: (scope, project, window, breakdown = "none") =>
          breakdown !== "none"
            ? `sum by(${breakdown})(rate(tide_tasks_failed_total{project="${project}"}[${window}])) / (sum by(${breakdown})(rate(tide_tasks_failed_total{project="${project}"}[${window}])) + sum by(${breakdown})(rate(tide_tasks_completed_total{project="${project}"}[${window}])))`
            : scope === "project"
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
        buildQuery: (scope, project, window, breakdown = "none") =>
          breakdown !== "none"
            ? `sum by(${breakdown})(increase(tide_tokens_input_total{project="${project}"}[${window}]))`
            : scope === "project"
              ? `sum(increase(tide_tokens_input_total{project="${project}"}[${window}]))`
              : `sum(increase(tide_tokens_input_total[${window}]))`,
      },
      {
        key: "output",
        buildQuery: (scope, project, window, breakdown = "none") =>
          breakdown !== "none"
            ? `sum by(${breakdown})(increase(tide_tokens_output_total{project="${project}"}[${window}]))`
            : scope === "project"
              ? `sum(increase(tide_tokens_output_total{project="${project}"}[${window}]))`
              : `sum(increase(tide_tokens_output_total[${window}]))`,
      },
      {
        key: "cache read",
        buildQuery: (scope, project, window, breakdown = "none") =>
          breakdown !== "none"
            ? `sum by(${breakdown})(increase(tide_tokens_cache_read_total{project="${project}"}[${window}]))`
            : scope === "project"
              ? `sum(increase(tide_tokens_cache_read_total{project="${project}"}[${window}]))`
              : `sum(increase(tide_tokens_cache_read_total[${window}]))`,
      },
      {
        key: "cache creation",
        buildQuery: (scope, project, window, breakdown = "none") =>
          breakdown !== "none"
            ? `sum by(${breakdown})(increase(tide_tokens_cache_creation_total{project="${project}"}[${window}]))`
            : scope === "project"
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
  breakdown: BreakdownKind = "none",
): Promise<PanelState> {
  const seriesDefs = panelDef.series;

  const results = await Promise.all(
    seriesDefs.map((sd) =>
      fetchQueryRange(
        sd.buildQuery(scope, projectName, window, breakdown),
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
  // Key derivation is scope/breakdown-aware:
  //   breakdown active + metric.<dim> present → key by breakdown label
  //   all-projects + metric.project present → key by project label (suffixed
  //     with sd.key when the panel has multiple SeriesDefs to avoid collisions).
  //   project scope or no project label → use the fixed sd.key (fallback "value").
  const allSeries: ResolvedSeries[] = [];
  results.forEach((r, i) => {
    if (r.kind !== "data") return;
    const sd = seriesDefs[i];
    r.result.forEach((matrix) => {
      let key: string;
      if (breakdown !== "none") {
        const dimLabel = matrix.metric[breakdown];
        key = dimLabel ? (seriesDefs.length > 1 ? `${sd.key} (${dimLabel})` : dimLabel) : (sd.key ?? "value");
      } else if (scope === "all") {
        const projectLabel = matrix.metric["project"];
        if (projectLabel) {
          key = seriesDefs.length > 1
            ? `${sd.key} (${projectLabel})`
            : projectLabel;
        } else {
          key = sd.key ?? "value";
        }
      } else {
        key = sd.key ?? matrix.metric["project"] ?? "value";
      }
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
  height?: number;
};

function TimeSeriesChart({ points, panelDef, range, height = 180 }: TimeSeriesChartProps) {
  if (points.length === 0) {
    return (
      <div
        style={{
          height: `${height}px`,
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
  const isSingleFailure = (panelDef.failureColor === true) && !hasMultipleSeries;

  const tickFmt = tickFormatterFor(range);

  return (
    <div aria-label={`${panelDef.label} chart`}>
      <ResponsiveContainer width="100%" height={height}>
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
            formatter={(value) => panelDef.yFormatter(typeof value === "number" ? value : 0)}
            labelFormatter={(label) => (typeof label === "number" ? tickFmt(label) : String(label))}
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

// ─── Cache efficiency panel ───────────────────────────────────────────────────

type CacheEfficiencyPanelProps = {
  scope: ScopeKind;
  project: string;
  window: string;
  step: number;
  startSec: number;
  endSec: number;
  range: RangeKey;
  breakdown: BreakdownKind;
};

/** Format a token count as abbreviated string matching Token Breakdown yFormatter. */
function formatTokenCount(v: number): string {
  if (v >= 1_000_000) return `${(v / 1_000_000).toFixed(1)}M`;
  if (v >= 1_000) return `${(v / 1_000).toFixed(1)}k`;
  return String(Math.round(v));
}

function CacheEfficiencyPanel({
  scope,
  project,
  window: windowStr,
  step,
  startSec,
  endSec,
  range,
  breakdown,
}: CacheEfficiencyPanelProps) {
  type CacheState =
    | { kind: "loading" }
    | { kind: "unavailable" }
    | { kind: "unreachable"; message: string }
    | { kind: "data"; hitRatio: number | null; creationTokens: number | null; savingsCents: number | null; sparkPoints: SeriesPoint[] };

  const [cacheState, setCacheState] = useState<CacheState>({ kind: "loading" });

  const scopeRef = useRef(scope);
  const projectRef = useRef(project);
  const windowRef = useRef(windowStr);
  const stepRef = useRef(step);
  const startSecRef = useRef(startSec);
  const endSecRef = useRef(endSec);
  const breakdownRef = useRef(breakdown);
  useEffect(() => { scopeRef.current = scope; }, [scope]);
  useEffect(() => { projectRef.current = project; }, [project]);
  useEffect(() => { windowRef.current = windowStr; }, [windowStr]);
  useEffect(() => { stepRef.current = step; }, [step]);
  useEffect(() => { startSecRef.current = startSec; }, [startSec]);
  useEffect(() => { endSecRef.current = endSec; }, [endSec]);
  useEffect(() => { breakdownRef.current = breakdown; }, [breakdown]);

  const fetchCache = () => {
    const w = windowRef.current;
    const p = projectRef.current;
    const bd = breakdownRef.current;
    const s = startSecRef.current;
    const e = endSecRef.current;
    const st = stepRef.current;

    const hitRatioQuery =
      bd !== "none"
        ? `sum by(${bd})(increase(tide_tokens_cache_read_total{project="${p}"}[${w}])) / (sum by(${bd})(increase(tide_tokens_cache_read_total{project="${p}"}[${w}])) + sum by(${bd})(increase(tide_tokens_cache_creation_total{project="${p}"}[${w}])))`
        : `sum(increase(tide_tokens_cache_read_total{project="${p}"}[${w}])) / (sum(increase(tide_tokens_cache_read_total{project="${p}"}[${w}])) + sum(increase(tide_tokens_cache_creation_total{project="${p}"}[${w}])))`;

    const creationQuery =
      bd !== "none"
        ? `sum by(${bd})(increase(tide_tokens_cache_creation_total{project="${p}"}[${w}]))`
        : `sum(increase(tide_tokens_cache_creation_total{project="${p}"}[${w}]))`;

    const savingsQuery =
      bd !== "none"
        ? `sum by(${bd})(increase(tide_cache_savings_cents_total{project="${p}"}[${w}]))`
        : `sum(increase(tide_cache_savings_cents_total{project="${p}"}[${w}]))`;

    Promise.all([
      fetchQueryRange(hitRatioQuery, s, e, st),
      fetchQueryRange(creationQuery, s, e, st),
      fetchQueryRange(savingsQuery, s, e, st),
    ]).then(([hitRes, creationRes, savingsRes]) => {
      // Any degradation wins.
      for (const r of [hitRes, creationRes, savingsRes]) {
        if (r.kind === "unreachable") {
          setCacheState({ kind: "unreachable", message: r.message });
          return;
        }
      }
      for (const r of [hitRes, creationRes, savingsRes]) {
        if (r.kind === "unavailable") {
          setCacheState({ kind: "unavailable" });
          return;
        }
      }
      if (hitRes.kind !== "data" || creationRes.kind !== "data" || savingsRes.kind !== "data") {
        setCacheState({ kind: "unavailable" });
        return;
      }

      // Extract scalar values from the first series result (last data point).
      const getScalar = (result: PromMatrix[]): number | null => {
        if (result.length === 0) return null;
        const vals = result[0].values;
        if (vals.length === 0) return null;
        return parseFloat(vals[vals.length - 1][1]);
      };

      const hitRatio = getScalar(hitRes.result);
      const creationTokens = getScalar(creationRes.result);
      const savingsCents = getScalar(savingsRes.result);

      // Build sparkline points from hit-ratio series.
      const sparkSeries: ResolvedSeries[] = hitRes.result.map((matrix) => ({
        key: "hit-ratio",
        matrix,
      }));
      const sparkPoints = matrixToPoints(sparkSeries);

      setCacheState({ kind: "data", hitRatio, creationTokens, savingsCents, sparkPoints });
    });
  };

  useEffect(() => {
    fetchCache();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [scope, project, windowStr, step, startSec, endSec, breakdown]);

  // Hit-ratio display: NaN (0/0 in PromQL) renders as "—"
  const hitRatioDisplay = (v: number | null): string => {
    if (v === null) return "—";
    if (!Number.isFinite(v) || isNaN(v)) return "—";
    return `${(v * 100).toFixed(1)}%`;
  };

  // Minimal PanelDef for the sparkline (hit-ratio, no failure color, no stacking)
  const sparklinePanelDef: PanelDef = {
    id: "cache-efficiency-sparkline",
    label: "Cache Efficiency",
    yFormatter: (v: number) => `${(v * 100).toFixed(1)}%`,
    series: [],
  };

  return (
    <div
      data-testid="panel-cache-efficiency"
      className="flex flex-col gap-1 rounded border p-4"
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
        Cache Efficiency
      </h3>

      {cacheState.kind === "loading" && (
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

      {cacheState.kind === "unavailable" && <TelemetryUnavailableNotice />}

      {cacheState.kind === "unreachable" && (
        <TelemetryUnavailableNotice message={cacheState.message} />
      )}

      {cacheState.kind === "data" && (
        <>
          {/* Trio: three stat figures */}
          <div style={{ display: "flex", gap: "16px", flexWrap: "wrap" }}>
            {/* Hit ratio */}
            <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
              <div
                style={{
                  fontSize: "20px",
                  fontWeight: 600,
                  fontFamily: "var(--font-mono)",
                  color: "var(--color-text-primary)",
                }}
              >
                {hitRatioDisplay(cacheState.hitRatio)}
              </div>
              <div
                style={{
                  fontSize: "12px",
                  fontWeight: 600,
                  fontFamily: "var(--font-mono)",
                  color: "var(--color-text-muted)",
                  textTransform: "uppercase",
                  letterSpacing: "0.05em",
                }}
              >
                hit
              </div>
            </div>
            {/* Cache creation tokens */}
            <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
              <div
                style={{
                  fontSize: "20px",
                  fontWeight: 600,
                  fontFamily: "var(--font-mono)",
                  color: "var(--color-text-primary)",
                }}
              >
                {cacheState.creationTokens !== null ? formatTokenCount(cacheState.creationTokens) : "—"}
              </div>
              <div
                style={{
                  fontSize: "12px",
                  fontWeight: 600,
                  fontFamily: "var(--font-mono)",
                  color: "var(--color-text-muted)",
                  textTransform: "uppercase",
                  letterSpacing: "0.05em",
                }}
              >
                creation
              </div>
            </div>
            {/* Realized savings $ */}
            <div style={{ display: "flex", flexDirection: "column", gap: "4px" }}>
              <div
                style={{
                  fontSize: "20px",
                  fontWeight: 600,
                  fontFamily: "var(--font-mono)",
                  color: "var(--color-text-primary)",
                }}
              >
                {cacheState.savingsCents !== null ? formatCents(Math.round(cacheState.savingsCents)) : "—"}
              </div>
              <div
                style={{
                  fontSize: "12px",
                  fontWeight: 600,
                  fontFamily: "var(--font-mono)",
                  color: "var(--color-text-muted)",
                  textTransform: "uppercase",
                  letterSpacing: "0.05em",
                }}
              >
                saved
              </div>
            </div>
          </div>

          {/* Sparkline: hit-ratio over time at 48px height */}
          {cacheState.sparkPoints.length > 0 && (
            <div>
              <TimeSeriesChart
                points={cacheState.sparkPoints}
                panelDef={sparklinePanelDef}
                range={range}
                height={48}
              />
              <div
                style={{
                  fontSize: "12px",
                  fontFamily: "var(--font-mono)",
                  color: "var(--color-text-muted)",
                  marginTop: "4px",
                }}
              >
                hit-rate over time
              </div>
            </div>
          )}
        </>
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

  // D-06: per-level breakdown selector, default "none" (= Project, no breakdown).
  const [breakdown, setBreakdown] = useState<BreakdownKind>("none");

  // Panel states — never reset to loading on polling (update in place).
  const [panelStates, setPanelStates] = useState<PanelState[]>(
    PANELS.map(() => ({ kind: "loading" })),
  );

  // TELEM-03 (plan 38-05, D-14): one-shot fetch of GET /api/v1/config for
  // the chart's prometheus.enabled toggle. null = unknown (fetch failed or
  // pending) — the banner derivation then falls back to the panel
  // unavailable-sentinel signal alone.
  const [telemetryEnabled, setTelemetryEnabled] = useState<boolean | null>(null);
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const res = await fetch("/api/v1/config");
        if (!res.ok) return;
        const body = (await res.json()) as { telemetryEnabled?: unknown };
        if (!cancelled && typeof body.telemetryEnabled === "boolean") {
          setTelemetryEnabled(body.telemetryEnabled);
        }
      } catch {
        // Config surface unreachable — stay null (unknown).
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // Track the current scope/range/project/breakdown in a ref so the polling callback
  // always reads the latest value without needing to re-register the interval.
  const scopeRef = useRef(scope);
  const rangeRef = useRef(range);
  const projectRef = useRef(selectedProject);
  const breakdownRef = useRef(breakdown);
  useEffect(() => { scopeRef.current = scope; }, [scope]);
  useEffect(() => { rangeRef.current = range; }, [range]);
  useEffect(() => { projectRef.current = selectedProject; }, [selectedProject]);
  useEffect(() => { breakdownRef.current = breakdown; }, [breakdown]);

  // Fetch all panels using current refs.
  const fetchAllPanels = () => {
    const nowSec = Math.floor(Date.now() / 1000);
    const cfg = RANGE_CONFIG[rangeRef.current];
    const startSec = nowSec - cfg.durationSec;
    const currentScope = scopeRef.current;
    const currentProject = projectRef.current ?? "";
    const currentBreakdown = breakdownRef.current;

    PANELS.forEach((panelDef, idx) => {
      void fetchPanel(
        panelDef,
        currentScope,
        currentProject,
        startSec,
        nowSec,
        cfg.step,
        cfg.window,
        currentBreakdown,
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
  }, [scope, range, breakdown]);

  const selectedProjectData = projects.find((p) => p.name === selectedProject) ?? null;

  // Compute time window for CacheEfficiencyPanel (mirrors fetchAllPanels logic).
  const nowSec = Math.floor(Date.now() / 1000);
  const rangeCfg = RANGE_CONFIG[range];
  const cacheStartSec = nowSec - rangeCfg.durationSec;

  const rangeOptions: Array<{ value: RangeKey; label: string }> = [
    { value: "24h", label: "24h" },
    { value: "7d", label: "7d" },
    { value: "30d", label: "30d" },
  ];

  const levelOptions: Array<{ value: BreakdownKind; label: string; mono?: boolean }> = [
    { value: "none", label: "Project" },
    { value: "phase", label: "Phase", mono: true },
    { value: "plan", label: "Plan", mono: true },
    { value: "wave", label: "Wave", mono: true },
  ];

  const scopeOptions: Array<{ value: ScopeKind; label: string; mono?: boolean }> = [];
  if (selectedProject != null) {
    scopeOptions.push({ value: "project", label: selectedProject, mono: true });
  }
  scopeOptions.push({ value: "all", label: "All projects" });

  // TELEM-03 banner derivation — UI-SPEC Banner Contract precedence.
  // Any panel with real data suppresses the banner outright (T-38-15: never
  // claim "disabled" while data flows); then disabled-by-config when the
  // config surface says false OR (defensively) every panel resolved the
  // unavailable sentinel; then no-data when telemetry is confirmed enabled
  // and every panel answered with zero points; hidden otherwise (loading /
  // unreachable — the per-panel TelemetryUnavailableNotice owns connectivity).
  const anyPanelData = panelStates.some(
    (s) => s.kind === "data" && s.series.length > 0,
  );
  const allPanelsUnavailable = panelStates.every(
    (s) => s.kind === "unavailable",
  );
  const allPanelsEmptyData = panelStates.every(
    (s) => s.kind === "data" && s.series.length === 0,
  );
  let bannerState: TelemetryDisabledBannerState | null = null;
  if (!anyPanelData) {
    if (telemetryEnabled === false || allPanelsUnavailable) {
      bannerState = "disabled-by-config";
    } else if (telemetryEnabled === true && allPanelsEmptyData) {
      bannerState = "no-data";
    }
  }

  return (
    <div
      data-testid="telemetry-view"
      className="flex flex-col gap-4 p-4"
      style={{ height: "100%", overflow: "auto" }}
    >
      {/* TELEM-03 view-level banner — first child, above the toolbar */}
      {bannerState !== null && <TelemetryDisabledBanner state={bannerState} />}
      {/* Toolbar: scope+level (left cluster) + range selector (right) — UI-SPEC C3, D-06 */}
      <div className="flex items-center justify-between">
        {/* Left cluster: scope toggle + level breakdown selector */}
        <div className="flex gap-2">
          {/* Scope toggle — C1 segmented control, aria-pressed (D-02/D-04) */}
          <div
            data-testid="telemetry-scope-toggle"
            style={{
              display: "inline-flex",
              borderRadius: "4px",
              border: "1px solid var(--color-border-subtle)",
            }}
          >
            {scopeOptions.map((opt) => {
              const isActive = opt.value === scope;
              return (
                <button
                  key={opt.value}
                  type="button"
                  aria-pressed={isActive}
                  onClick={() => setScope(opt.value)}
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
          {/* Per-level breakdown selector (D-06) */}
          <SegmentedControl
            options={levelOptions}
            value={breakdown}
            onChange={setBreakdown}
            testId="telemetry-level-selector"
          />
        </div>
        {/* Range selector — C1 segmented control, aria-pressed (D-07) */}
        <div
          data-testid="telemetry-range-selector"
          style={{
            display: "inline-flex",
            borderRadius: "4px",
            border: "1px solid var(--color-border-subtle)",
          }}
        >
          {rangeOptions.map((opt) => {
            const isActive = opt.value === range;
            return (
              <button
                key={opt.value}
                type="button"
                aria-pressed={isActive}
                onClick={() => setRange(opt.value)}
                style={{
                  padding: "4px 8px",
                  fontSize: "12px",
                  fontWeight: 600,
                  fontFamily: "var(--font-sans)",
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
        {/* Cache-efficiency panel (D-05, OBSV-03) — stat trio + sparkline */}
        <CacheEfficiencyPanel
          scope={scope}
          project={selectedProject ?? ""}
          window={rangeCfg.window}
          step={rangeCfg.step}
          startSec={cacheStartSec}
          endSec={nowSec}
          range={range}
          breakdown={breakdown}
        />
      </div>
    </div>
  );
}
