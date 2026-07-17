import "@testing-library/jest-dom";
import { configure } from "@testing-library/react";

// Raise the async-utility timeout above @testing-library's 1000ms default.
// Under vitest's parallel runner the CPU is shared across many suites, so an
// async findBy*/waitFor render can occasionally exceed 1000ms of wall-clock
// even though its element mounts promptly once React is scheduled (the
// ArtifactViewer "artifact-json" tab-switch flake under full-suite load). A
// findBy resolves the instant its element appears, so a higher ceiling costs
// passing tests nothing and removes the load-induced false timeout.
configure({ asyncUtilTimeout: 5000 });

// jsdom polyfills required by @xyflow/react v12 (plan 04-13).
//
// @xyflow internally uses ResizeObserver to measure node dimensions, and
// IntersectionObserver to virtualize off-screen nodes; jsdom ships
// neither. We stub both with no-op classes so the ReactFlow tree mounts
// without throwing. Tests that depend on real layout dimensions hand-feed
// node `width`/`height` and mock `useNodesInitialized` to return true.
//
// DOMMatrix is a Web API @xyflow's panzoom helpers reference; jsdom does
// not ship it. A minimal stand-in keeps the import side-effects quiet.

if (typeof globalThis.ResizeObserver === "undefined") {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (globalThis as any).ResizeObserver = class {
    observe() {
      /* no-op */
    }
    unobserve() {
      /* no-op */
    }
    disconnect() {
      /* no-op */
    }
  };
}

if (typeof globalThis.IntersectionObserver === "undefined") {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (globalThis as any).IntersectionObserver = class {
    root: Element | null = null;
    rootMargin = "";
    thresholds: number[] = [];
    observe() {
      /* no-op */
    }
    unobserve() {
      /* no-op */
    }
    disconnect() {
      /* no-op */
    }
    takeRecords(): IntersectionObserverEntry[] {
      return [];
    }
  };
}

if (typeof (globalThis as { DOMMatrix?: unknown }).DOMMatrix === "undefined") {
  // Minimal DOMMatrix stand-in for @xyflow/react v12's panzoom layer.
  // Only the no-arg ctor + the `.translate`/`.scale` builder method shape
  // is exercised during test renders; jsdom never reads back values.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  (globalThis as any).DOMMatrix = class {
    a = 1;
    b = 0;
    c = 0;
    d = 1;
    e = 0;
    f = 0;
    translate() {
      return this;
    }
    scale() {
      return this;
    }
    multiply() {
      return this;
    }
    inverse() {
      return this;
    }
  };
}

// HTMLElement.scrollIntoView is referenced by @xyflow's keyboard nav;
// jsdom doesn't implement it.
if (
  typeof HTMLElement !== "undefined" &&
  typeof HTMLElement.prototype.scrollIntoView !== "function"
) {
  HTMLElement.prototype.scrollIntoView = function () {
    /* no-op */
  };
}
