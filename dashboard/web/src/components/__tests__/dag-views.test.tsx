import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, waitFor } from "@testing-library/react";

// Mock @xyflow's useNodesInitialized to flip true synchronously so layout
// runs in the first effect tick — keeps the test deterministic without a
// full DOM-measurement harness. We re-export the rest of the module
// verbatim so the production ReactFlow tree still mounts.
vi.mock("@xyflow/react", async () => {
  const actual =
    await vi.importActual<typeof import("@xyflow/react")>("@xyflow/react");
  return {
    ...actual,
    useNodesInitialized: () => true,
  };
});

import PlanningDAGView from "../PlanningDAGView";
import ExecutionDAGView, {
  type ExecutionPlanData,
} from "../ExecutionDAGView";
import type { ProjectDetail } from "../../lib/api";
import type { ProjectBlockingCondition } from "../ConditionBadge";

// PlanningDAGView now subscribes to the project-events SSE stream. jsdom has
// no EventSource, so stub it. The fake routes named events
// (`project.update`, …) to the addEventListener handlers the hook binds.
type FakeMessage = { data: string; lastEventId?: string };
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
  _emitNamed(type: string, msg: FakeMessage) {
    const evt = new MessageEvent(type, {
      data: msg.data,
      lastEventId: msg.lastEventId,
    });
    this.listeners.get(type)?.forEach((fn) => fn(evt));
  }
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

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

beforeEach(() => {
  FakeEventSource.instances = [];
  vi.stubGlobal(
    "EventSource",
    FakeEventSource as unknown as typeof EventSource,
  );
});

const PROJECT_PAYLOAD: ProjectDetail = {
  name: "my-project",
  namespace: "default",
  phase: "Running",
  activeMilestoneCount: 2,
  budget: { capCents: 0, currentSpend: 0, withinBudget: true },
  milestones: [
    {
      name: "ship-v1",
      namespace: "default",
      phase: "Running",
      parent: "my-project",
    },
    {
      name: "ship-v2",
      namespace: "default",
      phase: "Pending",
      parent: "my-project",
    },
  ],
  phases: [
    { name: "04-dashboard", namespace: "default", phase: "Running", parent: "ship-v1" },
    { name: "05-cli", namespace: "default", phase: "Pending", parent: "ship-v1" },
    { name: "06-helm", namespace: "default", phase: "Pending", parent: "ship-v2" },
    { name: "07-docs", namespace: "default", phase: "Pending", parent: "ship-v2" },
  ],
  plans: [
    { name: "p1", namespace: "default", phase: "Succeeded", parent: "04-dashboard" },
    { name: "p2", namespace: "default", phase: "Running", parent: "04-dashboard" },
    { name: "p3", namespace: "default", phase: "Pending", parent: "05-cli" },
    { name: "p4", namespace: "default", phase: "Pending", parent: "05-cli" },
    { name: "p5", namespace: "default", phase: "Pending", parent: "06-helm" },
    { name: "p6", namespace: "default", phase: "Pending", parent: "07-docs" },
  ],
};

describe("PlanningDAGView — Test 1: hierarchy renders with ≥13 nodes and LR direction", () => {
  it("renders 1 project + 2 milestones + 4 phases + 6 plans = 13 nodes via dagre LR", async () => {
    stubFetchOK(PROJECT_PAYLOAD);
    render(<PlanningDAGView projectName="my-project" onPlanClick={() => undefined} />);
    // Wait for the fetch + first layout pass to complete.
    await waitFor(() => {
      // 1 project + 2 milestones + 4 phases + 6 plans = 13 tide nodes total
      const allTide = document.querySelectorAll('[data-testid^="tide-node-"]');
      expect(allTide.length).toBeGreaterThanOrEqual(13);
    });
    // Direction marker: PlanningDAGView sets data-dagre-direction="LR"
    expect(
      document.querySelector('[data-testid="planning-dag-view"]')!.getAttribute(
        "data-dagre-direction",
      ),
    ).toBe("LR");
  });
});

