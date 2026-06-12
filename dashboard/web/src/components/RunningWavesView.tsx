/*
 * RunningWavesView.tsx (plan 15-07) — UI-SPEC C3.
 *
 *   Aggregate "all running waves" view: right-pane default replacing the
 *   "Select a plan" empty state (D-13). Subscribes to the project-events SSE
 *   channel (own subscription — established multi-subscriber pattern) and
 *   consumes waves.snapshot events emitted by plan 15-06.
 *
 *   View states (locked by UI-SPEC C3 §View states table):
 *     null snapshot  → centered Loader2 spinner (L2 pane-loading)
 *     waves: []      → EmptyState variant "no-running-waves"
 *     waves: [...]   → header "ALL RUNNING WAVES" + card list
 *
 *   Defensive parse: malformed waves.snapshot events are ignored keeping last
 *   good state (T-15-21 mitigation — asserted by RunningWavesView.test.tsx).
 *
 *   XSS guard: all plan/task name strings render as React text children or
 *   attribute values — auto-escaped by React, no raw HTML injection (T-15-20).
 *
 *   Unknown status coerce: chip statuses pass through KNOWN_STATUS_VALUES;
 *   any string not in the list falls back to "Pending" (T-15-22 mitigation,
 *   shared guard from 15-05).
 */
import { useEffect, useRef, useState } from "react";
import { Loader2, Waves } from "lucide-react";

import StatusBadge, { KNOWN_STATUS_VALUES, type StatusValue } from "./StatusBadge";
import EmptyState from "./EmptyState";
import { useSSEStream } from "../lib/sse";
import { projectEventsURL } from "../lib/tasks";

// ── Wire types (UI-SPEC C3 §Props, matches server payload from plan 15-06) ──

/** A single task within a running wave, as delivered by waves.snapshot. */
export type RunningWaveTask = {
  /** Task name (matches tideproject.k8s/task CRD metadata.name). */
  name: string;
  /**
   * Task phase string from the wire payload. Coerced through KNOWN_STATUS_VALUES
   * before display — unknown values fall back to "Pending" (T-15-22).
   */
  status: string;
};

/** A running wave entry, as delivered by waves.snapshot. */
export type RunningWave = {
  /** Plan name — passed to onPlanClick on card activation (D-16). */
  planName: string;
  /**
   * 0-based wave index, same as WaveBackground labels in ExecutionDAGView.
   * Rendered as `WAVE <waveIndex + 1>` per the locked UI-SPEC label format.
   */
  waveIndex: number;
  /** Tasks in this wave, server-sorted by name asc. Client renders payload order. */
  tasks: RunningWaveTask[];
};

type WavesSnapshotPayload = {
  waves: RunningWave[];
};

type Props = {
  /** Selected project name — used to construct the SSE subscription URL. */
  projectName: string;
  /**
   * Callback invoked when a wave card is activated (click or Enter/Space).
   * Mirrors App.tsx's existing onPlanClick — sets hash + selectedPlan.
   */
  onPlanClick: (planName: string) => void;
  /**
   * Optional initial snapshot — tests bypass SSE by providing this directly,
   * mirroring PlanningDAGView.initialData. When provided the component skips
   * the pre-snapshot spinner and renders cards immediately.
   */
  initialSnapshot?: RunningWave[];
};

// ── Coerce guard (T-15-22) ─────────────────────────────────────────────────

/** Map any task status string to a known StatusValue; unknown → "Pending". */
function coerceStatus(status: string): StatusValue {
  return (KNOWN_STATUS_VALUES as readonly string[]).includes(status)
    ? (status as StatusValue)
    : "Pending";
}

// ── Running count helper ────────────────────────────────────────────────────

/** UI-SPEC C3: running = tasks in {Running, Dispatching}. */
function countRunning(tasks: RunningWaveTask[]): number {
  return tasks.filter(
    (t) => t.status === "Running" || t.status === "Dispatching",
  ).length;
}

// ── Wave card ───────────────────────────────────────────────────────────────

type WaveCardProps = {
  wave: RunningWave;
  onPlanClick: (planName: string) => void;
};

