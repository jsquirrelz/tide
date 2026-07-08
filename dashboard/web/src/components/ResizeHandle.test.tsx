/*
 * ResizeHandle.test.tsx (plan 37-04 Task 1)
 *
 *   Covers the four behaviors from the plan:
 *     1. a11y attributes (role=separator, aria-orientation, aria-value*)
 *     2. keyboard resize (arrows ±16, Home/End jump, clamp)
 *     3. pointer drag (onChange during move, onCommit once on release)
 *     4. usePersistedSize (localStorage init + clamp + commit write)
 *
 *   jsdom ships no PointerEvent and no setPointerCapture — the drag test
 *   dispatches native MouseEvents typed as pointer events (MouseEvent
 *   carries clientX/clientY) so the coordinate delta actually propagates.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, renderHook, screen } from "@testing-library/react";

import { ResizeHandle, usePersistedSize } from "./ResizeHandle";

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

describe("ResizeHandle — a11y", () => {
  it("renders role=separator with orientation and value attributes", () => {
    render(
      <ResizeHandle
        orientation="vertical"
        value={420}
        min={360}
        max={800}
        onChange={() => {}}
        onCommit={() => {}}
        label="Resize panel"
      />,
    );
    const handle = screen.getByRole("separator");
    expect(handle).toHaveAttribute("aria-orientation", "vertical");
    expect(handle).toHaveAttribute("aria-valuenow", "420");
    expect(handle).toHaveAttribute("aria-valuemin", "360");
    expect(handle).toHaveAttribute("aria-valuemax", "800");
    expect(handle).toHaveAttribute("aria-label", "Resize panel");
    expect(handle).toHaveAttribute("tabindex", "0");
  });

  it("reflects horizontal orientation", () => {
    render(
      <ResizeHandle
        orientation="horizontal"
        value={200}
        min={120}
        max={400}
        onChange={() => {}}
        onCommit={() => {}}
        label="Resize log area"
      />,
    );
    expect(screen.getByRole("separator")).toHaveAttribute("aria-orientation", "horizontal");
  });
});

describe("ResizeHandle — keyboard", () => {
  it("arrows adjust ±16, Home/End jump to clamps, values clamp", () => {
    const onChange = vi.fn();
    const { rerender } = render(
      <ResizeHandle
        orientation="vertical"
        value={420}
        min={360}
        max={800}
        onChange={onChange}
        onCommit={() => {}}
        label="Resize panel"
      />,
    );
    const handle = screen.getByRole("separator");

    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    expect(onChange).toHaveBeenLastCalledWith(404);

    fireEvent.keyDown(handle, { key: "ArrowRight" });
    expect(onChange).toHaveBeenLastCalledWith(436);

    fireEvent.keyDown(handle, { key: "Home" });
    expect(onChange).toHaveBeenLastCalledWith(360);

    fireEvent.keyDown(handle, { key: "End" });
    expect(onChange).toHaveBeenLastCalledWith(800);

    // Clamp low: at min, ArrowLeft stays at min.
    rerender(
      <ResizeHandle
        orientation="vertical"
        value={360}
        min={360}
        max={800}
        onChange={onChange}
        onCommit={() => {}}
        label="Resize panel"
      />,
    );
    onChange.mockClear();
    fireEvent.keyDown(handle, { key: "ArrowLeft" });
    expect(onChange).toHaveBeenLastCalledWith(360);

    // Clamp high: at max, ArrowRight stays at max.
    rerender(
      <ResizeHandle
        orientation="vertical"
        value={800}
        min={360}
        max={800}
        onChange={onChange}
        onCommit={() => {}}
        label="Resize panel"
      />,
    );
    onChange.mockClear();
    fireEvent.keyDown(handle, { key: "ArrowRight" });
    expect(onChange).toHaveBeenLastCalledWith(800);
  });

  it("horizontal orientation uses Up/Down arrows", () => {
    const onChange = vi.fn();
    render(
      <ResizeHandle
        orientation="horizontal"
        value={200}
        min={120}
        max={400}
        onChange={onChange}
        onCommit={() => {}}
        label="Resize log area"
      />,
    );
    const handle = screen.getByRole("separator");
    fireEvent.keyDown(handle, { key: "ArrowUp" });
    expect(onChange).toHaveBeenLastCalledWith(184);
    fireEvent.keyDown(handle, { key: "ArrowDown" });
    expect(onChange).toHaveBeenLastCalledWith(216);
  });
});

describe("ResizeHandle — pointer drag", () => {
  it("reports onChange during move and onCommit once on release", () => {
    const onChange = vi.fn();
    const onCommit = vi.fn();
    render(
      <ResizeHandle
        orientation="vertical"
        value={420}
        min={360}
        max={800}
        onChange={onChange}
        onCommit={onCommit}
        label="Resize panel"
      />,
    );
    const handle = screen.getByRole("separator");

    firePointer(handle, "pointerdown", { clientX: 500 });
    firePointer(window, "pointermove", { clientX: 460 });
    // Leading-edge handle: dragging left (500 → 460) widens by 40 → 460.
    expect(onChange).toHaveBeenLastCalledWith(460);
    firePointer(window, "pointermove", { clientX: 440 });
    expect(onChange).toHaveBeenLastCalledWith(480);

    firePointer(window, "pointerup", { clientX: 440 });
    expect(onCommit).toHaveBeenCalledTimes(1);
    expect(onCommit).toHaveBeenLastCalledWith(480);

    // After release, further moves do nothing.
    onChange.mockClear();
    firePointer(window, "pointermove", { clientX: 400 });
    expect(onChange).not.toHaveBeenCalled();
  });
});

describe("usePersistedSize", () => {
  it("initializes from a stored value, clamped", () => {
    localStorage.setItem("tide.test.size", "500");
    const { result } = renderHook(() => usePersistedSize("tide.test.size", 420, 360, 800));
    expect(result.current[0]).toBe(500);
  });

  it("clamps a stored value above max", () => {
    localStorage.setItem("tide.test.size", "9999");
    const { result } = renderHook(() => usePersistedSize("tide.test.size", 420, 360, 800));
    expect(result.current[0]).toBe(800);
  });

  it("falls back to default when no stored value", () => {
    const { result } = renderHook(() => usePersistedSize("tide.test.absent", 420, 360, 800));
    expect(result.current[0]).toBe(420);
  });

  it("falls back to default for a non-numeric stored value", () => {
    localStorage.setItem("tide.test.size", "not-a-number");
    const { result } = renderHook(() => usePersistedSize("tide.test.size", 420, 360, 800));
    expect(result.current[0]).toBe(420);
  });

  it("commit() writes the current value to the storage key", () => {
    const { result } = renderHook(() => usePersistedSize("tide.test.size", 420, 360, 800));
    act(() => result.current[1](450));
    expect(result.current[0]).toBe(450);
    act(() => result.current[2]());
    expect(localStorage.getItem("tide.test.size")).toBe("450");
  });

  it("setValue clamps out-of-range input", () => {
    const { result } = renderHook(() => usePersistedSize("tide.test.size", 420, 360, 800));
    act(() => result.current[1](10_000));
    expect(result.current[0]).toBe(800);
    act(() => result.current[1](0));
    expect(result.current[0]).toBe(360);
  });
});
