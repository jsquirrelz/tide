import {
  Ban,
  Circle,
  CircleCheck,
  CircleCheckBig,
  CircleDot,
  CircleX,
  Hand,
  Hourglass,
  LockKeyhole,
  Loader2,
  Pause,
  SearchCheck,
  ShieldAlert,
  ShieldBan,
  type LucideIcon,
} from "lucide-react";
import type { CSSProperties } from "react";
import { clsx } from "../lib/clsx";

/**
 * The 13 CRD `.status.phase` values rendered by `<StatusBadge>`. Sourced
 * verbatim from UI-SPEC §Status Vocabulary. Order matches the spec table.
 *
 * `Hourglass` is exported by lucide-react and intentionally imported here so
 * downstream "elapsed time" affordances (drawer header chrono) can share the
 * icon set with the badge without re-importing lucide entries. The plan's
 * artifact contract requires `Hourglass` to appear in this file (see
 * 04-15-PLAN.md `must_haves.artifacts[StatusBadge].contains`).
 *
 * `Verifying`/`VerifyHalted` (53-UI-SPEC §Status Vocabulary, OBS-04) mirror
 * `LevelPhaseVerifying`/`LevelPhaseVerifyHalted` in api/v1alpha3/shared_types.go
 * byte-for-byte.
 */
export type StatusValue =
  | "Pending"
  | "Dispatching"
  | "Running"
  | "AwaitingApproval"
  | "Paused"
  | "Succeeded"
  | "Complete"
  | "Failed"
  | "PushLeaseFailed"
  | "PushLeakBlocked"
  | "Rejected"
  | "Verifying"
  | "VerifyHalted";

type StatusRow = {
  icon: LucideIcon;
  iconName: string;
  label: string;
  colorVar: string;
  srDescription: string;
  animationClass?: "animate-spin" | "animate-pulse";
};

/**
 * Verbatim map from CRD status → presentation row. Sourced directly from
 * UI-SPEC §Status Vocabulary table (columns: Color, lucide Icon, Label,
 * Animation, SR Description). Any divergence is a UI-SPEC violation.
 */
const STATUS_TABLE: Record<StatusValue, StatusRow> = {
  Pending: {
    icon: Circle,
    iconName: "Circle",
    label: "Pending",
    colorVar: "var(--color-status-pending)",
    srDescription: "Pending dispatch",
  },
  Dispatching: {
    icon: Loader2,
    iconName: "Loader2",
    label: "Dispatching",
    colorVar: "var(--color-status-running)",
    srDescription: "Dispatching — Job creation in progress",
    animationClass: "animate-spin",
  },
  Running: {
    icon: CircleDot,
    iconName: "CircleDot",
    label: "Running",
    colorVar: "var(--color-status-running)",
    srDescription: "Running",
    animationClass: "animate-pulse",
  },
  AwaitingApproval: {
    icon: Hand,
    iconName: "Hand",
    label: "Awaiting approval",
    colorVar: "var(--color-status-warning)",
    srDescription:
      "Awaiting human approval — run `tide approve` to advance",
  },
  Paused: {
    icon: Pause,
    iconName: "Pause",
    label: "Paused",
    colorVar: "var(--color-status-warning)",
    srDescription: "Paused at slack tide — run `tide resume` to advance",
  },
  Succeeded: {
    icon: CircleCheck,
    iconName: "CircleCheck",
    label: "Succeeded",
    colorVar: "var(--color-status-success)",
    srDescription: "Succeeded",
  },
  // UI-SPEC C1 (15-UI-SPEC.md): Project CRD terminal success (PhaseComplete,
  // project_types.go:392). Same success family as Succeeded; distinct glyph
  // (CircleCheckBig vs CircleCheck) per the color-blindness rule — these two
  // badges share green and can appear side by side in the Planning DAG.
  Complete: {
    icon: CircleCheckBig,
    iconName: "CircleCheckBig",
    label: "Complete",
    colorVar: "var(--color-status-success)",
    srDescription: "Complete — all milestones succeeded",
  },
  Failed: {
    icon: CircleX,
    iconName: "CircleX",
    label: "Failed",
    colorVar: "var(--color-status-error)",
    srDescription: "Failed — see logs and Conditions for details",
  },
  PushLeaseFailed: {
    icon: LockKeyhole,
    iconName: "LockKeyhole",
    label: "Push lease failed",
    colorVar: "var(--color-status-error)",
    srDescription:
      "Push lease failed — concurrent push detected by force-with-lease",
  },
  PushLeakBlocked: {
    icon: ShieldAlert,
    iconName: "ShieldAlert",
    label: "Push leak blocked",
    colorVar: "var(--color-status-blocked)",
    srDescription:
      "Push blocked by gitleaks — a secret pattern was detected in the diff",
  },
  Rejected: {
    icon: Ban,
    iconName: "Ban",
    label: "Rejected",
    colorVar: "var(--color-status-error)",
    srDescription: "Rejected by operator — run `tide resume` to clear",
  },
  // 53-UI-SPEC §Status Vocabulary (OBS-04): the Task loop's in-progress
  // state — an independent evaluator is reviewing the current attempt.
  // running-family: same color token + animate-pulse as Running.
  Verifying: {
    icon: SearchCheck,
    iconName: "SearchCheck",
    label: "Verifying",
    colorVar: "var(--color-status-running)",
    srDescription: "Verifying — independent evaluator reviewing this attempt",
    animationClass: "animate-pulse",
  },
  // 53-UI-SPEC §Status Vocabulary (OBS-04): the Task loop's terminal halt —
  // verification exhausted its LoopPolicy without an APPROVED verdict.
  // blocked-family: same color token as PushLeakBlocked, distinct glyph
  // (ShieldBan, not ShieldAlert) — no shared glyph within a color family.
  // Distinctness-from-Failed contract: colorVar/iconName/label all differ
  // from the Failed row above (asserted by StatusBadge.test.tsx).
  VerifyHalted: {
    icon: ShieldBan,
    iconName: "ShieldBan",
    label: "Verify halted",
    colorVar: "var(--color-status-blocked)",
    srDescription:
      "Verify halted — verification ended without an approved verdict. Review findings and run `tide resume` to advance",
  },
};

