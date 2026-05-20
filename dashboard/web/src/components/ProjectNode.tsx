import { Layers } from "lucide-react";
import type { Node, NodeProps } from "@xyflow/react";

import TideNodeShell from "./TideNodeShell";
import type { StatusValue } from "./StatusBadge";

/**
 * <ProjectNode> — top-level node in the Planning DAG (UI-SPEC §5).
 *
 *   Width: 280px · Min height: 80px · Kind icon: Layers
 *   Header label: "project/<name>"
 *   Summary line: "<m> milestones · <p> phases · <q> plans"
 */
export type ProjectNodeData = {
  name: string;
  status: StatusValue;
  milestonesCount: number;
  phasesCount: number;
  plansCount: number;
} & Record<string, unknown>;

type ProjectNodeType = Node<ProjectNodeData, "project">;

export default function ProjectNode({ data, selected }: NodeProps<ProjectNodeType>) {
  return (
    <TideNodeShell
      kind="project"
      name={data.name}
      headerLabel={`project/${data.name}`}
      status={data.status}
      icon={Layers}
      iconName="Layers"
      summary={`${data.milestonesCount} milestones · ${data.phasesCount} phases · ${data.plansCount} plans`}
      selected={selected}
      width={280}
      minHeight={80}
      /* CR-04 fix: Project nodes in the Planning DAG are not clickable —
       * clicking would call setSelectedPlan(projectName) which has no
       * matching Plan and pollutes the right pane. */
      clickable={false}
    />
  );
}
