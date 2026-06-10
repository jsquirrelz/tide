import { describe, it, expect, vi, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ReactFlowProvider } from "@xyflow/react";
import { ToastProvider } from "../ToastContainer";

import ProjectNode from "../ProjectNode";
import MilestoneNode from "../MilestoneNode";
import PhaseNode from "../PhaseNode";
import PlanNode from "../PlanNode";
import TaskNode from "../TaskNode";
import { NodeClickContext } from "../NodeClickContext";

afterEach(() => cleanup());

/**
 * Build a faux NodeProps object that satisfies @xyflow/react's NodeProps shape
 * for unit-test purposes — we only exercise the rendered JSX, not any
 * @xyflow store lifecycle.
 *
 * The NodeProps type from @xyflow/react has many internal fields (id,
 * positionAbsoluteX/Y, dragging, etc.) that the leaf component does not need.
 * We cast via `as never` (preserving the data + selected props only) so the
 * test stays focused on the rendered output rather than the @xyflow runtime
 * contract.
 */
function makeProps<T>(data: T, selected = false) {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return { id: "x", data, selected, type: "task", dragging: false } as any;
}

function renderWithCtx(node: React.ReactNode, onClick: (name: string) => void = () => undefined) {
  // <Handle> inside TideNodeShell reads the React Flow zustand store, so the
  // node tree must mount under a <ReactFlowProvider> even in isolation.
  return render(
    <ReactFlowProvider>
      <ToastProvider>
        <NodeClickContext.Provider value={onClick}>{node}</NodeClickContext.Provider>
      </ToastProvider>
    </ReactFlowProvider>,
  );
}

describe("Custom Nodes — Test 1: 5 node kinds render with kind-icon + StatusBadge + summary line (UI-SPEC §5)", () => {
  it("ProjectNode renders Layers icon + StatusBadge + summary line", () => {
    renderWithCtx(
      <ProjectNode
        {...makeProps({
          name: "my-project",
          status: "Running",
          milestonesCount: 3,
          phasesCount: 12,
          plansCount: 24,
        })}
      />,
    );
    // (a) kind icon
    expect(document.querySelector('[data-icon="Layers"]')).not.toBeNull();
    // (b) StatusBadge inline
    expect(screen.getByTestId("status-badge-Running")).toBeInTheDocument();
    // (c) summary line per UI-SPEC §5 — "3 milestones · 12 phases · 24 plans"
    expect(screen.getByText("3 milestones · 12 phases · 24 plans")).toBeInTheDocument();
    // header label "project/<name>" mono
    expect(screen.getByText("project/my-project")).toBeInTheDocument();
  });

  it("MilestoneNode renders Flag icon + StatusBadge + summary line", () => {
    renderWithCtx(
      <MilestoneNode
        {...makeProps({
          name: "ship-v1",
          status: "Pending",
          phasesCount: 2,
          plansCount: 6,
        })}
      />,
    );
    expect(document.querySelector('[data-icon="Flag"]')).not.toBeNull();
    expect(screen.getByTestId("status-badge-Pending")).toBeInTheDocument();
    expect(screen.getByText("2 phases · 6 plans")).toBeInTheDocument();
    expect(screen.getByText("ship-v1")).toBeInTheDocument();
  });

  it("PhaseNode renders Compass icon + StatusBadge + summary line", () => {
    renderWithCtx(
      <PhaseNode
        {...makeProps({
          name: "04-dashboard",
          status: "Succeeded",
          plansCount: 6,
        })}
      />,
    );
    expect(document.querySelector('[data-icon="Compass"]')).not.toBeNull();
    expect(screen.getByTestId("status-badge-Succeeded")).toBeInTheDocument();
    expect(screen.getByText("6 plans")).toBeInTheDocument();
    expect(screen.getByText("04-dashboard")).toBeInTheDocument();
  });

  it("PlanNode renders ListTree icon + StatusBadge + click affordance summary", () => {
    renderWithCtx(
      <PlanNode
        {...makeProps({
          name: "04-13",
          status: "Dispatching",
        })}
      />,
    );
    expect(document.querySelector('[data-icon="ListTree"]')).not.toBeNull();
    expect(screen.getByTestId("status-badge-Dispatching")).toBeInTheDocument();
    // PlanNode shows a click affordance, not fake task/wave counts — the
    // Planning DAG payload carries no counts (Execution pane owns them).
    expect(screen.getByText("view execution →")).toBeInTheDocument();
    expect(screen.getByText("04-13")).toBeInTheDocument();
  });

  it("TaskNode renders SquareTerminal icon + StatusBadge + summary line", () => {
    renderWithCtx(
      <TaskNode
        {...makeProps({
          name: "t1",
          status: "Running",
          waveIndex: 2,
          attempt: 1,
        })}
      />,
    );
    expect(document.querySelector('[data-icon="SquareTerminal"]')).not.toBeNull();
    expect(screen.getByTestId("status-badge-Running")).toBeInTheDocument();
    expect(screen.getByText("wave 2 · attempt 1")).toBeInTheDocument();
    expect(screen.getByText("t1")).toBeInTheDocument();
  });
});

