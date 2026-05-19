import { useCallback } from "react";
import { clsx } from "../lib/clsx";
import { TOAST_COPY } from "../lib/toast-copy";
import { useToast } from "./ToastContainer";

/**
 * `<ClipboardCopyAction>` — the D-D6 surface (UI-SPEC §10 Clipboard-Copy
 * Action). Renders a single button that writes `command` to the system
 * clipboard via `navigator.clipboard.writeText` + emits a Toast confirming
 * the copy.
 *
 * Per UI-SPEC §Clipboard-Copy Action + §11 Toast: copy strings are
 * non-negotiable — sourced from `TOAST_COPY.clipboardCopySuccess` /
 * `TOAST_COPY.clipboardCopyFailure`. Failure uses an 8-second duration
 * (the spec's explicit deviation from the 4-second default — gives the
 * operator more time to copy the command from the toast body).
 *
 * Threat model: navigator.clipboard.writeText REQUIRES a user-initiated
 * event (browser platform guarantee; T-04-D1-clipboard "accept"). The
 * Toast feedback is also a verifiable echo of the copied text — the user
 * sees exactly what landed in the clipboard before pasting.
 *
 * Variant styling (UI-SPEC §10):
 *   - primary: bg --color-accent, fg #000
 *   - destructive: transparent bg, 1px --color-destructive border, fg --color-destructive
 *   - secondary: transparent bg, 1px --color-border-subtle border, fg --color-text-primary
 */
export type ClipboardVariant = "primary" | "destructive" | "secondary";

export type ClipboardCopyActionProps = {
  command: string;
  label: string;
  variant?: ClipboardVariant;
  /** Optional helper text rendered below the button (UI-SPEC §10). */
  description?: string;
};

const VARIANT_CLASS: Record<ClipboardVariant, string> = {
  primary:
    "bg-[var(--color-accent)] text-black border border-transparent hover:opacity-90",
  destructive:
    "bg-transparent border border-[var(--color-destructive)] text-[var(--color-destructive)] hover:bg-[var(--color-destructive-muted)]",
  secondary:
    "bg-transparent border border-[var(--color-border-subtle)] text-[var(--color-text-primary)] hover:bg-[var(--color-surface-overlay)]",
};

export default function ClipboardCopyAction({
  command,
  label,
  variant = "secondary",
  description,
}: ClipboardCopyActionProps) {
  const { toast } = useToast();

  const onClick = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(command);
      toast({
        variant: "success",
        title: TOAST_COPY.clipboardCopySuccess.title,
        body: TOAST_COPY.clipboardCopySuccess.body(command),
      });
    } catch {
      toast({
        variant: "error",
        title: TOAST_COPY.clipboardCopyFailure.title,
        body: TOAST_COPY.clipboardCopyFailure.body(command),
        duration: TOAST_COPY.clipboardCopyFailure.duration,
      });
    }
  }, [command, toast]);

  return (
    <div className="flex flex-col gap-1">
      <button
        type="button"
        onClick={onClick}
        data-variant={variant}
        data-testid={`clipboard-copy-${variant}`}
        className={clsx(
          "inline-flex items-center justify-center gap-2 rounded px-3 py-2",
          VARIANT_CLASS[variant],
        )}
        style={{
          fontSize: "13px",
          fontWeight: 600,
          fontFamily: "var(--font-mono)",
        }}
      >
        {label}
      </button>
      {description !== undefined && (
        <span
          className="text-[var(--color-text-muted)]"
          style={{ fontSize: "var(--text-label)" }}
        >
          {description}
        </span>
      )}
    </div>
  );
}
