/*
 * ArtifactViewer.test.tsx (plan 37-05 Task 2)
 *
 *   Covers the four behaviors from the plan:
 *     1. available — role=tablist strip, first *.md selected, JSON tab
 *        renders pretty-printed content.
 *     2. XSS + href safety — raw HTML stays escaped (no <script> node),
 *        javascript:-scheme links are sanitized (Assumption A2).
 *     3. five typed states with LOCKED copy + correct retry affordances.
 *     4. bounded 10s polling while gate-parked + absent; stops on
 *        available and on unmount.
 *
 *   fetchNodeArtifacts is mocked so each test drives the wire state
 *   deterministically without a real backend.
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, within } from "@testing-library/react";

// Mock the fetch layer BEFORE importing the component so vi intercepts it.
vi.mock("../lib/api", () => ({
  fetchNodeArtifacts: (...args: unknown[]) => mockFetch(...args),
}));

const mockFetch = vi.fn();

import ArtifactViewer from "./ArtifactViewer";

afterEach(() => {
  cleanup();
  mockFetch.mockReset();
  vi.restoreAllMocks();
});

describe("ArtifactViewer (Test 1) — available: tabs + markdown/JSON rendering", () => {
  it("renders a tablist of both files, first *.md selected, and pretty-prints the JSON tab", async () => {
    mockFetch.mockResolvedValue({
      state: "available",
      branch: "tide/run",
      commitSHA: "abc123",
      files: [
        {
          name: "MILESTONE.md",
          path: "MILESTONE.md",
          content: "# The Title\n\nbody text here",
          sizeBytes: 26,
        },
        {
          name: "children/phase-1.json",
          path: "children/phase-1.json",
          content: '{"wave":1}',
          sizeBytes: 10,
        },
      ],
    });

    render(
      <ArtifactViewer kind="milestone" name="m1" project="p1" gateParked={false} />,
    );

    const tablist = await screen.findByRole("tablist");
    // Both file names present, children/ prefix shown verbatim.
    expect(
      within(tablist).getByRole("tab", { name: "MILESTONE.md" }),
    ).toBeInTheDocument();
    expect(
      within(tablist).getByRole("tab", { name: "children/phase-1.json" }),
    ).toBeInTheDocument();

    // First *.md selected by default → rendered markdown heading is present.
    expect(screen.getByRole("heading", { name: "The Title" })).toBeInTheDocument();

    // Select the JSON tab → pretty-printed content in a pre block.
    fireEvent.click(
      within(tablist).getByRole("tab", { name: "children/phase-1.json" }),
    );
    const pre = await screen.findByTestId("artifact-json");
    expect(pre.tagName.toLowerCase()).toBe("pre");
    expect(pre.textContent).toContain('"wave": 1');
  });
});

describe("ArtifactViewer (Test 2) — XSS + href safety (A2)", () => {
  it("escapes raw HTML and sanitizes javascript: links", async () => {
    mockFetch.mockResolvedValue({
      state: "available",
      files: [
        {
          name: "hostile.md",
          path: "hostile.md",
          content:
            "<script>window.__pwned = true</script>\n\n[click me](javascript:alert(1))",
          sizeBytes: 60,
        },
      ],
    });

    render(
      <ArtifactViewer kind="phase" name="ph1" project="p1" gateParked={false} />,
    );

    await screen.findByTestId("artifact-state-available");

    // No script element injected into the DOM.
    expect(document.querySelector("script")).toBeNull();
    // No anchor carries a javascript: href.
    const anchors = Array.from(document.querySelectorAll("a"));
    expect(
      anchors.every(
        (a) => !(a.getAttribute("href") ?? "").toLowerCase().startsWith("javascript:"),
      ),
    ).toBe(true);
  });
});

describe("ArtifactViewer (Test 3) — five typed states with locked copy", () => {
  async function renderState(
    wire: Record<string, unknown>,
    gateParked: boolean,
  ) {
    mockFetch.mockResolvedValue(wire);
    render(
      <ArtifactViewer kind="milestone" name="m1" project="p1" gateParked={gateParked} />,
    );
  }

  it("absent + gateParked → materializing copy", async () => {
    await renderState({ state: "absent", files: [] }, true);
    expect(await screen.findByText("Artifacts materializing")).toBeInTheDocument();
    expect(screen.getByTestId("artifact-state-materializing")).toBeInTheDocument();
  });

  it("absent + not gateParked → absent copy with Check again", async () => {
    await renderState({ state: "absent", files: [] }, false);
    expect(
      await screen.findByText("Artifacts not available for this run"),
    ).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Check again" })).toBeInTheDocument();
  });

  it("no-git → no-git copy, NO retry button", async () => {
    await renderState({ state: "no-git", files: [] }, false);
    expect(await screen.findByText("No git remote configured")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Check again" })).toBeNull();
  });

  it("error → error copy with Retry", async () => {
    await renderState({ state: "error", files: [], error: "boom" }, false);
    expect(await screen.findByText("Couldn't fetch artifacts")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("0-byte file → empty-artifact copy", async () => {
    await renderState(
      {
        state: "available",
        files: [{ name: "EMPTY.md", path: "EMPTY.md", content: "", sizeBytes: 0 }],
      },
      false,
    );
    expect(await screen.findByText("This artifact is empty.")).toBeInTheDocument();
  });
});

describe("ArtifactViewer (Test 4) — bounded 10s polling", () => {
  it("re-fetches every 10s while materializing, stops on available", async () => {
    vi.useFakeTimers();
    mockFetch.mockResolvedValue({ state: "absent", files: [] });

    render(
      <ArtifactViewer kind="milestone" name="m1" project="p1" gateParked={true} />,
    );

    // Flush the mount fetch.
    await vi.advanceTimersByTimeAsync(0);
    expect(mockFetch).toHaveBeenCalledTimes(1);

    // One poll tick.
    await vi.advanceTimersByTimeAsync(10_000);
    expect(mockFetch).toHaveBeenCalledTimes(2);

    // Next tick resolves available → polling must stop.
    mockFetch.mockResolvedValue({
      state: "available",
      files: [{ name: "M.md", path: "M.md", content: "# ok", sizeBytes: 4 }],
    });
    await vi.advanceTimersByTimeAsync(10_000);
    const callsAtAvailable = mockFetch.mock.calls.length;

    await vi.advanceTimersByTimeAsync(30_000);
    expect(mockFetch).toHaveBeenCalledTimes(callsAtAvailable);

    vi.useRealTimers();
  });

  it("stops polling on unmount", async () => {
    vi.useFakeTimers();
    mockFetch.mockResolvedValue({ state: "absent", files: [] });

    const { unmount } = render(
      <ArtifactViewer kind="milestone" name="m1" project="p1" gateParked={true} />,
    );
    await vi.advanceTimersByTimeAsync(0);
    expect(mockFetch).toHaveBeenCalledTimes(1);

    unmount();
    await vi.advanceTimersByTimeAsync(30_000);
    expect(mockFetch).toHaveBeenCalledTimes(1);

    vi.useRealTimers();
  });
});
