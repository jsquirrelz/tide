import {
  type KeyboardEvent as ReactKeyboardEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";
import { PanelRightOpen, X } from "lucide-react";

import { clsx } from "../lib/clsx";
import { ResizeHandle, usePersistedSize } from "./ResizeHandle";

/**
 * <NodeDetailPanel> (UI-SPEC §1, D-05/D-06) — the generalization of
 * TaskDetailDrawer's right-side panel chrome for ALL Planning-DAG node
 * kinds. This plan (37-04) ships the shell only; plan 37-08 mounts the
 * artifact/settings content as children and wires kind-aware click routing.
 *
 * TaskDetailDrawer itself is NOT modified — its a11y kit (focus capture +
 * restore, Escape-close, Tab focus-trap, backdrop, role=dialog) is
 * copied here wholesale, then extended with:
 *
 *   - Resize (D-06): a left-edge <ResizeHandle> drives the panel width via
 *     usePersistedSize("tide.dashboard.panel-width", 420, [360, 70vw]). The
 *     70vw max is recomputed at drag time (window resize), not frozen at
 *     mount.
 *   - Collapse (D-06): a header chevron collapses the panel to a 32px rail
 *     pinned right; the rail expands back to the persisted width. Collapse
 *     is DISTINCT from close — Escape/backdrop close (onClose); the chevron
 *     only collapses and keeps node selection alive (the component stays
 *     mounted). Persisted to "tide.dashboard.panel-collapsed".
 *
 * Styling is token-only; motion is the existing 180ms slide for
 * open/collapse and none during drag (the ResizeHandle writes width
 * directly).
 */

export type PlanningNodeKind = "project" | "milestone" | "phase" | "plan";

export type NodeDetailPanelProps = {
  open: boolean;
  kind: PlanningNodeKind;
  name: string;
  onClose: () => void;
  children: ReactNode;
};

const WIDTH_KEY = "tide.dashboard.panel-width";
const COLLAPSED_KEY = "tide.dashboard.panel-collapsed";
const DEFAULT_WIDTH = 420;
const MIN_WIDTH = 360;
const RAIL_WIDTH = 32;

const FOCUSABLE_SELECTOR =
  'button:not([disabled]), [href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

function maxWidth(): number {
  const vw = typeof window !== "undefined" ? window.innerWidth : 1200;
  return Math.round(vw * 0.7);
}

function readCollapsed(): boolean {
  try {
    return localStorage.getItem(COLLAPSED_KEY) === "true";
  } catch {
    return false;
  }
}

export default function NodeDetailPanel({
  open,
  kind,
  name,
  onClose,
  children,
}: NodeDetailPanelProps) {
  const titleId = useId();
  const panelRef = useRef<HTMLDivElement>(null);
  const previouslyFocusedRef = useRef<HTMLElement | null>(null);

  // 70vw ceiling tracked in state so it stays current "at drag time".
  const [maxW, setMaxW] = useState<number>(() => maxWidth());
  useEffect(() => {
    const onResize = () => setMaxW(maxWidth());
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const [width, setWidth, commitWidth] = usePersistedSize(
    WIDTH_KEY,
    DEFAULT_WIDTH,
    MIN_WIDTH,
    maxW,
  );

  const [collapsed, setCollapsed] = useState<boolean>(() => readCollapsed());

  const setCollapsedPersisted = useCallback((next: boolean) => {
    setCollapsed(next);
    try {
      localStorage.setItem(COLLAPSED_KEY, String(next));
    } catch {
      // Best-effort persistence.
    }
  }, []);

  // Capture previously-focused element on open, move focus into the panel,
  // and restore focus to the triggering element on close (mirrors
  // TaskDetailDrawer's restore effect, plus an explicit focus-in on open).
  useEffect(() => {
    if (!open) return;
    previouslyFocusedRef.current =
      (typeof document !== "undefined"
        ? (document.activeElement as HTMLElement | null)
        : null) ?? null;
    // Focus the panel (tabIndex=-1) when it is actually rendered (not the rail).
    if (!collapsed) {
      const el = panelRef.current;
      if (el && typeof el.focus === "function") {
        try {
          el.focus();
        } catch {
          // Best-effort — focus-in is not load-bearing for correctness.
        }
      }
    }
    return () => {
      const el = previouslyFocusedRef.current;
      if (el && typeof el.focus === "function") {
        try {
          el.focus();
        } catch {
          // Best-effort focus restoration.
        }
      }
    };
    // Intentionally keyed on `open` only — collapse toggles must not
    // re-capture/restore focus (that would fight the rail interaction).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // Escape closes the panel (collapse ≠ close — Escape always closes).
  useEffect(() => {
    if (!open) return;
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  // Focus trap — Tab cycles inside the panel; Shift+Tab cycles backwards.
  const onTrapKey = useCallback((e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (e.key !== "Tab") return;
    const root = panelRef.current;
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

  if (!open) return null;

  const backdrop = (
    <div
      data-testid="panel-backdrop"
      onClick={onClose}
      aria-hidden="true"
      className="fixed inset-0 z-40"
      style={{
        background: "color-mix(in srgb, var(--color-surface-base) 60%, transparent)",
      }}
    />
  );

  // Collapsed: a 32px rail pinned right. Enter/click expands. Keeps the
  // component mounted so node selection survives (D-06: collapse ≠ close).
  if (collapsed) {
    return (
      <>
        {backdrop}
        <button
          type="button"
          data-testid="panel-rail"
          aria-label="Expand panel"
          onClick={() => setCollapsedPersisted(false)}
          className={clsx(
            "fixed top-0 right-0 z-50 flex h-full items-start justify-center pt-4",
            "border-l border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)]",
            "text-[var(--color-text-muted)] hover:bg-[var(--color-surface-overlay)]",
          )}
          style={{
            width: `${RAIL_WIDTH}px`,
            transition: "width 180ms cubic-bezier(0.4, 0, 0.2, 1)",
          }}
        >
          {/* Rotated 180° to point back toward the collapsed panel. */}
          <PanelRightOpen size={16} aria-hidden="true" style={{ transform: "rotate(180deg)" }} />
        </button>
      </>
    );
  }

  return (
    <>
      {backdrop}
      <div
        ref={panelRef}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        data-testid="node-detail-panel"
        tabIndex={-1}
        onKeyDown={onTrapKey}
        className={clsx(
          "fixed top-0 right-0 z-50 flex h-full flex-col overflow-y-auto",
          "bg-[var(--color-surface-raised)] text-[var(--color-text-primary)]",
          "border-l border-[var(--color-border-subtle)] shadow-xl outline-none",
        )}
        style={{
          width: `${width}px`,
          transition: "transform 180ms cubic-bezier(0.4, 0, 0.2, 1)",
          transform: "translateX(0)",
        }}
      >
        {/* Left-edge resize handle (D-06). Absolutely pinned; the handle
            reports px, this component writes the width via usePersistedSize. */}
        <div className="absolute inset-y-0 left-0 z-10">
          <ResizeHandle
            orientation="vertical"
            value={width}
            min={MIN_WIDTH}
            max={maxW}
            onChange={setWidth}
            onCommit={commitWidth}
            label="Resize panel"
          />
        </div>

        {/* Header — mono 18px/600 title `<kind>/<name>`, collapse chevron, close. */}
        <div className="flex items-center justify-between border-b border-[var(--color-border-subtle)] px-6 py-4">
          <h2
            id={titleId}
            style={{ fontFamily: "var(--font-mono)", fontSize: "18px", fontWeight: 600 }}
          >
            {kind}/{name}
          </h2>
          <div className="flex items-center gap-1">
            <button
              type="button"
              data-testid="panel-collapse-chevron"
              onClick={() => setCollapsedPersisted(true)}
              aria-label="Collapse panel"
              className="inline-flex h-8 w-8 items-center justify-center rounded text-[var(--color-text-muted)] hover:bg-[var(--color-surface-overlay)]"
            >
              <PanelRightOpen size={16} aria-hidden="true" />
            </button>
            <button
              type="button"
              data-testid="panel-close"
              onClick={onClose}
              aria-label="Close panel"
              className="inline-flex h-8 w-8 items-center justify-center rounded text-[var(--color-text-muted)] hover:bg-[var(--color-surface-overlay)]"
            >
              <X size={16} aria-hidden="true" />
            </button>
          </div>
        </div>

        {/* Caller-provided content (37-05 ArtifactViewer / 37-08 settings). */}
        <div className="flex flex-1 flex-col" data-testid="panel-content">
          {children}
        </div>
      </div>
    </>
  );
}
