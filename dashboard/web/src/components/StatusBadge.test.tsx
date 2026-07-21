import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import StatusBadge, { STATUS_TABLE, type StatusValue } from "./StatusBadge";

afterEach(cleanup);

// Verbatim labels + screen-reader descriptions from UI-SPEC §Status Vocabulary.
// Order matches the spec table; tests are table-driven against this map.
// 15-05: extended to 11 entries with the Complete row (UI-SPEC C1).
// 53-07: extended to 13 entries with Verifying/VerifyHalted (53-UI-SPEC).
const EXPECTED: Record<
  StatusValue,
  {
    label: string;
    srDescription: string;
    colorVar: string;
    iconName: string;
    animation?: "animate-spin" | "animate-pulse";
  }
> = {
  Pending: {
    label: "Pending",
    srDescription: "Pending dispatch",
    colorVar: "var(--color-status-pending)",
    iconName: "Circle",
  },
  Dispatching: {
    label: "Dispatching",
    srDescription: "Dispatching — Job creation in progress",
    colorVar: "var(--color-status-running)",
    iconName: "Loader2",
    animation: "animate-spin",
  },
  Running: {
    label: "Running",
    srDescription: "Running",
    colorVar: "var(--color-status-running)",
    iconName: "CircleDot",
    animation: "animate-pulse",
  },
  AwaitingApproval: {
    label: "Awaiting approval",
    srDescription: "Awaiting human approval — run `tide approve` to advance",
    colorVar: "var(--color-status-warning)",
    iconName: "Hand",
  },
  Paused: {
    label: "Paused",
    srDescription: "Paused at slack tide — run `tide resume` to advance",
    colorVar: "var(--color-status-warning)",
    iconName: "Pause",
  },
  Succeeded: {
    label: "Succeeded",
    srDescription: "Succeeded",
    colorVar: "var(--color-status-success)",
    iconName: "CircleCheck",
  },
  // 15-05: Project CRD terminal success (PhaseComplete). CircleCheckBig is
  // distinct from CircleCheck per the color-blindness rule (both are green).
  Complete: {
    label: "Complete",
    srDescription: "Complete — all milestones succeeded",
    colorVar: "var(--color-status-success)",
    iconName: "CircleCheckBig",
  },
  Failed: {
    label: "Failed",
    srDescription: "Failed — see logs and Conditions for details",
    colorVar: "var(--color-status-error)",
    iconName: "CircleX",
  },
  PushLeaseFailed: {
    label: "Push lease failed",
    srDescription:
      "Push lease failed — concurrent push detected by force-with-lease",
    colorVar: "var(--color-status-error)",
    iconName: "LockKeyhole",
  },
  PushLeakBlocked: {
    label: "Push leak blocked",
    srDescription:
      "Push blocked by gitleaks — a secret pattern was detected in the diff",
    colorVar: "var(--color-status-blocked)",
    iconName: "ShieldAlert",
  },
  Rejected: {
    label: "Rejected",
    srDescription: "Rejected by operator — run `tide resume` to clear",
    colorVar: "var(--color-status-error)",
    iconName: "Ban",
  },
  // 53-UI-SPEC §Status Vocabulary (OBS-04).
  Verifying: {
    label: "Verifying",
    srDescription: "Verifying — independent evaluator reviewing this attempt",
    colorVar: "var(--color-status-running)",
    iconName: "SearchCheck",
    animation: "animate-pulse",
  },
  VerifyHalted: {
    label: "Verify halted",
    srDescription:
      "Verify halted — verification ended without an approved verdict. Review findings and run `tide resume` to advance",
    colorVar: "var(--color-status-blocked)",
    iconName: "ShieldBan",
  },
};

const ALL_STATUSES = Object.keys(EXPECTED) as StatusValue[];

