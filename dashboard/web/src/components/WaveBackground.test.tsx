import { describe, it, expect, afterEach } from "vitest";
import { render, cleanup } from "@testing-library/react";
import WaveBackground from "./WaveBackground";

afterEach(cleanup);

// WaveBackground returns an SVG fragment (`<g>` + `<rect>` + `<text>`), so each
// test wraps it in a host `<svg>` to make it valid DOM.
function renderInSvg(node: React.ReactNode) {
  return render(<svg data-testid="host-svg">{node}</svg>);
}

describe("WaveBackground (UI-SPEC §Component Inventory #6)", () => {
  // Test 4: basic geometry + label render.
  it("renders a <rect> at the given bounds + a 'WAVE N · X tasks' label", () => {
    const { container } = renderInSvg(
      <WaveBackground
        waveIndex={2}
        bounds={{ x: 0, y: 0, width: 600, height: 200 }}
        isActiveDispatch={false}
        taskCount={5}
      />,
    );

    const rect = container.querySelector("rect");
    expect(rect).not.toBeNull();
    expect(rect?.getAttribute("width")).toBe("600");
    expect(rect?.getAttribute("height")).toBe("200");
    expect(rect?.getAttribute("x")).toBe("0");
    expect(rect?.getAttribute("y")).toBe("0");

    const text = container.querySelector("text");
    expect(text?.textContent).toBe("WAVE 2 · 5 tasks");
  });

  // Test 5: active-dispatch styling has stroke-dasharray (UI-SPEC §6 active band).
  it("applies stroke-dasharray when isActiveDispatch=true", () => {
    const { container } = renderInSvg(
      <WaveBackground
        waveIndex={0}
        bounds={{ x: 10, y: 20, width: 300, height: 100 }}
        isActiveDispatch={true}
        taskCount={3}
      />,
    );
    const rect = container.querySelector("rect");
    expect(rect?.getAttribute("stroke-dasharray")).toBe("4 2");
    expect(rect?.getAttribute("stroke")).toContain(
      "var(--color-accent)",
    );
  });

  // Inactive band has NO stroke-dasharray.
  it("does not set stroke-dasharray when isActiveDispatch=false", () => {
    const { container } = renderInSvg(
      <WaveBackground
        waveIndex={1}
        bounds={{ x: 0, y: 0, width: 100, height: 100 }}
        isActiveDispatch={false}
        taskCount={2}
      />,
    );
    const rect = container.querySelector("rect");
    expect(rect?.getAttribute("stroke-dasharray")).toBeNull();
  });

  // Test 6: failed band uses --color-status-blocked when failedCount > 0.
  it("uses the status-blocked color when failedCount > 0", () => {
    const { container } = renderInSvg(
      <WaveBackground
        waveIndex={3}
        bounds={{ x: 0, y: 0, width: 200, height: 100 }}
        isActiveDispatch={false}
        taskCount={4}
        failedCount={1}
      />,
    );
    const rect = container.querySelector("rect");
    const fill = rect?.getAttribute("fill") ?? "";
    expect(fill).toContain("var(--color-status-blocked)");
  });

  // Without failedCount the band uses the inactive --color-surface-overlay fill.
  it("uses the surface-overlay fill for inactive bands with failedCount=0", () => {
    const { container } = renderInSvg(
      <WaveBackground
        waveIndex={1}
        bounds={{ x: 0, y: 0, width: 200, height: 100 }}
        isActiveDispatch={false}
        taskCount={2}
        failedCount={0}
      />,
    );
    const rect = container.querySelector("rect");
    expect(rect?.getAttribute("fill")).toContain(
      "var(--color-surface-overlay)",
    );
  });
});
