import {
  MarkerType,
  ReactFlow,
  ReactFlowProvider,
  useNodesInitialized,
  useNodesState,
  useEdgesState,
  useReactFlow,
  type Edge,
  type Node,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import ProjectNode, { type ProjectNodeData } from "./ProjectNode";
import MilestoneNode, { type MilestoneNodeData } from "./MilestoneNode";
import PhaseNode, { type PhaseNodeData } from "./PhaseNode";
import PlanNode, { type PlanNodeData } from "./PlanNode";
import { NodeClickContext } from "./NodeClickContext";
import { applyDagreLayout } from "../lib/layout";
import { fetchProject, type ProjectDetail } from "../lib/api";
import { useSSEStream, type SSEState } from "../lib/sse";
import { projectEventsURL } from "../lib/tasks";
import { KNOWN_STATUS_VALUES, type StatusValue } from "./StatusBadge";

/**
 * <PlanningDAGView> (UI-SPEC §3).
 *
 *   Renders the Planning DAG for the currently-selected Project:
 *     Project → Milestone → Phase → Plan
 *
 *   Layout: dagre left-right (rankdir LR).
 *   The view fetches initial data on mount and re-fetches when `projectName`
 *   changes. It also subscribes to the project-events SSE stream
 *   (`/api/v1/projects/{name}/events`) via useSSEStream and debounces a
 *   `runFetch` on every planning-relevant event (Project/Milestone/Phase/Plan)
 *   so the pane live-updates without a manual refresh. Task/Wave events are
 *   ignored here — they only change the execution pane (useTasks owns those).
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
  /**
   * Reports the underlying project-events SSE connection state up to App so
   * the header connection pill reflects the real stream instead of a
   * hardcoded value. Fires on every state transition.
   */
  onConnectionStateChange?: (state: SSEState) => void;
};

// Map any backend phase string into the StatusBadge union; unknown phase
// strings coerce to "Pending" (defensive against schema drift between the
// K8s API server CRD and the dashboard build — same guard ProjectPicker uses).
// Uses the shared KNOWN_STATUS_VALUES derived from STATUS_TABLE keys (UI-SPEC
// C2, 15-05-PLAN.md) — adding a new CRD status to StatusBadge automatically
// propagates here without a separate KNOWN update.
function coerce(phase: string): StatusValue {
  return (KNOWN_STATUS_VALUES as readonly string[]).includes(phase)
    ? (phase as StatusValue)
    : "Pending";
}

