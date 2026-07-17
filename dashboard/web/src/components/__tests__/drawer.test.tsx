import { afterEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
} from "@testing-library/react";

import TaskDetailDrawer, {
  type TaskDetailData,
} from "../TaskDetailDrawer";
import { ToastProvider } from "../ToastContainer";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function renderWithToast(node: React.ReactNode) {
  return render(<ToastProvider>{node}</ToastProvider>);
}

const TASK_AWAITING: TaskDetailData = {
  name: "t1",
  projectName: "my-project",
  planName: "04-13",
  status: "AwaitingApproval",
  namespace: "default",
  attempt: 2,
  attemptMax: 3,
  podName: "tide-task-t1-abc",
  exitCode: null,
  waveIndex: 1,
  scheduledAt: "2026-05-16 14:32:01 UTC",
  envelopePath: "/var/lib/tide/envelopes/t1-1.json",
  elapsedText: "4m 12s",
  conditions: [{ type: "Approved", reason: "WaitingForHuman", age: "1m" }],
};

const TASK_RUNNING: TaskDetailData = {
  ...TASK_AWAITING,
  status: "Running",
  attempt: 1,
};

const TASK_FAILED: TaskDetailData = {
  ...TASK_AWAITING,
  status: "Failed",
  exitCode: 1,
};

describe("TaskDetailDrawer — Test 6: open/close + focus trap", () => {
  it("renders the slide-in panel with role=dialog when taskName is non-null", () => {
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_AWAITING}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );
    const dialog = screen.getByRole("dialog");
    expect(dialog).toBeInTheDocument();
    expect(dialog.getAttribute("aria-modal")).toBe("true");
  });

  it("renders nothing when taskName is null (closed state)", () => {
    renderWithToast(
      <TaskDetailDrawer
        taskName={null}
        task={null}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("clicking the backdrop fires onClose", () => {
    const onClose = vi.fn();
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_AWAITING}
        onClose={onClose}
        onOpenLogStream={() => undefined}
      />,
    );
    fireEvent.click(screen.getByTestId("drawer-backdrop"));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("pressing Escape fires onClose", () => {
    const onClose = vi.fn();
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_AWAITING}
        onClose={onClose}
        onOpenLogStream={() => undefined}
      />,
    );
    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it("clicking the X button fires onClose", () => {
    const onClose = vi.fn();
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_AWAITING}
        onClose={onClose}
        onOpenLogStream={() => undefined}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /close/i }));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

describe("TaskDetailDrawer — Test 7: drawer content (header, status row, metadata grid)", () => {
  it("header shows 'task/<name>' and a close button", () => {
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_AWAITING}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );
    expect(screen.getByText("task/t1")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /close/i })).toBeInTheDocument();
  });

  it("status row shows StatusBadge + elapsed time mono text", () => {
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_RUNNING}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );
    expect(screen.getByTestId("status-badge-Running")).toBeInTheDocument();
    expect(screen.getByText(/4m 12s/)).toBeInTheDocument();
  });

  it("metadata grid shows namespace, attempt, podName, exitCode, waveIndex, scheduledAt, envelopePath", () => {
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_FAILED}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );
    // Look for the field-label tokens rather than the values so the test
    // does not depend on the specific values besides the metadata existing.
    // Use the metadata grid scope to avoid matching the "Inspect wave"
    // button label in the Actions row.
    const grid = screen.getByTestId("drawer-metadata");
    expect(grid).toHaveTextContent(/namespace/i);
    expect(grid).toHaveTextContent(/attempt/i);
    expect(grid).toHaveTextContent(/pod/i);
    expect(grid).toHaveTextContent(/exit code/i);
    expect(grid).toHaveTextContent(/wave index/i);
    expect(grid).toHaveTextContent(/scheduled/i);
    expect(grid).toHaveTextContent(/envelope/i);
    // Verify the failed task's exitCode = 1 renders inside the grid (not
    // any other "1" in the document — e.g. attempt "1 of 3" if defaults
    // change).
    expect(grid.textContent).toMatch(/exit code\D*1/i);
  });
});