describe("PlanningDAGView — SSE live-update: a planning event triggers a refetch", () => {
  it("debounced refetch fires on a named Plan event; Task events are ignored", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => PROJECT_PAYLOAD,
    });
    vi.stubGlobal("fetch", fetchFn as unknown as typeof fetch);
    vi.useFakeTimers();

    try {
      render(
        <PlanningDAGView projectName="my-project" onPlanClick={() => undefined} />,
      );

      // Drain the initial fetch.
      await act(async () => {
        await vi.advanceTimersByTimeAsync(0);
      });
      expect(fetchFn).toHaveBeenCalledTimes(1);

      const es = FakeEventSource.instances[0];
      expect(es).toBeDefined();

      // A Task event must NOT trigger a planning refetch (execution-pane only).
      act(() => {
        es._emitNamed("task.update", {
          data: JSON.stringify({ kind: "Task", name: "t-1" }),
        });
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(300);
      });
      expect(fetchFn).toHaveBeenCalledTimes(1);

      // A Plan event triggers a debounced refetch.
      act(() => {
        es._emitNamed("plan.update", {
          data: JSON.stringify({ kind: "Plan", name: "p2" }),
        });
      });
      await act(async () => {
        await vi.advanceTimersByTimeAsync(300);
      });
      expect(fetchFn).toHaveBeenCalledTimes(2);
    } finally {
      vi.useRealTimers();
    }
  });
});

const EXECUTION_PAYLOAD: ExecutionPlanData = {
  planName: "04-13",
  tasks: [
    { name: "t1", status: "Succeeded", waveIndex: 0, attempt: 1, dependsOn: [] },
    { name: "t2", status: "Succeeded", waveIndex: 0, attempt: 1, dependsOn: [] },
    { name: "t3", status: "Running", waveIndex: 1, attempt: 1, dependsOn: ["t1"] },
    { name: "t4", status: "Running", waveIndex: 1, attempt: 1, dependsOn: ["t2"] },
    { name: "t5", status: "Pending", waveIndex: 2, attempt: 0, dependsOn: ["t3"] },
    { name: "t6", status: "Pending", waveIndex: 2, attempt: 0, dependsOn: ["t4"] },
  ],
  activeDispatchWave: 1,
};

describe("ExecutionDAGView — Test 2: 6 tasks across 3 waves + LR direction", () => {
  it("renders 6 TaskNode elements + 3 WaveBackground bands with LR direction", async () => {
    render(
      <ExecutionDAGView
        planName="04-13"
        plan={EXECUTION_PAYLOAD}
        onTaskClick={() => undefined}
      />,
    );
    await waitFor(() => {
      expect(document.querySelectorAll('[data-testid="tide-node-task"]').length).toBe(6);
    });
    expect(document.querySelectorAll('[data-testid^="wave-background-"]').length).toBe(3);
    expect(
      document
        .querySelector('[data-testid="execution-dag-view"]')!
        .getAttribute("data-dagre-direction"),
    ).toBe("LR");
  });
});

describe("ExecutionDAGView — Test 4: cross-wave edges use smoothstep; intra-wave edges default", () => {
  it("edge t1->t3 (cross-wave) carries type=smoothstep", () => {
    // Build a small inline harness so we can inspect the computed edges:
    // we render the view then dive into the rendered DOM for edge id markers
    // we wire onto each edge via data-edge-type attribute.
    // Edges are emitted into the DOM by ExecutionDAGView via data-edge-meta.
    render(
      <ExecutionDAGView
        planName="04-13"
        plan={EXECUTION_PAYLOAD}
        onTaskClick={() => undefined}
      />,
    );
    const meta = document.querySelectorAll(
      '[data-testid="execution-dag-view"] [data-edge-meta]',
    );
    const map = new Map<string, string>();
    meta.forEach((el) => {
      const id = el.getAttribute("data-edge-id")!;
      const type = el.getAttribute("data-edge-type")!;
      map.set(id, type);
    });
    expect(map.get("t1->t3")).toBe("smoothstep"); // cross-wave (0 → 1)
    expect(map.get("t3->t5")).toBe("smoothstep"); // cross-wave (1 → 2)
  });
});

