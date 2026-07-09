/*
 * PodLogStreamer.tsx (plan 04-16) — UI-SPEC §8 DASH-04 surface.
 *
 *   Mounts inside <TaskDetailDrawer> (or inline below it in App.tsx)
 *   when the drawer's "Open log stream" callback fires. Streams pod
 *   logs via the useTaskLog SSE hook (src/lib/sse.ts) and renders each
 *   line as a typed sequence of <span> segments via parseAnsi
 *   (src/lib/ansi.ts).
 *
 *   Key behaviors (UI-SPEC §8):
 *     - Ring-buffer cap at 5000 lines (owned by useTaskLog upstream).
 *     - Auto-scroll: toggles off when user scrolls up; re-enables when
 *       they scroll back to the bottom.
 *     - Connection-state UI: Connecting / Waiting-for-output /
 *       Disconnected copy verbatim per UI-SPEC §Copywriting Contract.
 *     - Toolbar: Copy logs + line-wrap toggle + close. Pause/resume is
 *       derived from autoScroll state — surfaces as a "Paused
 *       (scrolled up)" indicator when auto-scroll is off.
 *     - ANSI rendering: parseAnsi produces text + style props ONLY —
 *       never innerHTML. T-04-D1 XSS mitigation.
 */
import {
  type CSSProperties,
  type UIEvent,
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { AlertTriangle, Copy, Loader2, WrapText, X } from "lucide-react";

import { useTaskLog } from "../lib/sse";
import { type AnsiColor, type AnsiSegment, parseAnsi } from "../lib/ansi";

export type PodLogStreamerProps = {
  taskName: string;
  // DASH-04: the selected project's namespace, threaded into the log SSE
  // URL so pods outside "default" resolve. Optional/back-compat — omitted,
  // useTaskLog builds the default-namespace URL.
  namespace?: string;
  onClose: () => void;
};

// Locked connection-state copy per UI-SPEC §Copywriting Contract.
const COPY_CONNECTING = "Connecting to log stream…";
const COPY_WAITING = "Waiting for output…";
const COPY_DISCONNECTED = "Log stream closed by backend (5 min idle).";
// DASH-04 four-state extension (D-12..D-14). Verbatim per UI-SPEC §Copywriting
// Contract > Log drawer states.
const COPY_RECONNECTING = "Reconnecting to log stream…";
const COPY_POD_GONE = "Logs no longer available — pod garbage-collected.";
const COPY_STREAM_ERROR_HEADING = "Log stream error";
const COPY_STREAM_ERROR_BODY =
  "The stream failed unexpectedly — the pod may still be running.";

// SGR color name → CSS color value. We use Tailwind-aligned hex values
// so the rendered text reads sanely against the --color-surface-overlay
// log viewport background.
const ANSI_FG: Record<AnsiColor, string> = {
  black: "#1F2937",
  red: "#F85149",
  green: "#3FB950",
  yellow: "#D29922",
  blue: "#06B6D4", // approximate; pure-blue is hard on dark
  magenta: "#A371F7",
  cyan: "#06B6D4",
  white: "#E6EDF3",
};
const ANSI_BG: Record<AnsiColor, string> = {
  black: "#161B22",
  red: "#3C1A1B",
  green: "#1A3024",
  yellow: "#3A2D14",
  blue: "#0B2840",
  magenta: "#2A1C3D",
  cyan: "#0B2840",
  white: "#30363D",
};

function segmentStyle(seg: AnsiSegment): CSSProperties {
  const s: CSSProperties = {};
  if (seg.color) s.color = ANSI_FG[seg.color];
  if (seg.bgColor) s.background = ANSI_BG[seg.bgColor];
  if (seg.bold) s.fontWeight = 600;
  return s;
}

export default function PodLogStreamer({
  taskName,
  namespace,
  onClose,
}: PodLogStreamerProps) {
  const { lines, state, reconnect } = useTaskLog(taskName, namespace);
  const [autoScroll, setAutoScroll] = useState(true);
  const [lineWrap, setLineWrap] = useState(false);
  const viewportRef = useRef<HTMLDivElement>(null);

  // Auto-scroll: after each render, if autoScroll is on, jump to the
  // bottom. Layout effect so the scroll happens before the browser
  // paints — no visible flicker.
  useLayoutEffect(() => {
    if (!autoScroll) return;
    const el = viewportRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [lines.length, autoScroll]);

  // Scroll handler decides whether the user is at the bottom or not.
  // A tolerance of 2px absorbs sub-pixel rounding.
  const onScroll = useCallback((e: UIEvent<HTMLDivElement>) => {
    const el = e.currentTarget;
    const distanceFromBottom =
      el.scrollHeight - el.scrollTop - el.clientHeight;
    const atBottom = distanceFromBottom <= 2;
    setAutoScroll((prev) => (prev === atBottom ? prev : atBottom));
  }, []);

  // Copy-logs handler — writes the full ring buffer to the clipboard
  // via navigator.clipboard. Silent on failure; the toast surface for
  // clipboard errors lives in plan 04-15 ClipboardCopyAction and is
  // out of scope for the log viewer.
  const onCopy = useCallback(() => {
    if (typeof navigator !== "undefined" && navigator.clipboard) {
      navigator.clipboard.writeText(lines.join("\n")).catch(() => {
        /* swallow — best-effort */
      });
    }
  }, [lines]);

  // Pre-parse ANSI segments once per line render so the inner map is
  // cheap. useMemo keyed on lines reference — useTaskLog returns a new
  // array on every change so reference equality is the right gate.
  const parsed = useMemo(() => lines.map((l) => parseAnsi(l)), [lines]);

  // Auto-recover focus on the close button when first mounted so the
  // operator can dismiss with Enter / Space.
  const closeRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    closeRef.current?.focus();
  }, []);

  return (
    <section
      data-testid="pod-log-streamer"
      aria-label={`Pod log stream for task ${taskName}`}
      className="flex h-full w-full flex-col"
      style={{
        background: "var(--color-surface-overlay)",
        border: "1px solid var(--color-border-subtle)",
        fontFamily: "var(--font-mono)",
        fontSize: "13px",
        lineHeight: 1.4,
      }}
    >
      {/* Toolbar (32px tall) */}
      <div
        className="flex items-center justify-between border-b border-[var(--color-border-subtle)] px-3"
        style={{ height: "32px" }}
      >
        <div className="flex items-center gap-3" style={{ minHeight: "100%" }}>
          {!autoScroll && (
            <span
              data-testid="pod-log-pause-indicator"
              style={{
                fontSize: "12px",
                fontWeight: 600,
                color: "var(--color-status-warning)",
              }}
            >
              Paused (scrolled up)
            </span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <button
            type="button"
            data-testid="pod-log-copy"
            onClick={onCopy}
            aria-label="Copy logs"
            className="inline-flex items-center gap-1 rounded px-2 py-1 text-[var(--color-text-muted)] hover:bg-[var(--color-surface-raised)]"
            style={{ fontSize: "12px" }}
          >
            <Copy size={12} aria-hidden="true" /> Copy logs
          </button>
          <button
            type="button"
            data-testid="pod-log-wrap-toggle"
            onClick={() => setLineWrap((v) => !v)}
            aria-label="Toggle line wrap"
            aria-pressed={lineWrap}
            className="inline-flex items-center gap-1 rounded px-2 py-1 text-[var(--color-text-muted)] hover:bg-[var(--color-surface-raised)]"
            style={{ fontSize: "12px" }}
          >
            <WrapText size={12} aria-hidden="true" /> Wrap
          </button>
          <button
            type="button"
            ref={closeRef}
            data-testid="pod-log-close"
            onClick={onClose}
            aria-label="Close log stream"
            className="inline-flex h-6 w-6 items-center justify-center rounded text-[var(--color-text-muted)] hover:bg-[var(--color-surface-raised)]"
          >
            <X size={14} aria-hidden="true" />
          </button>
        </div>
      </div>

      {/* Viewport */}
      <div
        ref={viewportRef}
        data-testid="pod-log-viewport"
        onScroll={onScroll}
        className="flex-1 overflow-y-auto px-3 py-2"
        style={{
          color: "var(--color-text-primary)",
        }}
      >
        {/* Connection-state placeholders are mutually exclusive with line
            output: lines visible only when ≥1 present + state ∈ connected
            / reconnecting. */}
        {lines.length === 0 && state === "connecting" && (
          <div
            data-testid="pod-log-placeholder"
            style={{ color: "var(--color-text-muted)" }}
          >
            {COPY_CONNECTING}
          </div>
        )}
        {lines.length === 0 && state === "connected" && (
          <div
            data-testid="pod-log-placeholder"
            style={{ color: "var(--color-text-muted)" }}
          >
            {COPY_WAITING}
          </div>
        )}
        {(state === "offline" || state === "idle-closed") && (
          <div
            data-testid="pod-log-placeholder"
            style={{ color: "var(--color-status-warning)" }}
          >
            {COPY_DISCONNECTED}
          </div>
        )}
        {/* DASH-04 D-12: the reconnecting state was the silently-empty
            viewport — automatic backoff is in flight, so surface it. */}
        {state === "reconnecting" && (
          <div data-testid="pod-log-placeholder-reconnecting">
            <div
              className="flex items-center gap-2"
              style={{ color: "var(--color-text-muted)" }}
            >
              <Loader2 size={12} className="animate-spin" aria-hidden="true" />
              {COPY_RECONNECTING}
            </div>
            {/* Gap 37-G2: the auto-backoff continues in parallel, but the
                operator can force an immediate re-subscribe rather than wait
                it out. Secondary variant (NOT accent) per UI-SPEC Color rule —
                same styling as the stream-error Reconnect button. */}
            <button
              type="button"
              data-testid="pod-log-reconnecting-reconnect"
              onClick={() => reconnect()}
              className="mt-2 inline-flex items-center gap-1 rounded border border-[var(--color-border-subtle)] px-2 py-1 text-[var(--color-text-primary)] hover:bg-[var(--color-surface-raised)]"
              style={{ fontSize: "12px" }}
            >
              Reconnect
            </button>
          </div>
        )}
        {/* DASH-04 D-13: pod garbage-collected — honest message ONLY, no
            retry affordance (the pod will not come back). */}
        {state === "pod-gone" && (
          <div
            data-testid="pod-log-placeholder-pod-gone"
            style={{ color: "var(--color-text-muted)" }}
          >
            {COPY_POD_GONE}
          </div>
        )}
        {/* DASH-04 D-14: stream error — distinct from pod-gone, carries a
            manual Reconnect affordance and never claims the pod is gone. */}
        {state === "stream-error" && (
          <div data-testid="pod-log-placeholder-stream-error">
            <div
              className="flex items-center gap-2"
              style={{ color: "var(--color-destructive)", fontWeight: 600 }}
            >
              <AlertTriangle size={14} aria-hidden="true" />
              {COPY_STREAM_ERROR_HEADING}
            </div>
            <div
              style={{ color: "var(--color-text-muted)", marginTop: "4px" }}
            >
              {COPY_STREAM_ERROR_BODY}
            </div>
            <button
              type="button"
              data-testid="pod-log-reconnect"
              onClick={() => reconnect()}
              className="mt-2 inline-flex items-center gap-1 rounded border border-[var(--color-border-subtle)] px-2 py-1 text-[var(--color-text-primary)] hover:bg-[var(--color-surface-raised)]"
              style={{ fontSize: "12px" }}
            >
              Reconnect
            </button>
          </div>
        )}

        {parsed.map((segs, i) => (
          <div
            key={i}
            data-testid={`pod-log-line-${i}`}
            style={{
              whiteSpace: lineWrap ? "pre-wrap" : "pre",
              wordBreak: lineWrap ? "break-all" : "normal",
            }}
          >
            {segs.length === 0 ? (
              " "
            ) : (
              segs.map((seg, j) => (
                <span key={j} style={segmentStyle(seg)}>
                  {seg.text}
                </span>
              ))
            )}
          </div>
        ))}
      </div>
    </section>
  );
}
