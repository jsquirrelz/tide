import { describe, it, expect, vi, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { ReactFlowProvider } from "@xyflow/react";
import { Layers } from "lucide-react";
import { ToastProvider } from "../ToastContainer";

import ProjectNode from "../ProjectNode";
import MilestoneNode from "../MilestoneNode";
import PhaseNode from "../PhaseNode";
import PlanNode from "../PlanNode";
import TaskNode from "../TaskNode";
import TideNodeShell from "../TideNodeShell";
import { NodeClickContext } from "../NodeClickContext";
import type { ProjectBlockingCondition } from "../ConditionBadge";

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

// 14-UI-SPEC §C2: TideNodeShell blocking-conditions slot.
// Tests exercise TideNodeShell directly (via the "project" kind) so they don't
// depend on Task 3's ProjectNode passthrough. The existing ProjectNode/TaskNode
// tests above remain the regression suite for the node hierarchy.
const BUDGET_BLOCKED_CONDITION: ProjectBlockingCondition = {
  type: "BudgetBlocked",
  reason: "BudgetCapReached",
  message: "Cost spent 10100 cents exceeds cap 10000 cents; dispatch halted project-wide",
  age: "4m 12s",
};

const BILLING_HALT_CONDITION: ProjectBlockingCondition = {
  type: "BillingHalt",
  reason: "InsufficientCredits",
  message: "Provider credit balance too low; dispatch halted project-wide",
  age: "2m 0s",
};

function renderShell(props: Partial<React.ComponentProps<typeof TideNodeShell>> & {
  name?: string;
  status?: React.ComponentProps<typeof TideNodeShell>["status"];
}) {
  return renderWithCtx(
    <TideNodeShell
      kind="project"
      name={props.name ?? "alpha"}
      headerLabel={props.headerLabel ?? `project/${props.name ?? "alpha"}`}
      status={props.status ?? "Running"}
      icon={Layers}
      iconName="Layers"
      summary="1 milestone"
      width={360}
      minHeight={92}
      clickable={false}
      blockingConditions={props.blockingConditions}
    />,
  );
}

describe("Custom Nodes — Test 6: blocking-conditions slot (14-UI-SPEC §C2)", () => {
  // Test 1: blocked node gets ConditionBadge in summary row, data-blocked=true, purple border.
  it("blockingConditions=[BudgetBlocked] renders condition-badge-BudgetBlocked, data-blocked=true, purple border", () => {
    const { container } = renderShell({
      blockingConditions: [BUDGET_BLOCKED_CONDITION],
    });
    const root = container.querySelector('[data-testid="tide-node-project"]')!;

    // data-blocked attribute
    expect(root.getAttribute("data-blocked")).toBe("true");

    // Purple border classes (14-UI-SPEC §C2 — readable at DAG zoom)
    expect(root.className).toMatch(/border-l-4/);
    expect(root.className).toMatch(/border-l-\[var\(--color-status-blocked\)\]/);

    // ConditionBadge inside the node
    const badge = container.querySelector('[data-testid="condition-badge-BudgetBlocked"]');
    expect(badge).not.toBeNull();
  });

  // Test 2: destructive precedence — Failed + blocked → red border wins, data-blocked still true.
  it("status=Failed + blockingConditions=[BudgetBlocked] → red border-l, no purple border-l, data-blocked=true", () => {
    const { container } = renderShell({
      status: "Failed",
      blockingConditions: [BUDGET_BLOCKED_CONDITION],
    });
    const root = container.querySelector('[data-testid="tide-node-project"]')!;

    // Still data-blocked=true (the condition exists)
    expect(root.getAttribute("data-blocked")).toBe("true");

    // Destructive border wins
    expect(root.className).toMatch(/border-l-\[var\(--color-destructive\)\]/);

    // Purple border must NOT be present (destructive takes precedence per 14-UI-SPEC §C2)
    expect(root.className).not.toMatch(/border-l-\[var\(--color-status-blocked\)\]/);
  });

  // Test 3: zero conditions → data-blocked=false, no border-l-4, no condition badge,
  //         and all pre-existing specs pass unmodified.
  it("blockingConditions omitted → data-blocked=false, no border-l-4, no condition badge", () => {
    const { container } = renderShell({});
    const root = container.querySelector('[data-testid="tide-node-project"]')!;

    expect(root.getAttribute("data-blocked")).toBe("false");
    expect(root.className).not.toMatch(/border-l-4/);
    expect(
      container.querySelector('[data-testid^="condition-badge-"]'),
    ).toBeNull();
  });

  // Test 4: aria-label extends to include blocked labels when blocked.
  it("aria-label extends to '... blocked: Budget blocked' when BudgetBlocked condition present", () => {
    const { container } = renderShell({
      blockingConditions: [BUDGET_BLOCKED_CONDITION],
    });
    const root = container.querySelector('[data-testid="tide-node-project"]')!;
    const label = root.getAttribute("aria-label") ?? "";
    expect(label).toMatch(/blocked: Budget blocked/);
    // Must still contain the base kind + name + status
    expect(label).toMatch(/project/);
    expect(label).toMatch(/alpha/);
    expect(label).toMatch(/Running/);
  });

  // Test 5: two conditions render two badges in order.
  it("blockingConditions=[BudgetBlocked, BillingHalt] renders both badges", () => {
    const { container } = renderShell({
      blockingConditions: [BUDGET_BLOCKED_CONDITION, BILLING_HALT_CONDITION],
    });
    expect(
      container.querySelector('[data-testid="condition-badge-BudgetBlocked"]'),
    ).not.toBeNull();
    expect(
      container.querySelector('[data-testid="condition-badge-BillingHalt"]'),
    ).not.toBeNull();

    // aria-label lists both, comma-separated
    const root = container.querySelector('[data-testid="tide-node-project"]')!;
    const label = root.getAttribute("aria-label") ?? "";
    expect(label).toMatch(/blocked: Budget blocked, Billing halted/);
  });

  // Test 6: unknown-type-only payload (the vocabulary-drift scenario the
  // whitelist defends against) drives NO blocked surface — no purple border,
  // no data-blocked, no badge, no aria-label suffix.
  it("blockingConditions with only an unknown type → data-blocked=false, no border-l-4, no badge, base aria-label", () => {
    const { container } = renderShell({
      blockingConditions: [
        {
          type: "QuotaExceeded",
          reason: "FutureVocabulary",
          message: "condition type the client does not know",
          age: "1m 0s",
        },
      ],
    });
    const root = container.querySelector('[data-testid="tide-node-project"]')!;

    expect(root.getAttribute("data-blocked")).toBe("false");
    expect(root.className).not.toMatch(/border-l-4/);
    expect(
      container.querySelector('[data-testid^="condition-badge-"]'),
    ).toBeNull();
    expect(root.getAttribute("aria-label")).not.toMatch(/blocked/);
  });

  // Test 7: unknown types mixed with known ones are filtered out everywhere —
  // the known condition still drives border/badge/aria, the unknown one is inert.
  it("blockingConditions=[unknown, BudgetBlocked] → blocked surfaces driven by BudgetBlocked only", () => {
    const { container } = renderShell({
      blockingConditions: [
        {
          type: "QuotaExceeded",
          reason: "FutureVocabulary",
          message: "condition type the client does not know",
          age: "1m 0s",
        },
        BUDGET_BLOCKED_CONDITION,
      ],
    });
    const root = container.querySelector('[data-testid="tide-node-project"]')!;

    expect(root.getAttribute("data-blocked")).toBe("true");
    expect(root.className).toMatch(/border-l-\[var\(--color-status-blocked\)\]/);
    expect(
      container.querySelector('[data-testid="condition-badge-BudgetBlocked"]'),
    ).not.toBeNull();
    expect(
      container.querySelector('[data-testid="condition-badge-QuotaExceeded"]'),
    ).toBeNull();
    const label = root.getAttribute("aria-label") ?? "";
    expect(label).toMatch(/blocked: Budget blocked$/);
  });
});
