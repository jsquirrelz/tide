/*
 * view-switcher.test.tsx (phase-16-telemetry-completion, plan 16-05)
 *
 * App-level Vitest coverage for the D-01 header view switcher (UI-SPEC §C2,
 * §Validation Contract row 1). Pins four behaviours:
 *
 *   1. Default: view-tab-dags is aria-selected; telemetry-view absent;
 *      two-pane DAGs body present (running-waves-view right-pane default).
 *   2. Switch to Telemetry: clicking view-tab-telemetry mounts telemetry-view;
 *      DAGs body absent; view-tab-telemetry is aria-selected.
 *   3. Switch back: clicking view-tab-dags restores the DAGs two-pane body
 *      (running-waves-view present); telemetry-view gone.
 *   4. Keyboard: ArrowRight from view-tab-dags selects + focuses view-tab-telemetry
 *      (roving focus).
 *
 * Mocking strategy:
 *   - vi.mock("@xyflow/react") — useNodesInitialized: () => true so PlanningDAGView
 *     and ExecutionDAGView mount deterministically without DOM measurements.
 *   - vi.mock("../../lib/projects") + vi.mock("../../lib/tasks") — hook stubs matching
 *     the App.test.tsx harness so the two-column grid branch renders.
 *   - vi.mock("recharts") — ResponsiveContainer shim (jsdom has no layout engine;
 *     height=0 collapses recharts SVG — the TelemetryView.test.tsx pattern).
 *   - vi.stubGlobal("fetch", ...) — URL router:
 *       /api/v1/query_range   → { status: "unavailable" } (TelemetryView panels degraded)
 *       everything else        → EMPTY_PROJECT_DETAIL (PlanningDAGView detail)
 *   - vi.stubGlobal("EventSource", FakeEventSource) — jsdom has no EventSource.
 *
 * Note: vi.useFakeTimers() is NOT used globally here because waitFor() internally
 * polls via setTimeout, which fake timers would suppress. TelemetryView's 60s
 * polling is not under test here — the unavailable fetch stub keeps panels in
 * degraded state regardless of when they fire.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react";

// ── @xyflow/react stub — must be hoisted above App import ─────────────────────
vi.mock("@xyflow/react", async () => {
  const actual =
    await vi.importActual<typeof import("@xyflow/react")>("@xyflow/react");
  return {
    ...actual,
    useNodesInitialized: () => true,
  };
});

// ── recharts stub — ResponsiveContainer needs a definite height in jsdom ──────
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

// ── Hook stubs ────────────────────────────────────────────────────────────────
vi.mock("../../lib/projects", () => ({
  useProjects: vi.fn(),
}));

vi.mock("../../lib/tasks", () => ({
  useTasks: vi.fn(),
  useTaskDetail: vi.fn(),
  projectEventsURL: (name: string | null) =>
    name ? `/api/v1/projects/${name}/events` : "/dev/null/no-project",
}));

import App from "../../App";
import { useProjects } from "../../lib/projects";
import { useTasks, useTaskDetail } from "../../lib/tasks";

// ── window.matchMedia stub (Header.tsx theme detection) ───────────────────────
Object.defineProperty(window, "matchMedia", {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }),
});

// ── FakeEventSource seam (established multi-file pattern) ─────────────────────
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  onopen: ((e: Event) => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onerror: ((e: Event) => void) | null = null;
  listeners = new Map<string, Set<(e: MessageEvent) => void>>();
  closed = false;
  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }
  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    let set = this.listeners.get(type);
    if (!set) {
      set = new Set();
      this.listeners.set(type, set);
    }
    set.add(fn);
  }
  close() {
    this.closed = true;
  }
  _emitNamed(type: string, data: string) {
    const evt = new MessageEvent(type, { data });
    this.listeners.get(type)?.forEach((fn) => fn(evt));
  }
}

// ── Fixtures ──────────────────────────────────────────────────────────────────

/** Minimal project detail for PlanningDAGView detail fetch. */
const EMPTY_PROJECT_DETAIL = {
  name: "test-project",
  namespace: "default",
  phase: "Running",
  activeMilestoneCount: 0,
  budget: { capCents: 10000, currentSpend: 0, withinBudget: true },
  milestones: [],
  phases: [],
  plans: [],
};

/** URL-routing fetch stub: query_range → unavailable; project detail → empty. */
function stubFetch() {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockImplementation(async (url: string | URL) => {
      const urlStr = typeof url === "string" ? url : url.toString();
      if (urlStr.includes("/api/v1/query_range")) {
        return {
          ok: true,
          status: 200,
          json: async () => ({ status: "unavailable" }),
        };
      }
      // PlanningDAGView detail fetches and any other GET.
      return {
        ok: true,
        status: 200,
        json: async () => EMPTY_PROJECT_DETAIL,
      };
    }) as unknown as typeof fetch,
  );
}

/** Stub useProjects to return a single project — enters the two-column grid branch. */
function stubProjects() {
  (useProjects as ReturnType<typeof vi.fn>).mockReturnValue({
    projects: [
      {
        name: "test-project",
        namespace: "default",
        phase: "Running",
        activeMilestoneCount: 0,
        budget: { capCents: 10000, currentSpend: 0, withinBudget: true },
      },
    ],
    loading: false,
    error: null,
    refetch: vi.fn(),
  });
}

/** Stub useTasks / useTaskDetail to return null (no plan selected). */
function stubTasksNull() {
  (useTasks as ReturnType<typeof vi.fn>).mockReturnValue(null);
  (useTaskDetail as ReturnType<typeof vi.fn>).mockReturnValue({
    task: null,
    loading: false,
    error: null,
  });
}

