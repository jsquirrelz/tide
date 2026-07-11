/*
 * TelemetryDisabledBanner.tsx (Phase 38, plan 38-05 — TELEM-03)
 *
 * The single view-level banner rendered above the Telemetry toolbar when
 * telemetry is disabled-by-config (chart prometheus.enabled=false, D-14) or
 * enabled-but-empty (no samples in range). Purely presentational — state
 * derivation lives in TelemetryView; per-panel degradation stays with
 * TelemetryUnavailableNotice (EC-6).
 *
 * The two states are distinguishable by text, not just color (TELEM-03's
 * core requirement): "TELEMETRY DISABLED" vs "NO TELEMETRY DATA". Read-only
 * dashboard — no buttons, no links, no dismissal, no animation.
 */

export type TelemetryDisabledBannerState = "disabled-by-config" | "no-data";

export type TelemetryDisabledBannerProps = {
  state: TelemetryDisabledBannerState;
};

/**
 * Copy is locked by the 38-UI-SPEC Copywriting Contract (verbatim). The
 * border token is the state discriminator: warning for the actionable
 * config gap, subtle for the neutral no-data nudge — never error-red
 * (graceful-degradation contract, EC-6).
 */
const BANNER_COPY: Record<
  TelemetryDisabledBannerState,
  { label: string; message: string; borderToken: string }
> = {
  "disabled-by-config": {
    label: "TELEMETRY DISABLED",
    message:
      'prometheus.enabled is false — run telemetry beyond the budget tally is dark. Enable it via the "Enable telemetry" step in docs/INSTALL.md.',
    borderToken: "var(--color-status-warning)",
  },
  "no-data": {
    label: "NO TELEMETRY DATA",
    message:
      "Prometheus is enabled but returned no samples in this range — metrics appear once the first dispatch is scraped. Check the Targets page if this persists.",
    borderToken: "var(--color-border-subtle)",
  },
};

export default function TelemetryDisabledBanner({
  state,
}: TelemetryDisabledBannerProps) {
  const copy = BANNER_COPY[state];
  return (
    <div
      data-testid="telemetry-disabled-banner"
      data-state={state}
      role="status"
      className="flex flex-col gap-2 rounded border p-4"
      style={{
        background: "var(--color-surface-raised)",
        borderColor: copy.borderToken,
      }}
    >
      <span
        style={{
          fontSize: "12px",
          fontWeight: 600,
          fontFamily: "var(--font-mono)",
          textTransform: "uppercase",
          letterSpacing: "0.05em",
          color: copy.borderToken,
        }}
      >
        {copy.label}
      </span>
      <p
        style={{
          fontSize: "13px",
          fontFamily: "var(--font-mono)",
          color: "var(--color-text-muted)",
        }}
      >
        {copy.message}
      </p>
    </div>
  );
}
