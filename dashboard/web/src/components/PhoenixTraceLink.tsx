import { ExternalLink } from "lucide-react";

import { clsx } from "../lib/clsx";
import { phoenixSpanURL } from "../lib/phoenixLink";

/**
 * <PhoenixTraceLink> (plan 46-05, UI-SPEC §Component Contract) — the ONE
 * "View trace in Phoenix" implementation both NodeDetailPanel content
 * (App.tsx nodePanelContent) and TaskDetailDrawer mount. Owns eligibility,
 * copy, icon, typography, and states — mount points pass data, they never
 * re-decide visibility or re-style the anchor (UI-SPEC §Consistency
 * Contract).
 *
 * `traceId` is accepted (not just `spanId`) because `phoenixLink.ts` also
 * exports `phoenixTraceURL` as the documented fallback floor: if the
 * span-anchored redirect proves unusable on a target Phoenix instance,
 * swapping the `href` below to `phoenixTraceURL(baseURL, traceId)` is the
 * one-line change (UI-SPEC §Href) — this component is that change's only
 * touch point.
 */
export type PhoenixTraceLinkProps = {
  /** Raw phoenixBaseURL from GET /api/v1/config ("" when unset). */
  baseURL: string;
  /** Deterministic run TraceID hex ("" when the payload omits it). */
  traceId: string;
  /** The node's {Level}TraceSpanID hex, when present. */
  spanId?: string;
  /** Which side gets the section border — see UI-SPEC §Mount points. */
  edge: "top" | "bottom";
};

export default function PhoenixTraceLink({
  baseURL,
  spanId,
  edge,
}: PhoenixTraceLinkProps) {
  // Eligibility owned here (never duplicated at mount points): both baseURL
  // and spanId must be non-empty. traceId alone does not qualify — spanId is
  // the signal that this node's span was actually emitted.
  if (!baseURL || !spanId) return null;

  const href = phoenixSpanURL(baseURL, spanId);

  return (
    <div
      data-testid="phoenix-trace-link-row"
      className={clsx(
        "px-6 py-4 border-[var(--color-border-subtle)]",
        edge === "top" ? "border-t" : "border-b",
      )}
    >
      <a
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        aria-label="View trace in Phoenix — opens in new tab"
        data-testid="phoenix-trace-link"
        className="inline-flex items-center gap-1 rounded underline text-[var(--color-text-primary)] hover:bg-[var(--color-surface-overlay)]"
        style={{ fontFamily: "var(--font-mono)", fontSize: "13px" }}
      >
        View trace in Phoenix
        <ExternalLink size={14} aria-hidden="true" />
      </a>
    </div>
  );
}