// ── Lifecycle ─────────────────────────────────────────────────────────────────

beforeEach(() => {
  FakeEventSource.instances = [];
  vi.stubGlobal("EventSource", FakeEventSource as unknown as typeof EventSource);
  stubProjects();
  stubTasksNull();
  stubFetch();
  history.replaceState(null, "", "/");
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  FakeEventSource.instances = [];
});

// ── Test 1: default view — DAGs tab selected, telemetry-view absent ───────────

describe("ViewSwitcher — Test 1: default view is DAGs (UI-SPEC §C2 / §Validation Contract)", () => {
  it("renders with view-tab-dags aria-selected=true, telemetry-view absent, DAGs body present", async () => {
    render(<App />);

    await waitFor(() => {
      // View switcher renders both tabs.
      const dagsTab = document.querySelector('[data-testid="view-tab-dags"]');
      expect(dagsTab).not.toBeNull();

      // DAGs tab is selected by default.
      expect(dagsTab!.getAttribute("aria-selected")).toBe("true");

      // Telemetry tab present but not selected.
      const telTab = document.querySelector('[data-testid="view-tab-telemetry"]');
      expect(telTab).not.toBeNull();
      expect(telTab!.getAttribute("aria-selected")).toBe("false");
    });

    // TelemetryView must NOT be in the document by default.
    expect(document.querySelector('[data-testid="telemetry-view"]')).toBeNull();

    // RunningWavesView is the right-pane default in the DAGs two-pane body.
    expect(document.querySelector('[data-testid="running-waves-view"]')).not.toBeNull();
  });
});

// ── Test 2: switch to Telemetry — mounts telemetry-view; DAGs body absent ─────

describe("ViewSwitcher — Test 2: clicking Telemetry tab mounts TelemetryView (UI-SPEC §C2)", () => {
  it("clicking view-tab-telemetry shows telemetry-view and hides the DAGs two-pane body", async () => {
    render(<App />);

    // Wait for initial render.
    await waitFor(() => {
      expect(document.querySelector('[data-testid="view-tab-telemetry"]')).not.toBeNull();
    });

    // Click the Telemetry tab.
    act(() => {
      fireEvent.click(document.querySelector('[data-testid="view-tab-telemetry"]')!);
    });

    await waitFor(() => {
      // TelemetryView must be present.
      expect(document.querySelector('[data-testid="telemetry-view"]')).not.toBeNull();

      // Telemetry tab is now selected.
      expect(
        document.querySelector('[data-testid="view-tab-telemetry"]')!
          .getAttribute("aria-selected"),
      ).toBe("true");

      // DAGs body (running-waves-view) must be absent — conditional render, not hidden.
      expect(document.querySelector('[data-testid="running-waves-view"]')).toBeNull();
    });
  });
});

// ── Test 3: switch back to DAGs — two-pane body restored ─────────────────────

describe("ViewSwitcher — Test 3: switching back to DAGs restores Phase 15 body (UI-SPEC §C2)", () => {
  it("clicking view-tab-dags after Telemetry restores running-waves-view; telemetry-view gone", async () => {
    render(<App />);

    // Wait for initial render.
    await waitFor(() => {
      expect(document.querySelector('[data-testid="view-tab-telemetry"]')).not.toBeNull();
    });

    // Switch to Telemetry.
    act(() => {
      fireEvent.click(document.querySelector('[data-testid="view-tab-telemetry"]')!);
    });

    await waitFor(() => {
      expect(document.querySelector('[data-testid="telemetry-view"]')).not.toBeNull();
    });

    // Switch back to DAGs.
    act(() => {
      fireEvent.click(document.querySelector('[data-testid="view-tab-dags"]')!);
    });

    await waitFor(() => {
      // Phase 15 right-pane default (RunningWavesView) is restored.
      expect(document.querySelector('[data-testid="running-waves-view"]')).not.toBeNull();

      // TelemetryView is unmounted (conditional render).
      expect(document.querySelector('[data-testid="telemetry-view"]')).toBeNull();

      // DAGs tab is selected again.
      expect(
        document.querySelector('[data-testid="view-tab-dags"]')!
          .getAttribute("aria-selected"),
      ).toBe("true");
    });
  });
});

// ── Test 4: keyboard — ArrowRight from view-tab-dags selects view-tab-telemetry ─

describe("ViewSwitcher — Test 4: ArrowRight roving focus (UI-SPEC §C2 accessibility)", () => {
  it("ArrowRight on view-tab-dags selects view-tab-telemetry", async () => {
    render(<App />);

    await waitFor(() => {
      expect(document.querySelector('[data-testid="view-tab-dags"]')).not.toBeNull();
    });

    const dagsTab = document.querySelector(
      '[data-testid="view-tab-dags"]',
    ) as HTMLButtonElement;

    // Fire ArrowRight keydown on the dags tab.
    act(() => {
      fireEvent.keyDown(dagsTab, { key: "ArrowRight" });
    });

    await waitFor(() => {
      const telTab = document.querySelector('[data-testid="view-tab-telemetry"]')!;
      // Telemetry tab is now aria-selected (roving focus activated selection).
      expect(telTab.getAttribute("aria-selected")).toBe("true");
      // TelemetryView is mounted after selection change.
      expect(document.querySelector('[data-testid="telemetry-view"]')).not.toBeNull();
    });
  });
});
