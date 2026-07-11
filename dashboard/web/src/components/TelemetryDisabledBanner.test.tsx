/*
 * TelemetryDisabledBanner.test.tsx (Phase 38, plan 38-05 — TELEM-03)
 *
 * Locks the 38-UI-SPEC Banner Contract for the presentational component:
 * two text-distinct states (the words differ, not just color), warning vs
 * subtle border tokens, role="status", and zero interactive elements
 * (read-only dashboard — no buttons, no links, no dismissal).
 */
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import TelemetryDisabledBanner from "./TelemetryDisabledBanner";

afterEach(() => {
  cleanup();
});

describe("TelemetryDisabledBanner — disabled-by-config", () => {
  it("renders the TELEMETRY DISABLED label with warning border and fix-path message", () => {
    render(<TelemetryDisabledBanner state="disabled-by-config" />);

    const banner = screen.getByTestId("telemetry-disabled-banner");
    expect(banner.getAttribute("data-state")).toBe("disabled-by-config");
    expect(banner.getAttribute("role")).toBe("status");

    // Label copy (UI-SPEC Copywriting Contract, verbatim).
    expect(screen.getByText("TELEMETRY DISABLED")).toBeInTheDocument();

    // Message names the values key and the docs fix path.
    expect(banner.textContent).toContain("prometheus.enabled");
    expect(banner.textContent).toContain("docs/INSTALL.md");

    // Warning border token discriminates the state.
    expect(banner.style.borderColor).toBe("var(--color-status-warning)");
  });
});

describe("TelemetryDisabledBanner — no-data", () => {
  it("renders the NO TELEMETRY DATA label with subtle border and Targets-page message", () => {
    render(<TelemetryDisabledBanner state="no-data" />);

    const banner = screen.getByTestId("telemetry-disabled-banner");
    expect(banner.getAttribute("data-state")).toBe("no-data");
    expect(banner.getAttribute("role")).toBe("status");

    expect(screen.getByText("NO TELEMETRY DATA")).toBeInTheDocument();
    expect(banner.textContent).toContain("Targets page");

    expect(banner.style.borderColor).toBe("var(--color-border-subtle)");
  });
});

describe("TelemetryDisabledBanner — TELEM-03 invariants", () => {
  it("the two states are distinguishable by text alone", () => {
    const { unmount } = render(
      <TelemetryDisabledBanner state="disabled-by-config" />,
    );
    const disabledText =
      screen.getByTestId("telemetry-disabled-banner").textContent ?? "";
    unmount();

    render(<TelemetryDisabledBanner state="no-data" />);
    const noDataText =
      screen.getByTestId("telemetry-disabled-banner").textContent ?? "";

    expect(disabledText).toContain("DISABLED");
    expect(disabledText).not.toContain("NO TELEMETRY DATA");
    expect(noDataText).toContain("NO TELEMETRY DATA");
    expect(noDataText).not.toContain("DISABLED");
  });

  it("contains zero interactive elements (read-only dashboard)", () => {
    render(<TelemetryDisabledBanner state="disabled-by-config" />);
    const banner = screen.getByTestId("telemetry-disabled-banner");
    expect(banner.querySelectorAll("button, a")).toHaveLength(0);
  });
});
