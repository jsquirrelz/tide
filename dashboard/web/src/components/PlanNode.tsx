import { ListTree } from "lucide-react";
import type { Node, NodeProps } from "@xyflow/react";

import TideNodeShell from "./TideNodeShell";
import type { StatusValue } from "./StatusBadge";

/**
 * <PlanNode> — fourth level in the Planning DAG (UI-SPEC §5).
 *
 *   Width: 180px · Min height: 56px · Kind icon: ListTree
 *   Header label: "<name>"
 *   Summary line: "<n> tasks · <w> waves"
 *
 * Click swaps the right pane to that Plan's Execution DAG (UI-SPEC §5).
 */
export type PlanNodeData = {
  name: string;
  status: StatusValue;
  tasksCount: number;
  waveCount: number;
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
      summary={`${data.tasksCount} tasks · ${data.waveCount} waves`}
      selected={selected}
      width={180}
      minHeight={56}
    />
  );
}
