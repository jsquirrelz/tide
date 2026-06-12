import { describe, it, expect, afterEach, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import ProjectPicker, { type ProjectEntry } from "./ProjectPicker";

afterEach(cleanup);

const SINGLE: ProjectEntry[] = [
  { name: "p1", namespace: "ns", phase: "Running" },
];

const MULTI: ProjectEntry[] = [
  { name: "alpha", namespace: "team-a", phase: "Running" },
  { name: "beta", namespace: "team-b", phase: "AwaitingApproval" },
  { name: "gamma", namespace: "team-c", phase: "Succeeded" },
];

describe("ProjectPicker (UI-SPEC §Component Inventory #9)", () => {
  it("renders the empty-state copy when projects=[]", () => {
    render(<ProjectPicker projects={[]} value={null} onChange={() => undefined} />);
    // UI-SPEC §9 + §13 E1: empty state copy is "No projects in this cluster".
    expect(screen.getByText("No projects in this cluster")).toBeInTheDocument();
  });

  it("renders a single-project dropdown (no auto-select-only) and fires onChange when clicked", () => {
    const onChange = vi.fn();
    render(
      <ProjectPicker projects={SINGLE} value={null} onChange={onChange} />,
    );
    const trigger = screen.getByRole("button", { name: /open project picker/i });
    act(() => {
      fireEvent.click(trigger);
    });
    // single-item list; selecting fires onChange.
    const row = screen.getByRole("option", { name: /ns\/p1/i });
    act(() => {
      fireEvent.click(row);
    });
    expect(onChange).toHaveBeenCalledWith("p1");
  });

  it("renders multi-project dropdown with name + namespace + status badge per row", () => {
    render(
      <ProjectPicker
        projects={MULTI}
        value="alpha"
        onChange={() => undefined}
      />,
    );
    const trigger = screen.getByRole("button", { name: /open project picker/i });
    act(() => {
      fireEvent.click(trigger);
    });
    // All 3 rows visible
    expect(screen.getByRole("option", { name: /team-a\/alpha/i })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: /team-b\/beta/i })).toBeInTheDocument();
    expect(screen.getByRole("option", { name: /team-c\/gamma/i })).toBeInTheDocument();
    // Each row contains a StatusBadge (verified by data-testid containment).
    expect(screen.getByTestId("status-badge-Running")).toBeInTheDocument();
    expect(screen.getByTestId("status-badge-AwaitingApproval")).toBeInTheDocument();
    expect(screen.getByTestId("status-badge-Succeeded")).toBeInTheDocument();
  });

  // 15-05: finding-9b second coerce site — ProjectPicker row with phase "Complete"
  // must render status-badge-Complete (not status-badge-Pending). (UI-SPEC C2 / RESEARCH A2)
  it("renders status-badge-Complete for a project row with phase Complete (finding-9b, second coerce site)", () => {
    const projects: ProjectEntry[] = [
      { name: "done-project", namespace: "ns", phase: "Complete" },
    ];
    render(
      <ProjectPicker projects={projects} value={null} onChange={() => undefined} />,
    );
    const trigger = screen.getByRole("button", { name: /open project picker/i });
    act(() => {
      fireEvent.click(trigger);
    });
    expect(screen.getByTestId("status-badge-Complete")).toBeInTheDocument();
    expect(screen.queryByTestId("status-badge-Pending")).not.toBeInTheDocument();
  });
});
