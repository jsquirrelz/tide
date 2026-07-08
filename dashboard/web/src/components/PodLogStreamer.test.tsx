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

type TaskLogState =
  | "connecting"
  | "connected"
  | "reconnecting"
  | "offline"
  | "idle-closed"
  | "pod-gone"
  | "stream-error";

type TaskLogReturn = {
  lines: string[];
  state: TaskLogState;
  reconnect?: () => void;
};

beforeEach(() => {
  mockUseTaskLog.mockReset();
});

afterEach(() => {
  vi.restoreAllMocks();
});

function setHook(value: TaskLogReturn) {
  mockUseTaskLog.mockReturnValue({ reconnect: () => {}, ...value });
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

    // Synthesize scroll geometry the component reads. Mark scrollTop
    // as writable so the production code's useLayoutEffect auto-scroll
    // assignment (el.scrollTop = el.scrollHeight) doesn't throw.
    Object.defineProperty(viewport, "scrollTop", {
      configurable: true,
      writable: true,
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

    Object.defineProperty(viewport, "scrollTop", {
      configurable: true,
      writable: true,
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
    act(() => fireEvent.scroll(viewport));
    expect(screen.getByTestId("pod-log-pause-indicator")).toBeInTheDocument();

    // Scroll back to bottom — scrollTop + clientHeight ≈ scrollHeight.
    Object.defineProperty(viewport, "scrollTop", {
      configurable: true,
      writable: true,
      value: 800,
    });
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

    // Trailing plain segment — match the literal " fail" content via
    // an exact text matcher. getByText/regex normalizes whitespace so
    // a substring match would lose the leading space; instead we walk
    // the rendered line and assert the textContent contains both.
    const line = screen.getByTestId("pod-log-line-0");
    expect(line.textContent).toBe("ERROR fail");
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

// Plan 37-01 Task 2 — DASH-04 four-state model (D-12..D-14). Every display
// state must render explicit copy; no state may fall through to an empty
// viewport.
describe("PodLogStreamer (Plan 37-01 Task 2) — terminal state rendering", () => {
  it("Test 1: state 'reconnecting' with 0 lines renders the (previously-missing) placeholder", () => {
    setHook({ state: "reconnecting", lines: [] });
    render(<PodLogStreamer taskName="t-recon" onClose={() => {}} />);
    expect(
      screen.getByText(/Reconnecting to log stream…/i),
    ).toBeInTheDocument();
  });

  it("Test 2: state 'pod-gone' renders the garbage-collected message ONLY — no retry affordance", () => {
    setHook({ state: "pod-gone", lines: [] });
    render(<PodLogStreamer taskName="t-gone" onClose={() => {}} />);
    expect(
      screen.getByText("Logs no longer available — pod garbage-collected."),
    ).toBeInTheDocument();
    // D-13: message only — no Reconnect button in the pod-gone state.
    expect(
      screen.queryByRole("button", { name: /reconnect/i }),
    ).toBeNull();
  });

  it("Test 3: state 'stream-error' renders heading + body + a Reconnect button that invokes reconnect()", () => {
    const reconnect = vi.fn();
    setHook({ state: "stream-error", lines: [], reconnect });
    render(<PodLogStreamer taskName="t-err" onClose={() => {}} />);

    expect(screen.getByText("Log stream error")).toBeInTheDocument();
    expect(
      screen.getByText(
        "The stream failed unexpectedly — the pod may still be running.",
      ),
    ).toBeInTheDocument();

    const btn = screen.getByRole("button", { name: /reconnect/i });
    fireEvent.click(btn);
    expect(reconnect).toHaveBeenCalledOnce();
  });

  it("Test 4: every state renders non-empty viewport content with 0 lines (no silent-empty)", () => {
    const states: TaskLogState[] = [
      "connecting",
      "connected",
      "reconnecting",
      "offline",
      "idle-closed",
      "pod-gone",
      "stream-error",
    ];
    for (const state of states) {
      setHook({ state, lines: [] });
      const { unmount } = render(
        <PodLogStreamer taskName={`t-${state}`} onClose={() => {}} />,
      );
      const viewport = screen.getByTestId("pod-log-viewport");
      expect(
        viewport.textContent?.trim().length ?? 0,
        `state "${state}" rendered an empty viewport`,
      ).toBeGreaterThan(0);
      unmount();
    }
  });
});
