import { Flag } from "lucide-react";
import type { Node, NodeProps } from "@xyflow/react";

import TideNodeShell from "./TideNodeShell";
import type { StatusValue } from "./StatusBadge";
import { pluralize } from "../lib/pluralize";

/**
 * <MilestoneNode> — second level in the Planning DAG (UI-SPEC §5).
 *
 *   Width: 340px · Min height: 84px · Kind icon: Flag
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
      summary={`${pluralize(data.phasesCount, "phase")} · ${pluralize(data.plansCount, "plan")}`}
      selected={selected}
      width={340}
      minHeight={84}
      handleAxis="horizontal"
      /* CR-04 fix: Milestone nodes in Planning DAG are not clickable. */
      clickable={false}
    />
  );
}
