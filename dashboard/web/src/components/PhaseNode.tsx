import { Compass } from "lucide-react";
import type { Node, NodeProps } from "@xyflow/react";

import TideNodeShell from "./TideNodeShell";
import type { StatusValue } from "./StatusBadge";
import { pluralize } from "../lib/pluralize";

/**
 * <PhaseNode> — third level in the Planning DAG (UI-SPEC §5).
 *
 *   Width: 320px · Min height: 76px · Kind icon: Compass
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
      summary={pluralize(data.plansCount, "plan")}
      selected={selected}
      width={320}
      minHeight={76}
      handleAxis="horizontal"
      /* 37-08: Phase nodes are clickable — a ("phase", name) click routes to
       * the ArtifactViewer (D-05) via the kind-aware context. */
    />
  );
}
