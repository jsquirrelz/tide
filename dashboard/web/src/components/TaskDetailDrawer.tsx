import {
  type KeyboardEvent as ReactKeyboardEvent,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";
import { Loader2, ScanSearch, X } from "lucide-react";

import { clsx } from "../lib/clsx";
import { fetchNodeArtifacts, type NodeArtifacts } from "../lib/api";
import StatusBadge, { type StatusValue, Hourglass } from "./StatusBadge";
import ClipboardCopyAction from "./ClipboardCopyAction";
import PhoenixTraceLink from "./PhoenixTraceLink";

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

export type TaskLoopEvaluation = {
  decision: string;
  findingsCount: number;
  highSeverityCount: number;
};

/**
 * Verdict → status-color-token map (53-UI-SPEC §Component Contract 1 +
 * §Auto-Resolved Decision Log #12). Exported so App.tsx's plan-level
 * one-line mirror (Component Contract 3) colors its decision substring
 * with the SAME map — a single source of truth, never a second copy.
 * BLOCKED maps to the blocked-family token, never the error token: a
 * VerifyHalt verdict is a distinct halt class, not a Failed reinterpretation.
 */
export const VERDICT_COLOR: Record<string, string> = {
  APPROVED: "var(--color-status-success)",
  REPAIRABLE: "var(--color-status-warning)",
  BLOCKED: "var(--color-status-blocked)",
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
  /** Plan 46-05: OBS-04 deep-link identity — absent hides <PhoenixTraceLink>. */
  traceId?: string;
  traceSpanId?: string;
  /**
   * Plan 53-07: the Task loop's current-iteration summary (Phase 53 D-07 /
   * OBS-04) — typed here so lib/tasks.ts's mapper has a target; the
   * "Verification" drawer section that RENDERS these fields is 53-08's
   * scope, not this plan's.
   */
  hasVerification?: boolean;
  loopIteration?: number;
  verifyMaxIterations?: number;
  loopExitReason?: string;
  lastEvaluation?: TaskLoopEvaluation;
  loopRunId?: string;
  attemptId?: string;
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
  /**
   * Plan 46-05: raw phoenixBaseURL from App.tsx's one-shot GET
   * /api/v1/config fetch. Defaults to "" (hidden link) so pre-46-05
   * callers/tests are unaffected.
   */
  phoenixBaseURL?: string;
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
    case "Verifying":
      // 53-UI-SPEC §Component Contract 4: mirrors Running's arm — the
      // independent evaluator is a live process, same cancel/tail affordances.
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
    case "VerifyHalted":
      // 53-UI-SPEC §Component Contract 4: Resume leads (primary) — no
      // approve/reject buttons, the mutation-free surface stays clipboard-copy
      // only; the approve-an-exhausted-loop flow is CLI territory.
      return [
        { variant: "primary", label: "Resume", command: `tide resume ${proj}` },
        {
          variant: "secondary",
          label: "Inspect wave",
          command: `tide inspect-wave ${plan}`,
        },
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
  phoenixBaseURL = "",
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

        {/* Verification section (53-UI-SPEC §Component Contract 1, OBS-04).
            Eligibility owned here: renders ONLY when the task carries a
            verification contract — a task with none shows no section at
            all (absence renders nothing, zero noise). Placed immediately
            after the metadata grid and before the Phoenix deep link, which
            stays the section's span-level history exit (no second mount). */}
        {task.hasVerification && (
          <div
            data-testid="drawer-verification"
            className="border-t border-[var(--color-border-subtle)] px-6 py-4"
          >
            <div
              className="text-[var(--color-text-muted)]"
              style={{
                fontSize: "11px",
                textTransform: "uppercase",
                letterSpacing: "0.04em",
              }}
            >
              Verification
            </div>
            <dl
              className="mt-3 grid grid-cols-2 gap-x-4 gap-y-3"
              style={{ fontSize: "13px" }}
            >
              <MetaRow
                label="Iteration"
                value={`${task.loopIteration ?? 0} of ${task.verifyMaxIterations ?? 0}`}
              />
              <MetaRow
                label="Verdict"
                value={task.lastEvaluation ? task.lastEvaluation.decision : "No verdict yet"}
                mono={Boolean(task.lastEvaluation)}
                color={
                  task.lastEvaluation
                    ? VERDICT_COLOR[task.lastEvaluation.decision]
                    : "var(--color-text-muted)"
                }
              />
              <MetaRow
                label="Findings"
                value={
                  task.lastEvaluation
                    ? `${task.lastEvaluation.findingsCount} total · ${task.lastEvaluation.highSeverityCount} high`
                    : "—"
                }
              />
              <MetaRow label="Exit reason" value={task.loopExitReason || "—"} mono />
              <MetaRow
                label="Loop run"
                value={task.loopRunId || "—"}
                mono
                truncate
                title={task.loopRunId}
              />
              <MetaRow
                label="Attempt ID"
                value={task.attemptId || "—"}
                mono
                truncate
                title={task.attemptId}
              />
            </dl>
            <div className="mt-3">
              <FindingsDisclosure task={task} />
            </div>
          </div>
        )}

        {/* Phoenix deep link (UI-SPEC mount 2, OBS-04) — sits with the
            metadata it relates to; border-t matches the section-separator
            convention. Renders nothing when config or trace identity is
            absent (owned by PhoenixTraceLink, not re-decided here). */}
        <PhoenixTraceLink
          baseURL={phoenixBaseURL}
          traceId={task.traceId ?? ""}
          spanId={task.traceSpanId}
          edge="top"
        />

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
  color,
}: {
  label: string;
  value: string;
  mono?: boolean;
  truncate?: boolean;
  title?: string;
  /** Optional override for the Verdict row's decision coloring (53-UI-SPEC
   *  §Component Contract 1) — inline style wins over the default text-color
   *  class below. Absent for every pre-existing MetaRow call site. */
  color?: string;
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
          color,
        }}
      >
        {value}
      </dd>
    </div>
  );
}

