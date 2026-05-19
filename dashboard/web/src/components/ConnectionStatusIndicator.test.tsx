import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import ConnectionStatusIndicator from "./ConnectionStatusIndicator";

afterEach(cleanup);

// Verbatim copy strings from UI-SPEC §Copywriting Contract — "connection pill".
const LOCKED_COPY = {
  connected: "connected",
  reconnecting: "reconnecting",
  offline: "offline",
} as const;

describe("ConnectionStatusIndicator (UI-SPEC §Component Inventory #12)", () => {
  it.each(["connected", "reconnecting", "offline"] as const)(
    "renders the %s state with verbatim copy + role=status + aria-live=polite",
    (state) => {
      render(<ConnectionStatusIndicator state={state} />);
      const pill = screen.getByRole("status");
      expect(pill).toHaveAttribute("aria-live", "polite");
      expect(pill).toHaveAttribute("data-state", state);
      expect(pill).toHaveTextContent(LOCKED_COPY[state]);
    },
  );

  it("attaches the tooltip via the title attribute when provided", () => {
    render(
      <ConnectionStatusIndicator
        state="connected"
        tooltip="Last event received: 12s ago"
      />,
    );
    expect(screen.getByRole("status")).toHaveAttribute(
      "title",
      "Last event received: 12s ago",
    );
  });

  it("colors the status dot per the locked design-token mapping", () => {
    const { rerender } = render(
      <ConnectionStatusIndicator state="connected" />,
    );
    const dot = () => screen.getByTestId("connection-dot");
    expect(dot().getAttribute("style") ?? "").toContain(
      "var(--color-status-running)",
    );

    rerender(<ConnectionStatusIndicator state="reconnecting" />);
    expect(dot().getAttribute("style") ?? "").toContain(
      "var(--color-status-warning)",
    );

    rerender(<ConnectionStatusIndicator state="offline" />);
    expect(dot().getAttribute("style") ?? "").toContain(
      "var(--color-status-error)",
    );
  });

  it("only the reconnecting state animates the dot (UI-SPEC §12)", () => {
    const { rerender } = render(
      <ConnectionStatusIndicator state="connected" />,
    );
    expect(screen.getByTestId("connection-dot").className).not.toContain(
      "animate-pulse",
    );

    rerender(<ConnectionStatusIndicator state="reconnecting" />);
    expect(screen.getByTestId("connection-dot").className).toContain(
      "animate-pulse",
    );

    rerender(<ConnectionStatusIndicator state="offline" />);
    expect(screen.getByTestId("connection-dot").className).not.toContain(
      "animate-pulse",
    );
  });
});
