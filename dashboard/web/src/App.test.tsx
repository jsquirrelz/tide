/*
 * App.test.tsx (plan 15-07) — UI-SPEC Validation Contract (App row).
 *
 *   Extends App integration spec with assertions required by UI-SPEC C4:
 *     1. Right pane defaults to RunningWavesView when selectedPlan === null
 *        (the old "Select a plan to view its execution DAG" empty-state string is GONE).
 *     2. Clicking a wave card swaps the pane to ExecutionDAGView + sets #/plan/<name>.
 *     3. The All waves button (execution-pane-all-waves) returns to the aggregate
 *        view and clears the hash.
 *
 *   Mocking strategy: stub heavy I/O + @xyflow/react so App's body branching
 *   is testable without a full browser/DOM layout environment.
 *
 *   - useProjects: returns a single project so App enters the two-column grid.
 *   - useTasks / useTaskDetail: return null (no plan data needed for navigation tests).
 *   - @xyflow/react: return minimal mocks so PlanningDAGView/ExecutionDAGView mount.
 *   - EventSource: FakeEventSource seam (established multi-file pattern).
 *   - RunningWavesView: uses initialSnapshot=[] via its SSE seam to avoid
 *     pre-snapshot spinner blocking the test assertions. We pass a project name
 *     that maps to a FakeEventSource that emits a good snapshot.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";

// ── @xyflow/react stub — must be defined BEFORE the App import ───────────────
vi.mock("@xyflow/react", async () => {
  const actual =
    await vi.importActual<typeof import("@xyflow/react")>("@xyflow/react");
  return {
    ...actual,
    useNodesInitialized: () => true,
  };
});

// ── Hook stubs ────────────────────────────────────────────────────────────────
vi.mock("./lib/projects", () => ({
  useProjects: vi.fn(),
}));

vi.mock("./lib/tasks", () => ({
  useTasks: vi.fn(),
  useTaskDetail: vi.fn(),
  projectEventsURL: (name: string | null) =>
    name ? `/api/v1/projects/${name}/events` : "/dev/null/no-project",
}));

import App from "./App";
import { useProjects } from "./lib/projects";
import { useTasks, useTaskDetail } from "./lib/tasks";

// jsdom does not implement window.matchMedia (Header.tsx:25 uses it for
// theme detection). Provide a minimal stub so Header mounts without error.
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

// ── FakeEventSource seam (established pattern from dag-views.test.tsx) ────────
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
    if (!set) { set = new Set(); this.listeners.set(type, set); }
    set.add(fn);
  }
  close() { this.closed = true; }
  _emitNamed(type: string, data: string) {
    const evt = new MessageEvent(type, { data });
    this.listeners.get(type)?.forEach((fn) => fn(evt));
  }
}

// ── Helpers ───────────────────────────────────────────────────────────────────

/** Stub fetch to return a project payload (needed for PlanningDAGView refetch). */
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

/** The minimal project detail fetch uses empty hierarchy — layout still renders. */
const EMPTY_PROJECT_DETAIL = {
  name: "test-project",
  namespace: "default",
  phase: "Running",
  activeMilestoneCount: 0,
  budget: { capCents: 0, currentSpend: 0, withinBudget: true },
  milestones: [],
  phases: [],
  plans: [],
};

/** Stub useProjects to return a single project (enters two-column grid branch). */
function stubProjects() {
  (useProjects as ReturnType<typeof vi.fn>).mockReturnValue({
    projects: [{ name: "test-project", namespace: "default", phase: "Running" }],
    loading: false,
    error: null,
    refetch: vi.fn(),
  });
}

/** Stub useTasks / useTaskDetail to return null (no plan selected initially). */
function stubTasksNull() {
  (useTasks as ReturnType<typeof vi.fn>).mockReturnValue(null);
  (useTaskDetail as ReturnType<typeof vi.fn>).mockReturnValue({
    task: null,
    loading: false,
    error: null,
  });
}

/** A minimal running wave to emit via SSE so RunningWavesView shows cards. */
const RUNNING_WAVE_PAYLOAD = JSON.stringify({
  waves: [
    {
      planName: "nav-target-plan",
      waveIndex: 0,
      tasks: [{ name: "t1", status: "Running" }],
    },
  ],
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  FakeEventSource.instances = [];
  // Reset hash after each test.
  history.replaceState(null, "", "/");
});

beforeEach(() => {
  FakeEventSource.instances = [];
  vi.stubGlobal("EventSource", FakeEventSource as unknown as typeof EventSource);
  stubProjects();
  stubTasksNull();
  stubFetchOK(EMPTY_PROJECT_DETAIL);
});

// ── Test 1: right pane defaults to RunningWavesView; old string is gone ───────

describe("App — Test 1: right pane default (UI-SPEC C4, D-13)", () => {
  it("RunningWavesView mounts as the right-pane default when selectedPlan is null", async () => {
    render(<App />);

    await waitFor(() => {
      // running-waves-view is the right-pane default; it starts in spinner state
      // because no SSE snapshot has arrived yet.
      expect(document.querySelector('[data-testid="running-waves-view"]')).not.toBeNull();
    });
  });

  it("the old 'Select a plan to view its execution DAG' string is absent", async () => {
    render(<App />);

    await waitFor(() => {
      expect(document.querySelector('[data-testid="running-waves-view"]')).not.toBeNull();
    });

    // The literal old empty-state string must be gone (regression guard).
    expect(
      screen.queryByText("Select a plan to view its execution DAG"),
    ).toBeNull();
  });
});

