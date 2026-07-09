/*
 * ApproveStrip.test.tsx (plan 37-05 Task 3, D-08).
 *
 *   1. renders the AwaitingApproval badge + the locked strip label.
 *   2. exactly two ClipboardCopyAction buttons — Approve (primary) copies
 *      `tide approve <project>`, Reject (destructive) copies
 *      `tide reject <project>`.
 *   3. read-only lock — no form elements, no fetch/mutation on click.
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";

import ApproveStrip from "./ApproveStrip";
import { ToastProvider } from "./ToastContainer";

function ensureClipboardStub() {
  if (!("clipboard" in navigator)) {
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText: async (_: string) => undefined },
      configurable: true,
      writable: true,
    });
  }
}

function renderStrip(projectName = "my-project") {
  return render(
    <ToastProvider>
      <ApproveStrip projectName={projectName} />
    </ToastProvider>,
  );
}

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe("ApproveStrip (Test 1) — badge + locked label", () => {
  it("renders the AwaitingApproval badge and the locked strip label", () => {
    renderStrip();
    expect(
      screen.getByTestId("status-badge-AwaitingApproval"),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        "Awaiting approval — review the artifact above, then approve from your terminal.",
      ),
    ).toBeInTheDocument();
  });
});

describe("ApproveStrip (Test 2) — two copy-only actions", () => {
  it("renders Approve (primary) and Reject (destructive) copying the CLI commands", async () => {
    ensureClipboardStub();
    const spy = vi
      .spyOn(navigator.clipboard, "writeText")
      .mockResolvedValue(undefined);

    renderStrip("my-project");

    const approve = screen.getByRole("button", { name: "Approve" });
    const reject = screen.getByRole("button", { name: "Reject" });
    expect(approve).toHaveAttribute("data-variant", "primary");
    expect(reject).toHaveAttribute("data-variant", "destructive");

    // Exactly two clipboard actions — no extra copy surfaces.
    expect(
      screen
        .getAllByRole("button")
        .filter((b) => b.getAttribute("data-testid")?.startsWith("clipboard-copy-")),
    ).toHaveLength(2);

    await act(async () => {
      fireEvent.click(approve);
    });
    expect(spy).toHaveBeenLastCalledWith("tide approve my-project");

    await act(async () => {
      fireEvent.click(reject);
    });
    expect(spy).toHaveBeenLastCalledWith("tide reject my-project");
  });
});

describe("ApproveStrip (Test 3) — read-only lock", () => {
  it("has no form elements and issues no fetch on click", async () => {
    ensureClipboardStub();
    vi.spyOn(navigator.clipboard, "writeText").mockResolvedValue(undefined);
    const fetchSpy = vi.fn();
    vi.stubGlobal("fetch", fetchSpy);

    const { container } = renderStrip();

    expect(
      container.querySelector("form, input, textarea, select"),
    ).toBeNull();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Approve" }));
      fireEvent.click(screen.getByRole("button", { name: "Reject" }));
    });

    expect(fetchSpy).not.toHaveBeenCalled();
  });
});
