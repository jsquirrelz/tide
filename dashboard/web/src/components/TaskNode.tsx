import { SquareTerminal } from "lucide-react";
import type { Node, NodeProps } from "@xyflow/react";

import TideNodeShell from "./TideNodeShell";
import type { StatusValue } from "./StatusBadge";

/**
 * <TaskNode> — leaf node in the Execution DAG (UI-SPEC §5).
 *
 *   Width: 260px · Min height: 64px · Kind icon: SquareTerminal
 *   Header label: "<name>"
 *   Summary line: "wave <N> · attempt <K>"
 *
 * Click opens the TaskDetailDrawer (UI-SPEC §5 + §7).
 */
export type TaskNodeData = {
  name: string;
  status: StatusValue;
  waveIndex: number;
  attempt: number;
} & Record<string, unknown>;

type TaskNodeType = Node<TaskNodeData, "task">;

export default function TaskNode({ data, selected }: NodeProps<TaskNodeType>) {
  return (
    <TideNodeShell
      kind="task"
      name={data.name}
      headerLabel={data.name}
      status={data.status}
      icon={SquareTerminal}
      iconName="SquareTerminal"
      summary={`wave ${data.waveIndex} · attempt ${data.attempt}`}
      selected={selected}
      width={260}
      minHeight={64}
      handleAxis="horizontal"
    />
  );
}
