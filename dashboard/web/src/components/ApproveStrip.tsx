/*
 * ApproveStrip.tsx (plan 37-05, DASH-01 gate-parked, D-08).
 *
 *   The pinned gate-review strip shown only when a Planning-DAG node's
 *   status is AwaitingApproval. Sits at the BOTTOM of NodeDetailPanel's
 *   scroll container — the artifact content renders above it (D-08); plan
 *   37-08 owns the mounting. The sticky positioning is the strip's own CSS.
 *
 *   Read-only lock (D-08 / inherited D-D6): clipboard-copy only. No form,
 *   no fetch, no confirmation modal — running the pasted command in the
 *   operator's terminal IS the confirmation. The two actions reuse
 *   <ClipboardCopyAction> verbatim (same toast contract as Tail-logs).
 */
import StatusBadge from "./StatusBadge";
import ClipboardCopyAction from "./ClipboardCopyAction";

export type ApproveStripProps = {
  /** The Project CR name — the gate target for the approve/reject commands. */
  projectName: string;
};

export default function ApproveStrip({ projectName }: ApproveStripProps) {
  return (
    <div
      data-testid="approve-strip"
      className="sticky bottom-0 flex flex-col gap-3 border-t px-6 py-4"
      style={{
        background: "var(--color-surface-raised)",
        borderColor: "var(--color-border-subtle)",
      }}
    >
      <div className="flex items-center gap-3">
        <StatusBadge status="AwaitingApproval" />
        <span
          style={{
            fontSize: "12px",
            fontWeight: 600,
            lineHeight: 1.4,
            color: "var(--color-text-muted)",
          }}
        >
          Awaiting approval — review the artifact above, then approve from your
          terminal.
        </span>
      </div>
      <div className="flex gap-3">
        <ClipboardCopyAction
          variant="primary"
          label="Approve"
          command={`tide approve ${projectName}`}
        />
        <ClipboardCopyAction
          variant="destructive"
          label="Reject"
          command={`tide reject ${projectName}`}
        />
      </div>
    </div>
  );
}