/**
 * `<FindingsDisclosure>` (53-UI-SPEC §Component Contract 2) — the trigger
 * link + toggled inline area. Mirrors the Phoenix trace link's anchor
 * anatomy (mono 13px underline, leading icon) but is a <button> (not an
 * <a>): it
 * toggles a disclosure, it never navigates. Mounting <FindingsContent> only
 * while open is what "clicking toggles ... that fetches" means — the fetch
 * fires from FindingsContent's own mount effect, never eagerly.
 */
function FindingsDisclosure({ task }: { task: TaskDetailData }) {
  const disclosureId = useId();
  const [open, setOpen] = useState(false);

  return (
    <div>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={disclosureId}
        onClick={() => setOpen((v) => !v)}
        data-testid="drawer-findings-toggle"
        className="inline-flex items-center gap-1 rounded underline text-[var(--color-text-primary)] hover:bg-[var(--color-surface-overlay)]"
        style={{ fontFamily: "var(--font-mono)", fontSize: "13px" }}
      >
        <ScanSearch size={14} aria-hidden="true" />
        View findings
      </button>
      {open && (
        <div id={disclosureId} data-testid="drawer-findings-content" className="mt-2">
          <FindingsContent task={task} />
        </div>
      )}
    </div>
  );
}

function prettyJSON(content: string): string {
  try {
    return JSON.stringify(JSON.parse(content), null, 2);
  } catch {
    // Malformed JSON still renders verbatim — never a library, never a throw.
    return content;
  }
}

/**
 * A compact state placeholder for the findings disclosure — mirrors
 * ArtifactViewer's StatePanel (same copy contract) at a scale that fits an
 * inline drawer area rather than a full pane.
 */
