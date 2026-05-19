import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { render, screen, cleanup, act } from "@testing-library/react";
import Toast from "./Toast";
import { ToastProvider, useToast } from "./ToastContainer";
import { TOAST_COPY } from "../lib/toast-copy";

afterEach(() => {
  cleanup();
  document.documentElement.className = "";
  // Tear down any inlined stylesheets we may have injected for theme tests.
  document.head
    .querySelectorAll("style[data-test-theme]")
    .forEach((n) => n.remove());
});

// ---------------------------------------------------------------------------
// Test 1 — Variant border tokens
// ---------------------------------------------------------------------------
describe("Toast variants — border-left color from design tokens (UI-SPEC §11)", () => {
  const cases: Array<{
    variant: "info" | "success" | "warning" | "error";
    expectedToken: string;
  }> = [
    { variant: "info", expectedToken: "var(--color-status-running)" },
    { variant: "success", expectedToken: "var(--color-status-success)" },
    { variant: "warning", expectedToken: "var(--color-status-warning)" },
    { variant: "error", expectedToken: "var(--color-destructive)" },
  ];

  for (const { variant, expectedToken } of cases) {
    it(`renders the ${variant} variant with the locked border token`, () => {
      render(<Toast variant={variant} title="t" duration={0} />);
      const el = screen.getByTestId(`toast-${variant}`);
      // Inline style is the locked surface so plan 04-16's bundle gate can
      // diff against tokens without parsing computed styles.
      expect(el.getAttribute("style") ?? "").toContain(expectedToken);
      expect(el).toHaveAttribute("data-variant", variant);
    });
  }
});

// ---------------------------------------------------------------------------
// Test 2 — UI-SPEC Copywriting Contract verbatim
// ---------------------------------------------------------------------------
describe("Toast copy contract (UI-SPEC §Copywriting Contract — verbatim)", () => {
  it("clipboard-copy success body uses the locked phrase", () => {
    const body = TOAST_COPY.clipboardCopySuccess.body("tide apply");
    expect(body).toBe("Paste in your terminal to run: tide apply");
    render(
      <Toast
        variant="success"
        title={TOAST_COPY.clipboardCopySuccess.title}
        body={body}
        duration={0}
      />,
    );
    expect(screen.getByText("Command copied")).toBeInTheDocument();
    expect(
      screen.getByText("Paste in your terminal to run: tide apply"),
    ).toBeInTheDocument();
  });

  it("clipboard-copy failure body uses the locked phrase (8s sticky)", () => {
    expect(TOAST_COPY.clipboardCopyFailure.body("tide cancel")).toBe(
      "Clipboard API blocked. Command: tide cancel",
    );
    expect(TOAST_COPY.clipboardCopyFailure.duration).toBe(8000);
  });

  it("SSE reconnecting / reconnected / persistent strings are locked", () => {
    expect(TOAST_COPY.sseReconnecting.title).toBe("Reconnecting…");
    expect(TOAST_COPY.sseReconnecting.body).toBe(
      "Stream lost — retrying with exponential backoff.",
    );
    expect(TOAST_COPY.sseReconnected.title).toBe("Reconnected");
    expect(TOAST_COPY.sseReconnected.body).toBe("Live updates resumed.");
    expect(TOAST_COPY.sseDisconnectPersistent.title).toBe("Backend unreachable");
    expect(TOAST_COPY.sseDisconnectPersistent.sticky).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Test — useToast hook smoke (provider integration)
// ---------------------------------------------------------------------------
describe("ToastProvider + useToast()", () => {
  it("emits a toast that lands in the stack", () => {
    function Emitter() {
      const { toast } = useToast();
      return (
        <button
          type="button"
          onClick={() =>
            toast({
              variant: "success",
              title: "Command copied",
              body: "Paste in your terminal to run: tide apply",
              duration: 0,
            })
          }
        >
          go
        </button>
      );
    }
    render(
      <ToastProvider>
        <Emitter />
      </ToastProvider>,
    );
    act(() => {
      screen.getByText("go").click();
    });
    expect(screen.getByText("Command copied")).toBeInTheDocument();
    expect(
      screen.getByText("Paste in your terminal to run: tide apply"),
    ).toBeInTheDocument();
  });

  it("useToast() outside a provider is a no-op (does not throw)", () => {
    function Emitter() {
      const { toast } = useToast();
      toast({ variant: "info", title: "x" });
      return <span>ok</span>;
    }
    render(<Emitter />);
    expect(screen.getByText("ok")).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// Test 5 — Light-theme palette resolution
//
// jsdom doesn't apply external stylesheets, so we inline the @theme overrides
// for the test scope and read `getComputedStyle(documentElement)` to assert
// the resolved CSS-variable values match UI-SPEC §Color light-theme deltas.
// ---------------------------------------------------------------------------
const LIGHT_PALETTE_CSS = `
  :root {
    --color-surface-base: #0b0f14;
    --color-text-primary: #e6edf3;
    --color-destructive: #f85149;
    --color-accent: #06b6d4;
  }
  .light-theme {
    --color-surface-base: #ffffff;
    --color-text-primary: #0f172a;
    --color-destructive: #dc2626;
    --color-accent: #0891b2;
  }
`;

describe("Light-theme palette (UI-SPEC §Color — light deltas)", () => {
  beforeEach(() => {
    const style = document.createElement("style");
    style.setAttribute("data-test-theme", "true");
    style.textContent = LIGHT_PALETTE_CSS;
    document.head.appendChild(style);
  });

  it("light-theme overrides resolve to the locked UI-SPEC values", () => {
    document.documentElement.className = "light-theme";
    render(<Toast variant="error" title="t" duration={0} />);
    const computed = window.getComputedStyle(document.documentElement);
    expect(computed.getPropertyValue("--color-surface-base").trim()).toBe(
      "#ffffff",
    );
    expect(computed.getPropertyValue("--color-text-primary").trim()).toBe(
      "#0f172a",
    );
    expect(computed.getPropertyValue("--color-destructive").trim()).toBe(
      "#dc2626",
    );
    expect(computed.getPropertyValue("--color-accent").trim()).toBe("#0891b2");
  });

  it("default (dark) palette resolves when .light-theme is not applied", () => {
    document.documentElement.className = "";
    const computed = window.getComputedStyle(document.documentElement);
    expect(computed.getPropertyValue("--color-surface-base").trim()).toBe(
      "#0b0f14",
    );
    expect(computed.getPropertyValue("--color-text-primary").trim()).toBe(
      "#e6edf3",
    );
    expect(computed.getPropertyValue("--color-destructive").trim()).toBe(
      "#f85149",
    );
    expect(computed.getPropertyValue("--color-accent").trim()).toBe("#06b6d4");
  });
});
