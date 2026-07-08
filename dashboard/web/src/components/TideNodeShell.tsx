import { type KeyboardEvent, type ReactNode, useCallback } from "react";
import { Handle, Position } from "@xyflow/react";
import type { LucideIcon } from "lucide-react";

import { clsx } from "../lib/clsx";
import StatusBadge, { type StatusValue } from "./StatusBadge";
import ConditionBadge, {
  CONDITION_TABLE,
  type ProjectBlockingCondition,
} from "./ConditionBadge";
import { useNodeClick } from "./NodeClickContext";

/**
 * Shared visual shell for all 5 custom nodes (UI-SPEC §5).
 *
 *   ┌────────────────────────────────────────────┐
 *   │ <icon> <header>                            │
 *   │ ────────────────────────────────────────── │
 *   │ <StatusBadge>  <summary line>              │
 *   └────────────────────────────────────────────┘
 *
 * The title row owns the full node width so long names truncate as late as
 * possible; the StatusBadge moves down to lead the summary row.
 *
 * Edge attachment: React Flow custom nodes need <Handle> components in the DOM
 * for edges to connect. We render an invisible target/source pair (the graph is
 * read-only, isConnectable={false}) on the axis the laying-out direction uses —
 * vertical (Top/Bottom) for the Planning DAG (dagre TB), horizontal (Left/Right)
 * for the Execution DAG (dagre LR).
 *
 * Border / hover / selected / failed styling per UI-SPEC §5:
 *   - Base: 1px --color-border-subtle / --color-surface-raised.
 *   - Hover: --color-surface-overlay.
 *   - Selected (props.selected): 2px ring --color-accent.
 *   - Failed (status ∈ {Failed, PushLeaseFailed, PushLeakBlocked, Rejected}):
 *     4px destructive border-left (Argo CD sidebar-accent style).
 *
 * Click routing: invokes the parent's `NodeClickContext` callback with the
 * node's `(kind, name)` so the consumer can route per kind. Enter key
 * activates (a11y).
 *
 * Phase 1 v1.0 — UI-SPEC §5 says PushLeaseFailed lives in the failed family
 * via "red/purple-adjacent" iconography. We treat it as failed-border too so
 * the visual signal is consistent.
 */

// Handles must occupy the DOM (React Flow attaches edges to them) but show
// nothing — collapse to a 1px transparent point with no border.
const HANDLE_STYLE = {
  width: 1,
  height: 1,
  minWidth: 0,
  minHeight: 0,
  opacity: 0,
  background: "transparent",
  border: "none",
} as const;

const FAILED_STATUSES = new Set<string>([
  "Failed",
  "PushLeaseFailed",
  "PushLeakBlocked",
  "Rejected",
]);

const KIND_LABEL: Record<TideNodeKind, string> = {
  project: "project",
  milestone: "milestone",
  phase: "phase",
  plan: "plan",
  task: "task",
};

export type TideNodeKind = "project" | "milestone" | "phase" | "plan" | "task";

export type TideNodeShellProps = {
  kind: TideNodeKind;
  /** Name used both as the click-callback arg and the aria-label suffix. */
  name: string;
  /** Header text shown next to the kind icon. For ProjectNode this is "project/<name>". */
  headerLabel: string;
  /** Status string from the CRD `.status.phase`. */
  status: StatusValue;
  /** lucide icon for the node kind (UI-SPEC §5 table). */
  icon: LucideIcon;
  /** Stable string identity for the icon (for test assertions). */
  iconName: string;
  /** Summary line text rendered below the divider. */
  summary: ReactNode;
  /** From @xyflow's NodeProps. */
  selected?: boolean;
  /** Width per UI-SPEC §5 table (varies by kind). */
  width: number;
  /** Min height per UI-SPEC §5 table. */
  minHeight: number;
  /**
   * CR-04 fix: when false, the node renders without click affordance —
   * no onClick handler, no role="button", no tabIndex, no Enter/Space
   * keyboard handler, no hover/pointer cursor. Default true preserves
   * existing behavior for kinds that ARE clickable (Plan in the Planning
   * DAG, Task in the Execution DAG). Set false for Project/Milestone/Phase
   * nodes in the Planning DAG since those click callbacks pollute the
   * right-pane plan selection.
   */
  clickable?: boolean;
  /**
   * Axis the edge handles attach on. "vertical" (Top/Bottom) matches the
   * Planning DAG's dagre TB layout; "horizontal" (Left/Right) matches the
   * Execution DAG's dagre LR layout. Defaults to "vertical".
   */
  handleAxis?: "vertical" | "horizontal";
  /**
   * 14-UI-SPEC §C2: optional list of True blocking conditions on the Project CR
   * (e.g. BudgetBlocked, BillingHalt). Each whitelisted (CONDITION_TABLE) entry
   * renders one <ConditionBadge> in the summary row, immediately after
   * <StatusBadge>. When at least one whitelisted entry is present, a purple
   * border-l-4 is applied — the blocked sibling of the existing destructive
   * border-left, so blocked state reads at DAG-zoom scale where badges are
   * illegible. Unknown condition types drive nothing — no border, no badge, no
   * aria suffix (defensive against vocabulary drift, same as ConditionBadge).
   * Failed family (FAILED_STATUSES) takes precedence: destructive red wins if
   * both apply.
   * Defaults to [] so zero-condition render is byte-identical to today.
   */
  blockingConditions?: ProjectBlockingCondition[];
};

