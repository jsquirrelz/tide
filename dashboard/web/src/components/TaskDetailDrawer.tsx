import {
  type KeyboardEvent as ReactKeyboardEvent,
  useCallback,
  useEffect,
  useId,
  useRef,
} from "react";
import { X } from "lucide-react";

import { clsx } from "../lib/clsx";
import StatusBadge, { type StatusValue, Hourglass } from "./StatusBadge";
import ClipboardCopyAction from "./ClipboardCopyAction";

/**
 * <TaskDetailDrawer> (UI-SPEC §7).
 *
 *   Slide-in panel from the right edge. Overlays both panes (does NOT
 *   push them). 420px fixed width; height 100vh - header.
 *
 *   Animation: translateX 100% → 0 over 180ms, cubic-bezier(.4, 0, .2, 1).
 *   Backdrop is a --color-surface-base / 0.6 layer; click closes.
 *
 *   Internal layout (top to bottom):
 *     1. Header — drawer title `task/<name>` + close X button.
 *     2. Status row — <StatusBadge> + elapsed time mono text.
 *     3. Metadata grid — namespace / attempt / podName / exitCode / waveIndex /
 *        scheduledAt / envelopePath.
 *     4. Actions row — locked clipboard-copy buttons per UI-SPEC §10 table.
 *     5. Conditions list — bulleted CRD Status.Conditions[] with age.
 *     6. "Open log stream" button — full-width; wires to onOpenLogStream
 *        callback. The actual <PodLogStreamer> mount lands in plan 04-16.
 *
 *   Accessibility:
 *     - role="dialog" + aria-modal="true" + aria-labelledby.
 *     - Focus trap on Tab (cycles inside drawer when open).
 *     - Escape closes; focus returns to triggering element on close.
 */

export type TaskCondition = {
  type: string;
  reason: string;
  age: string;
};

export type TaskDetailData = {
  name: string;
  /** Project name needed for tide approve/reject/cancel/resume commands. */
  projectName: string;
  planName: string;
  status: StatusValue;
  namespace: string;
  attempt: number;
  attemptMax: number;
  podName: string;
  exitCode: number | null;
  waveIndex: number;
  scheduledAt: string;
  envelopePath: string;
  /** Pre-formatted elapsed string ("running for 4m 12s") for the chrono row. */
  elapsedText: string;
  conditions: TaskCondition[];
};

export type TaskDetailDrawerProps = {
  taskName: string | null;
  /** Resolved task data — null until fetch completes; component renders nothing if taskName is null. */
  task: TaskDetailData | null;
  onClose: () => void;
  /**
   * Wires to plan 04-16's <PodLogStreamer> mount. For this plan the
   * button is a wired stub that fires this callback — the streamer
   * component itself lands in plan 04-16.
   */
  onOpenLogStream: (taskName: string) => void;
};

type ActionButton = {
  variant: "primary" | "destructive" | "secondary";
  label: string;
  command: string;
};

/**
 * Build the Actions row buttons per UI-SPEC §10 "Locked button copy"
 * table. Commands are templated with the project / task / plan as
 * appropriate. This function is the SINGLE source of truth for the
 * status × action pairing; any locked-copy drift surfaces here.
 */