describe("TaskDetailDrawer — Test 8: Actions row by status (UI-SPEC §10 Locked button copy)", () => {
  it("AwaitingApproval renders Approve + Reject buttons with the locked commands", async () => {
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_AWAITING}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );
    expect(screen.getByRole("button", { name: /^approve$/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^reject$/i })).toBeInTheDocument();
    // Variant inspection: data-variant attribute is the lookup the
    // ClipboardCopyAction component sets.
    expect(
      screen.getByTestId("clipboard-copy-primary").textContent,
    ).toMatch(/approve/i);
    expect(
      screen.getByTestId("clipboard-copy-destructive").textContent,
    ).toMatch(/reject/i);
  });

  it("Running renders Cancel (destructive) + Tail logs (CLI) (secondary)", () => {
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_RUNNING}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );
    expect(screen.getByRole("button", { name: /cancel/i })).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /tail logs \(CLI\)/i }),
    ).toBeInTheDocument();
  });

  it("Failed renders Retry push + Cancel + Inspect wave", () => {
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_FAILED}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );
    expect(screen.getByRole("button", { name: /retry push/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /cancel/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /inspect wave/i })).toBeInTheDocument();
  });
});

describe("TaskDetailDrawer — Test 9: 'Open log stream' button is wired to onOpenLogStream", () => {
  it("renders the Open log stream button + invokes the callback with taskName", async () => {
    const onOpenLogStream = vi.fn();
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_RUNNING}
        onClose={() => undefined}
        onOpenLogStream={onOpenLogStream}
      />,
    );
    const btn = screen.getByRole("button", { name: /open log stream/i });
    await act(async () => {
      fireEvent.click(btn);
    });
    expect(onOpenLogStream).toHaveBeenCalledWith("t1");
  });
});

describe("TaskDetailDrawer — Test 10: PhoenixTraceLink deep link (plan 46-05, OBS-04)", () => {
  const TASK_WITH_TRACE: TaskDetailData = {
    ...TASK_RUNNING,
    traceId: "trace123",
    traceSpanId: "span456",
  };

  it("renders the link with correct href/target/rel when baseURL+spanId present", () => {
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_WITH_TRACE}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
        phoenixBaseURL="http://phoenix:6006"
      />,
    );
    const link = screen.getByTestId("phoenix-trace-link");
    expect(link).toHaveAttribute(
      "href",
      "http://phoenix:6006/redirects/spans/span456",
    );
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
    expect(link).toHaveTextContent("View trace in Phoenix");
    expect(link).toHaveAttribute(
      "aria-label",
      "View trace in Phoenix — opens in new tab",
    );
  });

  it("renders nothing when phoenixBaseURL is absent", () => {
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_WITH_TRACE}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );
    expect(screen.queryByTestId("phoenix-trace-link-row")).toBeNull();
  });

  it("renders nothing when traceSpanId is absent", () => {
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_RUNNING}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
        phoenixBaseURL="http://phoenix:6006"
      />,
    );
    expect(screen.queryByTestId("phoenix-trace-link-row")).toBeNull();
  });

  it("places the link row after the metadata grid, before the Actions row", () => {
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_WITH_TRACE}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
        phoenixBaseURL="http://phoenix:6006"
      />,
    );
    const dialog = screen.getByRole("dialog");
    const children = Array.from(dialog.children);
    const metaIdx = children.findIndex(
      (el) => el.getAttribute("data-testid") === "drawer-metadata",
    );
    const linkRowIdx = children.findIndex(
      (el) => el.getAttribute("data-testid") === "phoenix-trace-link-row",
    );
    const actionsIdx = children.findIndex(
      (el) => el.getAttribute("data-testid") === "drawer-actions",
    );
    expect(metaIdx).toBeGreaterThanOrEqual(0);
    expect(linkRowIdx).toBeGreaterThan(metaIdx);
    expect(actionsIdx).toBeGreaterThan(linkRowIdx);
  });
});
