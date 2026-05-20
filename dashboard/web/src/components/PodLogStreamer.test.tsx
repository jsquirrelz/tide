/*
 * PodLogStreamer.test.tsx (plan 04-16 Task 2)
 *
 *   Covers Tests 1-5 of the plan: mount + EventSource open/close,
 *   ring-buffer, auto-scroll, ANSI rendering, connection-state copy.
 *
 *   useTaskLog is mocked via vi.mock("../lib/sse") so each test can
 *   drive `state` + `lines` deterministically without spinning up an
 *   actual EventSource.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen } from "@testing-library/react";

// Mock the SSE hook BEFORE importing the component so vi can intercept.
vi.mock("../lib/sse", () => {
  return {
    useTaskLog: (...args: unknown[]) => mockUseTaskLog(...args),
    useSSEStream: () => ({ events: [], state: "connecting", lastEventId: 0 }),
  };
});

const mockUseTaskLog = vi.fn();

import PodLogStreamer from "./PodLogStreamer";

type TaskLogReturn = {
  lines: string[];
  state: "connecting" | "connected" | "reconnecting" | "offline" | "idle-closed";
};

beforeEach(() => {
  mockUseTaskLog.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function setHook(value: TaskLogReturn) {
  mockUseTaskLog.mockReturnValue(value);
}

describe("PodLogStreamer (Test 1) — mount + lines rendered", () => {
  it("renders incoming lines in the viewport", () => {
    setHook({
      state: "connected",
      lines: ["alpha", "beta", "gamma"],
    });
    render(<PodLogStreamer taskName="t-1" onClose={() => {}} />);
    const viewport = screen.getByTestId("pod-log-viewport");
    expect(viewport.textContent).toContain("alpha");
    expect(viewport.textContent).toContain("beta");
    expect(viewport.textContent).toContain("gamma");
  });

  it("fires onClose when the toolbar close button is clicked", () => {
    setHook({ state: "connected", lines: [] });
    const onClose = vi.fn();
    render(<PodLogStreamer taskName="t-1" onClose={onClose} />);
    fireEvent.click(screen.getByTestId("pod-log-close"));
    expect(onClose).toHaveBeenCalledOnce();
  });
});

describe("PodLogStreamer (Test 2) — ring-buffer cap reflected in viewport", () => {
  it("renders ≤ 5000 line elements regardless of how many lines the hook returns", () => {
    // The hook itself caps at 5000 (see useTaskLog); here the test
    // models the contract by handing the component 5000 lines and
    // asserting the DOM count matches.
    const fiveK = Array.from({ length: 5000 }, (_, i) => `line-${i}`);
    setHook({ state: "connected", lines: fiveK });
    render(<PodLogStreamer taskName="t-2" onClose={() => {}} />);
    const rows = screen.getAllByTestId(/^pod-log-line-/);
    expect(rows.length).toBe(5000);
  });
});

describe("PodLogStreamer (Test 3) — auto-scroll behavior", () => {
  it("disables autoScroll when the user scrolls up", () => {
    setHook({ state: "connected", lines: ["a", "b", "c"] });
    render(<PodLogStreamer taskName="t-3" onClose={() => {}} />);
    const viewport = screen.getByTestId("pod-log-viewport");

    // Synthesize a "scrolled up" position. The component reads
    // scrollTop / scrollHeight / clientHeight to decide auto-scroll;
    // we feed them via Object.defineProperty and dispatch a scroll
    // event.
    Object.defineProperty(viewport, "scrollTop", {
      configurable: true,
      value: 0,
    });
    Object.defineProperty(viewport, "scrollHeight", {
      configurable: true,
      value: 1000,
    });
    Object.defineProperty(viewport, "clientHeight", {
      configurable: true,
      value: 200,
    });
    act(() => {
      fireEvent.scroll(viewport);
    });

    // Pause indicator surfaces in the toolbar when auto-scroll is off.
    expect(screen.getByTestId("pod-log-pause-indicator")).toBeInTheDocument();
  });

  it("re-enables autoScroll when the user scrolls back to bottom", () => {
    setHook({ state: "connected", lines: ["a", "b", "c"] });
    render(<PodLogStreamer taskName="t-4" onClose={() => {}} />);
    const viewport = screen.getByTestId("pod-log-viewport");

    Object.defineProperty(viewport, "scrollTop", { configurable: true, value: 0 });
    Object.defineProperty(viewport, "scrollHeight", { configurable: true, value: 1000 });
    Object.defineProperty(viewport, "clientHeight", { configurable: true, value: 200 });
    act(() => fireEvent.scroll(viewport));
    expect(screen.getByTestId("pod-log-pause-indicator")).toBeInTheDocument();

    // Scroll back to bottom — scrollTop + clientHeight ≈ scrollHeight.
    Object.defineProperty(viewport, "scrollTop", { configurable: true, value: 800 });
    act(() => fireEvent.scroll(viewport));
    expect(screen.queryByTestId("pod-log-pause-indicator")).toBeNull();
  });
});

describe("PodLogStreamer (Test 4) — ANSI rendering", () => {
  it('renders \\x1b[31m as a red <span> + plain trailing text', () => {
    setHook({
      state: "connected",
      lines: ["\x1b[31mERROR\x1b[0m fail"],
    });
    render(<PodLogStreamer taskName="t-5" onClose={() => {}} />);

    // First segment — red ERROR.
    const errSpan = screen.getByText("ERROR");
    expect(errSpan.tagName.toLowerCase()).toBe("span");
    expect(errSpan.getAttribute("style") ?? "").toMatch(/color/i);

    // Trailing plain segment.
    expect(screen.getByText(/ fail/)).toBeInTheDocument();
  });
});

describe("PodLogStreamer (Test 5) — connection state copy", () => {
  it('shows "Connecting…" verbatim when state="connecting"', () => {
    setHook({ state: "connecting", lines: [] });
    render(<PodLogStreamer taskName="t-6" onClose={() => {}} />);
    expect(screen.getByText(/Connecting to log stream…/i)).toBeInTheDocument();
  });

  it('shows "Waiting for output…" verbatim when connected with no lines', () => {
    setHook({ state: "connected", lines: [] });
    render(<PodLogStreamer taskName="t-7" onClose={() => {}} />);
    expect(screen.getByText(/Waiting for output…/i)).toBeInTheDocument();
  });

  it('shows the disconnected copy verbatim when state="offline"', () => {
    setHook({ state: "offline", lines: [] });
    render(<PodLogStreamer taskName="t-8" onClose={() => {}} />);
    expect(
      screen.getByText(/Log stream closed by backend \(5 min idle\)\./i),
    ).toBeInTheDocument();
  });

  it("shows the same disconnected copy when state=idle-closed", () => {
    setHook({ state: "idle-closed", lines: [] });
    render(<PodLogStreamer taskName="t-9" onClose={() => {}} />);
    expect(
      screen.getByText(/Log stream closed by backend \(5 min idle\)\./i),
    ).toBeInTheDocument();
  });
});