function actionsForStatus(task: TaskDetailData): ActionButton[] {
  const proj = task.projectName;
  const plan = task.planName;
  switch (task.status) {
    case "AwaitingApproval":
      // Task-level approval — uses `tide approve <project>` (UI-SPEC §10
      // row 1). The wave-boundary form lives on the PlanNode drawer and
      // is not surfaced from the task drawer.
      return [
        { variant: "primary", label: "Approve", command: `tide approve ${proj}` },
        { variant: "destructive", label: "Reject", command: `tide reject ${proj}` },
      ];
    case "Paused":
      return [
        { variant: "primary", label: "Resume", command: `tide resume ${proj}` },
        {
          variant: "destructive",
          label: "Cancel",
          command: `tide cancel ${proj} --force`,
        },
      ];
    case "Running":
    case "Dispatching":
      return [
        {
          variant: "destructive",
          label: "Cancel",
          command: `tide cancel ${proj} --force`,
        },
        {
          variant: "secondary",
          label: "Tail logs (CLI)",
          command: `tide tail ${task.name}`,
        },
      ];
    case "Failed":
    case "PushLeaseFailed":
    case "PushLeakBlocked":
      return [
        {
          variant: "secondary",
          label: "Retry push",
          command: `kubectl annotate plan ${plan} tideproject.k8s/retry-push=true`,
        },
        {
          variant: "destructive",
          label: "Cancel",
          command: `tide cancel ${proj} --force`,
        },
        {
          variant: "secondary",
          label: "Inspect wave",
          command: `tide inspect-wave ${plan}`,
        },
      ];
    case "Rejected":
      return [
        { variant: "primary", label: "Resume", command: `tide resume ${proj}` },
      ];
    case "Succeeded":
    case "Pending":
      return [
        {
          variant: "secondary",
          label: "Inspect wave",
          command: `tide inspect-wave ${plan}`,
        },
        {
          variant: "secondary",
          label: "Tail logs (CLI)",
          command: `tide tail ${task.name}`,
        },
      ];
    default:
      // exhaustive switch — every StatusValue is covered above.
      return [];
  }
}

const FOCUSABLE_SELECTOR =
  'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

