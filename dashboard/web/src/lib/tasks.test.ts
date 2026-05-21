/*
 * tasks.test.ts (plan 04-17)
 *
 *   Covers useTasks() + useTaskDetail() hooks:
 *     1. useTasks happy path — initial fetchPlan resolves; tasks
 *        populated; statuses coerced to StatusValue via STATUS_TABLE.
 *     2. useTasks SSE refresh — FakeEventSource emits a Task event for
 *        the active planRef; after 250ms debounce, fetchPlan called a
 *        2nd time.
 *     3. useTasks plan change — re-rendering with a new planName fires
 *        the fetch for the new plan name.
 *     4. useTaskDetail null taskName — result.current === null and
 *        fetchTask is NOT called.
 *     5. useTaskDetail SSE filter — event for a different taskName does
 *        NOT trigger a re-fetch.
 *
 *   The FakeEventSource stub is the same pattern sse.test.ts established
 *   in plan 04-16 — useSSEStream is composed unmodified.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";

import { useTaskDetail, useTasks } from "./tasks";

// drainMicrotasks resolves the next microtask queue inside the act() boundary
// so fetch Promises can settle while vi.useFakeTimers() is engaged. With
// fake timers, `await waitFor(...)` polls via setTimeout which is itself
// mocked — the canonical pattern (mirrored from React Testing Library
// fake-timer guides) is to drive promises explicitly via flushPromises.
async function flushPromises() {
  // 4 round-trips is conservative for React 18's batched effects → promise
  // → setState → render → effect cycle. Each iteration drains one tick of
  // pending microtasks.
  for (let i = 0; i < 4; i++) {
    await Promise.resolve();
  }
}

type FakeMessage = { data: string; lastEventId?: string };

class FakeEventSource {
  static instances: FakeEventSource[] = [];
  static constructorCalls: { url: string; init?: EventSourceInit }[] = [];

  url: string;
  init?: EventSourceInit;
  readyState = 0;
  closed = false;
  closeCalls = 0;

  onopen: ((e: Event) => void) | null = null;
  onmessage: ((e: MessageEvent) => void) | null = null;
  onerror: ((e: Event) => void) | null = null;

  constructor(url: string, init?: EventSourceInit) {
    this.url = url;
    this.init = init;
    FakeEventSource.constructorCalls.push({ url, init });
    FakeEventSource.instances.push(this);
  }

  close() {
    this.closeCalls += 1;
    this.closed = true;
    this.readyState = 2;
  }

  _emitOpen() {
    this.readyState = 1;
    this.onopen?.(new Event("open"));
  }
  _emitMessage(msg: FakeMessage) {
    const evt = new MessageEvent("message", {
      data: msg.data,
      lastEventId: msg.lastEventId,
    });
    this.onmessage?.(evt);
  }
}

beforeEach(() => {
  FakeEventSource.instances = [];
  FakeEventSource.constructorCalls = [];
  vi.stubGlobal("EventSource", FakeEventSource as unknown as typeof EventSource);
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

// stubFetch returns a vi.fn() backed mock that always resolves with the
// given payload. Tracks calls so assertions can count them.
function stubFetch<T>(payload: T) {
  const fn = vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: async () => payload,
  });
  vi.stubGlobal("fetch", fn as unknown as typeof fetch);
  return fn;
}

const samplePlanDetail = {
  name: "auth-plan",
  namespace: "default",
  phase: "Running",
  phaseRef: "ph-1",
  tasks: [
    {
      name: "t-a",
      phase: "Succeeded",
      waveIndex: 0,
      attempt: 1,
      dependsOn: [],
    },
    {
      name: "t-b",
      phase: "Running",
      waveIndex: 1,
      attempt: 1,
      dependsOn: ["t-a"],
    },
  ],
  activeDispatchWave: 1,
};

const sampleTaskDetail = {
  name: "t-007",
  projectName: "prj-1",
  planName: "auth-plan",
  status: "Running",
  namespace: "default",
  attempt: 2,
  attemptMax: 5,
  podName: "t-007-pod",
  exitCode: null,
  waveIndex: 1,
  scheduledAt: "2026-05-21T19:00:00Z",
  envelopePath: "/workspace/envelopes/uid-007/result.json",
  elapsedText: "running for 2m 30s",
  conditions: [],
};

describe("useTasks (plan 04-17)", () => {
  it("happy path: fetches the plan, coerces statuses, sorts tasks", async () => {
    const fn = stubFetch(samplePlanDetail);
    const { result } = renderHook(() =>
      useTasks("prj-1", "auth-plan"),
    );

    // Drain pending microtasks so the initial fetchPlan resolves into
    // setState → render. Wrapped in act so React's batched updates flush
    // before we read result.current.
    await act(async () => {
      await flushPromises();
    });

    expect(fn).toHaveBeenCalledTimes(1);
    const plan = result.current;
    expect(plan).not.toBeNull();
    expect(plan?.planName).toBe("auth-plan");
    expect(plan?.tasks.length).toBe(2);
    expect(plan?.tasks[0].name).toBe("t-a");
    expect(plan?.tasks[1].name).toBe("t-b");
    // Coerced to StatusValue — Succeeded/Running are known.
    expect(plan?.tasks[0].status).toBe("Succeeded");
    expect(plan?.tasks[1].status).toBe("Running");
    expect(plan?.activeDispatchWave).toBe(1);
  });

  it("SSE refresh: Task event in same planRef triggers debounced re-fetch", async () => {
    const fn = stubFetch(samplePlanDetail);
    renderHook(() => useTasks("prj-1", "auth-plan"));

    // Drain initial fetch.
    await act(async () => {
      await flushPromises();
    });
    expect(fn).toHaveBeenCalledTimes(1);

    // SSE event for a Task in the same plan.
    const es = FakeEventSource.instances[0];
    expect(es).toBeDefined();
    act(() => {
      es._emitOpen();
      es._emitMessage({
        data: JSON.stringify({
          kind: "Task",
          name: "t-b",
          planRef: "auth-plan",
          phase: "Succeeded",
        }),
      });
    });

    // 250ms debounce — advance fake timers and let the scheduled
    // refetch run, then drain microtasks.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(260);
      await flushPromises();
    });

    expect(fn).toHaveBeenCalledTimes(2);
  });

  it("plan change: re-rendering with a new planName fires a fetch for the new plan", async () => {
    const fn = stubFetch(samplePlanDetail);
    const { rerender } = renderHook(
      ({ p, pl }: { p: string | null; pl: string | null }) => useTasks(p, pl),
      { initialProps: { p: "prj-1", pl: "plan-a" } },
    );

    await act(async () => {
      await flushPromises();
    });
    expect(fn).toHaveBeenCalledTimes(1);

    // Re-render with a different plan name → new fetchPlan call.
    rerender({ p: "prj-1", pl: "plan-b" });

    await act(async () => {
      await flushPromises();
    });

    expect(fn).toHaveBeenCalledTimes(2);
    // Most-recent call URL contains plan-b.
    const lastCallUrl = fn.mock.calls[fn.mock.calls.length - 1][0];
    expect(String(lastCallUrl)).toContain("/api/v1/plans/plan-b");
  });
});

describe("useTaskDetail (plan 04-17)", () => {
  it("null taskName: returns null without firing fetchTask", async () => {
    const fn = stubFetch(sampleTaskDetail);
    const { result } = renderHook(() =>
      useTaskDetail("prj-1", null),
    );

    // Give the effect a microtask to run; verify no fetch fired.
    await act(async () => {
      await flushPromises();
    });

    expect(result.current).toBeNull();
    expect(fn).not.toHaveBeenCalled();
  });

  it("SSE filter: Task event for a different name does NOT re-fetch", async () => {
    const fn = stubFetch(sampleTaskDetail);
    renderHook(() => useTaskDetail("prj-1", "t-007"));

    // Drain initial fetch.
    await act(async () => {
      await flushPromises();
    });
    expect(fn).toHaveBeenCalledTimes(1);

    // SSE event for a DIFFERENT task name → must NOT re-fetch.
    const es = FakeEventSource.instances[0];
    expect(es).toBeDefined();
    act(() => {
      es._emitOpen();
      es._emitMessage({
        data: JSON.stringify({
          kind: "Task",
          name: "t-other",
          planRef: "auth-plan",
          phase: "Succeeded",
        }),
      });
    });

    // Advance past the 250ms debounce — fetch should NOT have fired again.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(260);
      await flushPromises();
    });

    expect(fn).toHaveBeenCalledTimes(1);
  });
});
