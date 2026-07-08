import { createContext, useContext } from "react";

import type { TideNodeKind } from "./TideNodeShell";

/**
 * NodeClickContext threads an `onNodeClick(kind, name)` callback down through
 * the @xyflow/react node-rendering tree without prop-drilling.
 *
 * @xyflow's per-node React component receives `NodeProps<…>` (data, selected,
 * id, dragging, etc.) but does not surface arbitrary parent props. Routing
 * the click callback through a Context is the standard escape hatch — the
 * Planning/Execution DAG views provide the callback, and each custom node
 * consumes it via `useNodeClick()`.
 *
 * The callback is kind-aware (kind, name): the Planning DAG routes each node
 * kind to its own surface (project → settings, milestone/phase → artifacts,
 * plan → execution + artifacts); the Execution DAGs adapt their task-only
 * consumers to the same two-arg signature. Passing `kind` alongside `name`
 * is what lets App route without a per-kind provider tree.
 *
 * The default value is a no-op so leaf-component unit tests can render a
 * single node without a wrapping provider when click behavior is not under
 * test.
 */
export type NodeClickHandler = (kind: TideNodeKind, name: string) => void;

export const NodeClickContext = createContext<NodeClickHandler>(
  () => undefined,
);

export function useNodeClick(): NodeClickHandler {
  return useContext(NodeClickContext);
}
