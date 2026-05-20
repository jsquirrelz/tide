import { createContext, useContext } from "react";

/**
 * NodeClickContext threads an `onNodeClick(name)` callback down through the
 * @xyflow/react node-rendering tree without prop-drilling.
 *
 * @xyflow's per-node React component receives `NodeProps<…>` (data, selected,
 * id, dragging, etc.) but does not surface arbitrary parent props. Routing
 * the click callback through a Context is the standard escape hatch — the
 * Planning/Execution DAG views provide the callback, and each custom node
 * consumes it via `useNodeClick()`.
 *
 * The default value is a no-op so leaf-component unit tests can render a
 * single node without a wrapping provider when click behavior is not under
 * test.
 */
export const NodeClickContext = createContext<(name: string) => void>(
  () => undefined,
);

export function useNodeClick(): (name: string) => void {
  return useContext(NodeClickContext);
}
