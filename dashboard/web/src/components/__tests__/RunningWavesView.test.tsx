/*
 * RunningWavesView.test.tsx (plan 15-07) — UI-SPEC Validation Contract.
 *
 *   Covers the 8 required behaviors from UI-SPEC C3 §Validation Contract:
 *     1. Cards render plan name and locked wave label (WAVE N · x/y running).
 *     2. One wave-card-chip per task.
 *     3. Chip row carries aria-hidden="true".
 *     4. Card click fires onPlanClick(planName).
 *     5. Card keyboard Enter fires onPlanClick(planName).
 *     6. waves: [] renders the "No running waves" empty state.
 *     7. No snapshot yet (null) renders the spinner.
 *     8. Malformed waves.snapshot event is ignored; last good state persists.
 *
 *   Tests 1-7 use initialSnapshot to bypass SSE (mirrors PlanningDAGView's
 *   initialData pattern). Test 8 uses the SSE seam (FakeEventSource) to drive
 *   a malformed event through the onMessage handler.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";

import RunningWavesView, { type RunningWave } from "../RunningWavesView";

// ── FakeEventSource seam (same pattern as dag-views.test.tsx) ──────────────
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

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  FakeEventSource.instances = [];
});

beforeEach(() => {
  FakeEventSource.instances = [];
  vi.stubGlobal("EventSource", FakeEventSource as unknown as typeof EventSource);
});

// ── Fixtures ────────────────────────────────────────────────────────────────

const WAVE_FIXTURE: RunningWave[] = [
  {
    planName: "15-02-running-waves",
    waveIndex: 1, // label should read "WAVE 2"
    tasks: [
      { name: "t1", status: "Running" },
      { name: "t2", status: "Dispatching" },
      { name: "t3", status: "Pending" },
      { name: "t4", status: "Succeeded" },
    ],
  },
  {
    planName: "15-03-another-plan",
    waveIndex: 0, // label should read "WAVE 1"
    tasks: [
      { name: "a1", status: "Running" },
      { name: "a2", status: "Failed" },
    ],
  },
];

// ── Test 1: card content — plan name + locked wave label ─────────────────────

describe("RunningWavesView — Test 1: card content (plan name + wave label)", () => {
  it("renders plan name and the exact WAVE N · x/y running label per card", () => {
    render(
      <RunningWavesView
        projectName="my-project"
        onPlanClick={() => undefined}
        initialSnapshot={WAVE_FIXTURE}
      />,
    );

    // Plan names
    expect(screen.getByText("15-02-running-waves")).toBeInTheDocument();
    expect(screen.getByText("15-03-another-plan")).toBeInTheDocument();

    // Locked label format: "WAVE <N> · <running>/<total> running"
    // Card 0: waveIndex=1 → WAVE 2; running=2 (Running+Dispatching), total=4
    expect(screen.getByText("WAVE 2 · 2/4 running")).toBeInTheDocument();
    // Card 1: waveIndex=0 → WAVE 1; running=1 (Running), total=2
    expect(screen.getByText("WAVE 1 · 1/2 running")).toBeInTheDocument();
  });
});

// ── Test 2: one chip per task ────────────────────────────────────────────────

describe("RunningWavesView — Test 2: one wave-card-chip per task", () => {
  it("renders exactly one chip per task across all cards", () => {
    render(
      <RunningWavesView
        projectName="my-project"
        onPlanClick={() => undefined}
        initialSnapshot={WAVE_FIXTURE}
      />,
    );
    // WAVE_FIXTURE has 4 + 2 = 6 tasks total
    const chips = document.querySelectorAll('[data-testid="wave-card-chip"]');
    expect(chips.length).toBe(6);
  });
});

// ── Test 3: chip row aria-hidden ─────────────────────────────────────────────

describe("RunningWavesView — Test 3: chip row aria-hidden", () => {
  it("chip rows carry aria-hidden=true (decorative density read)", () => {
    render(
      <RunningWavesView
        projectName="my-project"
        onPlanClick={() => undefined}
        initialSnapshot={WAVE_FIXTURE}
      />,
    );
    // Each card has one chip row div with aria-hidden
    const chipRows = document.querySelectorAll(
      '[data-testid^="wave-card-"] [aria-hidden="true"]',
    );
    // The chip rows among other aria-hidden elements; use flex+gap-2 selector specificity
    // by counting the chip wrapper divs on each card
    const cards = document.querySelectorAll('[role="button"]');
    expect(cards.length).toBe(2);
    // Each card's chip container has aria-hidden="true"
    cards.forEach((card) => {
      const chipRow = card.querySelector('.flex.flex-wrap[aria-hidden="true"]');
      expect(chipRow).not.toBeNull();
      expect(chipRow!.getAttribute("aria-hidden")).toBe("true");
    });
    void chipRows; // suppress unused-var hint
  });
});

// ── Test 4: card click fires onPlanClick ────────────────────────────────────

describe("RunningWavesView — Test 4: card click fires onPlanClick", () => {
  it("clicking a wave card calls onPlanClick with the card's planName", () => {
    const onPlanClick = vi.fn();
    render(
      <RunningWavesView
        projectName="my-project"
        onPlanClick={onPlanClick}
        initialSnapshot={WAVE_FIXTURE}
      />,
    );
    const card = document.querySelector(
      '[data-testid="wave-card-15-02-running-waves-1"]',
    );
    expect(card).not.toBeNull();
    act(() => { fireEvent.click(card!); });
    expect(onPlanClick).toHaveBeenCalledOnce();
    expect(onPlanClick).toHaveBeenCalledWith("15-02-running-waves");
  });
});

// ── Test 5: keyboard Enter fires onPlanClick ─────────────────────────────────

describe("RunningWavesView — Test 5: keyboard Enter fires onPlanClick", () => {
  it("pressing Enter on a wave card calls onPlanClick with the card's planName", () => {
    const onPlanClick = vi.fn();
    render(
      <RunningWavesView
        projectName="my-project"
        onPlanClick={onPlanClick}
        initialSnapshot={WAVE_FIXTURE}
      />,
    );
    const card = document.querySelector(
      '[data-testid="wave-card-15-03-another-plan-0"]',
    );
    expect(card).not.toBeNull();
    act(() => { fireEvent.keyDown(card!, { key: "Enter" }); });
    expect(onPlanClick).toHaveBeenCalledOnce();
    expect(onPlanClick).toHaveBeenCalledWith("15-03-another-plan");
  });
});

// ── Test 6: waves: [] renders empty state ───────────────────────────────────

describe("RunningWavesView — Test 6: waves: [] renders no-running-waves empty state", () => {
  it("renders the No running waves heading when snapshot has no waves", () => {
    render(
      <RunningWavesView
        projectName="my-project"
        onPlanClick={() => undefined}
        initialSnapshot={[]}
      />,
    );
    expect(
      screen.getByRole("heading", { name: "No running waves" }),
    ).toBeInTheDocument();
    // Cards must NOT be present
    expect(
      document.querySelector('[role="button"]'),
    ).toBeNull();
  });
});

// ── Test 7: pre-snapshot renders spinner ────────────────────────────────────

describe("RunningWavesView — Test 7: pre-snapshot (no initialSnapshot) renders spinner", () => {
  it("renders a Loader2 spinner before any snapshot arrives", () => {
    // No initialSnapshot → null state → spinner
    render(
      <RunningWavesView
        projectName="my-project"
        onPlanClick={() => undefined}
      />,
    );
    // Loader2 is aria-label="Loading running waves" per the component
    expect(
      document.querySelector('[aria-label="Loading running waves"]'),
    ).not.toBeNull();
    // No cards yet
    expect(document.querySelector('[role="button"]')).toBeNull();
  });
});

// ── Test 8: malformed waves.snapshot event is ignored ───────────────────────

describe("RunningWavesView — Test 8: malformed event ignored; last good state persists", () => {
  it("a malformed waves.snapshot payload does not clear the last good state", async () => {
    // Mount with a good initial snapshot so we have a known last good state.
    render(
      <RunningWavesView
        projectName="my-project"
        onPlanClick={() => undefined}
        initialSnapshot={WAVE_FIXTURE}
      />,
    );

    // Verify good state is rendered.
    expect(screen.getByText("15-02-running-waves")).toBeInTheDocument();

    // A component with initialSnapshot disables the SSE subscription (url = "").
    // To test the malformed-event path we mount a fresh instance WITHOUT initialSnapshot
    // and then emit both a good event followed by a malformed event.
    cleanup();
    FakeEventSource.instances = [];

    const onPlanClick = vi.fn();
    render(
      <RunningWavesView
        projectName="test-project"
        onPlanClick={onPlanClick}
      />,
    );

    // Pre-snapshot → spinner is present.
    expect(document.querySelector('[aria-label="Loading running waves"]')).not.toBeNull();

    // Emit a well-formed waves.snapshot event to establish good state.
    const es = FakeEventSource.instances.find((e) =>
      e.url.includes("test-project"),
    );
    expect(es).toBeDefined();

    act(() => {
      es!._emitNamed(
        "waves.snapshot",
        JSON.stringify({ waves: [{ planName: "good-plan", waveIndex: 0, tasks: [{ name: "t1", status: "Running" }] }] }),
      );
    });
    expect(screen.getByText("good-plan")).toBeInTheDocument();

    // Emit a malformed event (invalid JSON) — should be ignored, good state persists.
    act(() => {
      es!._emitNamed("waves.snapshot", "this is not json {{{");
    });
    // Still showing good state — plan name still present.
    expect(screen.getByText("good-plan")).toBeInTheDocument();

    // Emit another malformed event (valid JSON but wrong shape).
    act(() => {
      es!._emitNamed("waves.snapshot", JSON.stringify({ notWaves: [] }));
    });
    // Still showing good state.
    expect(screen.getByText("good-plan")).toBeInTheDocument();
  });
});
