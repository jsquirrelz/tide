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
  /**
   * Latest accumulator of MessageEvents received since mount. Capped at
   * MAX_SSE_EVENTS (oldest dropped on overflow) so a long-lived stream
   * cannot leak unbounded MessageEvent references (CR-05 fix — each
   * MessageEvent holds DOM-side references; uncapped growth on a
   * project-event stream produced a hundreds-of-MB leak on workday-open
   * dashboard tabs).
   */
  events: MessageEvent[];
  /** Connection state — drives the header pill + log-streamer chrome. */
  state: SSEState;
  /** Highest numeric event id observed; 0 if none. */
  lastEventId: number;
  /**
   * Monotonic total of events received since mount (CR-05 fix). Consumers
   * use this — not `events.length` — to detect new events when the buffer
   * has been sliced. Resets to 0 on remount (url change).
   */
  totalReceived: number;
};

const MAX_BACKOFF_MS = 30_000;
const INITIAL_BACKOFF_MS = 1_000;
const MAX_RING_LINES = 5_000;
/**
 * CR-05 fix: hard cap on retained MessageEvent references inside the
 * stream-level buffer. Chosen at 1000 because most UI consumers
 * (useTaskLog ring buffer at 5000 lines) cap their own derived state
 * downstream — the stream buffer is a short-lived hand-off, not a log
 * store. Combined with totalReceived consumers can detect slice drops
 * and re-sync.
 */
export const MAX_SSE_EVENTS = 1_000;

/**
 * Named SSE event types the dashboard backend emits on the project-events
 * stream (cmd/dashboard/api/events_sse.go). The backend names every event
 * `<kind>.<action>` — e.g. `project.create`, `plan.update`, `task.delete` —
 * so the browser-native EventSource `onmessage` handler (which fires ONLY for
 * unnamed `event: message` frames) never sees them. useSSEStream registers its
 * handler for every name in this list via addEventListener so consumers
 * actually receive data and the panes live-update.
 */
export const SSE_PROJECT_EVENT_TYPES: string[] = (
  ["project", "milestone", "phase", "plan", "task", "wave"] as const
).flatMap((kind) =>
  (["create", "update", "delete"] as const).map(
    (action) => `${kind}.${action}`,
  ),
// UI-SPEC C3 (15-07-PLAN.md): waves.snapshot is a named event outside the
// <kind>.<action> generator matrix — plural "waves" keeps it distinct from
// the Wave-CRD wave.create/update/delete events. Named events NOT in this list
// never reach onMessage — registering here is build-blocking if missed.
).concat(["waves.snapshot"]);

export type SSEStreamOptions = {
  /**
   * CR-05 fix: per-event callback fired synchronously for EVERY incoming
   * MessageEvent (not subject to the stream-buffer cap). Consumers that
   * derive their own state (e.g. useTaskLog's 5000-line ring buffer) MUST
   * use this rather than reading `events` so they don't lose events when
   * the stream buffer slices on overflow. The callback runs inside
   * EventSource.onmessage; keep it cheap.
   */
  onMessage?: (e: MessageEvent) => void;
};

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
export function useSSEStream(
  url: string,
  options?: SSEStreamOptions,
): SSEStreamResult {
  const [, forceRender] = useReducer((x: number) => x + 1, 0);
  const resultRef = useRef<SSEStreamResult>({
    events: [],
    state: "connecting",
    lastEventId: 0,
    totalReceived: 0,
  });
  // Latest onMessage callback — held in a ref so identity changes don't
  // re-open the EventSource. The latest callback fires for every event.
  const onMessageRef = useRef<SSEStreamOptions["onMessage"]>(options?.onMessage);
  onMessageRef.current = options?.onMessage;
  const attemptRef = useRef(0);
  const esRef = useRef<EventSource | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const unmountedRef = useRef(false);

  useEffect(() => {
    unmountedRef.current = false;

    const open = () => {
      if (unmountedRef.current) return;
      // Empty url = disabled stream (e.g. PlanningDAGView with no project
      // selected). Skip construction so tests and the no-project state don't
      // spawn EventSources or trip the reconnect-backoff loop.
      if (!url) return;
      const es = new EventSource(url);
      esRef.current = es;

      es.onopen = () => {
        if (unmountedRef.current) return;
        attemptRef.current = 0;
        resultRef.current = { ...resultRef.current, state: "connected" };
        forceRender();
      };

      // Shared message handler. The backend names every project event
      // (`project.create`, `plan.update`, …), so binding `onmessage` alone
      // would never fire — `onmessage` only catches unnamed `event: message`
      // frames. We register this same handler for every named event below.
      const handler = (e: MessageEvent) => {
        if (unmountedRef.current) return;
        // CR-05 fix: invoke the per-event callback FIRST (before any buffer
        // mutation) so derived consumers like useTaskLog observe every
        // event independent of the stream-buffer cap.
        if (onMessageRef.current) {
          try {
            onMessageRef.current(e);
          } catch {
            /* consumer-thrown errors must not poison the stream */
          }
        }
        // CR-05 fix: cap the retained events array at MAX_SSE_EVENTS.
        // Slice from the end on overflow (keep the newest) so consumers
        // see continuous tail behavior. Track totalReceived monotonically
        // so derived consumers can distinguish "new events" from "events
        // I already saw at the same index" when slicing shifts indices.
        const appended = resultRef.current.events.concat(e);
        const nextEvents =
          appended.length > MAX_SSE_EVENTS
            ? appended.slice(appended.length - MAX_SSE_EVENTS)
            : appended;
        const eid = parseInt(e.lastEventId ?? "", 10);
        const nextLast = Number.isFinite(eid)
          ? Math.max(resultRef.current.lastEventId, eid)
          : resultRef.current.lastEventId;
        resultRef.current = {
          events: nextEvents,
          state: "connected",
          lastEventId: nextLast,
          totalReceived: resultRef.current.totalReceived + 1,
        };
        forceRender();
      };

      // Harmless fallback for unnamed frames (e.g. the log stream, which
      // emits plain `event: message`).
      es.onmessage = handler;
      // Bind the same handler for every named project event the backend
      // sends so the planning/execution panes live-update. These listeners
      // are discarded together with the EventSource instance on close() —
      // the unmount/error cleanup closes `es`, so no explicit
      // removeEventListener is needed.
      for (const type of SSE_PROJECT_EVENT_TYPES) {
        es.addEventListener(type, handler as EventListener);
      }

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

  // CR-05 fix: derive lines from a per-event callback instead of polling
  // stream.events. The stream-level buffer is capped at MAX_SSE_EVENTS, so
  // reading events.length-based offsets would miss messages on overflow.
  // The callback fires for every event independent of the stream buffer.
  //
  // setLines uses the functional updater so React 18's automatic batching
  // can coalesce N synchronous emissions into one render commit; each call
  // updates `prev` in the closure so no events are lost across the
  // synchronous burst.
  const [lines, setLines] = useState<string[]>([]);

  const stream = useSSEStream(url, {
    onMessage: (e: MessageEvent) => {
      const data = String(e.data);
      setLines((prev) => {
        const next = prev.length >= MAX_RING_LINES
          ? // Already at cap: drop the oldest, append the new line.
            // slice(1) is O(n) but n is bounded at MAX_RING_LINES (5000),
            // and the per-event work stays O(MAX_RING_LINES) total.
            [...prev.slice(1), data]
          : [...prev, data];
        return next;
      });
    },
  });

  // Reset the buffer when the task name (and therefore url) changes.
  useEffect(() => {
    setLines([]);
  }, [taskName]);

  return { lines, state: stream.state };
}