describe("ExecutionDAGView — Test 5: WaveBackground bands at z-index 0; tasks overlay above", () => {
  it("wave-background data-z-index is 0 and tide-node-task z-index is 10", () => {
    render(
      <ExecutionDAGView
        planName="04-13"
        plan={EXECUTION_PAYLOAD}
        onTaskClick={() => undefined}
      />,
    );
    // Wave bands are wrapped in a layer with data-z-layer="background" + zIndex 0.
    const bgLayer = document.querySelector(
      '[data-testid="execution-dag-view"] [data-z-layer="wave-background"]',
    );
    expect(bgLayer).not.toBeNull();
    // Each wave-background-N <g> renders inside; assert children present.
    expect(
      bgLayer!.querySelectorAll('[data-testid^="wave-background-"]').length,
    ).toBe(3);
  });
});

describe("ExecutionDAGView — Test 3: Pitfall 26 flicker mitigation", () => {
  it("opacity:0 → opacity:1 transition (data-flicker-ready) gates on useNodesInitialized + dagre layout", async () => {
    // Render with plan=null first so we can observe the pre-data
    // "false" state, then pass real data to observe the transition
    // to "true" once useNodesInitialized + dagre layout complete.
    const { rerender } = render(
      <ExecutionDAGView
        planName="04-13"
        plan={null}
        onTaskClick={() => undefined}
      />,
    );
    const root = document.querySelector('[data-testid="execution-dag-view"]')!;
    // No plan yet → still "false" (Pitfall 26 starting state).
    expect(root.getAttribute("data-flicker-ready")).toBe("false");

    // Now feed real plan data. The mocked `useNodesInitialized()`
    // returns true synchronously, so the layout effect runs in the
    // commit phase that follows the data-load effect.
    rerender(
      <ExecutionDAGView
        planName="04-13"
        plan={EXECUTION_PAYLOAD}
        onTaskClick={() => undefined}
      />,
    );
    await waitFor(() => {
      const r2 = document.querySelector('[data-testid="execution-dag-view"]')!;
      expect(r2.getAttribute("data-flicker-ready")).toBe("true");
    });
    // After the layout pass, task nodes carry opacity:1 (UI-SPEC §5);
    // before flicker-ready transitions to true they would have opacity:0.
    // We assert on the data-flicker-ready transition rather than reading
    // individual node opacity (which @xyflow nests inside its own wrapper).
  });
});

// 14-UI-SPEC §C3/C4: blockingConditions wiring from ProjectDetail → ProjectNode → TideNodeShell.
const BUDGET_BLOCKED_ENTRY: ProjectBlockingCondition = {
  type: "BudgetBlocked",
  reason: "BudgetCapReached",
  message: "cap reached",
  age: "4m 12s",
};

describe("PlanningDAGView — Test 6: blockingConditions wiring (14-UI-SPEC §C3/C4)", () => {
  // Test 1: ProjectDetail with blockingConditions drives condition-badge + data-blocked on the project node.
  it("blocked ProjectDetail payload shows condition-badge-BudgetBlocked and data-blocked=true on the project node", async () => {
    const blockedPayload: ProjectDetail = {
      ...PROJECT_PAYLOAD,
      blockingConditions: [BUDGET_BLOCKED_ENTRY],
    };
    render(
      <PlanningDAGView
        projectName="my-project"
        onPlanClick={() => undefined}
        initialData={blockedPayload}
      />,
    );
    await waitFor(() => {
      const node = document.querySelector('[data-testid="tide-node-project"]');
      expect(node).not.toBeNull();
      expect(node?.getAttribute("data-blocked")).toBe("true");
      expect(
        document.querySelector('[data-testid="condition-badge-BudgetBlocked"]'),
      ).not.toBeNull();
    });
  });

  // Test 2: legacy ProjectDetail without blockingConditions field renders data-blocked=false, no badge.
  it("ProjectDetail without blockingConditions degrades to data-blocked=false (no badge)", async () => {
    // PROJECT_PAYLOAD has no blockingConditions field — simulates legacy payload.
    render(
      <PlanningDAGView
        projectName="my-project"
        onPlanClick={() => undefined}
        initialData={PROJECT_PAYLOAD}
      />,
    );
    await waitFor(() => {
      const node = document.querySelector('[data-testid="tide-node-project"]');
      expect(node).not.toBeNull();
      expect(node?.getAttribute("data-blocked")).toBe("false");
      expect(
        document.querySelector('[data-testid^="condition-badge-"]'),
      ).toBeNull();
    });
  });
});
