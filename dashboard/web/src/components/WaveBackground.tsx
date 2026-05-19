import type { CSSProperties } from "react";

/**
 * `<WaveBackground>` (UI-SPEC §Component Inventory #6).
 *
 * Renders a single wave's background band inside the Execution DAG. Consumed
 * by plan 04-13's `<ExecutionDAGView>` as a per-wave SVG layer drawn behind
 * the dagre-laid-out task nodes — wave bands sit BELOW nodes and edges in
 * z-order per the React Flow v12 "Custom Background" recipe.
 *
 * The label format is "WAVE N · X tasks" — locked Copywriting Contract (see
 * UI-SPEC §Copywriting Contract). The literal "WAVE" string is required to
 * appear in this file per plan 04-15 must_haves.artifacts[WaveBackground].
 *
 * Active-band styling: stroke + stroke-dasharray "4 2" in --color-accent
 * (UI-SPEC §6). Failed-band styling: --color-status-blocked fill at 30%
 * opacity (UI-SPEC §6 "Failed band").
 */
export type WaveBackgroundProps = {
  /** 0-based index used to label the band ("WAVE 0 · …", "WAVE 1 · …"). */
  waveIndex: number;
  /** Geometry computed by the parent from member task positions + padding. */
  bounds: { x: number; y: number; width: number; height: number };
  /** True for the wave currently dispatching Jobs — gets the accent dash stroke. */
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

  // Stroke is only set on the active band (subtle dashed accent border).
  // React serializes camelCase SVG props to the kebab-case DOM attributes
  // (strokeDasharray → stroke-dasharray, fillOpacity → fill-opacity).
  const rectStyle: CSSProperties = {};
  type RectProps = {
    x: number;
    y: number;
    width: number;
    height: number;
    fill: string;
    fillOpacity: number;
    stroke?: string;
    strokeWidth?: number;
    strokeDasharray?: string;
  };
  const rectProps: RectProps = {
    x: bounds.x,
    y: bounds.y,
    width: bounds.width,
    height: bounds.height,
    fill: fillVar,
    fillOpacity: fillOpacity,
  };
  if (isActiveDispatch) {
    rectProps.stroke = "var(--color-accent)";
    rectProps.strokeWidth = 1;
    rectProps.strokeDasharray = "4 2";
  }

  // Label position: top-left corner inset by a small padding (~space-2 = 8px).
  const labelX = bounds.x + 8;
  const labelY = bounds.y + 16; // baseline ~ 16px from band top
  const labelText = `WAVE ${waveIndex} · ${taskCount} tasks`;

  return (
    <g
      data-testid={`wave-background-${waveIndex}`}
      data-wave-index={waveIndex}
      data-active-dispatch={isActiveDispatch ? "true" : "false"}
      data-failed={isFailed ? "true" : "false"}
      style={{ zIndex: 0 }}
    >
      <rect
        {...rectProps}
        style={rectStyle}
        aria-hidden="true"
      />
      <text
        x={labelX}
        y={labelY}
        fill="var(--color-text-muted)"
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: "12px",
          fontWeight: 600,
          // Decorative — the wave structure is conveyed via node aria-labels;
          // this label is for sighted scanning only.
          pointerEvents: "none",
        }}
        aria-hidden="true"
      >
        {labelText}
      </text>
    </g>
  );
}