// Edge presentation: React Flow's default edges are near-invisible on the dark
// theme. Give every edge a visible stroke + arrowhead off the border tokens.
const EDGE_STROKE = "var(--color-border-strong)";
const EDGE_STYLE = { stroke: EDGE_STROKE, strokeWidth: 1.5 } as const;
const EDGE_MARKER = {
  type: MarkerType.ArrowClosed,
  color: EDGE_STROKE,
  width: 16,
  height: 16,
} as const;

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
    // 14-UI-SPEC §C4: map blocking conditions from ProjectDetail.
    // The `?? []` default ensures legacy payloads (field absent) degrade
    // gracefully to today's render — no badge, no border-l.
    // No new SSE wiring: PLANNING_KINDS already includes "Project" and the
    // debounced refetch already delivers live condition updates (UI-SPEC C4).
    blockingConditions: detail.blockingConditions ?? [],
  };
  nodes.push({
    id: projectId,
    type: "project",
    position: { x: 0, y: 0 },
    data: projectData as unknown as Record<string, unknown>,
    width: 360,
    height: 92,
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
      width: 340,
      height: 84,
    });
    edges.push({
      id: `${projectId}->${id}`,
      source: projectId,
      target: id,
      style: EDGE_STYLE,
      markerEnd: EDGE_MARKER,
    });
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
      width: 320,
      height: 76,
    });
    edges.push({
      id: `milestone:${ph.parent}->${id}`,
      source: `milestone:${ph.parent}`,
      target: id,
      style: EDGE_STYLE,
      markerEnd: EDGE_MARKER,
    });
  }
  for (const pl of detail.plans) {
    const id = `plan:${pl.name}`;
    // The projectDetail payload carries no task/wave counts; PlanNode renders
    // a "view execution →" affordance instead, and the Execution pane is the
    // source of truth for per-plan counts.
    const data: PlanNodeData = {
      name: pl.name,
      status: coerce(pl.phase),
    };
    nodes.push({
      id,
      type: "plan",
      position: { x: 0, y: 0 },
      data: data as unknown as Record<string, unknown>,
      width: 300,
      height: 72,
    });
    edges.push({
      id: `phase:${pl.parent}->${id}`,
      source: `phase:${pl.parent}`,
      target: id,
      style: EDGE_STYLE,
      markerEnd: EDGE_MARKER,
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

// Debounce window for SSE-triggered refetches — mirrors useTasks in
// lib/tasks.ts. Planning events (Project/Milestone/Phase/Plan create/update)
// can burst as a milestone fans out; collapse N events into one refetch.
const REFETCH_DEBOUNCE_MS = 250;
// Wire-shape `kind` values whose changes alter the PLANNING graph. Task/Wave
// events only change the execution pane (useTasks owns those), so they're
// ignored here to avoid pointless refetches.
const PLANNING_KINDS = new Set(["Project", "Milestone", "Phase", "Plan"]);

function PlanningDAGViewInner({
  projectName,
  onPlanClick,
  initialData,
  onConnectionStateChange,
}: PlanningDAGViewProps) {
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, _setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const [flickerReady, setFlickerReady] = useState(false);
  // Sentinel: increments on each fresh data load so the layout effect
  // knows it has a new batch to position; bumping prevents an
  // infinite setNodes → layout → setNodes loop (a node legitimately at
  // x=0/y=0 after layout would otherwise re-trigger the effect).
  const layoutBatchRef = useRef(0);
  const lastPositionedBatchRef = useRef(-1);
  // Active SSE-debounce timer; cleared on cleanup. Mirrors useTasks.
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const ready = useNodesInitialized();
  const { fitView } = useReactFlow();

  // Fetch + build graph for the current project. Wrapped so both the
  // projectName-change effect and the SSE onMessage callback can invoke it.
  // Tests can short-circuit by passing `initialData` directly.
  const runFetch = useCallback(async () => {
    const data = initialData ?? (await fetchProject(projectName));
    const { nodes: ns, edges: es } = buildPlanningGraph(data);
    // Pitfall 26 mitigation: insert with opacity 0; flips to 1 after layout.
    setNodes(ns.map((n) => ({ ...n, style: { ...n.style, opacity: 0 } })));
    _setEdges(es);
    layoutBatchRef.current += 1;
    setFlickerReady(false);
  }, [projectName, initialData, setNodes, _setEdges]);

  // Initial fetch + refetch on projectName change.
  useEffect(() => {
    void runFetch();
  }, [runFetch]);

  // SSE: refresh-trigger only. The minimal event projection lacks the full
  // hierarchy; we re-fetch the rich ProjectDetail on every planning-relevant
  // event (debounced 250ms). Empty url when no project is selected → the
  // stream is disabled (useSSEStream skips construction on an empty url).
  const stream = useSSEStream(
    projectName ? projectEventsURL(projectName) : "",
    {
      onMessage: (e: MessageEvent) => {
        let parsed: { kind?: string } = {};
        try {
          parsed = JSON.parse(String(e.data));
        } catch {
          return;
        }
        if (!parsed.kind || !PLANNING_KINDS.has(parsed.kind)) return;
        if (debounceRef.current) clearTimeout(debounceRef.current);
        debounceRef.current = setTimeout(() => {
          debounceRef.current = null;
          void runFetch();
        }, REFETCH_DEBOUNCE_MS);
      },
    },
  );

  // Clear any pending debounce on unmount.
  useEffect(() => {
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
        debounceRef.current = null;
      }
    };
  }, []);

  // Report connection-state transitions up to App for the header pill.
  useEffect(() => {
    onConnectionStateChange?.(stream.state);
  }, [stream.state, onConnectionStateChange]);

  // After nodes mount + measure, run dagre LR layout, then flip opacity 1.
  // The batch sentinel ensures the effect fires exactly once per data load.
  useEffect(() => {
    if (!ready) return;
    if (nodes.length === 0) return;
    if (lastPositionedBatchRef.current === layoutBatchRef.current) return;
    const positioned = applyDagreLayout(nodes, edges, "LR");
    lastPositionedBatchRef.current = layoutBatchRef.current;
    setNodes(
      positioned.map((n) => ({
        ...n,
        style: { ...n.style, opacity: 1 },
      })),
    );
    setFlickerReady(true);
  }, [ready, nodes, edges, setNodes]);

  // Re-fit once the dagre-positioned nodes have painted. The `fitView` prop
  // fits on init against the opacity-0 nodes still stacked at (0,0), so the
  // final layout ends up off-center; this re-centers on the real positions.
  useEffect(() => {
    if (!flickerReady) return;
    const id = requestAnimationFrame(() =>
      fitView({ padding: 0.2, maxZoom: 1 }),
    );
    return () => cancelAnimationFrame(id);
  }, [flickerReady, fitView]);

  const nodeTypes = useMemo(() => planningNodeTypes, []);

  return (
    <NodeClickContext.Provider value={onPlanClick}>
      <div
        data-testid="planning-dag-view"
        data-dagre-direction="LR"
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
          // maxZoom 1 keeps the whole linear DAG visible: without it, fitView
          // zooms a sparse 4-node chain past 1.8x and pushes lower nodes off-screen.
          fitViewOptions={{ padding: 0.2, maxZoom: 1 }}
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
