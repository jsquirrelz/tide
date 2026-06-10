/*
 * sse.test.ts (plan 04-16 Task 1)
 *
 *   Covers Tests 1-3 of the plan: useSSEStream + useTaskLog hooks +
 *   Last-Event-ID reconnect.
 *
 *   The hooks live in lib/sse.ts and use the browser-native EventSource
 *   constructor. We stub the constructor on globalThis so the test can
 *   pump synthetic events into the hook + assert cleanup/reconnect
 *   semantics without an actual SSE server.
 *
 *   Test 1 — useSSEStream: subscribes, receives messages, flips to
 *     "reconnecting" on error, closes on unmount (Pitfall 22 client side).
 *   Test 2 — useTaskLog: URL shape /api/v1/tasks/{name}/log; ring-buffer
 *     5000 line cap; oldest dropped first.
 *   Test 3 — Last-Event-ID reconnect: on error, the hook re-instantiates
 *     EventSource with the same URL (browser handles the Last-Event-ID
 *     header automatically per the WHATWG SSE spec).
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";

import {
  MAX_SSE_EVENTS,
  SSE_PROJECT_EVENT_TYPES,
  useSSEStream,
  useTaskLog,
} from "./sse";

// FakeEventSource captures construction args and exposes hooks to pump
// open/message/error events from the test.
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
  // Named-event listeners registered via addEventListener — the real
  // EventSource fires these for `event: <name>` frames. The backend names
  // every project event (`project.update`, …), so the hook MUST bind here.
  listeners = new Map<string, Set<(e: MessageEvent) => void>>();

  constructor(url: string, init?: EventSourceInit) {
    this.url = url;
    this.init = init;
    FakeEventSource.constructorCalls.push({ url, init });
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, fn: (e: MessageEvent) => void) {
    let set = this.listeners.get(type);
    if (!set) {
      set = new Set();
      this.listeners.set(type, set);
    }
    set.add(fn);
  }

  close() {
    this.closeCalls += 1;
    this.closed = true;
    this.readyState = 2;
  }

  // Test-only helpers
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
  // Dispatch a NAMED event (e.g. "project.update") to the addEventListener
  // handlers — mirrors the real EventSource routing the backend depends on.
  _emitNamed(type: string, msg: FakeMessage) {
    const evt = new MessageEvent(type, {
      data: msg.data,
      lastEventId: msg.lastEventId,
    });
    this.listeners.get(type)?.forEach((fn) => fn(evt));
  }
  _emitError() {
    this.readyState = 2;
    this.onerror?.(new Event("error"));
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
});

describe("useSSEStream (Test 1)", () => {
  it("subscribes to URL, grows events on message, flips reconnecting on error, closes on unmount", () => {
    const { result, unmount } = renderHook(() =>
      useSSEStream("/api/v1/projects/p1/events"),
    );

    // One EventSource constructed with the right URL.
    expect(FakeEventSource.constructorCalls.length).toBe(1);
    expect(FakeEventSource.constructorCalls[0].url).toBe(
      "/api/v1/projects/p1/events",
    );

    const es = FakeEventSource.instances[0];

    // open → state "connected"
    act(() => es._emitOpen());
    expect(result.current.state).toBe("connected");

    // 3 messages → events array grows to 3.
    act(() => {
      es._emitMessage({ data: '{"hello":"a"}', lastEventId: "1" });
      es._emitMessage({ data: '{"hello":"b"}', lastEventId: "2" });
      es._emitMessage({ data: '{"hello":"c"}', lastEventId: "3" });
    });
    expect(result.current.events.length).toBe(3);
    expect(result.current.lastEventId).toBe(3);

    // error → "reconnecting"
    act(() => es._emitError());
    expect(result.current.state).toBe("reconnecting");

    // unmount → EventSource.close called exactly once on the active socket.
    // The reconnect path schedules a new EventSource; we count the close
    // calls across all spawned sockets together — total must be ≥ 1 and the
    // first socket must have been closed exactly once.
    unmount();
    expect(es.closeCalls).toBe(1);
  });

  // Live-update regression: the backend emits NAMED events
  // (`project.update`, `plan.create`, …), NOT unnamed `message` frames. The
  // hook MUST bind its handler to every named type via addEventListener or no
  // consumer ever receives data and nothing live-updates.
  it("fires onMessage + grows events for a NAMED event (project.update)", () => {
    const onMessage = vi.fn();
    const { result } = renderHook(() =>
      useSSEStream("/api/v1/projects/p-named/events", { onMessage }),
    );
    const es = FakeEventSource.instances[0];
    act(() => es._emitOpen());

    // The hook must have registered the handler for every project event type.
    for (const type of SSE_PROJECT_EVENT_TYPES) {
      expect(es.listeners.has(type)).toBe(true);
    }

    // Dispatch a named event — NOT an unnamed `message` frame.
    act(() =>
      es._emitNamed("project.update", {
        data: '{"kind":"Project","name":"p-named"}',
        lastEventId: "7",
      }),
    );

    expect(onMessage).toHaveBeenCalledTimes(1);
    expect(result.current.events.length).toBe(1);
    expect(result.current.totalReceived).toBe(1);
    expect(result.current.lastEventId).toBe(7);
  });

  // Empty url = disabled stream: no EventSource is constructed (used by
  // PlanningDAGView when no project is selected so tests / the idle state
  // don't spawn sockets or trip the reconnect loop).
  it("does NOT open a connection for an empty url", () => {
    renderHook(() => useSSEStream(""));
    expect(FakeEventSource.constructorCalls.length).toBe(0);
  });

  // CR-05 + WR-05 regression test: useSSEStream MUST cap its retained
  // MessageEvent buffer at MAX_SSE_EVENTS even when the consumer does NOT
  // attach an onMessage callback (i.e. some future "raw events" consumer
  // is added without its own cap). Without this, a long-lived SSE tab
  // leaked unbounded MessageEvent references — Pitfall 22 / T-04-D.
  it("caps events at MAX_SSE_EVENTS on overflow (CR-05 / WR-05 regression)", () => {
    const { result } = renderHook(() =>
      useSSEStream("/api/v1/projects/p-cap/events"),
    );
    const es = FakeEventSource.instances[0];
    act(() => es._emitOpen());

    // Push MAX_SSE_EVENTS + 500 events; buffer must be exactly MAX_SSE_EVENTS.
    const overflow = 500;
    const total = MAX_SSE_EVENTS + overflow;
    act(() => {
      for (let i = 0; i < total; i++) {
        es._emitMessage({ data: `evt-${i}`, lastEventId: String(i + 1) });
      }
    });

    // Cap holds — the array does NOT grow past MAX_SSE_EVENTS.
    expect(result.current.events.length).toBe(MAX_SSE_EVENTS);
    // Newest preserved (last event is evt-(total-1)).
    expect(result.current.events[result.current.events.length - 1].data).toBe(
      `evt-${total - 1}`,
    );
    // Oldest dropped (first kept event is evt-(overflow) since overflow items
    // were sliced off the head).
    expect(result.current.events[0].data).toBe(`evt-${overflow}`);
    // totalReceived is monotonic — independent of the buffer cap.
    expect(result.current.totalReceived).toBe(total);
    // lastEventId tracks the highest id seen.
    expect(result.current.lastEventId).toBe(total);
  });
});

describe("useTaskLog (Test 2)", () => {
  it("subscribes to /api/v1/tasks/{name}/log and ring-buffers at 5000 lines", () => {
    const { result } = renderHook(() => useTaskLog("t-007"));

    expect(FakeEventSource.constructorCalls[0].url).toBe(
      "/api/v1/tasks/t-007/log",
    );
    const es = FakeEventSource.instances[0];

    act(() => es._emitOpen());
    expect(result.current.state).toBe("connected");

    // Push 6000 lines; ring buffer caps at 5000; first 1000 must be dropped.
    act(() => {
      for (let i = 0; i < 6000; i++) {
        es._emitMessage({ data: `line-${i}`, lastEventId: String(i + 1) });
      }
    });

    expect(result.current.lines.length).toBe(5000);
    // Oldest dropped — first remaining line should be line-1000 (0..999 evicted).
    expect(result.current.lines[0]).toBe("line-1000");
    // Newest preserved.
    expect(result.current.lines[result.current.lines.length - 1]).toBe(
      "line-5999",
    );
  });
});

describe("Last-Event-ID reconnect (Test 3)", () => {
  it("re-instantiates EventSource on error after backoff (browser-native Last-Event-ID)", () => {
    const { unmount } = renderHook(() =>
      useSSEStream("/api/v1/projects/p2/events"),
    );

    expect(FakeEventSource.constructorCalls.length).toBe(1);
    const first = FakeEventSource.instances[0];

    // Trigger error → schedule reconnect.
    act(() => first._emitError());

    // Advance scheduled reconnect timer.
    act(() => {
      vi.advanceTimersByTime(2000);
    });

    expect(FakeEventSource.constructorCalls.length).toBeGreaterThanOrEqual(2);
    // Reconnect to the same URL — browser auto-sends Last-Event-ID header.
    expect(
      FakeEventSource.constructorCalls[
        FakeEventSource.constructorCalls.length - 1
      ].url,
    ).toBe("/api/v1/projects/p2/events");

    unmount();
  });
});
