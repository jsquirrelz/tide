/*
 * NodeDetailPanel.test.tsx (plan 37-04 Task 2)
 *
 *   Covers the four behaviors from the plan:
 *     1. dialog chrome — role=dialog aria-modal, `<kind>/<name>` header,
 *        children, close button; Escape + backdrop both close.
 *     2. collapse-to-rail — chevron collapses to a 32px rail (children
 *        hidden), rail click expands; collapse never calls onClose.
 *     3. resize + persistence — dragging the left-edge ResizeHandle changes
 *        the width style and persists; collapse persists too.
 *     4. focus capture on open + restore on close.
 *
 *   jsdom ships no PointerEvent/setPointerCapture — the drag test dispatches
 *   native MouseEvents typed as pointer events (they carry clientX).
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, within } from "@testing-library/react";

import NodeDetailPanel from "./NodeDetailPanel";

function firePointer(target: EventTarget, type: string, coord: { clientX?: number; clientY?: number }) {
  act(() => {
    target.dispatchEvent(new MouseEvent(type, { bubbles: true, ...coord }));
  });
}

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  vi.restoreAllMocks();
});

describe("NodeDetailPanel — dialog chrome", () => {
  it("renders dialog role, kind/name header, children, and close affordances", () => {
    const onClose = vi.fn();
    render(
      <NodeDetailPanel open kind="milestone" name="m1" onClose={onClose}>
        <div data-testid="content">artifact body</div>
      </NodeDetailPanel>,
    );

    const panel = screen.getByTestId("node-detail-panel");
    expect(panel).toHaveAttribute("role", "dialog");
    expect(panel).toHaveAttribute("aria-modal", "true");
    expect(screen.getByText("milestone/m1")).toBeInTheDocument();
    expect(screen.getByTestId("content")).toBeInTheDocument();
    expect(screen.getByTestId("panel-close")).toBeInTheDocument();
  });

  it("closes on Escape and on backdrop click", () => {
    const onClose = vi.fn();
    render(
      <NodeDetailPanel open kind="phase" name="p1" onClose={onClose}>
        <div>body</div>
      </NodeDetailPanel>,
    );

    fireEvent.keyDown(document, { key: "Escape" });
    expect(onClose).toHaveBeenCalledTimes(1);

    fireEvent.click(screen.getByTestId("panel-backdrop"));
    expect(onClose).toHaveBeenCalledTimes(2);
  });

  it("renders nothing when open is false", () => {
    render(
      <NodeDetailPanel open={false} kind="plan" name="x" onClose={() => {}}>
        <div data-testid="content">body</div>
      </NodeDetailPanel>,
    );
    expect(screen.queryByTestId("node-detail-panel")).not.toBeInTheDocument();
    expect(screen.queryByTestId("content")).not.toBeInTheDocument();
  });
});

describe("NodeDetailPanel — collapse to rail", () => {
  it("collapses to a rail hiding content and expands back without closing", () => {
    const onClose = vi.fn();
    render(
      <NodeDetailPanel open kind="milestone" name="m1" onClose={onClose}>
        <div data-testid="content">artifact body</div>
      </NodeDetailPanel>,
    );

    expect(screen.getByTestId("content")).toBeInTheDocument();

    // Collapse via the chevron.
    fireEvent.click(screen.getByTestId("panel-collapse-chevron"));
    expect(screen.queryByTestId("content")).not.toBeInTheDocument();
    const rail = screen.getByTestId("panel-rail");
    expect(rail).toBeInTheDocument();
    expect(rail).toHaveAttribute("aria-label", "Expand panel");
    expect(onClose).not.toHaveBeenCalled();

    // Expand via the rail.
    fireEvent.click(rail);
    expect(screen.getByTestId("content")).toBeInTheDocument();
    expect(screen.queryByTestId("panel-rail")).not.toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
  });

  it("persists collapsed state to localStorage", () => {
    render(
      <NodeDetailPanel open kind="milestone" name="m1" onClose={() => {}}>
        <div>body</div>
      </NodeDetailPanel>,
    );
    fireEvent.click(screen.getByTestId("panel-collapse-chevron"));
    expect(localStorage.getItem("tide.dashboard.panel-collapsed")).toBe("true");
    fireEvent.click(screen.getByTestId("panel-rail"));
    expect(localStorage.getItem("tide.dashboard.panel-collapsed")).toBe("false");
  });

  it("starts collapsed when localStorage says so", () => {
    localStorage.setItem("tide.dashboard.panel-collapsed", "true");
    render(
      <NodeDetailPanel open kind="milestone" name="m1" onClose={() => {}}>
        <div data-testid="content">body</div>
      </NodeDetailPanel>,
    );
    expect(screen.queryByTestId("content")).not.toBeInTheDocument();
    expect(screen.getByTestId("panel-rail")).toBeInTheDocument();
  });
});

describe("NodeDetailPanel — resize + persistence", () => {
  it("drag changes the width style and persists on release", () => {
    render(
      <NodeDetailPanel open kind="phase" name="p1" onClose={() => {}}>
        <div>body</div>
      </NodeDetailPanel>,
    );
    const panel = screen.getByTestId("node-detail-panel");
    expect(panel).toHaveStyle({ width: "420px" });

    const handle = within(panel).getByTestId("resize-handle");
    // Leading-edge handle: drag left (500 → 460) widens by 40 → 460px.
    firePointer(handle, "pointerdown", { clientX: 500 });
    firePointer(window, "pointermove", { clientX: 460 });
    expect(panel).toHaveStyle({ width: "460px" });

    firePointer(window, "pointerup", { clientX: 460 });
    expect(localStorage.getItem("tide.dashboard.panel-width")).toBe("460");
  });

  it("restores a persisted width on mount", () => {
    localStorage.setItem("tide.dashboard.panel-width", "512");
    render(
      <NodeDetailPanel open kind="phase" name="p1" onClose={() => {}}>
        <div>body</div>
      </NodeDetailPanel>,
    );
    expect(screen.getByTestId("node-detail-panel")).toHaveStyle({ width: "512px" });
  });
});

describe("NodeDetailPanel — focus management", () => {
  it("captures focus into the panel on open and restores it on close", () => {
    const onClose = vi.fn();
    const { rerender } = render(
      <>
        <button data-testid="trigger">trigger</button>
        <NodeDetailPanel open={false} kind="milestone" name="m1" onClose={onClose}>
          <div>body</div>
        </NodeDetailPanel>
      </>,
    );

    const trigger = screen.getByTestId("trigger");
    act(() => trigger.focus());
    expect(trigger).toHaveFocus();

    rerender(
      <>
        <button data-testid="trigger">trigger</button>
        <NodeDetailPanel open kind="milestone" name="m1" onClose={onClose}>
          <div>body</div>
        </NodeDetailPanel>
      </>,
    );

    const panel = screen.getByTestId("node-detail-panel");
    expect(panel.contains(document.activeElement)).toBe(true);

    rerender(
      <>
        <button data-testid="trigger">trigger</button>
        <NodeDetailPanel open={false} kind="milestone" name="m1" onClose={onClose}>
          <div>body</div>
        </NodeDetailPanel>
      </>,
    );
    expect(trigger).toHaveFocus();
  });
});
