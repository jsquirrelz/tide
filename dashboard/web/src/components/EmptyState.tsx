/*
 * EmptyState.tsx (plan 04-16) — UI-SPEC §13.
 *
 *   Three empty-state surfaces, each centered in the available pane area.
 *   Copy is locked verbatim per UI-SPEC §13 + Copywriting Contract table.
 *
 *   E1 — no-projects: heading "No projects in this cluster" + body
 *     mentioning the `tide apply` snippet + an ASCII wave decoration as
 *     a <pre> block (the single place this decoration appears outside
 *     of the header wordmark underline).
 *   E2 — awaiting-first-milestone: terse copy; no button.
 *   E3 — plan-accepted-no-tasks: terse copy; no button.
 */
import type { ReactNode } from "react";

export type EmptyStateVariant =
  | "no-projects"
  | "awaiting-first-milestone"
  | "plan-accepted-no-tasks"
  // UI-SPEC C3 (15-07-PLAN.md): rendered by RunningWavesView when waves: [].
  | "no-running-waves"
  // Plan 26-04 (D-07): rendered by GlobalExecutionDAGView pre-data and on error.
  | "global-dag-no-tasks"
  | "global-dag-fetch-error";

export type EmptyStateProps = {
  variant: EmptyStateVariant;
};

/**
 * The wave-shape ASCII decoration from UI-SPEC §13 E1. Rendered as text
 * content inside a <pre> block so the spacing reproduces exactly.
 */
const WAVE_ART = `                          ◢◣
                       ◢━━━━━◣
                    ━━━━━━━━━━━━━
                  ━━━━━━━━━━━━━━━━━`;

function CenteredCard({ children }: { children: ReactNode }) {
  return (
    <div
      data-testid="empty-state"
      className="flex h-full w-full flex-col items-center justify-center px-6 py-16 text-center"
      style={{ color: "var(--color-text-primary)" }}
    >
      {children}
    </div>
  );
}

export default function EmptyState({ variant }: EmptyStateProps) {
  switch (variant) {
    case "no-projects":
      return (
        <CenteredCard>
          <pre
            data-testid="empty-wave-decoration"
            aria-hidden="true"
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: "18px",
              lineHeight: 1.0,
              color: "var(--color-text-muted)",
              margin: 0,
            }}
          >
            {WAVE_ART}
          </pre>
          <h2
            style={{
              marginTop: "48px",
              fontSize: "18px",
              fontWeight: 600,
              color: "var(--color-text-primary)",
            }}
          >
            No projects in this cluster
          </h2>
          <p
            style={{
              marginTop: "16px",
              fontSize: "14px",
              color: "var(--color-text-muted)",
              maxWidth: "440px",
            }}
          >
            Apply one with{" "}
            <code style={{ fontFamily: "var(--font-mono)" }}>
              tide apply -f project.yaml
            </code>{" "}
            or use kubectl directly:{" "}
            <code style={{ fontFamily: "var(--font-mono)" }}>
              kubectl apply -f project.yaml
            </code>
          </p>
        </CenteredCard>
      );

    case "awaiting-first-milestone":
      return (
        <CenteredCard>
          <h2
            style={{
              fontSize: "18px",
              fontWeight: 600,
              color: "var(--color-text-primary)",
            }}
          >
            Awaiting first milestone
          </h2>
          <p
            style={{
              marginTop: "16px",
              fontSize: "14px",
              color: "var(--color-text-muted)",
              maxWidth: "440px",
            }}
          >
            The orchestrator is composing the rising tide. This usually
            takes &lt; 30s.
          </p>
        </CenteredCard>
      );

    case "plan-accepted-no-tasks":
      return (
        <CenteredCard>
          <h2
            style={{
              fontSize: "18px",
              fontWeight: 600,
              color: "var(--color-text-primary)",
            }}
          >
            Plan accepted
          </h2>
          <p
            style={{
              marginTop: "16px",
              fontSize: "14px",
              color: "var(--color-text-muted)",
            }}
          >
            Computing wave structure…
          </p>
        </CenteredCard>
      );

    // UI-SPEC C3 (15-07-PLAN.md) §Copywriting Contract: no-running-waves variant.
    // Rendered by RunningWavesView when waves.snapshot delivers waves: [].
    case "no-running-waves":
      return (
        <CenteredCard>
          <h2
            style={{
              fontSize: "18px",
              fontWeight: 600,
              color: "var(--color-text-primary)",
            }}
          >
            No running waves
          </h2>
          <p
            style={{
              marginTop: "16px",
              fontSize: "14px",
              color: "var(--color-text-muted)",
              maxWidth: "440px",
            }}
          >
            Wave cards appear here while task Jobs run — select a plan to view
            its execution DAG.
          </p>
        </CenteredCard>
      );

    // Plan 26-04 (D-07) §Copywriting Contract: GlobalExecutionDAGView empty states.
    case "global-dag-no-tasks":
      return (
        <CenteredCard>
          <h2
            style={{
              marginTop: "48px",
              fontSize: "18px",
              fontWeight: 600,
              color: "var(--color-text-primary)",
            }}
          >
            No tasks in global DAG
          </h2>
          <p
            style={{
              marginTop: "16px",
              fontSize: "14px",
              color: "var(--color-text-muted)",
              maxWidth: "440px",
            }}
          >
            Wave derivation has not run yet — planning may still be in progress.
          </p>
        </CenteredCard>
      );

    case "global-dag-fetch-error":
      return (
        <CenteredCard>
          <h2
            style={{
              marginTop: "48px",
              fontSize: "18px",
              fontWeight: 600,
              color: "var(--color-text-primary)",
            }}
          >
            Could not load global DAG
          </h2>
          <p
            style={{
              marginTop: "16px",
              fontSize: "14px",
              color: "var(--color-text-muted)",
              maxWidth: "440px",
            }}
          >
            The execution-dag endpoint returned an error. Check the dashboard API logs.
          </p>
        </CenteredCard>
      );
  }
}
