/*
 * TelemetryView.tsx (phase-03-telemetry-ui)
 *
 * The Telemetry tab view, composed of:
 *   (a) A live Project budget card sourced from Project.Status.Budget —
 *       renders in ALL modes with NO Prometheus dependency.
 *   (b) Four chart-panel slots backed by the same-origin chi PromQL proxy
 *       (GET /api/v1/query_range):
 *         — Cost Over Time
 *         — Dispatch Counts
 *         — Failure Rate
 *         — Token Breakdown
 *
 * The proxy emits THREE response shapes (MILESTONE.md §Q1=PROXY):
 *   1. HTTP 200 + {status:"success", data:{resultType:"matrix", result:[…]}}
 *      → chart data rendered in the panel slot.
 *   2. HTTP 200 + {status:"unavailable"}
 *      → TelemetryUnavailableNotice (generic "not configured" wording).
 *   3. HTTP non-2xx (502 etc.) + any body
 *      → TelemetryUnavailableNotice with "unreachable" wording.
 *
 * No Prometheus call ever causes an ErrorState/EmptyState takeover — those
 * are reserved for CRD-.status failures (out of scope here).
 */
import { useEffect, useState } from "react";
import type { BudgetSummary } from "../lib/api";
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
  | { kind: "data"; series: PromMatrix[] }
  | { kind: "unavailable" }
  | { kind: "unreachable"; message: string };

// ─── Panel definitions ───────────────────────────────────────────────────────

type PanelDef = {
  id: string;
  label: string;
  query: string;
};

const PANELS: PanelDef[] = [
  {
    id: "cost-over-time",
    label: "Cost Over Time",
    query: "sum(rate(tide_cost_cents_total[5m])) by (project)",
  },
  {
    id: "dispatch-counts",
    label: "Dispatch Counts",
    query: "sum(increase(tide_tasks_dispatched_total[5m])) by (project)",
  },
  {
    id: "failure-rate",
    label: "Failure Rate",
    query:
      "sum(rate(tide_tasks_failed_total[5m])) / sum(rate(tide_tasks_dispatched_total[5m]))",
  },
  {
    id: "token-breakdown",
    label: "Token Breakdown",
    query: "sum(tide_tokens_used_total) by (model)",
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
  step: string,
): Promise<PanelState> {
  const params = new URLSearchParams({
    query,
    start: String(startSec),
    end: String(endSec),
    step,
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
    return { kind: "data", series: body.data.result };
  }

  if (body.status === "unavailable") {
    return { kind: "unavailable" };
  }

  // status === "error" on a 200 response — treat as unavailable.
  return { kind: "unavailable" };
}

// ─── Sub-components ──────────────────────────────────────────────────────────

/** A single data point in a series rendered as a minimal inline sparkline. */
function Sparkline({ series }: { series: PromMatrix[] }) {
  if (series.length === 0) {
    return (
      <p
        style={{
          fontSize: "12px",
          fontFamily: "var(--font-mono)",
          color: "var(--color-text-muted)",
        }}
      >
        No data in range
      </p>
    );
  }

  return (
    <ul
      style={{
        fontSize: "12px",
        fontFamily: "var(--font-mono)",
        color: "var(--color-text-primary)",
        listStyle: "none",
        padding: 0,
        margin: 0,
      }}
    >
      {series.slice(0, 5).map((s, i) => {
        const lastVal = s.values[s.values.length - 1]?.[1] ?? "—";
        const label =
          Object.values(s.metric).join(", ") || `series-${i}`;
        return (
          <li key={i}>
            {label}: {lastVal}
          </li>
        );
      })}
    </ul>
  );
}

type ChartPanelProps = {
  def: PanelDef;
  state: PanelState;
};

function ChartPanel({ def, state }: ChartPanelProps) {
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

      {state.kind === "data" && <Sparkline series={state.series} />}

      {state.kind === "unavailable" && <TelemetryUnavailableNotice />}

      {state.kind === "unreachable" && (
        <TelemetryUnavailableNotice message={state.message} />
      )}
    </div>
  );
}

// ─── Budget card ─────────────────────────────────────────────────────────────

function BudgetCard({ budget }: { budget: BudgetSummary }) {
  const spent = formatCents(budget.currentSpend);
  const cap = formatCents(budget.capCents);
  const withinBudget = budget.withinBudget;

  return (
    <div
      data-testid="budget-card"
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
        Budget
      </h3>
      <div
        style={{
          fontSize: "24px",
          fontWeight: 700,
          fontFamily: "var(--font-mono)",
          color: "var(--color-text-primary)",
        }}
      >
        {spent}
      </div>
      <div
        style={{
          fontSize: "13px",
          fontFamily: "var(--font-mono)",
          color: "var(--color-text-muted)",
        }}
      >
        of {cap} cap
      </div>
      <div
        data-testid="budget-status"
        style={{
          fontSize: "12px",
          fontWeight: 600,
          fontFamily: "var(--font-mono)",
          color: withinBudget
            ? "var(--color-success, #22c55e)"
            : "var(--color-destructive, #ef4444)",
          marginTop: "4px",
        }}
      >
        {withinBudget ? "Within budget" : "Over budget"}
      </div>
    </div>
  );
}

// ─── TelemetryView ───────────────────────────────────────────────────────────

export type TelemetryViewProps = {
  projectName: string;
  namespace: string;
  budget: BudgetSummary;
};

export default function TelemetryView({
  projectName: _projectName,
  namespace: _namespace,
  budget,
}: TelemetryViewProps) {
  // Default time range: last 24 hours, 5-minute step.
  const nowSec = Math.floor(Date.now() / 1000);
  const startSec = nowSec - 24 * 60 * 60;
  const step = "300";

  const [panelStates, setPanelStates] = useState<PanelState[]>(
    PANELS.map(() => ({ kind: "loading" })),
  );

  useEffect(() => {
    let cancelled = false;

    const fetches = PANELS.map((panel, idx) =>
      fetchQueryRange(panel.query, startSec, nowSec, step).then((state) => {
        if (!cancelled) {
          setPanelStates((prev) => {
            const next = [...prev];
            next[idx] = state;
            return next;
          });
        }
      }),
    );

    // Eagerly subscribe; cleanup cancels state updates from in-flight requests.
    void fetches;

    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div
      data-testid="telemetry-view"
      className="flex flex-col gap-4 p-4"
      style={{ height: "100%", overflow: "auto" }}
    >
      {/* Budget card — always rendered, no Prometheus dependency */}
      <BudgetCard budget={budget} />

      {/* Chart panels — Prometheus-backed, show notice on degraded paths */}
      <div
        data-testid="telemetry-panels"
        className="grid grid-cols-2 gap-4"
      >
        {PANELS.map((def, idx) => (
          <ChartPanel key={def.id} def={def} state={panelStates[idx]} />
        ))}
      </div>
    </div>
  );
}