// ── Helper: emit a waves.snapshot to all project-events SSE instances ─────────
//
// Multiple components subscribe to the same project-events URL (PlanningDAGView,
// RunningWavesView, useTasks, useTaskDetail each hold their own subscription per
// the established multi-subscriber pattern). emitToAll broadcasts to every
// FakeEventSource instance that matches the project URL so RunningWavesView
// receives the event regardless of which instance was created first.
function emitToAll(type: string, data: string) {
  FakeEventSource.instances
    .filter((e) => e.url.includes("test-project"))
    .forEach((e) => e._emitNamed(type, data));
}

// ── Test 2: card click swaps pane to ExecutionDAGView + sets hash ─────────────

describe("App — Test 2: wave card click → ExecutionDAGView + hash (UI-SPEC C4, D-16)", () => {
  it("clicking a wave card selects the plan, mounts ExecutionDAGView, and sets #/plan/<name>", async () => {
    (useTasks as ReturnType<typeof vi.fn>).mockReturnValue({
      planName: "nav-target-plan",
      tasks: [],
      activeDispatchWave: null,
    });

    render(<App />);

    // Wait for the right-pane default to render.
    await waitFor(() => {
      expect(document.querySelector('[data-testid="running-waves-view"]')).not.toBeNull();
    });

    // Wait until at least one SSE connection to the project-events URL exists.
    // (The selectedProject default effect is async; connections may not exist on
    // the first render tick. RunningWavesView and PlanningDAGView both subscribe.)
    await waitFor(() => {
      expect(
        FakeEventSource.instances.some((e) => e.url.includes("test-project")),
      ).toBe(true);
    });

    // Emit a waves.snapshot to all matching EventSource instances so RunningWavesView
    // shows a card (multi-subscriber pattern — each component owns its instance).
    act(() => { emitToAll("waves.snapshot", RUNNING_WAVE_PAYLOAD); });

    // Wave card should now be visible.
    await waitFor(() => {
      expect(
        document.querySelector('[data-testid="wave-card-nav-target-plan-0"]'),
      ).not.toBeNull();
    });

    // Click the card.
    act(() => {
      fireEvent.click(
        document.querySelector('[data-testid="wave-card-nav-target-plan-0"]')!,
      );
    });

    // After click: ExecutionDAGView should be present, RunningWavesView should be gone.
    await waitFor(() => {
      expect(
        document.querySelector('[data-testid="execution-dag-view"]'),
      ).not.toBeNull();
      expect(
        document.querySelector('[data-testid="running-waves-view"]'),
      ).toBeNull();
    });

    // Hash should be set to #/plan/nav-target-plan.
    expect(window.location.hash).toBe("#/plan/nav-target-plan");
  });
});

// ── Test 3: All waves button returns to aggregate and clears hash ─────────────

describe("App — Test 3: All waves button → aggregate + cleared hash (UI-SPEC C4)", () => {
  it("All waves button returns the pane to RunningWavesView and clears the URL hash", async () => {
    (useTasks as ReturnType<typeof vi.fn>).mockReturnValue({
      planName: "nav-target-plan",
      tasks: [],
      activeDispatchWave: null,
    });

    render(<App />);

    await waitFor(() => {
      expect(document.querySelector('[data-testid="running-waves-view"]')).not.toBeNull();
    });

    // Wait for SSE connections to be established.
    await waitFor(() => {
      expect(
        FakeEventSource.instances.some((e) => e.url.includes("test-project")),
      ).toBe(true);
    });

    // Emit snapshot to all instances so card is visible.
    act(() => { emitToAll("waves.snapshot", RUNNING_WAVE_PAYLOAD); });

    await waitFor(() => {
      expect(
        document.querySelector('[data-testid="wave-card-nav-target-plan-0"]'),
      ).not.toBeNull();
    });

    // Navigate to a plan via card click.
    act(() => {
      fireEvent.click(
        document.querySelector('[data-testid="wave-card-nav-target-plan-0"]')!,
      );
    });

    // Wait for ExecutionDAGView.
    await waitFor(() => {
      expect(
        document.querySelector('[data-testid="execution-dag-view"]'),
      ).not.toBeNull();
    });

    // "All waves" button should be present.
    const allWavesBtn = document.querySelector(
      '[data-testid="execution-pane-all-waves"]',
    );
    expect(allWavesBtn).not.toBeNull();

    // Click All waves.
    act(() => {
      fireEvent.click(allWavesBtn!);
    });

    // Pane should return to the aggregate (RunningWavesView).
    await waitFor(() => {
      expect(
        document.querySelector('[data-testid="running-waves-view"]'),
      ).not.toBeNull();
      expect(
        document.querySelector('[data-testid="execution-dag-view"]'),
      ).toBeNull();
    });

    // Hash should be cleared.
    expect(window.location.hash).toBe("");
  });
});
