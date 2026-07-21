import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup } from "@testing-library/react";
import ConditionBadge, {
  type ProjectBlockingCondition,
  CONDITION_TABLE,
} from "./ConditionBadge";

afterEach(cleanup);

/**
 * Vocabulary fixture matching 14-UI-SPEC.md §C1 (LOCKED vocabulary table).
 * All values are verbatim from the spec — any divergence is a UI-SPEC violation.
 */
const BUDGET_BLOCKED: ProjectBlockingCondition = {
  type: "BudgetBlocked",
  reason: "BudgetCapReached",
  message:
    "Cost spent 10100 cents (+ 220 reserved) exceeds cap 10000 cents; dispatch halted project-wide",
  age: "4m 12s",
};

const BILLING_HALT: ProjectBlockingCondition = {
  type: "BillingHalt",
  reason: "InsufficientCredits",
  message: "Provider credit balance too low; dispatch halted project-wide",
  age: "2m 0s",
};

// 53-UI-SPEC §Condition Vocabulary (OBS-04) — verbatim from the spec.
const VERIFY_HALT: ProjectBlockingCondition = {
  type: "VerifyHalt",
  reason: "LoopExhausted",
  message: "Verification exhausted its loop policy without an approved verdict",
  age: "1m 30s",
};

describe("ConditionBadge (14-UI-SPEC §C1 — blocking-condition vocabulary)", () => {
  // Test 1: BudgetBlocked renders with correct testid, icon, label, and role.
  it("BudgetBlocked renders data-testid, Wallet icon, label 'Budget blocked', role=status", () => {
    render(<ConditionBadge condition={BUDGET_BLOCKED} />);
    const badge = screen.getByTestId("condition-badge-BudgetBlocked");
    expect(badge).toBeInTheDocument();
    expect(badge).toHaveAttribute("data-condition", "BudgetBlocked");
    expect(badge).toHaveAttribute("role", "status");
    expect(badge).toHaveTextContent("Budget blocked");

    // Icon identity verified via data-icon attribute on the icon wrapper.
    const icon = badge.querySelector("[data-icon]");
    expect(icon).not.toBeNull();
    expect(icon?.getAttribute("data-icon")).toBe("Wallet");
  });

  // Test 2: BillingHalt renders with correct testid, CreditCard icon, label 'Billing halted'.
  it("BillingHalt renders data-testid, CreditCard icon, label 'Billing halted'", () => {
    render(<ConditionBadge condition={BILLING_HALT} />);
    const badge = screen.getByTestId("condition-badge-BillingHalt");
    expect(badge).toBeInTheDocument();
    expect(badge).toHaveAttribute("data-condition", "BillingHalt");
    expect(badge).toHaveAttribute("role", "status");
    expect(badge).toHaveTextContent("Billing halted");

    const icon = badge.querySelector("[data-icon]");
    expect(icon).not.toBeNull();
    expect(icon?.getAttribute("data-icon")).toBe("CreditCard");
  });

  // Test 3: native tooltip equals the condition message verbatim.
  it("title attribute equals condition.message verbatim (native tooltip)", () => {
    render(<ConditionBadge condition={BUDGET_BLOCKED} />);
    const badge = screen.getByTestId("condition-badge-BudgetBlocked");
    expect(badge).toHaveAttribute("title", BUDGET_BLOCKED.message);
  });

  // Test 4: aria-label carries the SR description from the vocabulary table (verbatim).
  it("aria-label carries the vocabulary SR description — BudgetBlocked", () => {
    render(<ConditionBadge condition={BUDGET_BLOCKED} />);
    const badge = screen.getByTestId("condition-badge-BudgetBlocked");
    expect(badge).toHaveAttribute(
      "aria-label",
      "Budget cap reached — dispatch halted. Raise spec.budget.absoluteCapCents or apply the bypass annotation to resume",
    );
  });

  it("aria-label carries the vocabulary SR description — BillingHalt", () => {
    render(<ConditionBadge condition={BILLING_HALT} />);
    const badge = screen.getByTestId("condition-badge-BillingHalt");
    expect(badge).toHaveAttribute(
      "aria-label",
      "Provider credit balance too low — dispatch halted. Refill credits and run `tide resume`",
    );
  });

  // 53-UI-SPEC: VerifyHalt renders with correct testid, OctagonPause icon,
  // label 'Verify halted', blocked color token, and SR text naming tide resume.
  it("VerifyHalt renders data-testid, OctagonPause icon, label 'Verify halted'", () => {
    render(<ConditionBadge condition={VERIFY_HALT} />);
    const badge = screen.getByTestId("condition-badge-VerifyHalt");
    expect(badge).toBeInTheDocument();
    expect(badge).toHaveAttribute("data-condition", "VerifyHalt");
    expect(badge).toHaveAttribute("role", "status");
    expect(badge).toHaveTextContent("Verify halted");

    const icon = badge.querySelector("[data-icon]");
    expect(icon).not.toBeNull();
    expect(icon?.getAttribute("data-icon")).toBe("OctagonPause");

    const style = badge.getAttribute("style") ?? "";
    expect(style).toContain("var(--color-status-blocked)");
  });

  it("VerifyHalt aria-label carries the vocabulary SR description naming tide resume", () => {
    render(<ConditionBadge condition={VERIFY_HALT} />);
    const badge = screen.getByTestId("condition-badge-VerifyHalt");
    expect(badge).toHaveAttribute(
      "aria-label",
      "Verification halted without an approved verdict — dispatch held. Review staged findings and run `tide resume`",
    );
  });

  it("VerifyHalt title attribute equals condition.message verbatim (native tooltip)", () => {
    render(<ConditionBadge condition={VERIFY_HALT} />);
    const badge = screen.getByTestId("condition-badge-VerifyHalt");
    expect(badge).toHaveAttribute("title", VERIFY_HALT.message);
  });

  // Test 5: unknown condition type renders nothing (null guard — mirrors coerce() philosophy).
  it("unknown condition type renders nothing (returns null)", () => {
    const unknown: ProjectBlockingCondition = {
      type: "Ready",
      reason: "AllReady",
      message: "All systems go",
      age: "1s",
    };
    render(<ConditionBadge condition={unknown} />);
    expect(
      document.querySelector('[data-testid^="condition-badge-"]'),
    ).toBeNull();
  });

  // Test 6: CONDITION_TABLE is exported and iterable with exactly the three known keys.
  // 53-07: extended from 2 to 3 keys with VerifyHalt (53-UI-SPEC §Condition Vocabulary).
  it("CONDITION_TABLE is exported and has exactly keys BudgetBlocked, BillingHalt, VerifyHalt", () => {
    const keys = Object.keys(CONDITION_TABLE);
    expect(keys).toHaveLength(3);
    expect(keys).toContain("BudgetBlocked");
    expect(keys).toContain("BillingHalt");
    expect(keys).toContain("VerifyHalt");
  });

  // Pill shape mirrors StatusBadge: inline-flex gap-1 p-1 px-2 + mono 12px 600.
  it("renders as an inline-flex pill with the StatusBadge anatomy (gap-1 / p-1 / px-2 + mono 12px 600)", () => {
    render(<ConditionBadge condition={BUDGET_BLOCKED} />);
    const badge = screen.getByTestId("condition-badge-BudgetBlocked");
    expect(badge.className).toContain("inline-flex");
    expect(badge.className).toContain("gap-1");
    expect(badge.className).toContain("p-1");
    expect(badge.className).toContain("px-2");

    const style = badge.getAttribute("style") ?? "";
    expect(style).toContain("var(--font-mono)");
    expect(style).toMatch(/font-size:\s*12px/);
    expect(style).toMatch(/font-weight:\s*600/);
    expect(style).toMatch(/border-radius:\s*4px/);
    // Color token: both conditions share var(--color-status-blocked).
    expect(style).toContain("var(--color-status-blocked)");
  });
});
