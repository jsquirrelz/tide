import { Layers } from "lucide-react";
import type { Node, NodeProps } from "@xyflow/react";

import TideNodeShell from "./TideNodeShell";
import type { StatusValue } from "./StatusBadge";
import type { ProjectBlockingCondition } from "./ConditionBadge";
import { pluralize } from "../lib/pluralize";

/**
 * <ProjectNode> — top-level node in the Planning DAG (UI-SPEC §5).
 *
 *   Width: 360px · Min height: 92px · Kind icon: Layers
 *   Header label: "project/<name>"
 *   Summary line: "<m> milestones · <p> phases · <q> plans"
 */
export type ProjectNodeData = {
  name: string;
  status: StatusValue;
  milestonesCount: number;
  phasesCount: number;
  plansCount: number;
  /** 14-UI-SPEC §C3: True blocking conditions forwarded to TideNodeShell.
   *  buildPlanningGraph always supplies this (via `?? []`), so required here. */
  blockingConditions: ProjectBlockingCondition[];
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
      summary={`${pluralize(data.milestonesCount, "milestone")} · ${pluralize(data.phasesCount, "phase")} · ${pluralize(data.plansCount, "plan")}`}
      selected={selected}
      width={360}
      minHeight={92}
      handleAxis="horizontal"
      /* 37-08: Project nodes are clickable — the kind-aware NodeClickContext
       * routes a ("project", name) click to the ProjectSettingsPanel rather
       * than the old plan-only setSelectedPlan (which caused the CR-04
       * pollution the previous clickable={false} guarded against). */
      /* 14-UI-SPEC §C3: pass blocking conditions through to TideNodeShell. */
      blockingConditions={data.blockingConditions}
    />
  );
}
