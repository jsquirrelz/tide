import dagre from "dagre";
import type { Edge, Node } from "@xyflow/react";

/**
 * applyDagreLayout — wraps `dagre` for both directions used by Phase 4:
 *
 *   - "TB" (top-down): PlanningDAGView — Project → Milestone → Phase → Plan.
 *   - "LR" (left-right): ExecutionDAGView — Task waves laid out as columns.
 *
 * Uses the React Flow v12 + dagre integration recipe (RESEARCH §608). The
 * production caller waits for `useNodesInitialized` to fire before invoking
 * this (so node `width` + `height` are measured); tests hand in known
 * dimensions.
 *
 * Dagre returns its own coordinate system (rectangle center). React Flow
 * uses top-left; we translate by half-width/half-height to convert.
 *
 * Pitfall 26 mitigation lives in the view component (opacity:0 until
 * `useNodesInitialized` → layout → opacity:1). This function is pure.
 */
export type DagreDirection = "TB" | "LR";

const DEFAULT_NODE_WIDTH = 180;
const DEFAULT_NODE_HEIGHT = 56;

export function applyDagreLayout(
  nodes: Node[],
  edges: Edge[],
  direction: DagreDirection,
): Node[] {
  const g = new dagre.graphlib.Graph();
  g.setDefaultEdgeLabel(() => ({}));
  g.setGraph({
    rankdir: direction,
    // 80px between ranks gives the dagre layout room to draw edges without
    // them overlapping node body geometry. Tuned for UI-SPEC §5 node sizes.
    nodesep: 24,
    ranksep: 80,
  });

  for (const n of nodes) {
    g.setNode(n.id, {
      width: n.width ?? DEFAULT_NODE_WIDTH,
      height: n.height ?? DEFAULT_NODE_HEIGHT,
    });
  }
  for (const e of edges) {
    g.setEdge(e.source, e.target);
  }

  dagre.layout(g);

  return nodes.map((n) => {
    const dagreNode = g.node(n.id);
    if (!dagreNode) return n;
    const width = n.width ?? DEFAULT_NODE_WIDTH;
    const height = n.height ?? DEFAULT_NODE_HEIGHT;
    // dagre returns the center; React Flow uses top-left.
    return {
      ...n,
      position: {
        x: dagreNode.x - width / 2,
        y: dagreNode.y - height / 2,
      },
    };
  });
}
