import { describe, it, expect, afterEach } from "vitest";
import { render, cleanup } from "@testing-library/react";
import WaveBackground from "./WaveBackground";

afterEach(cleanup);

// WaveBackground renders a positioned <div> band (it lives inside React Flow's
// <ViewportPortal> in production); each test grabs that band via its testid.
function band(waveIndex: number): HTMLElement {
  return document.querySelector(
    `[data-testid="wave-background-${waveIndex}"]`,
  ) as HTMLElement;
}

describe("WaveBackground (UI-SPEC §Component Inventory #6)", () => {
  // Test 4: basic geometry + label render.
  it("renders a positioned band at the given bounds + a 'WAVE N · X tasks' label", () => {
    render(
      <WaveBackground
        waveIndex={2}
        bounds={{ x: 0, y: 0, width: 600, height: 200 }}
        isActiveDispatch={false}
        taskCount={5}
      />,
    );

    const el = band(2);
    expect(el).not.toBeNull();
    expect(el.style.left).toBe("0px");
    expect(el.style.top).toBe("0px");
    expect(el.style.width).toBe("600px");
    expect(el.style.height).toBe("200px");

    expect(el.textContent).toBe("WAVE 2 · 5 tasks");
  });

  // Test 5: active-dispatch styling has a dashed accent border (UI-SPEC §6).
  it("applies a dashed accent border when isActiveDispatch=true", () => {
    render(
      <WaveBackground
        waveIndex={0}
        bounds={{ x: 10, y: 20, width: 300, height: 100 }}
        isActiveDispatch={true}
        taskCount={3}
      />,
    );
    const el = band(0);
    expect(el.style.border).toContain("dashed");
    expect(el.style.border).toContain("var(--color-accent)");
    expect(el.getAttribute("data-active-dispatch")).toBe("true");
  });

  // Inactive band uses a solid subtle border, not the accent dash.
  it("uses a solid subtle border when isActiveDispatch=false", () => {
    render(
      <WaveBackground
        waveIndex={1}
        bounds={{ x: 0, y: 0, width: 100, height: 100 }}
        isActiveDispatch={false}
        taskCount={2}
      />,
    );
    const el = band(1);
    expect(el.style.border).toContain("solid");
    expect(el.style.border).not.toContain("var(--color-accent)");
  });

  // Test 6: failed band uses --color-status-blocked when failedCount > 0.
  it("uses the status-blocked fill when failedCount > 0", () => {
    render(
      <WaveBackground
        waveIndex={3}
        bounds={{ x: 0, y: 0, width: 200, height: 100 }}
        isActiveDispatch={false}
        taskCount={4}
        failedCount={1}
      />,
    );
    const el = band(3);
    expect(el.style.background).toContain("var(--color-status-blocked)");
    expect(el.getAttribute("data-failed")).toBe("true");
  });

  // Without failedCount the band uses the inactive --color-surface-overlay fill.
  it("uses the surface-overlay fill for inactive bands with failedCount=0", () => {
    render(
      <WaveBackground
        waveIndex={1}
        bounds={{ x: 0, y: 0, width: 200, height: 100 }}
        isActiveDispatch={false}
        taskCount={2}
        failedCount={0}
      />,
    );
    const el = band(1);
    expect(el.style.background).toContain("var(--color-surface-overlay)");
  });
});
