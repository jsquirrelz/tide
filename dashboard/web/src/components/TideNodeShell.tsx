import { type KeyboardEvent, type ReactNode, useCallback } from "react";
import type { LucideIcon } from "lucide-react";

import { clsx } from "../lib/clsx";
import StatusBadge, { type StatusValue } from "./StatusBadge";
import { useNodeClick } from "./NodeClickContext";

/**
 * Shared visual shell for all 5 custom nodes (UI-SPEC §5).
 *
 *   ┌────────────────────────────────────────────┐
 *   │ <icon> <header>            <StatusBadge>   │
 *   │ ────────────────────────────────────────── │
 *   │ <summary line>                             │
 *   └────────────────────────────────────────────┘
 *
 * Border / hover / selected / failed styling per UI-SPEC §5:
 *   - Base: 1px --color-border-subtle / --color-surface-raised.
 *   - Hover: --color-surface-overlay.
 *   - Selected (props.selected): 2px ring --color-accent.
 *   - Failed (status ∈ {Failed, PushLeaseFailed, PushLeakBlocked, Rejected}):
 *     4px destructive border-left (Argo CD sidebar-accent style).
 *
 * Click routing: invokes the parent's `NodeClickContext` callback with the
 * node's `name`. Enter key activates (a11y).
 *
 * Phase 1 v1.0 — UI-SPEC §5 says PushLeaseFailed lives in the failed family
 * via "red/purple-adjacent" iconography. We treat it as failed-border too so
 * the visual signal is consistent.
 */

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
}: TideNodeShellProps) {
  const onNodeClick = useNodeClick();

  const fire = useCallback(() => {
    onNodeClick(name);
  }, [onNodeClick, name]);

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
      data-clickable={clickable ? "true" : "false"}
      {...(clickable
        ? {
            role: "button" as const,
            tabIndex: 0,
            onClick: fire,
            onKeyDown: onKey,
          }
        : {})}
      aria-label={`${KIND_LABEL[kind]} ${name}, status ${status}`}
      className={containerClass}
      style={{
        width: `${width}px`,
        minHeight: `${minHeight}px`,
      }}
    >
      {/* Header row */}
      <div className="flex items-center justify-between gap-2 px-3 py-2">
        <div className="flex items-center gap-2 min-w-0">
          <span data-icon={iconName} className="inline-flex shrink-0 text-[var(--color-text-muted)]">
            <Icon size={14} aria-hidden="true" />
          </span>
          <span
            className="truncate text-[var(--color-text-primary)]"
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: "13px",
              fontWeight: 600,
            }}
          >
            {headerLabel}
          </span>
        </div>
        <StatusBadge status={status} />
      </div>
      {/* Divider */}
      <div
        aria-hidden="true"
        className="h-px bg-[var(--color-border-subtle)]"
      />
      {/* Summary line */}
      <div
        className="px-3 py-2 text-[var(--color-text-muted)]"
        style={{
          fontSize: "12px",
          lineHeight: 1.3,
        }}
      >
        {summary}
      </div>
    </div>
  );
}
