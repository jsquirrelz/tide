/*
 * TelemetryView.test.tsx (phase-16-telemetry-completion, plan 16-04)
 *
 * Vitest coverage for TelemetryView per UI-SPEC §Validation Contract:
 *   1. Degradation shape 1 (locked TELEM-02): 200 {status:"unavailable"}
 *      → TelemetryUnavailableNotice in every panel
 *   2. Degradation shape 2 (locked TELEM-02): 502 error
 *      → all four notices + wording matches /unreachable/
 *   3. Scope queries (D-02/D-04): selectedProject drives project filter;
 *      All projects toggle → by (project) queries
 *   4. Range selector (D-07): clicking 7d issues fetches with step=1800
 *   5. Budget surface (D-03): project scope/all-projects/empty cluster
 *   6. Chart render (D-05): success payload → svg element; empty → "No data in range"
 *
 * Uses vi.useFakeTimers() in beforeEach (Pitfall 6 — polling interval
 * must never fire realtime in jsdom).
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import TelemetryView from "../TelemetryView";
import type { ProjectSummary } from "../../lib/api";

// Mock recharts ResponsiveContainer to render children with fixed dimensions.
// jsdom has no layout engine — ResponsiveContainer measures 0x0 and won't
// render SVG children. This mock supplies a 200x200 container so recharts
// fully renders its SVG tree in test environments (Pitfall 5 / jsdom note).
vi.mock("recharts", async (importOriginal) => {
  const actual = await importOriginal<typeof import("recharts")>();
  const { cloneElement } = await import("react");
  return {
    ...actual,
    ResponsiveContainer: ({
      children,
    }: {
      children: React.ReactElement;
      width?: number | string;
      height?: number | string;
    }) => cloneElement(children, { width: 200, height: 200 }),
  };
});

// ─── Timer + cleanup lifecycle ────────────────────────────────────────────────

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  cleanup();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// ─── Fixtures ─────────────────────────────────────────────────────────────────

const PROJECT_P1: ProjectSummary = {
  name: "p1",
  namespace: "default",
  phase: "Running",
  activeMilestoneCount: 1,
  budget: { capCents: 10000, currentSpend: 500, withinBudget: true },
};

const PROJECT_ZERO_CAP: ProjectSummary = {
  name: "p2",
  namespace: "default",
  phase: "Running",
  activeMilestoneCount: 0,
  budget: { capCents: 0, currentSpend: 0, withinBudget: true },
};

const TWO_PROJECTS = [PROJECT_P1, PROJECT_ZERO_CAP];

/** A success matrix payload with one series. */
const SUCCESS_PAYLOAD = {
  status: "success",
  data: {
    resultType: "matrix",
    result: [
      {
        metric: { project: "p1" },
        values: [
          [1700000000, "1"],
          [1700000300, "2"],
        ],
      },
    ],
  },
};

/** An empty success payload — no series. */
const EMPTY_SUCCESS_PAYLOAD = {
  status: "success",
  data: { resultType: "matrix", result: [] },
};

// ─── Fetch stub helpers ───────────────────────────────────────────────────────

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

function stubFetchError(
  status = 502,
  body: unknown = { status: "error", message: "upstream unreachable" },
) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: false,
      status,
      json: async () => body,
    }) as unknown as typeof fetch,
  );
}

/** Flush microtasks + initial fetch effect without advancing the 60s interval. */
async function flushInitialFetch() {
  await act(async () => {
    await vi.advanceTimersByTimeAsync(0);
  });
}

// ─── 1. Degradation shape 1: 200 {status:"unavailable"} (locked TELEM-02) ────

describe("TelemetryView — degradation: 200 unavailable sentinel (TELEM-02)", () => {
  it("renders TelemetryUnavailableNotice in all four panel slots", async () => {
    stubFetchOK({ status: "unavailable" });
    render(
      <TelemetryView projects={TWO_PROJECTS} selectedProject="p1" />,
    );
    await flushInitialFetch();
    const notices = screen.getAllByTestId("telemetry-unavailable-notice");
    expect(notices).toHaveLength(4);
  });
});

