import { clsx } from "../lib/clsx";

export type ConnectionState = "connected" | "reconnecting" | "offline";

export type ConnectionStatusIndicatorProps = {
  state: ConnectionState;
  /** Optional tooltip text — e.g. "Last event received: 12s ago". */
  tooltip?: string;
};

// Locked copy strings from UI-SPEC §Copywriting Contract. Verbatim — do not
// edit without re-running the gsd-ui-checker.
const LABELS: Record<ConnectionState, string> = {
  connected: "connected",
  reconnecting: "reconnecting",
  offline: "offline",
};

// Token-driven dot color per state. Dark-theme palette values; light-theme
// overrides are applied via the `.light-theme` selector in src/index.css.
const DOT_TOKEN: Record<ConnectionState, string> = {
  connected: "var(--color-status-running)",
  reconnecting: "var(--color-status-warning)",
  offline: "var(--color-status-error)",
};

/**
 * Header connection-status pill (UI-SPEC §Component Inventory #12).
 *
 * Three states with verbatim labels (LABELS above). Each renders a colored dot
 * + label. `aria-live="polite"` so screen readers announce state changes
 * without interrupting the user.
 */
export default function ConnectionStatusIndicator({
  state,
  tooltip,
}: ConnectionStatusIndicatorProps) {
  return (
    <span
      role="status"
      aria-live="polite"
      title={tooltip}
      data-state={state}
      className={clsx(
        "mono inline-flex items-center gap-2 rounded-full border border-[var(--color-border-subtle)] bg-[var(--color-surface-overlay)] px-3 py-1",
      )}
      style={{ fontSize: "var(--text-label)", fontWeight: 600 }}
    >
      <span
        data-testid="connection-dot"
        aria-hidden="true"
        className={clsx(
          "inline-block size-2 rounded-full",
          state === "reconnecting" && "animate-pulse",
        )}
        style={{ backgroundColor: DOT_TOKEN[state] }}
      />
      <span className="text-[var(--color-text-primary)]">{LABELS[state]}</span>
    </span>
  );
}
