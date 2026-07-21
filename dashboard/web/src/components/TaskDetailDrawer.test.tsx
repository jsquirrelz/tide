/*
 * TaskDetailDrawer.test.tsx (plan 53-08 Task 1, OBS-04)
 *
 *   NEW test file covering the D-07/D-08 Verification depth added to the
 *   drawer per 53-UI-SPEC.md Component Contracts 1/2/4:
 *     (a) the Attempt/Iteration firewall — both rows render simultaneously
 *         with independent values, sourced from independent fields.
 *     (b) section eligibility — no verification contract renders no section.
 *     (c) the "No verdict yet" placeholder when lastEvaluation is absent.
 *     (d) actionsForStatus arms for Verifying/VerifyHalted.
 *     (e) the findings disclosure's aria-expanded toggle wiring, driven by a
 *         mocked fetchNodeArtifacts (no real backend).
 *
 *   The pre-existing open/close/focus-trap/metadata/actions/Phoenix-link
 *   coverage lives in components/__tests__/drawer.test.tsx — not duplicated
 *   here.
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";

// Mock the fetch layer BEFORE importing the component so vi intercepts it.
vi.mock("../lib/api", () => ({
  fetchNodeArtifacts: (...args: unknown[]) => mockFetchArtifacts(...args),
}));

const mockFetchArtifacts = vi.fn();

import TaskDetailDrawer, { type TaskDetailData } from "./TaskDetailDrawer";
import { ToastProvider } from "./ToastContainer";

function renderWithToast(node: React.ReactNode) {
  return render(<ToastProvider>{node}</ToastProvider>);
}

function ensureClipboardStub() {
  if (!("clipboard" in navigator)) {
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: async (_: string) => undefined },
      configurable: true,
      writable: true,
    });
  }
}

afterEach(() => {
  cleanup();
  mockFetchArtifacts.mockReset();
  vi.restoreAllMocks();
});

const BASE_TASK: TaskDetailData = {
  name: "t1",
  projectName: "my-project",
  planName: "53-08",
  status: "Running",
  namespace: "default",
  attempt: 1,
  attemptMax: 3,
  podName: "tide-task-t1-abc",
  exitCode: null,
  waveIndex: 1,
  scheduledAt: "2026-07-21 03:00:00 UTC",
  envelopePath: "/var/lib/tide/envelopes/t1-1.json",
  elapsedText: "4m 12s",
  conditions: [],
};

describe("TaskDetailDrawer — Verification section eligibility + firewall (53-UI-SPEC Contract 1)", () => {
  it("(a) Attempt row and Iteration row render simultaneously with independent values", () => {
    const task: TaskDetailData = {
      ...BASE_TASK,
      attempt: 1,
      attemptMax: 3,
      hasVerification: true,
      loopIteration: 2,
      verifyMaxIterations: 2,
      lastEvaluation: { decision: "REPAIRABLE", findingsCount: 3, highSeverityCount: 1 },
    };
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={task}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );

    // The EXISTING Attempt row (Caps.Iterations source) — untouched.
    const metadata = screen.getByTestId("drawer-metadata");
    expect(metadata).toHaveTextContent(/attempt/i);
    expect(metadata.textContent).toMatch(/1 of 3/);

    // The NEW Iteration row (spec.verification.maxIterations source) —
    // independent value, same render pass.
    const verification = screen.getByTestId("drawer-verification");
    expect(verification.textContent).toMatch(/2 of 2/);
    expect(verification).toHaveTextContent(/REPAIRABLE/);
    expect(verification).toHaveTextContent(/3 total.*1 high/);
  });

  it("(b) renders no section at all when the task carries no verification contract", () => {
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={BASE_TASK}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );
    expect(screen.queryByTestId("drawer-verification")).not.toBeInTheDocument();
  });

  it("(b) hasVerification: false also renders no section", () => {
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={{ ...BASE_TASK, hasVerification: false }}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );
    expect(screen.queryByTestId("drawer-verification")).not.toBeInTheDocument();
  });

  it("renders an em-dash denominator (never 'of 0') when the effective bound is unknown", () => {
    // WR-01: pre-engagement (or a pre-Phase-53 object) the server has no
    // stamped effective bound and omits verifyMaxIterations — the drawer
    // must render "1 of —", never the wrong "1 of 0".
    const task: TaskDetailData = {
      ...BASE_TASK,
      hasVerification: true,
      loopIteration: 1,
      lastEvaluation: { decision: "REPAIRABLE", findingsCount: 1, highSeverityCount: 0 },
    };
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={task}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );
    const verification = screen.getByTestId("drawer-verification");
    expect(verification.textContent).toMatch(/1 of —/);
    expect(verification.textContent).not.toMatch(/1 of 0/);
  });

  it("(c) renders 'No verdict yet' when lastEvaluation is absent", () => {
    const task: TaskDetailData = {
      ...BASE_TASK,
      hasVerification: true,
      loopIteration: 1,
      verifyMaxIterations: 3,
    };
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={task}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );
    const verification = screen.getByTestId("drawer-verification");
    expect(verification).toHaveTextContent("No verdict yet");
    // Findings falls to the locked "—" placeholder when there's no evaluation.
    expect(verification.textContent).toMatch(/Findings[\s\S]*—/);
  });
});

describe("TaskDetailDrawer — actionsForStatus arms for Verifying/VerifyHalted (53-UI-SPEC Contract 4)", () => {
  it("(d) VerifyHalted leads with primary Resume copying `tide resume {project}`, then secondary Inspect wave", async () => {
    ensureClipboardStub();
    const spy = vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue(undefined);

    const task: TaskDetailData = { ...BASE_TASK, status: "VerifyHalted" };
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={task}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );

    const actionsRow = screen.getByTestId("drawer-actions");
    const buttons = within(actionsRow).getAllByRole("button");
    expect(buttons[0]).toHaveTextContent(/^Resume$/i);
    expect(buttons[0]).toHaveAttribute("data-variant", "primary");
    expect(buttons[1]).toHaveTextContent(/inspect wave/i);
    expect(buttons[1]).toHaveAttribute("data-variant", "secondary");

    // No approve/reject buttons for VerifyHalted (mutation-free surface).
    expect(within(actionsRow).queryByRole("button", { name: /approve/i })).toBeNull();
    expect(within(actionsRow).queryByRole("button", { name: /reject/i })).toBeNull();

    await act(async () => {
      fireEvent.click(buttons[0]);
    });
    expect(spy).toHaveBeenCalledWith("tide resume my-project");
  });

  it("(d) Verifying mirrors Running's arm — Cancel (destructive) + Tail logs (CLI) (secondary)", () => {
    const task: TaskDetailData = { ...BASE_TASK, status: "Verifying" };
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={task}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );
    const actionsRow = screen.getByTestId("drawer-actions");
    const buttons = within(actionsRow).getAllByRole("button");
    expect(buttons[0]).toHaveTextContent(/^Cancel$/i);
    expect(buttons[0]).toHaveAttribute("data-variant", "destructive");
    expect(buttons[1]).toHaveTextContent(/tail logs \(CLI\)/i);
    expect(buttons[1]).toHaveAttribute("data-variant", "secondary");
  });
});

describe("TaskDetailDrawer — findings disclosure (53-UI-SPEC Contract 2)", () => {
  const TASK_WITH_VERIFICATION: TaskDetailData = {
    ...BASE_TASK,
    hasVerification: true,
    loopIteration: 1,
    verifyMaxIterations: 3,
    lastEvaluation: { decision: "BLOCKED", findingsCount: 2, highSeverityCount: 2 },
  };

  it("(e) toggles aria-expanded + fetches via fetchNodeArtifacts('task', ...) only once opened", async () => {
    mockFetchArtifacts.mockResolvedValue({
      state: "available",
      files: [
        {
          name: "findings.json",
          path: "findings.json",
          content: '{"blockers":1}',
          sizeBytes: 14,
        },
      ],
    });

    // A NON-default namespace: the fetch must thread task.namespace as the
    // 4th argument (CR-01 — the backend defaults a missing namespace param
    // to "default", 404ing every non-default-namespace install).
    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={{ ...TASK_WITH_VERIFICATION, namespace: "proj-ns" }}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );

    const toggle = screen.getByTestId("drawer-findings-toggle");
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(mockFetchArtifacts).not.toHaveBeenCalled();

    await act(async () => {
      fireEvent.click(toggle);
    });

    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(mockFetchArtifacts).toHaveBeenCalledWith("task", "t1", "my-project", "proj-ns");

    const content = await screen.findByTestId("findings-state-available");
    expect(content.textContent).toContain('"blockers": 1');

    // Toggling again collapses the disclosure.
    fireEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByTestId("drawer-findings-content")).not.toBeInTheDocument();
  });

  it("renders the locked absent-state copy verbatim", async () => {
    mockFetchArtifacts.mockResolvedValue({ state: "absent", files: [] });

    renderWithToast(
      <TaskDetailDrawer
        taskName="t1"
        task={TASK_WITH_VERIFICATION}
        onClose={() => undefined}
        onOpenLogStream={() => undefined}
      />,
    );

    fireEvent.click(screen.getByTestId("drawer-findings-toggle"));

    await waitFor(() => {
      expect(screen.getByTestId("findings-state-absent")).toBeInTheDocument();
    });
    expect(screen.getByText("No findings staged yet")).toBeInTheDocument();
    expect(
      screen.getByText(
        "Findings land on the run branch after a verifier attempt completes. Check again after the current iteration finishes.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /check again/i })).toBeInTheDocument();
  });
});
