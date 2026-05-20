import { describe, it, expect } from "vitest";
import type { Node, Edge } from "@xyflow/react";

import { applyDagreLayout } from "./layout";

/**
 * Build a minimal Node array with measured widths/heights so dagre has
 * something concrete to lay out (RESEARCH §608 — dagre needs measured
 * dimensions; the production code reads them from @xyflow's node store
 * after `useNodesInitialized` fires, but for the test we hand them in).
 */
function makeNode(id: string, width = 160, height = 48): Node {
  return {
    id,
    position: { x: 0, y: 0 },
    data: {},
    width,
    height,
  };
}

function makeEdge(source: string, target: string): Edge {
  return { id: `${source}->${target}`, source, target };
}

describe("applyDagreLayout — Test 6: 'TB' direction stacks downstream nodes vertically", () => {
  it("returns nodes with position.y increasing for topologically-deeper nodes", () => {
    // 4-level hierarchy: project -> milestone -> phase -> plan
    const nodes: Node[] = [
      makeNode("project", 280, 80),
      makeNode("milestone", 240, 72),
      makeNode("phase", 200, 64),
      makeNode("plan", 180, 56),
    ];
    const edges: Edge[] = [
      makeEdge("project", "milestone"),
      makeEdge("milestone", "phase"),
      makeEdge("phase", "plan"),
    ];

    const positioned = applyDagreLayout(nodes, edges, "TB");
    const yOf = (id: string): number =>
      positioned.find((n) => n.id === id)!.position.y;

    expect(yOf("milestone")).toBeGreaterThan(yOf("project"));
    expect(yOf("phase")).toBeGreaterThan(yOf("milestone"));
    expect(yOf("plan")).toBeGreaterThan(yOf("phase"));
  });
});

describe("applyDagreLayout — Test 7: 'LR' direction stacks downstream nodes horizontally (waves)", () => {
  it("returns nodes with position.x increasing for tasks in later waves", () => {
    // wave 1: a, b ; wave 2: c (depends a) ; wave 3: d (depends c)
    const nodes: Node[] = [
      makeNode("a", 160, 48),
      makeNode("b", 160, 48),
      makeNode("c", 160, 48),
      makeNode("d", 160, 48),
    ];
    const edges: Edge[] = [
      makeEdge("a", "c"),
      makeEdge("c", "d"),
    ];

    const positioned = applyDagreLayout(nodes, edges, "LR");
    const xOf = (id: string): number =>
      positioned.find((n) => n.id === id)!.position.x;

    expect(xOf("c")).toBeGreaterThan(xOf("a"));
    expect(xOf("d")).toBeGreaterThan(xOf("c"));
  });

  it("returns nodes that all carry numeric position.x and position.y after layout", () => {
    const nodes: Node[] = [makeNode("a"), makeNode("b")];
    const edges: Edge[] = [makeEdge("a", "b")];

    const positioned = applyDagreLayout(nodes, edges, "LR");
    for (const n of positioned) {
      expect(Number.isFinite(n.position.x)).toBe(true);
      expect(Number.isFinite(n.position.y)).toBe(true);
    }
  });
});