describe("StatusBadge (UI-SPEC §Status Vocabulary — 11 variants)", () => {
  // Test 1 (table-driven): each status renders the correct icon, label, aria-label.
  it.each(ALL_STATUSES)(
    "renders %s with the locked icon + label + aria-label",
    (status) => {
      render(<StatusBadge status={status} />);
      const badge = screen.getByTestId(`status-badge-${status}`);

      // Label text — verbatim from UI-SPEC.
      expect(badge).toHaveTextContent(EXPECTED[status].label);

      // aria-label = "Status: <screen-reader description>"
      expect(badge).toHaveAttribute(
        "aria-label",
        `Status: ${EXPECTED[status].srDescription}`,
      );

      // Icon identity verified via data-icon attribute we ship on the icon wrapper.
      const icon = badge.querySelector("[data-icon]");
      expect(icon).not.toBeNull();
      expect(icon?.getAttribute("data-icon")).toBe(EXPECTED[status].iconName);

      // Color token threaded through inline style (the badge owns its foreground +
      // tinted background derived from the same status color variable).
      const style = badge.getAttribute("style") ?? "";
      expect(style).toContain(EXPECTED[status].colorVar);
    },
  );

  // Test 2: animation classes only on Dispatching / Running.
  it.each(ALL_STATUSES)(
    "applies the correct animation class for %s",
    (status) => {
      render(<StatusBadge status={status} />);
      const badge = screen.getByTestId(`status-badge-${status}`);
      const expected = EXPECTED[status].animation;
      if (expected === "animate-spin") {
        expect(badge.innerHTML).toContain("animate-spin");
        expect(badge.innerHTML).not.toContain("animate-pulse");
      } else if (expected === "animate-pulse") {
        expect(badge.innerHTML).toContain("animate-pulse");
        expect(badge.innerHTML).not.toContain("animate-spin");
      } else {
        expect(badge.innerHTML).not.toContain("animate-spin");
        expect(badge.innerHTML).not.toContain("animate-pulse");
      }
    },
  );

  // Test 3: inline-flex pill shape per UI-SPEC Status Vocabulary "shape" diagram.
  it("renders as an inline-flex pill with gap-1 / p-1 / px-2 + mono 12px semibold", () => {
    render(<StatusBadge status="Running" />);
    const badge = screen.getByTestId("status-badge-Running");
    const className = badge.className;
    expect(className).toContain("inline-flex");
    expect(className).toContain("gap-1");
    expect(className).toContain("p-1");
    expect(className).toContain("px-2");

    const style = badge.getAttribute("style") ?? "";
    // Mono 12px semibold per UI-SPEC Status Vocabulary "shape" rules.
    expect(style).toContain("var(--font-mono)");
    expect(style).toMatch(/font-size:\s*12px/);
    expect(style).toMatch(/font-weight:\s*600/);
    // Border radius 4px per shape diagram.
    expect(style).toMatch(/border-radius:\s*4px/);
  });
});

// 53-UI-SPEC §Component Test Contract — the OBS-04 "visually distinct"
// criterion, executable: VerifyHalted must differ from Failed on all three
// axes (color token, lucide glyph, label), never a reinterpretation of the
// Failed wave-semantics class.
describe("VerifyHalted vs Failed — distinctness contract (OBS-04)", () => {
  it("colorVar differs — VerifyHalted uses blocked, Failed uses error", () => {
    expect(STATUS_TABLE.VerifyHalted.colorVar).toBe(
      "var(--color-status-blocked)",
    );
    expect(STATUS_TABLE.Failed.colorVar).toBe("var(--color-status-error)");
    expect(STATUS_TABLE.VerifyHalted.colorVar).not.toBe(
      STATUS_TABLE.Failed.colorVar,
    );
  });

  it("iconName differs — ShieldBan vs CircleX", () => {
    expect(STATUS_TABLE.VerifyHalted.iconName).not.toBe(
      STATUS_TABLE.Failed.iconName,
    );
  });

  it("label differs — 'Verify halted' vs 'Failed'", () => {
    expect(STATUS_TABLE.VerifyHalted.label).not.toBe(STATUS_TABLE.Failed.label);
  });

  it("status-badge-VerifyHalted renders with data-icon=ShieldBan", () => {
    render(<StatusBadge status="VerifyHalted" />);
    const badge = screen.getByTestId("status-badge-VerifyHalted");
    const icon = badge.querySelector("[data-icon]");
    expect(icon?.getAttribute("data-icon")).toBe("ShieldBan");
  });

  it("the Verifying badge carries animate-pulse", () => {
    render(<StatusBadge status="Verifying" />);
    const badge = screen.getByTestId("status-badge-Verifying");
    expect(badge.innerHTML).toContain("animate-pulse");
  });
});
