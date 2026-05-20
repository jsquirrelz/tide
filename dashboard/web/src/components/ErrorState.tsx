/*
 * ErrorState.tsx (plan 04-16) — UI-SPEC §14.
 *
 *   Two full-screen takeover error surfaces. The full-screen card
 *   (max-width 480px) sits centered on the page; copy + kubectl hint
 *   lines are verbatim per UI-SPEC §14.
 *
 *   ERR1 — backend-unreachable: the initial app shell could not reach
 *     the dashboard backend. Heading + body cite the configured URL +
 *     three kubectl hint commands.
 *   ERR2 — permission-denied: the dashboard SA cannot read Projects
 *     (RBAC misconfigured). Heading + body + the two RBAC kubectl/helm
 *     hint commands.
 */
import { AlertTriangle, Ban } from "lucide-react";

export type ErrorStateVariant = "backend-unreachable" | "permission-denied";

export type ErrorStateProps = {
  variant: ErrorStateVariant;
  /** Backend URL the browser tried to reach (rendered in ERR1 body). */
  url?: string;
  /** Click handler for the Retry button. Default: window.location.reload. */
  onRetry?: () => void;
};

function defaultRetry() {
  if (typeof window !== "undefined") window.location.reload();
}

export default function ErrorState({
  variant,
  url,
  onRetry = defaultRetry,
}: ErrorStateProps) {
  const Icon = variant === "permission-denied" ? Ban : AlertTriangle;
  const heading =
    variant === "permission-denied"
      ? "Permission denied"
      : "Dashboard backend unreachable";

  return (
    <div
      data-testid="error-state"
      role="alert"
      className="flex h-full w-full items-center justify-center px-6 py-16"
      style={{ background: "var(--color-surface-base)" }}
    >
      <div
        className="rounded border"
        style={{
          maxWidth: "480px",
          padding: "48px",
          background: "var(--color-surface-raised)",
          borderColor: "var(--color-border-subtle)",
        }}
      >
        <div className="flex items-start gap-3">
          <Icon
            size={20}
            aria-hidden="true"
            style={{ color: "var(--color-destructive)", marginTop: "2px" }}
          />
          <h2
            style={{
              fontSize: "18px",
              fontWeight: 600,
              color: "var(--color-text-primary)",
            }}
          >
            {heading}
          </h2>
        </div>

        {variant === "backend-unreachable" ? (
          <Err1Body url={url ?? ""} />
        ) : (
          <Err2Body />
        )}

        <div style={{ marginTop: "24px" }}>
          <button
            type="button"
            onClick={onRetry}
            aria-label="Retry"
            className="rounded border px-3 py-2"
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: "13px",
              fontWeight: 600,
              borderColor: "var(--color-border-subtle)",
              color: "var(--color-text-primary)",
              background: "transparent",
            }}
          >
            Retry
          </button>
        </div>
      </div>
    </div>
  );
}

function Err1Body({ url }: { url: string }) {
  return (
    <div
      style={{
        marginTop: "16px",
        fontSize: "14px",
        color: "var(--color-text-muted)",
      }}
    >
      <p>
        The browser couldn't connect to the dashboard backend at:
        <br />
        <code style={{ fontFamily: "var(--font-mono)" }}>{url}</code>
      </p>
      <p style={{ marginTop: "16px" }}>Possible causes:</p>
      <ul style={{ marginTop: "8px", paddingLeft: "20px", listStyle: "disc" }}>
        <li>The dashboard pod isn't running</li>
        <li>You're not port-forwarded to the dashboard service</li>
        <li>An ingress rule is blocking access</li>
      </ul>
      <p style={{ marginTop: "16px" }}>Hints:</p>
      <pre
        style={{
          marginTop: "8px",
          fontFamily: "var(--font-mono)",
          fontSize: "13px",
          color: "var(--color-text-primary)",
          background: "var(--color-surface-overlay)",
          padding: "12px",
          borderRadius: "4px",
          overflowX: "auto",
        }}
      >
{`$ kubectl get deploy -n tide-system tide-dashboard
$ kubectl logs -n tide-system deploy/tide-dashboard
$ kubectl port-forward -n tide-system svc/tide-dashboard 8080:80`}
      </pre>
    </div>
  );
}

function Err2Body() {
  return (
    <div
      style={{
        marginTop: "16px",
        fontSize: "14px",
        color: "var(--color-text-muted)",
      }}
    >
      <p>
        The dashboard backend's ServiceAccount cannot read Projects in this
        cluster.
      </p>
      <p style={{ marginTop: "16px" }}>
        Required ClusterRole:{" "}
        <code style={{ fontFamily: "var(--font-mono)" }}>
          tide-dashboard-readonly
        </code>{" "}
        (granted by the Helm chart's{" "}
        <code style={{ fontFamily: "var(--font-mono)" }}>
          dashboard.rbac.enabled: true
        </code>
        )
      </p>
      <p style={{ marginTop: "16px" }}>Hints:</p>
      <pre
        style={{
          marginTop: "8px",
          fontFamily: "var(--font-mono)",
          fontSize: "13px",
          color: "var(--color-text-primary)",
          background: "var(--color-surface-overlay)",
          padding: "12px",
          borderRadius: "4px",
          overflowX: "auto",
        }}
      >
{`$ kubectl describe clusterrolebinding tide-dashboard
$ helm get values tide -n tide-system | grep -A5 dashboard`}
      </pre>
    </div>
  );
}
