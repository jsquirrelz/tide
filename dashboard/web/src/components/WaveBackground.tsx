/**
 * `<WaveBackground>` (UI-SPEC §Component Inventory #6).
 *
 * Renders a single wave's background band inside the Execution DAG. Consumed
 * by `<ExecutionDAGView>` inside React Flow's `<ViewportPortal>` so the band
 * shares the pan/zoom transform and sits in flow coordinates, aligned with the
 * dagre-laid-out task nodes. Bands sit BELOW nodes and edges in z-order
 * (z-index 0) per the React Flow v12 "Custom Background" recipe.
 *
 * The label format is "WAVE N · X task(s)" — locked Copywriting Contract (see
 * UI-SPEC §Copywriting Contract). The literal "WAVE" string is required to
 * appear in this file per plan 04-15 must_haves.artifacts[WaveBackground].
 *
 * Active-band styling: 1px dashed --color-accent border (UI-SPEC §6). Failed-
 * band styling: --color-status-blocked fill at 30% opacity (UI-SPEC §6).
 */
import { pluralize } from "../lib/pluralize";

export type WaveBackgroundProps = {
  /** 0-based index used to label the band ("WAVE 0 · …", "WAVE 1 · …"). */
  waveIndex: number;
  /** Geometry computed by the parent from member task positions + padding. */
  bounds: { x: number; y: number; width: number; height: number };
  /** True for the wave currently dispatching Jobs — gets the accent dash border. */
  isActiveDispatch: boolean;
  /** Number of tasks in the wave (used in the label). */
  taskCount: number;
  /** Number of failed tasks; > 0 triggers the blocked-color fill. */
  failedCount?: number;
};

export default function WaveBackground({
  waveIndex,
  bounds,
  isActiveDispatch,
  taskCount,
  failedCount = 0,
}: WaveBackgroundProps) {
  const isFailed = failedCount > 0;

  // Fill source: --color-status-blocked for failed bands; --color-surface-overlay
  // otherwise. Opacity tracks UI-SPEC §6: 0.4 inactive, 0.6 active, 0.3 failed.
  const fillVar = isFailed
    ? "var(--color-status-blocked)"
    : "var(--color-surface-overlay)";
  const fillOpacity = isFailed ? 0.3 : isActiveDispatch ? 0.6 : 0.4;

  // Active band gets a dashed accent border; otherwise a subtle solid border.
  const border = isActiveDispatch
    ? "1px dashed var(--color-accent)"
    : "1px solid var(--color-border-subtle)";

  const labelText = `WAVE ${waveIndex} · ${pluralize(taskCount, "task")}`;

  return (
    <div
      data-testid={`wave-background-${waveIndex}`}
      data-wave-index={waveIndex}
      data-active-dispatch={isActiveDispatch ? "true" : "false"}
      data-failed={isFailed ? "true" : "false"}
      style={{
        position: "absolute",
        left: bounds.x,
        top: bounds.y,
        width: bounds.width,
        height: bounds.height,
        background: fillVar,
        opacity: fillOpacity,
        border,
        borderRadius: 4,
        pointerEvents: "none",
        zIndex: 0,
      }}
      aria-hidden="true"
    >
      <span
        style={{
          position: "absolute",
          left: 8,
          top: 8,
          fontFamily: "var(--font-mono)",
          fontSize: "12px",
          fontWeight: 600,
          color: "var(--color-text-muted)",
          // Decorative — the wave structure is conveyed via node aria-labels;
          // this label is for sighted scanning only.
          pointerEvents: "none",
        }}
      >
        {labelText}
      </span>
    </div>
  );
}