// ─── 2. Degradation shape 2: 502 error (locked TELEM-02) ─────────────────────

describe("TelemetryView — degradation: 502 error response (TELEM-02)", () => {
  it("renders notice in all four panels with unreachable wording", async () => {
    stubFetchError(502, { status: "error", message: "upstream unreachable" });
    render(
      <TelemetryView projects={TWO_PROJECTS} selectedProject="p1" />,
    );
    await flushInitialFetch();
    const notices = screen.getAllByTestId("telemetry-unavailable-notice");
    expect(notices).toHaveLength(4);
    notices.forEach((n) => {
      expect(n.textContent).toMatch(/unreachable/);
    });
  });

  it("renders notice on non-502 non-2xx with generic wording", async () => {
    stubFetchError(503, { status: "error", message: "service unavailable" });
    render(
      <TelemetryView projects={[PROJECT_P1]} selectedProject="p1" />,
    );
    await flushInitialFetch();
    const notices = screen.getAllByTestId("telemetry-unavailable-notice");
    expect(notices.length).toBeGreaterThanOrEqual(1);
  });
});

// ─── 3. Scope queries (D-02/D-04) ─────────────────────────────────────────────

describe("TelemetryView — scope queries (D-02/D-04)", () => {
  it("with selectedProject='p1', every fetch URL query param contains project=\"p1\"", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: "unavailable" }),
    });
    vi.stubGlobal("fetch", fetchFn as unknown as typeof fetch);

    render(
      <TelemetryView projects={TWO_PROJECTS} selectedProject="p1" />,
    );
    await flushInitialFetch();

    expect(fetchFn).toHaveBeenCalled();
    const calls = (fetchFn as ReturnType<typeof vi.fn>).mock.calls as [string][];
    calls.forEach(([url]) => {
      const searchStr = url.split("?")[1] ?? "";
      const params = new URLSearchParams(searchStr);
      const query = params.get("query") ?? "";
      // Each query must filter by project="p1"
      expect(query).toContain('project="p1"');
    });
  });

  it("with selectedProject=null, all-projects mode: queries contain 'by (project)' or omit project filter", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: "unavailable" }),
    });
    vi.stubGlobal("fetch", fetchFn as unknown as typeof fetch);

    render(
      <TelemetryView projects={TWO_PROJECTS} selectedProject={null} />,
    );
    await flushInitialFetch();

    expect(fetchFn).toHaveBeenCalled();
    const calls = (fetchFn as ReturnType<typeof vi.fn>).mock.calls as [string][];
    // In all-projects mode, queries should NOT contain project="p1"
    calls.forEach(([url]) => {
      const searchStr = url.split("?")[1] ?? "";
      const params = new URLSearchParams(searchStr);
      const query = params.get("query") ?? "";
      expect(query).not.toContain('project="p1"');
    });
    // At least one query should have by (project) aggregation
    const hasAggregation = calls.some(([url]) => {
      const searchStr = url.split("?")[1] ?? "";
      const params = new URLSearchParams(searchStr);
      const query = params.get("query") ?? "";
      return query.includes("by (project)");
    });
    expect(hasAggregation).toBe(true);
  });

  it("clicking All projects segment fires fetches with by (project) queries", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: "unavailable" }),
    });
    vi.stubGlobal("fetch", fetchFn as unknown as typeof fetch);

    render(
      <TelemetryView projects={TWO_PROJECTS} selectedProject="p1" />,
    );
    await flushInitialFetch();

    // Clear calls from initial project-scope fetch
    fetchFn.mockClear();

    // Click "All projects" segment
    const allProjectsBtn = screen.getByText("All projects");
    await act(async () => {
      fireEvent.click(allProjectsBtn);
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(fetchFn).toHaveBeenCalled();
    const calls = (fetchFn as ReturnType<typeof vi.fn>).mock.calls as [string][];
    const hasAggregation = calls.some(([url]) => {
      const searchStr = url.split("?")[1] ?? "";
      const params = new URLSearchParams(searchStr);
      const query = params.get("query") ?? "";
      return query.includes("by (project)");
    });
    expect(hasAggregation).toBe(true);
  });

  it("with selectedProject=null, the scope toggle shows only 'All projects' (no project segment)", () => {
    stubFetchOK({ status: "unavailable" });
    render(
      <TelemetryView projects={TWO_PROJECTS} selectedProject={null} />,
    );
    // The scope toggle should only contain "All projects" — no project name segment button
    const toggle = screen.getByTestId("telemetry-scope-toggle");
    const buttons = toggle.querySelectorAll("button");
    // Only one button in the toggle when no project is selected
    expect(buttons).toHaveLength(1);
    expect(buttons[0].textContent).toBe("All projects");
  });
});