// Re-export so consumers can iterate the table (e.g. a primitives gallery).
export { STATUS_TABLE };

/**
 * Single source-of-truth list of all known status values, derived from
 * STATUS_TABLE keys. Both coerce guards (PlanningDAGView, ProjectPicker)
 * import this list instead of maintaining local literals — killing the
 * silent-drift bug class (UI-SPEC C2, 15-05-PLAN.md).
 */
export const KNOWN_STATUS_VALUES = Object.keys(
  STATUS_TABLE,
) as readonly StatusValue[];

// Re-export Hourglass so drawer chronograph affordances can share the icon set
// without re-importing lucide-react directly. Listed in plan must_haves.
export { Hourglass };

export type StatusBadgeProps = {
  status: StatusValue;
  className?: string;
};

/**
 * `<StatusBadge>` (UI-SPEC §Status Vocabulary).
 *
 * Inline-flex pill with a 14px lucide icon + label-size mono text. Background
 * is a 15%-alpha tint of the status color; border 40% alpha; foreground full
 * saturation. The aria-label uses the verbatim screen-reader description from
 * UI-SPEC.
 *
 * Color is threaded as a CSS variable via inline style so the badge inherits
 * the dark/light token surface without duplicating the table.
 */
export default function StatusBadge({ status, className }: StatusBadgeProps) {
  const row = STATUS_TABLE[status];
  const Icon = row.icon;

  // currentColor pattern: foreground = the status color; tinted bg/border via
  // color-mix on the same variable.
  const style: CSSProperties = {
    color: row.colorVar,
    background: `color-mix(in srgb, ${row.colorVar} 15%, transparent)`,
    border: `1px solid color-mix(in srgb, ${row.colorVar} 40%, transparent)`,
    borderRadius: "4px",
    fontFamily: "var(--font-mono)",
    fontSize: "12px",
    fontWeight: 600,
    lineHeight: 1.4,
  };

  return (
    <span
      data-testid={`status-badge-${status}`}
      data-status={status}
      role="status"
      aria-label={`Status: ${row.srDescription}`}
      className={clsx("inline-flex items-center gap-1 p-1 px-2", className)}
      style={style}
    >
      <span data-icon={row.iconName} className={clsx("inline-flex", row.animationClass)}>
        <Icon size={14} aria-hidden="true" />
      </span>
      <span>{row.label}</span>
    </span>
  );
}
