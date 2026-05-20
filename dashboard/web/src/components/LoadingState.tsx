/*
 * LoadingState.tsx (plan 04-16) — UI-SPEC §15.
 *
 *   Three loading surfaces, each replacing what it loads (not a global
 *   overlay).
 *
 *   L1 — initial: centered Loader2 spinner + "Loading dashboard…" label.
 *   L2 — pane: spinner centered in its pane bounds. Sibling pane remains
 *     usable.
 *   L3 — drawer: skeleton placeholder bars (gray bars at
 *     --color-surface-overlay) for the title, status row, and metadata
 *     grid. Spinner overlays as a soft progress indicator.
 *
 *   Spinner uses lucide-react Loader2 with animate-spin per UI-SPEC §15.
 *   Animations respect prefers-reduced-motion via Tailwind defaults.
 */
import { Loader2 } from "lucide-react";

export type LoadingStateVariant = "initial" | "pane" | "drawer";

export type LoadingStateProps = {
  variant: LoadingStateVariant;
};

export default function LoadingState({ variant }: LoadingStateProps) {
  if (variant === "drawer") return <DrawerSkeleton />;

  // L1 + L2 share the centered-spinner layout. L1 adds a label below.
  const size = variant === "initial" ? 32 : 24;
  return (
    <div
      data-testid="loading-state"
      className="flex h-full w-full flex-col items-center justify-center gap-3"
    >
      <Loader2
        data-testid="loading-spinner"
        size={size}
        aria-label="Loading"
        className="animate-spin"
        style={{ color: "var(--color-accent)" }}
      />
      {variant === "initial" && (
        <span
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: "12px",
            fontWeight: 600,
            color: "var(--color-text-muted)",
          }}
        >
          Loading dashboard…
        </span>
      )}
    </div>
  );
}

/**
 * Drawer skeleton — three gray placeholder bars per UI-SPEC §15 L3 at
 * 60% / 80% / 40% width. The spinner sits in the bottom-right as a soft
 * progress indicator.
 */
function DrawerSkeleton() {
  return (
    <div
      data-testid="loading-state"
      className="flex h-full w-full flex-col gap-4 p-6"
    >
      <SkelBar widthPct={60} />
      <SkelBar widthPct={80} />
      <SkelBar widthPct={40} />
      <div className="mt-auto flex justify-end">
        <Loader2
          data-testid="loading-spinner"
          size={16}
          aria-label="Loading"
          className="animate-spin"
          style={{ color: "var(--color-text-muted)" }}
        />
      </div>
    </div>
  );
}

function SkelBar({ widthPct }: { widthPct: number }) {
  return (
    <div
      role="presentation"
      style={{
        height: "16px",
        width: `${widthPct}%`,
        background: "var(--color-surface-overlay)",
        borderRadius: "4px",
      }}
    />
  );
}