// ─── 4. Range selector (D-07) ─────────────────────────────────────────────────

describe("TelemetryView — range selector (D-07)", () => {
  it("clicking 7d issues fetches with step=1800 and start≈now-604800", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: "unavailable" }),
    });
    vi.stubGlobal("fetch", fetchFn as unknown as typeof fetch);

    const beforeRender = Math.floor(Date.now() / 1000);

    render(
      <TelemetryView projects={[PROJECT_P1]} selectedProject="p1" />,
    );
    await flushInitialFetch();

    // Clear initial 24h fetches
    fetchFn.mockClear();

    // Click "7d" range
    const sevenDayBtn = screen.getByText("7d");
    await act(async () => {
      fireEvent.click(sevenDayBtn);
      await vi.advanceTimersByTimeAsync(0);
    });

    expect(fetchFn).toHaveBeenCalled();
    const calls = (fetchFn as ReturnType<typeof vi.fn>).mock.calls as [string][];

    calls.forEach(([url]) => {
      const searchStr = url.split("?")[1] ?? "";
      const params = new URLSearchParams(searchStr);
      expect(params.get("step")).toBe("1800");
      const start = parseInt(params.get("start") ?? "0", 10);
      const end = parseInt(params.get("end") ?? "0", 10);
      // start should be ~604800s before end
      const diff = end - start;
      expect(diff).toBeGreaterThanOrEqual(604800 - 10);
      expect(diff).toBeLessThanOrEqual(604800 + 10);
      // end should be ≥ beforeRender
      expect(end).toBeGreaterThanOrEqual(beforeRender);
    });
  });

  it("clicking 30d issues fetches with step=7200", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: "unavailable" }),
    });
    vi.stubGlobal("fetch", fetchFn as unknown as typeof fetch);

    render(
      <TelemetryView projects={[PROJECT_P1]} selectedProject="p1" />,
    );
    await flushInitialFetch();
    fetchFn.mockClear();

    const thirtyDayBtn = screen.getByText("30d");
    await act(async () => {
      fireEvent.click(thirtyDayBtn);
      await vi.advanceTimersByTimeAsync(0);
    });

    const calls = (fetchFn as ReturnType<typeof vi.fn>).mock.calls as [string][];
    calls.forEach(([url]) => {
      const params = new URLSearchParams(url.split("?")[1] ?? "");
      expect(params.get("step")).toBe("7200");
    });
  });
});

// ─── 5. Budget surface (D-03) ─────────────────────────────────────────────────

