/*
 * EmptyState.test.tsx (plan 04-16 Task 1)
 *
 *   Covers Tests 7-8 of the plan: the three EmptyState variants render
 *   the verbatim UI-SPEC §13 copy.
 *
 *   E1 — no-projects: heading "No projects in this cluster" + body
 *     mentioning the `tide apply -f project.yaml` snippet + an ASCII
 *     wave decoration as a <pre> block.
 *   E2 — awaiting-first-milestone: verbatim §13 copy.
 *   E3 — plan-accepted-no-tasks: verbatim §13 copy.
 */
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import EmptyState from "./EmptyState";

describe("EmptyState (Test 7) — no-projects variant", () => {
  it("renders the verbatim heading, body, and ASCII wave decoration", () => {
    render(<EmptyState variant="no-projects" />);

    // Heading — verbatim UI-SPEC §13 / Copywriting Contract table.
    expect(
      screen.getByRole("heading", { name: "No projects in this cluster" }),
    ).toBeInTheDocument();

    // Body — must contain the locked `tide apply` reference.
    expect(
      screen.getByText(/tide apply -f project\.yaml/i),
    ).toBeInTheDocument();

    // ASCII wave decoration — exposed as a <pre> block.
    const pre = screen.getByTestId("empty-wave-decoration");
    expect(pre.tagName.toLowerCase()).toBe("pre");
    expect(pre.textContent ?? "").toMatch(/[━◢◣]/);
  });
});

describe("EmptyState (Test 8) — E2 + E3 variants", () => {
  it("E2 awaiting-first-milestone renders verbatim §13 copy", () => {
    render(<EmptyState variant="awaiting-first-milestone" />);
    expect(
      screen.getByRole("heading", { name: "Awaiting first milestone" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        /The orchestrator is composing the rising tide\. This usually takes < 30s\./,
      ),
    ).toBeInTheDocument();
  });

  it("E3 plan-accepted-no-tasks renders verbatim §13 copy", () => {
    render(<EmptyState variant="plan-accepted-no-tasks" />);
    expect(
      screen.getByRole("heading", { name: "Plan accepted" }),
    ).toBeInTheDocument();
    // Note: ellipsis is the single-char "…" per UI-SPEC §13 copy.
    expect(screen.getByText(/Computing wave structure…/)).toBeInTheDocument();
  });
});