describe("Custom Nodes — Test 2: selection ring (UI-SPEC §5 selected = 2px accent ring)", () => {
  it("selected:true on TaskNode adds the 2px accent ring class", () => {
    const { container } = renderWithCtx(
      <TaskNode
        {...makeProps(
          { name: "t1", status: "Pending", waveIndex: 0, attempt: 0 },
          true,
        )}
      />,
    );
    const root = container.querySelector('[data-testid="tide-node-task"]')!;
    expect(root.getAttribute("data-selected")).toBe("true");
    // The 2px accent ring is expressed via Tailwind ring-2 + ring-[var(--color-accent)]
    expect(root.className).toMatch(/ring-2/);
    expect(root.className).toMatch(/ring-\[var\(--color-accent\)\]/);
  });

  it("selected:false on TaskNode does NOT include the accent ring class", () => {
    const { container } = renderWithCtx(
      <TaskNode
        {...makeProps(
          { name: "t1", status: "Pending", waveIndex: 0, attempt: 0 },
          false,
        )}
      />,
    );
    const root = container.querySelector('[data-testid="tide-node-task"]')!;
    expect(root.getAttribute("data-selected")).toBe("false");
    expect(root.className).not.toMatch(/ring-2 /);
  });
});

describe("Custom Nodes — Test 3: failed border (UI-SPEC §5 failed family = 4px destructive border-left)", () => {
  it.each(["Failed", "PushLeakBlocked", "Rejected"] as const)(
    "status=%s adds the 4px destructive border-left",
    (status) => {
      const { container } = renderWithCtx(
        <TaskNode
          {...makeProps({
            name: "t1",
            status,
            waveIndex: 0,
            attempt: 0,
          })}
        />,
      );
      const root = container.querySelector('[data-testid="tide-node-task"]')!;
      expect(root.getAttribute("data-failed")).toBe("true");
      expect(root.className).toMatch(/border-l-4/);
      expect(root.className).toMatch(/border-l-\[var\(--color-destructive\)\]/);
    },
  );

  it("status=Succeeded does NOT add the destructive border", () => {
    const { container } = renderWithCtx(
      <TaskNode
        {...makeProps({
          name: "t1",
          status: "Succeeded",
          waveIndex: 0,
          attempt: 0,
        })}
      />,
    );
    const root = container.querySelector('[data-testid="tide-node-task"]')!;
    expect(root.getAttribute("data-failed")).toBe("false");
    expect(root.className).not.toMatch(/border-l-4/);
  });
});

describe("Custom Nodes — Test 4: click handler via NodeClickContext", () => {
  it("clicking a TaskNode invokes the onClick callback with the node's name", () => {
    const onClick = vi.fn();
    renderWithCtx(
      <TaskNode
        {...makeProps({
          name: "t1",
          status: "Running",
          waveIndex: 0,
          attempt: 0,
        })}
      />,
      onClick,
    );
    fireEvent.click(screen.getByTestId("tide-node-task"));
    expect(onClick).toHaveBeenCalledWith("t1");
  });

  it("clicking a PlanNode invokes the onClick callback with the plan's name", () => {
    const onClick = vi.fn();
    renderWithCtx(
      <PlanNode
        {...makeProps({
          name: "04-13",
          status: "Running",
        })}
      />,
      onClick,
    );
    fireEvent.click(screen.getByTestId("tide-node-plan"));
    expect(onClick).toHaveBeenCalledWith("04-13");
  });
});

describe("Custom Nodes — Test 5: Accessibility (role=button, tabIndex=0, Enter activates)", () => {
  it("TaskNode has role=button and tabIndex=0", () => {
    renderWithCtx(
      <TaskNode
        {...makeProps({
          name: "t1",
          status: "Pending",
          waveIndex: 0,
          attempt: 0,
        })}
      />,
    );
    const root = screen.getByTestId("tide-node-task");
    expect(root.getAttribute("role")).toBe("button");
    expect(root.getAttribute("tabindex")).toBe("0");
  });

  it("pressing Enter on a focused TaskNode fires the click handler", () => {
    const onClick = vi.fn();
    renderWithCtx(
      <TaskNode
        {...makeProps({
          name: "t1",
          status: "Pending",
          waveIndex: 0,
          attempt: 0,
        })}
      />,
      onClick,
    );
    const root = screen.getByTestId("tide-node-task");
    fireEvent.keyDown(root, { key: "Enter" });
    expect(onClick).toHaveBeenCalledWith("t1");
  });

  it("TaskNode aria-label contains kind + name + status per UI-SPEC §Accessibility", () => {
    renderWithCtx(
      <TaskNode
        {...makeProps({
          name: "t1",
          status: "Running",
          waveIndex: 0,
          attempt: 0,
        })}
      />,
    );
    const root = screen.getByTestId("tide-node-task");
    const label = root.getAttribute("aria-label") || "";
    expect(label).toMatch(/task/i);
    expect(label).toMatch(/t1/);
    expect(label).toMatch(/Running/);
  });
});