describe("TelemetryView — budget surface (D-03)", () => {
  it("project scope: renders budget-card with spend and cap", async () => {
    stubFetchOK({ status: "unavailable" });
    render(
      <TelemetryView projects={TWO_PROJECTS} selectedProject="p1" />,
    );
    await flushInitialFetch();

    const card = screen.getByTestId("budget-card");
    expect(card).toBeDefined();
    // $5.00 spend (500 cents) and $100.00 cap (10000 cents)
    expect(card.textContent).toContain("$5.00");
    expect(card.textContent).toContain("of $100.00 cap");
  });

  it("all-projects scope: renders budget-card-grid with one card per project", async () => {
    stubFetchOK({ status: "unavailable" });
    render(
      <TelemetryView projects={TWO_PROJECTS} selectedProject={null} />,
    );
    await flushInitialFetch();

    const grid = screen.getByTestId("budget-card-grid");
    expect(grid).toBeDefined();

    // One card per project
    expect(screen.getByTestId("budget-card-p1")).toBeDefined();
    expect(screen.getByTestId("budget-card-p2")).toBeDefined();
  });

  it("zero-cap project renders 'No budget configured' instead of $0.00", async () => {
    stubFetchOK({ status: "unavailable" });
    render(
      <TelemetryView projects={TWO_PROJECTS} selectedProject={null} />,
    );
    await flushInitialFetch();

    const p2Card = screen.getByTestId("budget-card-p2");
    expect(p2Card.textContent).toContain("No budget configured");
    expect(p2Card.textContent).not.toContain("$0.00");
    expect(p2Card.textContent).not.toContain("NaN");
  });

  it("zero projects renders 'No projects in this cluster'", async () => {
    stubFetchOK({ status: "unavailable" });
    render(
      <TelemetryView projects={[]} selectedProject={null} />,
    );
    await flushInitialFetch();

    expect(screen.getByText("No projects in this cluster")).toBeDefined();
  });
});

// ─── 6. Chart render (D-05) ───────────────────────────────────────────────────

describe("TelemetryView — chart render (D-05)", () => {
  it("success payload with data renders an SVG chart element inside a panel", async () => {
    stubFetchOK(SUCCESS_PAYLOAD);
    render(
      <TelemetryView projects={[PROJECT_P1]} selectedProject="p1" />,
    );
    await flushInitialFetch();

    // recharts renders SVG elements
    const panels = screen.getAllByTestId(/^panel-/);
    expect(panels.length).toBeGreaterThanOrEqual(1);

    // At least one panel should contain an svg (recharts)
    const hasSvg = panels.some((p) => p.querySelector("svg") !== null);
    expect(hasSvg).toBe(true);

    // No unavailable notices when data is present
    expect(screen.queryByTestId("telemetry-unavailable-notice")).not.toBeInTheDocument();
  });

  it("empty result [] renders 'No data in range' in the chart area", async () => {
    stubFetchOK(EMPTY_SUCCESS_PAYLOAD);
    render(
      <TelemetryView projects={[PROJECT_P1]} selectedProject="p1" />,
    );
    await flushInitialFetch();

    const noDataMessages = screen.getAllByText("No data in range");
    expect(noDataMessages.length).toBeGreaterThanOrEqual(1);

    // Should NOT render unavailable notice (Prometheus answered, just no data)
    expect(screen.queryByTestId("telemetry-unavailable-notice")).not.toBeInTheDocument();
  });
});

// ─── Polling (D-07 — timer behavior) ─────────────────────────────────────────

describe("TelemetryView — polling (D-07)", () => {
  it("re-fetches after 60 seconds without resetting panels to loading", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ status: "unavailable" }),
    });
    vi.stubGlobal("fetch", fetchFn as unknown as typeof fetch);

    render(
      <TelemetryView projects={[PROJECT_P1]} selectedProject="p1" />,
    );
    await flushInitialFetch();

    const initialCallCount = fetchFn.mock.calls.length;
    expect(initialCallCount).toBeGreaterThan(0);

    // Advance 60 seconds — should trigger a polling re-fetch
    await act(async () => {
      await vi.advanceTimersByTimeAsync(60_000);
    });

    expect(fetchFn.mock.calls.length).toBeGreaterThan(initialCallCount);

    // Panels should not go back to loading state (no "Loading…" text after initial load)
    // The unavailable notices should still be present (not replaced by loading)
    const notices = screen.getAllByTestId("telemetry-unavailable-notice");
    expect(notices.length).toBeGreaterThanOrEqual(1);
  });
});