function WaveCard({ wave, onPlanClick }: WaveCardProps) {
  const { planName, waveIndex, tasks } = wave;
  const running = countRunning(tasks);
  const total = tasks.length;
  // UI-SPEC C3 §Copywriting Contract: "WAVE <N> · <running>/<total> running"
  // waveIndex is 0-based; label displays 1-based to match WaveBackground idiom.
  const waveLabel = `WAVE ${waveIndex + 1} · ${running}/${total} running`;
  const ariaLabel = `plan ${planName}, wave ${waveIndex + 1}, ${running} of ${total} tasks running`;

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      onPlanClick(planName);
    }
  };

  return (
    <div
      role="button"
      tabIndex={0}
      data-testid={`wave-card-${planName}-${waveIndex}`}
      data-plan={planName}
      data-wave-index={waveIndex}
      aria-label={ariaLabel}
      onClick={() => onPlanClick(planName)}
      onKeyDown={handleKeyDown}
      className="rounded border border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)] cursor-pointer hover:bg-[var(--color-surface-overlay)]"
    >
      {/* Header row: Waves icon + plan name + wave label */}
      <div className="flex items-center gap-2 px-3 py-2">
        <Waves
          size={14}
          aria-hidden="true"
          data-icon="Waves"
          style={{ color: "var(--color-text-muted)", flexShrink: 0 }}
        />
        <span
          className="min-w-0 flex-1 truncate"
          title={planName}
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: "13px",
            fontWeight: 600,
            lineHeight: 1.4,
          }}
        >
          {planName}
        </span>
        <span
          className="shrink-0"
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: "12px",
            fontWeight: 600,
            lineHeight: 1.4,
            color: "var(--color-text-muted)",
          }}
        >
          {waveLabel}
        </span>
      </div>
      {/* 1px divider — UI-SPEC C3 card anatomy */}
      <div
        style={{
          height: "1px",
          background: "var(--color-border-subtle)",
        }}
      />
      {/* Chip row: one StatusBadge per task, decorative (aria-hidden) */}
      <div
        className="flex flex-wrap gap-2 px-3 py-2"
        aria-hidden="true"
      >
        {tasks.map((task) => (
          <span
            key={task.name}
            title={task.name}
            data-testid="wave-card-chip"
          >
            <StatusBadge status={coerceStatus(task.status)} />
          </span>
        ))}
      </div>
    </div>
  );
}

// ── RunningWavesView ─────────────────────────────────────────────────────────

/**
 * <RunningWavesView> (UI-SPEC C3, plan 15-07).
 *
 * Aggregate running-waves view for a selected project. Subscribes to the
 * project-events SSE stream and consumes waves.snapshot events (registered
 * in SSE_PROJECT_EVENT_TYPES by this plan — build-blocking if absent).
 *
 * Snapshot-replace semantics: each waves.snapshot event replaces the whole
 * view state (no merging). Malformed events are ignored keeping last good
 * state. The server emits a snapshot on SSE subscribe so the pane populates
 * without waiting for a Task transition (plan 15-06).
 */
export default function RunningWavesView({
  projectName,
  onPlanClick,
  initialSnapshot,
}: Props) {
  // null = pre-snapshot (spinner); RunningWave[] = snapshot received
  const [waves, setWaves] = useState<RunningWave[] | null>(
    initialSnapshot ?? null,
  );

  // Track whether initialSnapshot was provided so we don't override it
  // with the SSE subscription when tests bypass SSE.
  const hasInitialRef = useRef(initialSnapshot !== undefined);

  // Subscribe to project-events SSE. waves.snapshot must be registered in
  // SSE_PROJECT_EVENT_TYPES (done in sse.ts by this plan — UI-SPEC C3
  // integration pitfall note).
  useSSEStream(
    hasInitialRef.current ? "" : projectEventsURL(projectName),
    {
      onMessage: (e: MessageEvent) => {
        if (e.type !== "waves.snapshot") return;
        // Defensive parse: malformed events are ignored (T-15-21).
        let parsed: WavesSnapshotPayload;
        try {
          parsed = JSON.parse(String(e.data)) as WavesSnapshotPayload;
        } catch {
          return;
        }
        if (!parsed || !Array.isArray(parsed.waves)) return;
        // Snapshot-replace semantics: full state replacement, no merging.
        setWaves(parsed.waves);
      },
    },
  );

  // Sync initialSnapshot prop changes (e.g. test re-renders with new fixture).
  useEffect(() => {
    if (initialSnapshot !== undefined) {
      setWaves(initialSnapshot);
    }
  }, [initialSnapshot]);

  // ── View states (UI-SPEC C3 §View states table) ─────────────────────────

  if (waves === null) {
    // Pre-snapshot: L2 pane-loading pattern — centered Loader2 spinner.
    return (
      <div
        data-testid="running-waves-view"
        className="flex h-full w-full items-center justify-center"
      >
        <Loader2
          size={24}
          aria-label="Loading running waves"
          className="animate-spin"
          style={{ color: "var(--color-text-muted)" }}
        />
      </div>
    );
  }

  if (waves.length === 0) {
    // Empty snapshot: no-running-waves EmptyState variant (UI-SPEC C3).
    return (
      <div data-testid="running-waves-view" className="h-full w-full">
        <EmptyState variant="no-running-waves" />
      </div>
    );
  }

  // Snapshot with ≥ 1 wave: header + scrollable card list.
  return (
    <div
      data-testid="running-waves-view"
      className="flex h-full flex-col overflow-y-auto p-4 gap-2"
    >
      {/* View header (UI-SPEC C3 §Layout) */}
      <div
        aria-hidden="true"
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: "12px",
          fontWeight: 600,
          color: "var(--color-text-muted)",
        }}
      >
        ALL RUNNING WAVES
      </div>
      {/* Card list — render payload order (D-15: client never re-sorts) */}
      {waves.map((wave) => (
        <WaveCard
          key={`${wave.planName}-${wave.waveIndex}`}
          wave={wave}
          onPlanClick={onPlanClick}
        />
      ))}
    </div>
  );
}