export default function TaskDetailDrawer({
  taskName,
  task,
  onClose,
  onOpenLogStream,
}: TaskDetailDrawerProps) {
  const titleId = useId();
  const drawerRef = useRef<HTMLDivElement>(null);
  const previouslyFocusedRef = useRef<HTMLElement | null>(null);

  const isOpen = taskName !== null;

  // Capture previously-focused element on open + restore on close.
  useEffect(() => {
    if (!isOpen) return;
    previouslyFocusedRef.current =
      (typeof document !== "undefined" ? (document.activeElement as HTMLElement | null) : null) ??
      null;
    return () => {
      // Return focus to the triggering element on close (UI-SPEC §7).
      const el = previouslyFocusedRef.current;
      if (el && typeof el.focus === "function") {
        try {
          el.focus();
        } catch {
          // Fall through — restoring focus is best-effort.
        }
      }
    };
  }, [isOpen]);

  // Escape closes the drawer (UI-SPEC §Accessibility keyboard map).
  useEffect(() => {
    if (!isOpen) return;
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [isOpen, onClose]);

  // Focus trap — Tab cycles inside the drawer; Shift+Tab cycles backwards.
  const onTrapKey = useCallback((e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (e.key !== "Tab") return;
    const root = drawerRef.current;
    if (!root) return;
    const focusables = Array.from(
      root.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR),
    );
    if (focusables.length === 0) return;
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    const active = document.activeElement as HTMLElement | null;
    if (e.shiftKey && active === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && active === last) {
      e.preventDefault();
      first.focus();
    }
  }, []);

  if (!isOpen || !task) return null;

  const actions = actionsForStatus(task);

  return (
    <>
      <div
        data-testid="drawer-backdrop"
        onClick={onClose}
        aria-hidden="true"
        className="fixed inset-0 z-40"
        style={{
          background:
            "color-mix(in srgb, var(--color-surface-base) 60%, transparent)",
        }}
      />
      <div
        ref={drawerRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        data-testid="task-detail-drawer"
        onKeyDown={onTrapKey}
        className={clsx(
          "fixed top-0 right-0 z-50 flex h-full flex-col overflow-y-auto",
          "bg-[var(--color-surface-raised)] text-[var(--color-text-primary)]",
          "border-l border-[var(--color-border-subtle)] shadow-xl",
        )}
        style={{
          width: "420px",
          // Slide-in transition (UI-SPEC §7 Drawer Slide).
          transition: "transform 180ms cubic-bezier(0.4, 0, 0.2, 1)",
          transform: "translateX(0)",
        }}
      >
        {/* Header */}
        <div
          className="flex items-center justify-between border-b border-[var(--color-border-subtle)] px-6 py-4"
        >
          <h2
            id={titleId}
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: "18px",
              fontWeight: 600,
            }}
          >
            task/{task.name}
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close drawer"
            className="inline-flex h-8 w-8 items-center justify-center rounded text-[var(--color-text-muted)] hover:bg-[var(--color-surface-overlay)]"
          >
            <X size={16} aria-hidden="true" />
          </button>
        </div>

        {/* Status row */}
        <div className="flex items-center gap-3 px-6 py-4">
          <StatusBadge status={task.status} />
          <span
            className="inline-flex items-center gap-2 text-[var(--color-text-muted)]"
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: "13px",
            }}
            data-testid="drawer-elapsed"
          >
            <Hourglass size={14} aria-hidden="true" />
            {task.elapsedText}
          </span>
        </div>

        {/* Metadata grid */}
        <dl
          className="grid grid-cols-2 gap-x-4 gap-y-3 px-6 pb-4"
          data-testid="drawer-metadata"
          style={{ fontSize: "13px" }}
        >
          <MetaRow label="Namespace" value={task.namespace} mono />
          <MetaRow
            label="Attempt"
            value={`${task.attempt} of ${task.attemptMax}`}
          />
          <MetaRow label="Pod name" value={task.podName} mono />
          <MetaRow
            label="Exit code"
            value={task.exitCode === null ? "—" : String(task.exitCode)}
            mono
          />
          <MetaRow label="Wave index" value={String(task.waveIndex)} />
          <MetaRow label="Scheduled at" value={task.scheduledAt} mono />
          <MetaRow
            label="Envelope path"
            value={task.envelopePath}
            mono
            title={task.envelopePath}
            truncate
          />
        </dl>

        {/* Actions row */}
        <div
          className="flex flex-col gap-2 border-t border-[var(--color-border-subtle)] px-6 py-4"
          data-testid="drawer-actions"
        >
          {actions.map((a) => (
            <ClipboardCopyAction
              key={a.label}
              command={a.command}
              label={a.label}
              variant={a.variant}
            />
          ))}
        </div>

        {/* Conditions list */}
        {task.conditions.length > 0 && (
          <ul
            className="flex flex-col gap-1 border-t border-[var(--color-border-subtle)] px-6 py-4 text-[var(--color-text-muted)]"
            data-testid="drawer-conditions"
            style={{ fontSize: "12px" }}
          >
            {task.conditions.map((c) => (
              <li key={`${c.type}-${c.reason}`}>
                <span style={{ fontFamily: "var(--font-mono)" }}>{c.type}</span>
                {" · "}
                <span>{c.reason}</span>
                {" · "}
                <span>{c.age}</span>
              </li>
            ))}
          </ul>
        )}

        {/* Open log stream button — wired stub for plan 04-16 */}
        <div className="mt-auto border-t border-[var(--color-border-subtle)] px-6 py-4">
          <button
            type="button"
            onClick={() => onOpenLogStream(task.name)}
            aria-label="Open log stream"
            className={clsx(
              "w-full rounded border px-3 py-2",
              "border-[var(--color-accent)] text-[var(--color-accent)] bg-transparent",
              "hover:bg-[color-mix(in_srgb,var(--color-accent)_10%,transparent)]",
            )}
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: "13px",
              fontWeight: 600,
            }}
          >
            Open log stream
          </button>
        </div>
      </div>
    </>
  );
}

function MetaRow({
  label,
  value,
  mono = false,
  truncate = false,
  title,
}: {
  label: string;
  value: string;
  mono?: boolean;
  truncate?: boolean;
  title?: string;
}) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt
        className="text-[var(--color-text-muted)]"
        style={{ fontSize: "11px", textTransform: "uppercase", letterSpacing: "0.04em" }}
      >
        {label}
      </dt>
      <dd
        className={clsx(
          "text-[var(--color-text-primary)]",
          truncate && "truncate",
        )}
        title={title}
        style={{
          fontFamily: mono ? "var(--font-mono)" : undefined,
          fontSize: "13px",
        }}
      >
        {value}
      </dd>
    </div>
  );
}
