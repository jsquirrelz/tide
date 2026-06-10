import { ListTree } from "lucide-react";
import type { Node, NodeProps } from "@xyflow/react";

import TideNodeShell from "./TideNodeShell";
import type { StatusValue } from "./StatusBadge";

/**
 * <PlanNode> — fourth level in the Planning DAG (UI-SPEC §5).
 *
 *   Width: 300px · Min height: 72px · Kind icon: ListTree
 *   Summary line: "view execution →" (click affordance, muted mono)
 *
 * Click swaps the right pane to that Plan's Execution DAG (UI-SPEC §5). The
 * Planning DAG payload carries no task/wave counts, so the summary is a click
 * affordance rather than a count — the Execution pane is the source of truth
 * for per-plan task and wave counts. `tasksCount`/`waveCount` remain optional
 * on the data shape for forward-compat but are not rendered.
 */
export type PlanNodeData = {
  name: string;
  status: StatusValue;
  tasksCount?: number;
  waveCount?: number;
} & Record<string, unknown>;

type PlanNodeType = Node<PlanNodeData, "plan">;

export default function PlanNode({ data, selected }: NodeProps<PlanNodeType>) {
  return (
    <TideNodeShell
      kind="plan"
      name={data.name}
      headerLabel={data.name}
      status={data.status}
      icon={ListTree}
      iconName="ListTree"
      summary={
        <span style={{ fontFamily: "var(--font-mono)" }}>view execution →</span>
      }
      selected={selected}
      width={300}
      minHeight={72}
      handleAxis="vertical"
    />
  );
}
