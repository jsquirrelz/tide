import {
  type KeyboardEvent as ReactKeyboardEvent,
  type PointerEvent as ReactPointerEvent,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";

import { clsx } from "../lib/clsx";

/**
 * <ResizeHandle> (UI-SPEC §6, D-06) — one hand-rolled primitive, two
 * instances: panel-width (vertical track on the left edge of
 * NodeDetailPanel) and log-area-height (horizontal track on the top edge
 * of the log region). react-resizable-panels was SUS-flagged and forces a
 * layout refactor owned by the deferred IDE-layout phase — so we hand-roll.
 *
 * This component ONLY reports px values via onChange/onCommit; the CALLER
 * owns the actual style write (direct, un-animated during drag — no layout
 * thrash). It stays layout-agnostic apart from the leading-edge sign
 * convention below.
 *
 *   Visual: 6px transparent track; --color-border-strong on
 *     hover/drag/focus. 12px hit area via a ::before pseudo-element.
 *     Cursor col-resize (vertical) / row-resize (horizontal).
 *   A11y: role=separator, aria-orientation, aria-valuenow/min/max,
 *     aria-label from `label`, tabIndex=0. Arrow keys adjust ±16px per
 *     press (vertical: Left/Right; horizontal: Up/Down); Home/End jump to
 *     the clamp limits; onCommit fires on key release.
 *   Pointer: setPointerCapture on pointerdown, delta from the drag-start
 *     position and start value, clamp, onChange per move, onCommit once on
 *     pointerup/pointercancel.
 *
 * Sign convention: both instances are LEADING-edge handles (left/top of a
 * trailing-pinned region), so dragging the handle toward the origin
 * (decreasing clientX/clientY) GROWS the region.
 */

export type ResizeOrientation = "vertical" | "horizontal";

export type ResizeHandleProps = {
  orientation: ResizeOrientation;
  value: number;
  min: number;
  max: number;
  onChange: (px: number) => void;
  onCommit: (px: number) => void;
  label: string;
};

const STEP_PX = 16;

const ARROW_KEYS = new Set(["ArrowLeft", "ArrowRight", "ArrowUp", "ArrowDown", "Home", "End"]);

export function ResizeHandle({
  orientation,
  value,
  min,
  max,
  onChange,
  onCommit,
  label,
}: ResizeHandleProps) {
  const isVertical = orientation === "vertical";
  const [active, setActive] = useState(false);

  // Latest props kept in a ref so the window-level pointer handlers can stay
  // referentially stable (same fn passed to add/removeEventListener) while
  // still reading current values after re-renders mid-drag.
  const propsRef = useRef({ isVertical, min, max, value, onChange, onCommit });
  propsRef.current = { isVertical, min, max, value, onChange, onCommit };

  const dragRef = useRef<{ startPos: number; startValue: number } | null>(null);

  const clamp = useCallback((n: number) => {
    const { min: lo, max: hi } = propsRef.current;
    return Math.min(hi, Math.max(lo, n));
  }, []);

  const handlePointerMove = useCallback(
    (e: PointerEvent | MouseEvent) => {
      const drag = dragRef.current;
      if (!drag) return;
      const { isVertical: vert, onChange: change } = propsRef.current;
      const pos = vert ? e.clientX : e.clientY;
      change(clamp(drag.startValue + (drag.startPos - pos)));
    },
    [clamp],
  );

  const handlePointerUp = useCallback(
    (e: PointerEvent | MouseEvent) => {
      const drag = dragRef.current;
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
      window.removeEventListener("pointercancel", handlePointerUp);
      dragRef.current = null;
      setActive(false);
      if (!drag) return;
      const { isVertical: vert, onCommit: commit } = propsRef.current;
      const pos = vert ? e.clientX : e.clientY;
      commit(clamp(drag.startValue + (drag.startPos - pos)));
    },
    [clamp, handlePointerMove],
  );

  // Belt-and-suspenders: drop any lingering window listeners on unmount.
  useEffect(() => {
    return () => {
      window.removeEventListener("pointermove", handlePointerMove);
      window.removeEventListener("pointerup", handlePointerUp);
      window.removeEventListener("pointercancel", handlePointerUp);
    };
  }, [handlePointerMove, handlePointerUp]);

  const onPointerDown = useCallback(
    (e: ReactPointerEvent<HTMLDivElement>) => {
      e.preventDefault();
      const el = e.currentTarget;
      // jsdom (and some old browsers) lack setPointerCapture — guard it.
      if (typeof el.setPointerCapture === "function") {
        try {
          el.setPointerCapture(e.pointerId);
        } catch {
          // capture is best-effort; the window listeners still track the drag.
        }
      }
      dragRef.current = {
        startPos: isVertical ? e.clientX : e.clientY,
        startValue: propsRef.current.value,
      };
      setActive(true);
      window.addEventListener("pointermove", handlePointerMove);
      window.addEventListener("pointerup", handlePointerUp);
      window.addEventListener("pointercancel", handlePointerUp);
    },
    [isVertical, handlePointerMove, handlePointerUp],
  );

  const onKeyDown = useCallback(
    (e: ReactKeyboardEvent<HTMLDivElement>) => {
      if (!ARROW_KEYS.has(e.key)) return;
      const { min: lo, max: hi, value: cur, onChange: change } = propsRef.current;
      const dec = isVertical ? "ArrowLeft" : "ArrowUp";
      const inc = isVertical ? "ArrowRight" : "ArrowDown";
      let next: number | null = null;
      if (e.key === dec) next = cur - STEP_PX;
      else if (e.key === inc) next = cur + STEP_PX;
      else if (e.key === "Home") next = lo;
      else if (e.key === "End") next = hi;
      if (next === null) return;
      e.preventDefault();
      change(Math.min(hi, Math.max(lo, next)));
    },
    [isVertical],
  );

  const onKeyUp = useCallback((e: ReactKeyboardEvent<HTMLDivElement>) => {
    if (!ARROW_KEYS.has(e.key)) return;
    const { value: cur, onCommit: commit } = propsRef.current;
    commit(cur);
  }, []);

  return (
    <div
      role="separator"
      aria-orientation={orientation}
      aria-valuenow={Math.round(value)}
      aria-valuemin={min}
      aria-valuemax={max}
      aria-label={label}
      tabIndex={0}
      data-testid="resize-handle"
      onPointerDown={onPointerDown}
      onKeyDown={onKeyDown}
      onKeyUp={onKeyUp}
      style={{
        // Track dimensions per orientation. The caller positions the handle
        // (absolute left-0 / top-0) — we only own the track + hit area.
        ...(isVertical
          ? { width: "6px", height: "100%", cursor: "col-resize" }
          : { width: "100%", height: "6px", cursor: "row-resize" }),
        background: active ? "var(--color-border-strong)" : "transparent",
        touchAction: "none",
      }}
      className={clsx(
        "group relative select-none",
        // 12px hit area straddling the 6px track via a ::before pseudo.
        isVertical
          ? "before:absolute before:inset-y-0 before:-left-[3px] before:-right-[3px] before:content-['']"
          : "before:absolute before:inset-x-0 before:-top-[3px] before:-bottom-[3px] before:content-['']",
        // Track lights up on hover + keyboard focus (drag handled inline above).
        "hover:bg-[var(--color-border-strong)] focus-visible:bg-[var(--color-border-strong)]",
        "focus-visible:outline-none",
      )}
    />
  );
}

/**
 * usePersistedSize — useState seeded from localStorage (clamped on read;
 * try/catch because storage may be unavailable / non-numeric), plus a
 * debounce-free commit() that writes the current value to `storageKey`.
 * Callers invoke commit() from ResizeHandle's onCommit, which already fires
 * only on release/keyup — that IS the debounce (no per-move writes).
 *
 * Returns `[value, setValue, commit]`.
 */
export function usePersistedSize(
  storageKey: string,
  defaultPx: number,
  min: number,
  max: number,
): [number, (px: number) => void, () => void] {
  const clamp = useCallback(
    (n: number) => Math.min(max, Math.max(min, n)),
    [min, max],
  );

  const [value, setValueState] = useState<number>(() => {
    try {
      const raw = localStorage.getItem(storageKey);
      if (raw !== null) {
        const parsed = Number(raw);
        if (Number.isFinite(parsed)) {
          return Math.min(max, Math.max(min, parsed));
        }
      }
    } catch {
      // localStorage unavailable (private mode, disabled) — fall back.
    }
    return Math.min(max, Math.max(min, defaultPx));
  });

  const valueRef = useRef(value);
  valueRef.current = value;

  const setValue = useCallback(
    (px: number) => {
      const clamped = clamp(px);
      valueRef.current = clamped;
      setValueState(clamped);
    },
    [clamp],
  );

  const commit = useCallback(() => {
    try {
      localStorage.setItem(storageKey, String(valueRef.current));
    } catch {
      // Best-effort persistence.
    }
  }, [storageKey]);

  return [value, setValue, commit];
}
