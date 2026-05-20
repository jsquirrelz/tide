import { Compass } from "lucide-react";
import type { Node, NodeProps } from "@xyflow/react";

import TideNodeShell from "./TideNodeShell";
import type { StatusValue } from "./StatusBadge";

/**
 * <PhaseNode> — third level in the Planning DAG (UI-SPEC §5).
 *
 *   Width: 200px · Min height: 64px · Kind icon: Compass
 *   Header label: "<name>"
 *   Summary line: "<q> plans"
 */
export type PhaseNodeData = {
  name: string;
  status: StatusValue;
  plansCount: number;
} & Record<string, unknown>;

type PhaseNodeType = Node<PhaseNodeData, "phase">;

export default function PhaseNode({ data, selected }: NodeProps<PhaseNodeType>) {
  return (
    <TideNodeShell
      kind="phase"
      name={data.name}
      headerLabel={data.name}
      status={data.status}
      icon={Compass}
      iconName="Compass"
      summary={`${data.plansCount} plans`}
      selected={selected}
      width={200}
      minHeight={64}
      /* CR-04 fix: Phase nodes in Planning DAG are not clickable. */
      clickable={false}
    />
  );
}
