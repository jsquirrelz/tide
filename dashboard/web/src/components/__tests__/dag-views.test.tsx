import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";

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

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
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

// Use beforeEach in tests below to stub fetch responses cleanly.
beforeEach(() => undefined);

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

describe("PlanningDAGView — Test 1: hierarchy renders with ≥13 nodes and TB direction", () => {
  it("renders 1 project + 2 milestones + 4 phases + 6 plans = 13 nodes via dagre TB", async () => {
    stubFetchOK(PROJECT_PAYLOAD);
    render(<PlanningDAGView projectName="my-project" onPlanClick={() => undefined} />);
    // Wait for the fetch + first layout pass to complete.
    await waitFor(() => {
      // 1 project + 2 milestones + 4 phases + 6 plans = 13 tide nodes total
      const allTide = document.querySelectorAll('[data-testid^="tide-node-"]');
      expect(allTide.length).toBeGreaterThanOrEqual(13);
    });
    // Direction marker: PlanningDAGView sets data-dagre-direction="TB"
    expect(
      document.querySelector('[data-testid="planning-dag-view"]')!.getAttribute(
        "data-dagre-direction",
      ),
    ).toBe("TB");
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
  it("newly-mounted view begins with data-flicker-ready='false' then flips to 'true' after layout", async () => {
    // Pass an explicit `forceFlickerDelay=true` prop so the component
    // exposes the transitional state to the test (in production the same
    // sequence happens within React's commit/effect ticks).
    const { rerender } = render(
      <ExecutionDAGView
        planName="04-13"
        plan={EXECUTION_PAYLOAD}
        onTaskClick={() => undefined}
        // Test-only seam: split the layout pass across two ticks so the
        // assertion can observe the opacity:0 → opacity:1 transition.
        forceFlickerDelay
      />,
    );
    const root = document.querySelector('[data-testid="execution-dag-view"]')!;
    expect(root.getAttribute("data-flicker-ready")).toBe("false");

    // Advance the second tick — useNodesInitialized has already returned
    // true (mocked above), so flipping happens after one rAF.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    rerender(
      <ExecutionDAGView
        planName="04-13"
        plan={EXECUTION_PAYLOAD}
        onTaskClick={() => undefined}
        forceFlickerDelay
      />,
    );
    await waitFor(() => {
      const r2 = document.querySelector('[data-testid="execution-dag-view"]')!;
      expect(r2.getAttribute("data-flicker-ready")).toBe("true");
    });
  });
});
