import { describe, it, expect, afterEach, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import ClipboardCopyAction from "./ClipboardCopyAction";
import { ToastProvider } from "./ToastContainer";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function ensureClipboardStub() {
  // jsdom doesn't ship navigator.clipboard by default — define a writable stub
  // so vi.spyOn / writes can target it consistently across tests.
  if (!("clipboard" in navigator)) {
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: async (_: string) => undefined },
      configurable: true,
      writable: true,
    });
  }
}

function renderWithToast(node: React.ReactNode) {
  return render(<ToastProvider>{node}</ToastProvider>);
}

describe("ClipboardCopyAction (UI-SPEC §10 Clipboard-Copy Action + §11 Toast copy)", () => {
  it("primary variant: success path writes to clipboard + emits success toast with verbatim UI-SPEC body", async () => {
    ensureClipboardStub();
    const spy = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue(undefined);

    renderWithToast(
      <ClipboardCopyAction
        command="tide apply"
        label="Apply"
        variant="primary"
      />,
    );

    const button = screen.getByRole("button", { name: "Apply" });
    await act(async () => {
      fireEvent.click(button);
    });

    expect(spy).toHaveBeenCalledWith("tide apply");
    await waitFor(() => {
      expect(screen.getByText("Command copied")).toBeInTheDocument();
    });
    expect(
      screen.getByText("Paste in your terminal to run: tide apply"),
    ).toBeInTheDocument();
  });

  it("failure path: clipboard rejection emits error toast with verbatim copy + duration 8000", async () => {
    ensureClipboardStub();
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValueOnce(
      new Error("blocked"),
    );

    renderWithToast(
      <ClipboardCopyAction
        command="tide apply"
        label="Apply"
        variant="primary"
      />,
    );

    const button = screen.getByRole("button", { name: "Apply" });
    await act(async () => {
      fireEvent.click(button);
    });

    await waitFor(() => {
      expect(screen.getByText("Couldn't copy")).toBeInTheDocument();
    });
    // Body uses the locked copy from TOAST_COPY.clipboardCopyFailure.
    expect(
      screen.getByText("Clipboard API blocked. Command: tide apply"),
    ).toBeInTheDocument();
    // The error toast is rendered via role=alert (UI-SPEC §Accessibility +
    // plan 04-12's Toast variant→role mapping). Duration is verified via the
    // exported constant in toast-copy.ts (8000ms) — see Toast.test.tsx for
    // the timer-driven assertion. Here we assert the variant routing.
    expect(screen.getByRole("alert")).toBeInTheDocument();
  });

  it.each([
    ["primary", "bg-[var(--color-accent)]"],
    ["destructive", "border-[var(--color-destructive)]"],
    ["secondary", "border-[var(--color-border-subtle)]"],
  ] as const)(
    "%s variant applies the locked UI-SPEC classNames",
    (variant, expectedSubstr) => {
      ensureClipboardStub();
      renderWithToast(
        <ClipboardCopyAction
          command="x"
          label={variant}
          variant={variant}
        />,
      );
      const button = screen.getByRole("button", { name: variant });
      expect(button.className).toContain(expectedSubstr);
    },
  );
});
