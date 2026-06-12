import {
  type KeyboardEvent as ReactKeyboardEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { ChevronDown } from "lucide-react";
import { clsx } from "../lib/clsx";
import StatusBadge, { KNOWN_STATUS_VALUES, type StatusValue } from "./StatusBadge";

/**
 * One entry in the project picker. The dashboard surfaces a project's
 * `<namespace>/<name>` identity + the current `.status.phase` rendered as a
 * `<StatusBadge>`. Phase strings flow through to the badge directly; any
 * unrecognized phase falls back to "Pending" via the runtime guard.
 */
export type ProjectEntry = {
  name: string;
  namespace: string;
  phase: string;
};

export type ProjectPickerProps = {
  projects: ProjectEntry[];
  value: string | null;
  onChange: (name: string) => void;
};

// Uses the shared KNOWN_STATUS_VALUES derived from STATUS_TABLE keys (UI-SPEC
// C2, 15-05-PLAN.md) — adding a new CRD status to StatusBadge automatically
// propagates here without a separate local-list update.
function coerceStatus(phase: string): StatusValue {
  return (KNOWN_STATUS_VALUES as readonly string[]).includes(phase)
    ? (phase as StatusValue)
    : "Pending";
}

/**
 * `<ProjectPicker>` (UI-SPEC §Component Inventory #9).
 *
 * Header-mounted dropdown. Click the trigger to open a panel listing all
 * Projects the dashboard's ServiceAccount can read. Selecting a row fires
 * `onChange(name)`; clicking outside or pressing Escape closes the panel.
 *
 * Empty / single / multi states (UI-SPEC §9):
 *   - empty: disabled trigger; "No projects in this cluster" in the panel.
 *   - single: full dropdown (no auto-select-only) — UX consistency.
 *   - multi: rows with `<namespace>/<name>` mono identity + StatusBadge.
 *
 * Keyboard shortcut `/` to open is deferred to plan 04-13 (see plan 04-15
 * task 2 description).
 */
export default function ProjectPicker({
  projects,
  value,
  onChange,
}: ProjectPickerProps) {
  const isEmpty = projects.length === 0;
  // Empty-state copy is surfaced immediately (UI-SPEC §13 E1) — operators
  // landing on an empty cluster shouldn't need to click the trigger to see
  // why nothing else is rendered. Non-empty states keep the panel hidden
  // until the operator opens it.
  const [isOpen, setIsOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  // Close on outside click + Escape (UI-SPEC §9).
  useEffect(() => {
    if (!isOpen) return;
    const onDocClick = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setIsOpen(false);
    };
    const onKey = (e: globalThis.KeyboardEvent) => {
      if (e.key === "Escape") setIsOpen(false);
    };
    document.addEventListener("mousedown", onDocClick);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDocClick);
      document.removeEventListener("keydown", onKey);
    };
  }, [isOpen]);

  const toggle = useCallback(() => {
    if (isEmpty) {
      // Even when disabled, opening the empty-state panel is still useful
      // so the operator sees the "No projects in this cluster" copy. We
      // therefore allow open=true with an empty body — the trigger's
      // aria-disabled still signals "no selection possible".
      setIsOpen((prev) => !prev);
      return;
    }
    setIsOpen((prev) => !prev);
  }, [isEmpty]);

  const onRowKey = useCallback(
    (e: ReactKeyboardEvent<HTMLLIElement>, name: string) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        onChange(name);
        setIsOpen(false);
      }
    },
    [onChange],
  );

  const triggerLabel =
    value ??
    (isEmpty
      ? "No projects"
      : projects.length === 1
        ? `${projects[0].namespace}/${projects[0].name}`
        : "Select project");

  return (
    <div ref={rootRef} className="relative">
      <button
        type="button"
        onClick={toggle}
        aria-label="Open project picker"
        aria-expanded={isOpen}
        aria-haspopup="listbox"
        aria-disabled={isEmpty}
        data-testid="project-picker-trigger"
        className={clsx(
          "inline-flex items-center gap-2 rounded border border-[var(--color-border-subtle)] bg-transparent px-3 py-1",
          "text-[var(--color-text-primary)] hover:bg-[var(--color-surface-overlay)]",
        )}
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: "13px",
        }}
      >
        <span>{triggerLabel}</span>
        <ChevronDown size={14} aria-hidden="true" />
      </button>
      {/* Empty-state copy is surfaced unconditionally — see comment above.
          Non-empty panels still gate on isOpen so the dropdown stays out of
          the operator's way until they click. */}
      {isEmpty && (
        <p
          data-testid="project-picker-empty"
          className="mt-1 text-[var(--color-text-muted)]"
          style={{ fontSize: "var(--text-body)" }}
        >
          No projects in this cluster
        </p>
      )}
      {!isEmpty && isOpen && (
        <div
          role="dialog"
          aria-label="Project picker panel"
          className={clsx(
            "absolute top-full left-0 z-40 mt-2 w-72 rounded border border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)] p-2 shadow-lg",
          )}
        >
          <ul role="listbox" className="flex flex-col gap-1">
            {projects.map((p) => {
              const id = `${p.namespace}/${p.name}`;
              const isSelected = value === p.name;
              return (
                <li
                  key={id}
                  role="option"
                  aria-label={id}
                  aria-selected={isSelected}
                  tabIndex={0}
                  onClick={() => {
                    onChange(p.name);
                    setIsOpen(false);
                  }}
                  onKeyDown={(e) => onRowKey(e, p.name)}
                  className={clsx(
                    "flex items-center justify-between gap-3 rounded px-2 py-2 cursor-pointer",
                    "hover:bg-[var(--color-surface-overlay)]",
                    isSelected && "bg-[var(--color-surface-overlay)]",
                  )}
                >
                  <span
                    className="mono text-[var(--color-text-primary)]"
                    style={{ fontSize: "13px" }}
                  >
                    <span className="text-[var(--color-text-muted)]">
                      {p.namespace}
                    </span>
                    <span aria-hidden="true">/</span>
                    <span>{p.name}</span>
                  </span>
                  <StatusBadge status={coerceStatus(p.phase)} />
                </li>
              );
            })}
          </ul>
        </div>
      )}
    </div>
  );
}
