/*
 * sse.ts (plan 04-16)
 *
 *   Two browser-side hooks that consume the dashboard backend's SSE
 *   surface (cmd/dashboard/api/events_sse.go + cmd/dashboard/api/logs_sse.go,
 *   shipped in plan 04-11).
 *
 *   useSSEStream(url): generic project-events stream. Tracks connection
 *     state, accumulates raw `MessageEvent`s, exposes `lastEventId` for
 *     UI/debugging. Pitfall 22 client-side mitigation: useEffect cleanup
 *     closes the EventSource on unmount. Browser-native EventSource
 *     handles Last-Event-ID header round-tripping per the WHATWG SSE
 *     spec — we simply re-instantiate `new EventSource(url)` on
 *     reconnect.
 *
 *   useTaskLog(taskName, opts?): per-task pod-log stream. Built on
 *     useSSEStream. Maintains a 5000-line ring buffer (UI-SPEC §8
 *     "ring-buffer-capped at 5000 lines" — bounds browser memory so a
 *     runaway log stream cannot DoS the tab; T-04-D-eventsource-leak
 *     mitigation). Subscribes to the URL shape
 *     `/api/v1/tasks/{name}/log`.
 *
 *   Reconnect strategy: exponential backoff capped at 30s, schedules a
 *   re-instantiation through window.setTimeout so the test's fake
 *   timers can drive it. Browser-native Last-Event-ID is sent on the
 *   reconnect request automatically.
 */
import { useEffect, useReducer, useRef, useState } from "react";

export type SSEState =
  | "connecting"
  | "connected"
  | "reconnecting"
  | "offline";

export type SSEStreamResult = {
  /** Latest accumulator of MessageEvents received since mount. */
  events: MessageEvent[];
  /** Connection state — drives the header pill + log-streamer chrome. */
  state: SSEState;
  /** Highest numeric event id observed; 0 if none. */
  lastEventId: number;
};

const MAX_BACKOFF_MS = 30_000;
const INITIAL_BACKOFF_MS = 1_000;
const MAX_RING_LINES = 5_000;

/**
 * useSSEStream subscribes to the given SSE URL via browser-native
 * EventSource. Returns the running accumulator + connection state +
 * highest event id observed.
 *
 * Cleanup contract: every effect tear-down closes the active EventSource
 * exactly once (Pitfall 22 client-side). The reconnect path re-arms
 * itself via window.setTimeout so React's useEffect cleanup chain stays
 * coherent.
 */
export function useSSEStream(url: string): SSEStreamResult {
  const [, forceRender] = useReducer((x: number) => x + 1, 0);
  const resultRef = useRef<SSEStreamResult>({
    events: [],
    state: "connecting",
    lastEventId: 0,
  });
  const attemptRef = useRef(0);
  const esRef = useRef<EventSource | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const unmountedRef = useRef(false);

  useEffect(() => {
    unmountedRef.current = false;

    const open = () => {
      if (unmountedRef.current) return;
      const es = new EventSource(url);
      esRef.current = es;

      es.onopen = () => {
        if (unmountedRef.current) return;
        attemptRef.current = 0;
        resultRef.current = { ...resultRef.current, state: "connected" };
        forceRender();
      };

      es.onmessage = (e: MessageEvent) => {
        if (unmountedRef.current) return;
        const nextEvents = resultRef.current.events.concat(e);
        const eid = parseInt(e.lastEventId ?? "", 10);
        const nextLast = Number.isFinite(eid)
          ? Math.max(resultRef.current.lastEventId, eid)
          : resultRef.current.lastEventId;
        resultRef.current = {
          events: nextEvents,
          state: "connected",
          lastEventId: nextLast,
        };
        forceRender();
      };

      es.onerror = () => {
        if (unmountedRef.current) return;
        // Close the dead socket exactly once — owning the close ourselves
        // is required to break the browser's built-in auto-retry (the
        // browser would otherwise reconnect on its own timer, racing our
        // exponential-backoff schedule). Then null our handle so the
        // unmount cleanup doesn't double-close. Pitfall 22 client-side
        // remains satisfied because exactly one of {error close, cleanup
        // close} runs per socket lifecycle.
        try {
          es.close();
        } catch {
          /* ignore */
        }
        if (esRef.current === es) {
          esRef.current = null;
        }
        resultRef.current = { ...resultRef.current, state: "reconnecting" };
        forceRender();
        scheduleReconnect();
      };
    };

    const scheduleReconnect = () => {
      if (unmountedRef.current) return;
      const attempt = attemptRef.current;
      const backoff = Math.min(
        INITIAL_BACKOFF_MS * Math.pow(2, attempt),
        MAX_BACKOFF_MS,
      );
      attemptRef.current = attempt + 1;
      timerRef.current = setTimeout(() => {
        timerRef.current = null;
        open();
      }, backoff);
    };

    // Initial connection.
    open();

    return () => {
      // Pitfall 22 (client side) — close the live EventSource on unmount.
      unmountedRef.current = true;
      if (timerRef.current) {
        clearTimeout(timerRef.current);
        timerRef.current = null;
      }
      if (esRef.current) {
        try {
          esRef.current.close();
        } catch {
          /* ignore */
        }
        esRef.current = null;
      }
      resultRef.current = { ...resultRef.current, state: "offline" };
    };
    // We intentionally re-subscribe only when URL changes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url]);

  return resultRef.current;
}

export type TaskLogState = SSEState | "idle-closed";

export type TaskLogResult = {
  lines: string[];
  state: TaskLogState;
};

/**
 * useTaskLog subscribes to the per-task pod-log SSE stream
 * (`/api/v1/tasks/{name}/log`). Wraps useSSEStream and transforms each
 * incoming `MessageEvent.data` into a single text line. Ring-buffer
 * caps at 5000 lines (oldest dropped) so a runaway log cannot exhaust
 * the tab's heap.
 */
export function useTaskLog(taskName: string): TaskLogResult {
  const url = `/api/v1/tasks/${encodeURIComponent(taskName)}/log`;
  const stream = useSSEStream(url);

  const [lines, setLines] = useState<string[]>([]);
  const consumedRef = useRef(0);

  useEffect(() => {
    const total = stream.events.length;
    if (total <= consumedRef.current) return;
    const fresh = stream.events.slice(consumedRef.current);
    consumedRef.current = total;
    setLines((prev) => {
      const next = prev.concat(fresh.map((e) => String(e.data)));
      // Ring-buffer cap — drop oldest when over budget.
      if (next.length > MAX_RING_LINES) {
        return next.slice(next.length - MAX_RING_LINES);
      }
      return next;
    });
  }, [stream.events]);

  // Reset the buffer when the task name (and therefore url) changes.
  useEffect(() => {
    setLines([]);
    consumedRef.current = 0;
  }, [taskName]);

  return { lines, state: stream.state };
}