function FindingsStatePanel({
  testId,
  spinner,
  heading,
  body,
  action,
}: {
  testId: string;
  spinner?: boolean;
  heading: string;
  body: string;
  action?: { label: string; onClick: () => void };
}) {
  return (
    <div
      data-testid={testId}
      className="flex flex-col items-center px-2 py-6 text-center"
      style={{ color: "var(--color-text-primary)" }}
    >
      {spinner && (
        <Loader2
          size={18}
          aria-label="Loading"
          className="animate-spin"
          style={{ color: "var(--color-text-muted)", marginBottom: "8px" }}
        />
      )}
      <h3 style={{ fontSize: "13px", fontWeight: 600 }}>{heading}</h3>
      <p
        style={{
          marginTop: "8px",
          fontSize: "12px",
          color: "var(--color-text-muted)",
        }}
      >
        {body}
      </p>
      {action && (
        <button
          type="button"
          onClick={action.onClick}
          className="rounded border px-3 py-2"
          style={{
            marginTop: "12px",
            fontFamily: "var(--font-mono)",
            fontSize: "12px",
            fontWeight: 600,
            borderColor: "var(--color-border-subtle)",
            color: "var(--color-text-primary)",
            background: "transparent",
          }}
        >
          {action.label}
        </button>
      )}
    </div>
  );
}

/**
 * `<FindingsContent>` — fetches the task's staged findings.json via the
 * EXISTING GET /api/v1/nodes/task/{name}/artifacts endpoint
 * (fetchNodeArtifacts("task", ...)); no new endpoint. Reuses
 * ArtifactViewer's StatePanel state vocabulary (loading/error/no-git/
 * absent/available) with task-kind absent copy per 53-UI-SPEC §Component
 * Contract 2 — error/no-git copy is the existing locked ArtifactViewer text
 * verbatim.
 */
function FindingsContent({ task }: { task: TaskDetailData }) {
  const [data, setData] = useState<NodeArtifacts | null>(null);
  const mountedRef = useRef(true);

  const load = useCallback(async () => {
    try {
      // The namespace MUST be threaded through (4th arg) — the backend
      // defaults a missing `namespace` query param to "default" (debug #14,
      // lib/tasks.ts), so omitting it 404s every non-default-namespace
      // install. Mirrors ArtifactViewer's own fetchNodeArtifacts call.
      const result = await fetchNodeArtifacts(
        "task",
        task.name,
        task.projectName,
        task.namespace,
      );
      if (!mountedRef.current) return;
      setData(result);
    } catch (err) {
      if (!mountedRef.current) return;
      setData({
        state: "error",
        files: [],
        error: err instanceof Error ? err.message : String(err),
      });
    }
  }, [task.name, task.projectName, task.namespace]);

  useEffect(() => {
    mountedRef.current = true;
    void load();
    return () => {
      mountedRef.current = false;
    };
  }, [load]);

  if (data === null) {
    return (
      <div
        data-testid="findings-state-loading"
        className="flex items-center justify-center py-6"
      >
        <Loader2
          size={18}
          aria-label="Loading"
          className="animate-spin"
          style={{ color: "var(--color-text-muted)" }}
        />
      </div>
    );
  }

  if (data.state === "error") {
    return (
      <FindingsStatePanel
        testId="findings-state-error"
        heading="Couldn't fetch artifacts"
        body="The dashboard could not read the run branch. Check the dashboard logs: kubectl logs deploy/tide-dashboard"
        action={{ label: "Retry", onClick: () => void load() }}
      />
    );
  }

  if (data.state === "no-git") {
    return (
      <FindingsStatePanel
        testId="findings-state-no-git"
        heading="No git remote configured"
        body="Artifacts are stored on the run branch — this project has no git remote, so there is nothing to fetch."
      />
    );
  }

  if (data.state === "absent") {
    return (
      <FindingsStatePanel
        testId="findings-state-absent"
        heading="No findings staged yet"
        body="Findings land on the run branch after a verifier attempt completes. Check again after the current iteration finishes."
        action={{ label: "Check again", onClick: () => void load() }}
      />
    );
  }

  // state === "available"
  const findingsFile = (data.files ?? [])[0];
  return (
    <pre
      data-testid="findings-state-available"
      style={{
        fontFamily: "var(--font-mono)",
        fontSize: "12px",
        background: "var(--color-surface-overlay)",
        padding: "12px",
        borderRadius: "4px",
        overflowX: "auto",
        whiteSpace: "pre",
      }}
    >
      {findingsFile ? prettyJSON(findingsFile.content) : ""}
    </pre>
  );
}
