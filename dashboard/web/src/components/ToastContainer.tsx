import {
  createContext,
  type ReactNode,
  useCallback,
  useContext,
  useMemo,
  useState,
} from "react";
import Toast, { type ToastProps, type ToastVariant } from "./Toast";

export type ToastInput = {
  variant: ToastVariant;
  title: string;
  body?: string;
  duration?: number;
};

type StackedToast = ToastInput & { id: number };

type ToastContextValue = {
  toast: (input: ToastInput) => void;
};

const ToastContext = createContext<ToastContextValue | null>(null);

const MAX_VISIBLE = 4; // UI-SPEC §Toast: "Max 4 visible at once".

/**
 * `<ToastProvider>` wraps the app and exposes the `useToast()` hook + portal
 * mount via `<ToastContainer>`. AppShell renders the container; this provider
 * is mounted higher (in App.tsx) so any subtree can `useToast()`.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [stack, setStack] = useState<StackedToast[]>([]);

  const dismiss = useCallback((id: number) => {
    setStack((current) => current.filter((t) => t.id !== id));
  }, []);

  const toast = useCallback((input: ToastInput) => {
    setStack((current) => {
      const next: StackedToast = { ...input, id: Date.now() + Math.random() };
      const combined = [...current, next];
      // Drop oldest if exceeding the visible cap (UI-SPEC §11).
      return combined.length > MAX_VISIBLE
        ? combined.slice(combined.length - MAX_VISIBLE)
        : combined;
    });
  }, []);

  const value = useMemo<ToastContextValue>(() => ({ toast }), [toast]);

  return (
    <ToastContext.Provider value={value}>
      {children}
      <ToastStackPortal stack={stack} onDismiss={dismiss} />
    </ToastContext.Provider>
  );
}

/**
 * Hook for consumers to emit toasts. Outside a `<ToastProvider>` it returns a
 * no-op (so unit tests of leaf components don't need to wrap them).
 */
export function useToast(): ToastContextValue {
  const ctx = useContext(ToastContext);
  if (!ctx) return { toast: () => undefined };
  return ctx;
}

/**
 * Bare container — kept as a separate export so `<AppShell>` can mount a
 * stable DOM anchor independent of when toasts arrive. The actual rendering
 * happens inside `<ToastProvider>` via `ToastStackPortal`.
 */
export default function ToastContainer() {
  // No-op anchor; the actual stack is rendered by the provider. Keeping a DOM
  // node here gives plan 04-13 a reliable selector if it ever needs to portal
  // anything else into the same z-stack region.
  return <div data-testid="toast-container-anchor" />;
}

type ToastStackProps = {
  stack: StackedToast[];
  onDismiss: (id: number) => void;
};

function ToastStackPortal({ stack, onDismiss }: ToastStackProps) {
  if (stack.length === 0) return null;
  return (
    <div
      data-testid="toast-stack"
      className="pointer-events-none fixed right-6 bottom-6 z-50 flex flex-col gap-2"
      aria-label="Notifications"
    >
      {stack.map((t) => {
        const props: ToastProps = {
          variant: t.variant,
          title: t.title,
          body: t.body,
          duration: t.duration,
          onDismiss: () => onDismiss(t.id),
        };
        return (
          <div key={t.id} className="pointer-events-auto">
            <Toast {...props} />
          </div>
        );
      })}
    </div>
  );
}
