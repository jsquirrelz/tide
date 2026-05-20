import {
  ReactFlow,
  ReactFlowProvider,
  useNodesInitialized,
  useNodesState,
  useEdgesState,
  type Edge,
  type Node,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { useEffect, useMemo, useRef, useState } from "react";

import ProjectNode, { type ProjectNodeData } from "./ProjectNode";
import MilestoneNode, { type MilestoneNodeData } from "./MilestoneNode";
import PhaseNode, { type PhaseNodeData } from "./PhaseNode";
import PlanNode, { type PlanNodeData } from "./PlanNode";
import { NodeClickContext } from "./NodeClickContext";
import { applyDagreLayout } from "../lib/layout";
import { fetchProject, type ProjectDetail } from "../lib/api";
import type { StatusValue } from "./StatusBadge";

/**
 * <PlanningDAGView> (UI-SPEC §3).
 *
 *   Renders the Planning DAG for the currently-selected Project:
 *     Project → Milestone → Phase → Plan
 *
 *   Layout: dagre top-down (rankdir TB).
 *   For v1.0 the view fetches initial data on mount and re-fetches when
 *   `projectName` changes. Plan 04-16 wires SSE (`useProjectEvents`) into
 *   the same node/edge state; the seam is the `useEffect` that calls
 *   `fetchProject` — that effect will be replaced by a hook that streams
 *   incremental updates.
 *
 *   Pitfall 26 mitigation (RESEARCH §567-579): on data change, new nodes
 *   are inserted with `style.opacity: 0`. The `useNodesInitialized` hook
 *   flips `flickerReady` to true in a follow-up tick, which un-hides them.
 */
export type PlanningDAGViewProps = {
  projectName: string;
  /** Click on a PlanNode swaps the right pane to that Plan's Execution DAG. */
  onPlanClick: (planName: string) => void;
  /** Optional initial data — primarily for tests to bypass fetch. */
  initialData?: ProjectDetail;
};

// Map any backend phase string into the StatusBadge union; unknown phase
// strings coerce to "Pending" (defensive against schema drift between the
// K8s API server CRD and the dashboard build — same guard ProjectPicker uses).
const KNOWN: readonly StatusValue[] = [
  "Pending",
  "Dispatching",
  "Running",
  "AwaitingApproval",
  "Paused",
  "Succeeded",
  "Failed",
  "PushLeaseFailed",
  "PushLeakBlocked",
  "Rejected",
];
function coerce(phase: string): StatusValue {
  return (KNOWN as readonly string[]).includes(phase)
    ? (phase as StatusValue)
    : "Pending";
}

/**
 * Build the @xyflow node + edge arrays from a ProjectDetail payload.
 * Each level becomes a typed Node<DataShape, kind>. Plans are leaves.
 */
function buildPlanningGraph(detail: ProjectDetail): {
  nodes: Node[];
  edges: Edge[];
} {
  const nodes: Node[] = [];
  const edges: Edge[] = [];

  const projectId = `project:${detail.name}`;
  const projectData: ProjectNodeData = {
    name: detail.name,
    status: coerce(detail.phase),
    milestonesCount: detail.milestones.length,
    phasesCount: detail.phases.length,
    plansCount: detail.plans.length,
  };
  nodes.push({
    id: projectId,
    type: "project",
    position: { x: 0, y: 0 },
    data: projectData as unknown as Record<string, unknown>,
    width: 280,
    height: 80,
  });

  for (const m of detail.milestones) {
    const id = `milestone:${m.name}`;
    const plansForM = detail.plans.filter((pl) =>
      detail.phases
        .filter((ph) => ph.parent === m.name)
        .some((ph) => pl.parent === ph.name),
    ).length;
    const phasesForM = detail.phases.filter((ph) => ph.parent === m.name).length;
    const data: MilestoneNodeData = {
      name: m.name,
      status: coerce(m.phase),
      phasesCount: phasesForM,
      plansCount: plansForM,
    };
    nodes.push({
      id,
      type: "milestone",
      position: { x: 0, y: 0 },
      data: data as unknown as Record<string, unknown>,
      width: 240,
      height: 72,
    });
    edges.push({ id: `${projectId}->${id}`, source: projectId, target: id });
  }
  for (const ph of detail.phases) {
    const id = `phase:${ph.name}`;
    const plansForPh = detail.plans.filter((pl) => pl.parent === ph.name).length;
    const data: PhaseNodeData = {
      name: ph.name,
      status: coerce(ph.phase),
      plansCount: plansForPh,
    };
    nodes.push({
      id,
      type: "phase",
      position: { x: 0, y: 0 },
      data: data as unknown as Record<string, unknown>,
      width: 200,
      height: 64,
    });
    edges.push({
      id: `milestone:${ph.parent}->${id}`,
      source: `milestone:${ph.parent}`,
      target: id,
    });
  }
  for (const pl of detail.plans) {
    const id = `plan:${pl.name}`;
    const data: PlanNodeData = {
      name: pl.name,
      status: coerce(pl.phase),
      // Counts not in the projectDetail payload — placeholder values for v1.0.
      // Plan 04-16's SSE stream will hydrate task counts incrementally; for
      // now we render 0/0 rather than fail the type check.
      tasksCount: 0,
      waveCount: 0,
    };
    nodes.push({
      id,
      type: "plan",
      position: { x: 0, y: 0 },
      data: data as unknown as Record<string, unknown>,
      width: 180,
      height: 56,
    });
    edges.push({
      id: `phase:${pl.parent}->${id}`,
      source: `phase:${pl.parent}`,
      target: id,
    });
  }

  return { nodes, edges };
}

// Memoize the nodeTypes map (RESEARCH §611 — avoid re-render storms).
const planningNodeTypes = {
  project: ProjectNode,
  milestone: MilestoneNode,
  phase: PhaseNode,
  plan: PlanNode,
} as const;

function PlanningDAGViewInner({
  projectName,
  onPlanClick,
  initialData,
}: PlanningDAGViewProps) {
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, _setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  // _setEdges retained for future SSE-driven edge updates (plan 04-16).
  void _setEdges;
  const [flickerReady, setFlickerReady] = useState(false);
  // Sentinel: increments on each fresh data load so the layout effect
  // knows it has a new batch to position; bumping prevents an
  // infinite setNodes → layout → setNodes loop (a node legitimately at
  // x=0/y=0 after layout would otherwise re-trigger the effect).
  const layoutBatchRef = useRef(0);
  const lastPositionedBatchRef = useRef(-1);
  const ready = useNodesInitialized();

  // Fetch + build graph on projectName change. Tests can short-circuit by
  // passing `initialData` directly.
  useEffect(() => {
    let cancelled = false;
    const load = async () => {
      const data = initialData ?? (await fetchProject(projectName));
      if (cancelled) return;
      const { nodes: ns, edges: es } = buildPlanningGraph(data);
      // Pitfall 26 mitigation: insert with opacity 0; flips to 1 after layout.
      layoutBatchRef.current += 1;
      setNodes(ns.map((n) => ({ ...n, style: { ...n.style, opacity: 0 } })));
      _setEdges(es);
      setFlickerReady(false);
    };
    void load();
    return () => {
      cancelled = true;
    };
  }, [projectName, initialData, setNodes, _setEdges]);

  // After nodes mount + measure, run dagre TB layout, then flip opacity 1.
  // The batch sentinel ensures the effect fires exactly once per data load.
  useEffect(() => {
    if (!ready) return;
    if (nodes.length === 0) return;
    if (lastPositionedBatchRef.current === layoutBatchRef.current) return;
    const positioned = applyDagreLayout(nodes, edges, "TB");
    lastPositionedBatchRef.current = layoutBatchRef.current;
    setNodes(
      positioned.map((n) => ({
        ...n,
        style: { ...n.style, opacity: 1 },
      })),
    );
    setFlickerReady(true);
  }, [ready, nodes, edges, setNodes]);

  const nodeTypes = useMemo(() => planningNodeTypes, []);

  return (
    <NodeClickContext.Provider value={onPlanClick}>
      <div
        data-testid="planning-dag-view"
        data-dagre-direction="TB"
        data-flicker-ready={flickerReady ? "true" : "false"}
        className="h-full w-full"
      >
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.2 }}
          // Read-only graph — the dashboard never edits the DAG, only views it.
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable={true}
          // Pan/zoom enabled per UI-SPEC §Scroll Behavior.
          panOnDrag
        />
      </div>
    </NodeClickContext.Provider>
  );
}

/**
 * The outer wrapper adds `<ReactFlowProvider>` so the inner hooks
 * (useNodesInitialized) work in standalone contexts. Plan 04-16's SSE
 * provider will sit at the same level if it ever needs context.
 */
export default function PlanningDAGView(props: PlanningDAGViewProps) {
  return (
    <ReactFlowProvider>
      <PlanningDAGViewInner {...props} />
    </ReactFlowProvider>
  );
}