export default function TideNodeShell({
  kind,
  name,
  headerLabel,
  status,
  icon: Icon,
  iconName,
  summary,
  selected = false,
  width,
  minHeight,
  clickable = true,
  handleAxis = "vertical",
  blockingConditions = [],
}: TideNodeShellProps) {
  const onNodeClick = useNodeClick();

  const fire = useCallback(() => {
    onNodeClick(kind, name);
  }, [onNodeClick, kind, name]);

  const onKey = useCallback(
    (e: KeyboardEvent<HTMLDivElement>) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        fire();
      }
    },
    [fire],
  );

  const isFailed = FAILED_STATUSES.has(status);
  // 14-UI-SPEC §C2: every blocked-state surface (border, badge row, aria-label)
  // derives from the whitelist-filtered list so unknown condition types render
  // nothing anywhere — the same vocabulary-drift defense ConditionBadge enforces.
  const knownConditions = blockingConditions.filter(
    (c) => CONDITION_TABLE[c.type],
  );
  // isBlocked drives the purple border-l-4 and data-blocked attribute.
  const isBlocked = knownConditions.length > 0;

  // Read-only graph: handles exist only so edges have attachment anchors.
  // They must be in the DOM but invisible (no dots, no connect affordance).
  const targetPosition =
    handleAxis === "horizontal" ? Position.Left : Position.Top;
  const sourcePosition =
    handleAxis === "horizontal" ? Position.Right : Position.Bottom;

  // Tailwind v4 arbitrary-value classes for tokens; the Tailwind compiler
  // emits these as raw CSS variable references.
  // CR-04 fix: suppress hover/cursor affordance for non-clickable nodes so
  // the UI signal matches the click behavior.
  const containerClass = clsx(
    "flex flex-col rounded border border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)]",
    clickable && "cursor-pointer hover:bg-[var(--color-surface-overlay)]",
    // 2px accent ring when selected
    selected && "ring-2 ring-[var(--color-accent)] ring-offset-0",
    // 4px destructive border-left for failed family
    isFailed && "border-l-4 border-l-[var(--color-destructive)]",
    // 4px blocked border-left for policy-halted conditions (14-UI-SPEC §C2).
    // Destructive red takes precedence: only applied when not already failed.
    !isFailed && isBlocked && "border-l-4 border-l-[var(--color-status-blocked)]",
  );

  // CR-04 fix: when clickable=false, omit role="button"/tabIndex/onClick/
  // onKeyDown so the node is presentational only and clicks don't trigger
  // the right-pane callback. aria-label still names the node for screen
  // readers.
  return (
    <div
      data-testid={`tide-node-${kind}`}
      data-kind={kind}
      data-selected={selected ? "true" : "false"}
      data-failed={isFailed ? "true" : "false"}
      data-blocked={isBlocked ? "true" : "false"}
      data-clickable={clickable ? "true" : "false"}
      {...(clickable
        ? {
            role: "button" as const,
            tabIndex: 0,
            onClick: fire,
            onKeyDown: onKey,
          }
        : {})}
      aria-label={(() => {
        // 14-UI-SPEC §C2: extend with ", blocked: <Label1>[, <Label2>]" when blocked.
        const base = `${KIND_LABEL[kind]} ${name}, status ${status}`;
        if (!isBlocked) return base;
        const labels = knownConditions
          .map((c) => CONDITION_TABLE[c.type].label)
          .join(", ");
        return `${base}, blocked: ${labels}`;
      })()}
      className={containerClass}
      style={{
        width: `${width}px`,
        minHeight: `${minHeight}px`,
      }}
    >
      {/* Invisible edge-attachment handles (read-only graph). */}
      <Handle
        type="target"
        position={targetPosition}
        isConnectable={false}
        style={HANDLE_STYLE}
      />
      {/* Header row — icon + title only; title gets the full node width. */}
      <div className="flex items-center gap-2 px-3 py-2">
        <span data-icon={iconName} className="inline-flex shrink-0 text-[var(--color-text-muted)]">
          <Icon size={14} aria-hidden="true" />
        </span>
        <span
          // Native tooltip: tight node widths truncate long names, so hover
          // reveals the full header label.
          title={headerLabel}
          className="min-w-0 flex-1 truncate text-[var(--color-text-primary)]"
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: "13px",
            fontWeight: 600,
          }}
        >
          {headerLabel}
        </span>
      </div>
      {/* Divider */}
      <div
        aria-hidden="true"
        className="h-px bg-[var(--color-border-subtle)]"
      />
      {/* Summary row — StatusBadge leads, then any ConditionBadges (14-UI-SPEC §C2),
          then the summary text. The row's existing gap-2 provides badge separation;
          summary text absorbs the squeeze via min-w-0 truncate. */}
      <div
        className="flex items-center gap-2 px-3 py-2 text-[var(--color-text-muted)]"
        style={{
          fontSize: "12px",
          lineHeight: 1.3,
        }}
      >
        <span className="shrink-0">
          <StatusBadge status={status} />
        </span>
        {knownConditions.map((c) => (
          <span key={c.type} className="shrink-0">
            <ConditionBadge condition={c} />
          </span>
        ))}
        <span className="min-w-0 truncate">{summary}</span>
      </div>
      <Handle
        type="source"
        position={sourcePosition}
        isConnectable={false}
        style={HANDLE_STYLE}
      />
    </div>
  );
}
