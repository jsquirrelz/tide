/*
 * TelemetryUnavailableNotice.tsx (phase-03-telemetry-ui)
 *
 * Rendered in each chart-panel slot when Prometheus telemetry cannot be
 * delivered — either because the proxy endpoint is unconfigured (HTTP 200
 * with {"status":"unavailable"} sentinel) or because Prometheus is
 * configured-but-unreachable (HTTP 502).
 *
 * Styled with neutral muted tones only — no error-red, no spinner, no
 * blank space — per the graceful-degradation contract in MILESTONE.md EC-6.
 */

export type TelemetryUnavailableNoticeProps = {
  /**
   * Human-readable reason shown inside the panel slot.
   * Defaults to the generic "Prometheus not configured" wording.
   * Pass a message that includes the word "unreachable" for the
   * configured-but-unreachable (HTTP 502) case.
   */
  message?: string;
};

export default function TelemetryUnavailableNotice({
  message = "Telemetry unavailable — Prometheus not configured",
}: TelemetryUnavailableNoticeProps) {
  return (
    <div
      data-testid="telemetry-unavailable-notice"
      className="flex min-h-28 items-center justify-center rounded border border-dashed p-6"
      style={{
        borderColor: "var(--color-border-subtle)",
        color: "var(--color-text-muted)",
      }}
    >
      <p
        style={{
          fontSize: "13px",
          fontFamily: "var(--font-mono)",
          textAlign: "center",
          lineHeight: 1.6,
        }}
      >
        {message}
      </p>
    </div>
  );
}
