import { type ReactNode, useEffect, useState } from "react";
import ToastContainer from "./ToastContainer";

const SPLIT_RATIO_KEY = "tide.dashboard.split-ratio";
const SPLIT_RATIO_DEFAULT = 0.5;
const SPLIT_RATIO_MIN = 0.3;
const SPLIT_RATIO_MAX = 0.7;

export type AppShellProps = {
  header: ReactNode;
  children: ReactNode;
};

/**
 * Top-level layout (UI-SPEC §Component Inventory #1 `<AppShell>`).
 *
 * Owns no domain state. Renders:
 *   - The persistent header bar (height 48px = --spacing-12) via the `header` slot.
 *   - The two-pane body via `children`.
 *   - The `<ToastContainer>` portal mount (always on).
 *
 * State: `splitRatio` is persisted to localStorage but not yet wired to a
 * resizable divider — plan 04-13 adds the drag affordance and replaces the
 * `children` shape with the explicit (left, right) split.
 */
export default function AppShell({ header, children }: AppShellProps) {
  const [splitRatio, setSplitRatio] = useState<number>(() => {
    if (typeof window === "undefined") return SPLIT_RATIO_DEFAULT;
    const raw = window.localStorage.getItem(SPLIT_RATIO_KEY);
    if (!raw) return SPLIT_RATIO_DEFAULT;
    const parsed = Number.parseFloat(raw);
    if (!Number.isFinite(parsed)) return SPLIT_RATIO_DEFAULT;
    return Math.min(SPLIT_RATIO_MAX, Math.max(SPLIT_RATIO_MIN, parsed));
  });

  useEffect(() => {
    if (typeof window === "undefined") return;
    window.localStorage.setItem(SPLIT_RATIO_KEY, splitRatio.toString());
  }, [splitRatio]);

  // splitRatio is read by plan 04-13's resizable divider. Surface a no-op
  // setter on window so the future drag handle can wire in without changing
  // this component's API. Suppress unused-var warning until then.
  void setSplitRatio;

  return (
    <div className="flex h-screen flex-col bg-[var(--color-surface-base)] text-[var(--color-text-primary)]">
      {header}
      <main className="flex-1 overflow-hidden" data-split-ratio={splitRatio}>
        {children}
      </main>
      <ToastContainer />
    </div>
  );
}
