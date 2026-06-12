import { type ReactNode, useCallback, useEffect, useState } from "react";
import { Monitor, Moon, Sun, type LucideIcon } from "lucide-react";

const THEME_KEY = "tide.dashboard.theme";
type Theme = "system" | "dark" | "light";

// Icon reflecting the current theme setting (UI-SPEC §Header). The cycle order
// is system → dark → light; the glyph signals the current setting, not the next.
const THEME_ICON: Record<Theme, LucideIcon> = {
  system: Monitor,
  dark: Moon,
  light: Sun,
};

export type HeaderProps = {
  /** Slot for ConnectionStatusIndicator — populated by App.tsx. */
  connectionStatus: ReactNode;
  /** Slot for ProjectPicker — populated by plan 04-15 once ProjectPicker exists. */
  projectPicker?: ReactNode;
  /** Slot for the DAGs | Telemetry view switcher (UI-SPEC §C2, plan 16-05). */
  viewSwitcher?: ReactNode;
};

function resolveTheme(theme: Theme): "dark" | "light" {
  if (theme === "dark" || theme === "light") return theme;
  if (typeof window === "undefined") return "dark";
  return window.matchMedia("(prefers-color-scheme: light)").matches
    ? "light"
    : "dark";
}

function applyTheme(theme: Theme) {
  if (typeof document === "undefined") return;
  const resolved = resolveTheme(theme);
  document.documentElement.classList.toggle(
    "light-theme",
    resolved === "light",
  );
}

/**
 * The persistent top bar (UI-SPEC §Component Inventory #2 `<Header>`).
 *
 * Renders left-to-right:
 *   1. TIDE wordmark — text-only, 18px semibold mono. The decorative
 *      wave-shape underline lands in plan 04-15 via a small inline SVG.
 *   2. ProjectPicker slot (left of center) — populated by plan 04-15.
 *   3. ConnectionStatusIndicator slot (right of center) — populated by App.
 *   4. Theme toggle — minimal cycle button in v1.0 (system | dark | light);
 *      the dropdown menu UX lands in plan 04-15 alongside ProjectPicker.
 *
 * Height: 48px fixed (`--spacing-12`). Border-bottom: 1px subtle. Background:
 * --color-surface-raised.
 */
export default function Header({ connectionStatus, projectPicker, viewSwitcher }: HeaderProps) {
  const [theme, setTheme] = useState<Theme>(() => {
    if (typeof window === "undefined") return "system";
    const raw = window.localStorage.getItem(THEME_KEY);
    if (raw === "dark" || raw === "light" || raw === "system") return raw;
    return "system";
  });

  useEffect(() => {
    applyTheme(theme);
    if (typeof window !== "undefined") {
      window.localStorage.setItem(THEME_KEY, theme);
    }
  }, [theme]);

  const cycleTheme = useCallback(() => {
    setTheme((prev) => (prev === "system" ? "dark" : prev === "dark" ? "light" : "system"));
  }, []);

  const ThemeIcon = THEME_ICON[theme];

  return (
    <header
      className="flex h-12 items-center justify-between border-b border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)] px-4"
      role="banner"
    >
      <div className="flex items-center gap-4">
        <span
          className="mono font-semibold tracking-tight text-[var(--color-text-primary)]"
          style={{ fontSize: "var(--text-heading)", fontWeight: 600 }}
          aria-label="TIDE dashboard"
        >
          TIDE
        </span>
        {projectPicker}
        {viewSwitcher}
      </div>
      <div className="flex items-center gap-3">
        {connectionStatus}
        <button
          type="button"
          onClick={cycleTheme}
          aria-label={`Theme: ${theme}. Click to change.`}
          className="inline-flex rounded border border-[var(--color-border-subtle)] bg-transparent px-2 py-1 text-[var(--color-text-muted)] hover:bg-[var(--color-surface-overlay)]"
          style={{ fontSize: "var(--text-label)", fontWeight: 600 }}
        >
          <ThemeIcon size={14} aria-hidden="true" />
        </button>
      </div>
    </header>
  );
}
