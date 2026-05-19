import { useEffect } from "react";
import { clsx } from "../lib/clsx";

export type ToastVariant = "info" | "success" | "warning" | "error";

export type ToastProps = {
  variant: ToastVariant;
  title: string;
  body?: string;
  /** ms before auto-dismiss. Defaults to 4000. Use 0 for sticky. */
  duration?: number;
  onDismiss?: () => void;
};

/**
 * Single toast (UI-SPEC §Component Inventory #11).
 *
 * Border-left 4px in variant color from the design tokens:
 *   - info / success → status-success / status-running
 *   - warning → status-warning
 *   - error → destructive
 *
 * Stack composition + lifecycle is managed by `<ToastContainer>` / `useToast()`.
 * This component is purely presentational — `onDismiss` is called once when
 * the duration elapses (or never if `duration === 0`).
 */
const BORDER_TOKEN: Record<ToastVariant, string> = {
  info: "var(--color-status-running)",
  success: "var(--color-status-success)",
  warning: "var(--color-status-warning)",
  error: "var(--color-destructive)",
};

// ARIA: success/info are "status" (non-interrupting), warning/error are "alert"
// (interrupting). Per UI-SPEC §Accessibility.
const ROLE: Record<ToastVariant, "status" | "alert"> = {
  info: "status",
  success: "status",
  warning: "alert",
  error: "alert",
};

export default function Toast({
  variant,
  title,
  body,
  duration = 4000,
  onDismiss,
}: ToastProps) {
  useEffect(() => {
    if (!onDismiss || duration <= 0) return;
    const id = window.setTimeout(onDismiss, duration);
    return () => window.clearTimeout(id);
  }, [duration, onDismiss]);

  return (
    <div
      role={ROLE[variant]}
      data-variant={variant}
      data-testid={`toast-${variant}`}
      className={clsx(
        "flex w-80 flex-col gap-1 rounded-md bg-[var(--color-surface-raised)] p-4 shadow-lg",
      )}
      style={{ borderLeft: `4px solid ${BORDER_TOKEN[variant]}` }}
    >
      <strong
        className="text-[var(--color-text-primary)]"
        style={{ fontSize: "var(--text-label)", fontWeight: 600 }}
      >
        {title}
      </strong>
      {body !== undefined && (
        <span
          className="text-[var(--color-text-muted)]"
          style={{ fontSize: "var(--text-body)", lineHeight: 1.5 }}
        >
          {body}
        </span>
      )}
    </div>
  );
}
