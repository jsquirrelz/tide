import { Flag } from "lucide-react";
import type { Node, NodeProps } from "@xyflow/react";

import TideNodeShell from "./TideNodeShell";
import type { StatusValue } from "./StatusBadge";

/**
 * <MilestoneNode> — second level in the Planning DAG (UI-SPEC §5).
 *
 *   Width: 240px · Min height: 72px · Kind icon: Flag
 *   Header label: "<name>"
 *   Summary line: "<p> phases · <q> plans"
 */
export type MilestoneNodeData = {
  name: string;
  status: StatusValue;
  phasesCount: number;
  plansCount: number;
} & Record<string, unknown>;

type MilestoneNodeType = Node<MilestoneNodeData, "milestone">;

export default function MilestoneNode({ data, selected }: NodeProps<MilestoneNodeType>) {
  return (
    <TideNodeShell
      kind="milestone"
      name={data.name}
      headerLabel={data.name}
      status={data.status}
      icon={Flag}
      iconName="Flag"
      summary={`${data.phasesCount} phases · ${data.plansCount} plans`}
      selected={selected}
      width={240}
      minHeight={72}
      /* CR-04 fix: Milestone nodes in Planning DAG are not clickable. */
      clickable={false}
    />
  );
}
