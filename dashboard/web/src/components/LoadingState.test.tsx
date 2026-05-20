/*
 * LoadingState.test.tsx (plan 04-16 Task 1)
 *
 *   Covers Test 11 of the plan: three LoadingState variants each render
 *   the lucide-react Loader2 icon with the animate-spin className.
 *
 *   L1 — initial: full-page centered spinner.
 *   L2 — pane: spinner centered inside its pane bounds.
 *   L3 — drawer: skeleton placeholder bars + spinner.
 */
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import LoadingState from "./LoadingState";

describe("LoadingState (Test 11) — Loader2 with animate-spin", () => {
  it.each(["initial", "pane", "drawer"] as const)(
    "variant=%s renders a Loader2 spinner with animate-spin",
    (variant) => {
      render(<LoadingState variant={variant} />);
      const spinner = screen.getByTestId("loading-spinner");
      // animate-spin class drives the lucide Loader2 rotation. Use
      // getAttribute("class") because lucide-react renders an SVG, and
      // SVGElement.className is an SVGAnimatedString object (not a
      // string) in both jsdom and real browsers.
      const cls = spinner.getAttribute("class") ?? "";
      expect(cls).toMatch(/animate-spin/);
    },
  );

  it("L1 initial variant shows label-size copy", () => {
    render(<LoadingState variant="initial" />);
    expect(screen.getByText(/Loading dashboard…/i)).toBeInTheDocument();
  });
});
