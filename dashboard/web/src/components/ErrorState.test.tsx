/*
 * ErrorState.test.tsx (plan 04-16 Task 1)
 *
 *   Covers Tests 9-10 of the plan: the two ErrorState variants render
 *   the verbatim UI-SPEC §14 copy + the kubectl hint lines.
 *
 *   ERR1 — backend-unreachable: heading + body + the URL + three kubectl
 *     hint commands.
 *   ERR2 — permission-denied: heading + body + RBAC kubectl hint lines.
 */
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import ErrorState from "./ErrorState";

describe("ErrorState (Test 9) — ERR1 backend-unreachable", () => {
  it("renders heading, URL in body, and kubectl hint lines verbatim", () => {
    render(
      <ErrorState
        variant="backend-unreachable"
        url="http://localhost:8080"
      />,
    );
    expect(
      screen.getByRole("heading", { name: "Dashboard backend unreachable" }),
    ).toBeInTheDocument();
    expect(screen.getByText(/http:\/\/localhost:8080/)).toBeInTheDocument();
    // Three kubectl hint lines per UI-SPEC §14 ERR1.
    expect(
      screen.getByText(/kubectl get deploy -n tide-system tide-dashboard/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/kubectl logs -n tide-system deploy\/tide-dashboard/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        /kubectl port-forward -n tide-system svc\/tide-dashboard 8080:80/,
      ),
    ).toBeInTheDocument();
  });
});

describe("ErrorState (Test 10) — ERR2 permission-denied", () => {
  it("renders verbatim §14 ERR2 copy + RBAC kubectl hint", () => {
    render(<ErrorState variant="permission-denied" />);
    expect(
      screen.getByRole("heading", { name: "Permission denied" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText(
        /The dashboard backend's ServiceAccount cannot read Projects/,
      ),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/kubectl describe clusterrolebinding tide-dashboard/),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/helm get values tide -n tide-system/),
    ).toBeInTheDocument();
  });
});
