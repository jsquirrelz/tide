import { Wallet, CreditCard, OctagonPause, type LucideIcon } from "lucide-react";
import type { CSSProperties } from "react";
import { clsx } from "../lib/clsx";

/**
 * Wire-mirror of the `blockingCondition` JSON shape emitted by
 * cmd/dashboard/api/projects.go (plan 14-06). The `type` field is an open
 * string so unknown condition types render nothing — defensive against
 * vocabulary drift, mirroring `coerce()`'s philosophy.
 *
 * See 14-UI-SPEC.md §C1 for the locked vocabulary table.
 */
export type ProjectBlockingCondition = {
  type: string;    // "BudgetBlocked" | "BillingHalt" | "VerifyHalt" (open string — unknown types render nothing)
  reason: string;  // e.g. "BudgetCapReached"
  message: string; // controller-stamped human message — surfaces as native title tooltip
  age: string;     // server-formatted relative time, e.g. "4m 12s"
};

type ConditionRow = {
  icon: LucideIcon;
  iconName: string;
  label: string;
  colorVar: string;
  srDescription: string;
};

/**
 * Verbatim map from Project blocking-condition type → presentation row.
 * Sourced directly from 14-UI-SPEC.md §C1 vocabulary table (columns: Color
 * token, lucide Icon, Label, Animation, SR Description). All three
 * conditions share --color-status-blocked (policy-halted, recoverable by
 * operator action — not a failure). Any divergence is a UI-SPEC violation.
 */
const CONDITION_TABLE: Record<string, ConditionRow> = {
  BudgetBlocked: {
    icon: Wallet,
    iconName: "Wallet",
    label: "Budget blocked",
    colorVar: "var(--color-status-blocked)",
    srDescription:
      "Budget cap reached — dispatch halted. Raise spec.budget.absoluteCapCents or apply the bypass annotation to resume",
  },
  BillingHalt: {
    icon: CreditCard,
    iconName: "CreditCard",
    label: "Billing halted",
    colorVar: "var(--color-status-blocked)",
    srDescription:
      "Provider credit balance too low — dispatch halted. Refill credits and run `tide resume`",
  },
  // 53-UI-SPEC §Condition Vocabulary (OBS-04): the Task/plan-check loop's
  // project-wide halt mirror of ConditionVerifyHalt. OctagonPause is
  // deliberately distinct from the task-level VerifyHalted status badge's
  // glyph (StatusBadge.tsx) — the project-level condition badge and the
  // task-level status badge can co-occur in one viewport; the blocked
  // family never shares a glyph across its rows.
  VerifyHalt: {
    icon: OctagonPause,
    iconName: "OctagonPause",
    label: "Verify halted",
    colorVar: "var(--color-status-blocked)",
    srDescription:
      "Verification halted without an approved verdict — dispatch held. Review staged findings and run `tide resume`",
  },
};

// Re-export so consumers can iterate the table (e.g. TideNodeShell aria-label
// builder, a primitives gallery). Mirrors STATUS_TABLE's re-export comment.
export { CONDITION_TABLE };

export type ConditionBadgeProps = {
  condition: ProjectBlockingCondition;
  className?: string;
};

/**
 * `<ConditionBadge>` (14-UI-SPEC §C1).
 *
 * Condition-vocabulary sibling of `<StatusBadge>` — identical pill anatomy
 * (14px lucide icon + 12px semibold mono label, tinted fill/border), keyed on
 * Project blocking-condition types instead of StatusValue. Returns null for
 * unknown condition types (defensive against vocabulary drift).
 *
 * `title` carries the controller-stamped message verbatim (never paraphrased)
 * so the native browser tooltip exposes the full machine-readable detail at
 * DAG-zoom scale where the badge label may be illegible. React escapes all JSX
 * attributes, so no XSS surface is introduced (T-14-07-01).
 */
export default function ConditionBadge({
  condition,
  className,
}: ConditionBadgeProps) {
  const row = CONDITION_TABLE[condition.type];

  // Unknown type → render nothing (T-14-07-02: client-side whitelist).
  if (!row) return null;

  const Icon = row.icon;

  // currentColor pattern identical to StatusBadge: foreground = colorVar full
  // saturation; background = 15% alpha tint; border = 40% alpha. Do not diverge.
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
      data-testid={`condition-badge-${condition.type}`}
      data-condition={condition.type}
      role="status"
      aria-label={row.srDescription}
      // title = controller message verbatim — native tooltip, never paraphrased.
      title={condition.message}
      className={clsx("inline-flex items-center gap-1 p-1 px-2", className)}
      style={style}
    >
      <span data-icon={row.iconName} className="inline-flex">
        <Icon size={14} aria-hidden="true" />
      </span>
      <span>{row.label}</span>
    </span>
  );
}
