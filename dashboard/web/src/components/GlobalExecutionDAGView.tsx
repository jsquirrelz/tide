import {
  MarkerType,
  ReactFlow,
  ReactFlowProvider,
  ViewportPortal,
  useNodesInitialized,
  useNodesState,
  useEdgesState,
  useReactFlow,
  type Edge,
  type Node,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { Loader2 } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";

import TaskNode, { type TaskNodeData } from "./TaskNode";
import WaveBackground from "./WaveBackground";
import { NodeClickContext } from "./NodeClickContext";
import { applyDagreLayout } from "../lib/layout";
import EmptyState from "./EmptyState";
// ExecutionTaskData is imported from ./ExecutionDAGView — reuse the type directly.
import type { ExecutionTaskData } from "./ExecutionDAGView";

/**
 * <GlobalExecutionDAGView> (Plan 26-04, D-07).
 *
 *   Renders the global Execution DAG for the selected Project:
 *     TaskNode × N across ALL Milestones, grouped into wave bands.
 *     Data is sourced from GET /api/v1/projects/{name}/execution-dag.
 *
 *   Layout: dagre left-right (rankdir LR). Wave bands are presentational —
 *   tasks belong to waves by global Kahn derivation (Phases 24-25), not
 *   by @xyflow parent containment. Bands render inside React Flow's
 *   <ViewportPortal> at z-index 0; task nodes overlay above.
 *
 *   Cross-wave edges use type "smoothstep"; intra-wave edges use the
 *   default (straight) type — same contract as ExecutionDAGView.
 *
 *   Mirrors ExecutionDAGView with project scope in place of plan scope.
 *   No milestone provenance shown on individual nodes (UI-SPEC §Node Visual
 *   Contract — "NOT shown on individual nodes").
 */

export type ProjectExecutionDAGData = {
  projectName: string;
  tasks: ExecutionTaskData[];
  /** Global wave currently dispatching Jobs (gets accent dash stroke). */
  activeDispatchWave?: number;
};

export type GlobalExecutionDAGViewProps = {
  projectName: string;
  /** Inlined project data from fetchProjectExecutionDAG. */
  project: ProjectExecutionDAGData | null;
  onTaskClick: (taskName: string) => void;
  /** Set to true when the /execution-dag endpoint returned an error. */
  fetchError?: boolean;
};

const PADDING = 24;
const TASK_WIDTH = 260;
const TASK_HEIGHT = 64;

// Edge presentation — matches ExecutionDAGView constants (UI-SPEC §Edge Contract).
const EDGE_STROKE = "var(--color-border-strong)";
const EDGE_STYLE = { stroke: EDGE_STROKE, strokeWidth: 1.5 } as const;
const EDGE_MARKER = {
  type: MarkerType.ArrowClosed,
  color: EDGE_STROKE,
  width: 16,
  height: 16,
} as const;

function buildExecutionGraph(project: ProjectExecutionDAGData): {
  nodes: Node[];
  edges: Edge[];
  waveMap: Map<string, number>;
} {
  const nodes: Node[] = [];
  const edges: Edge[] = [];
  const waveMap = new Map<string, number>();

  for (const t of project.tasks) {
    waveMap.set(t.name, t.waveIndex);
    const data: TaskNodeData = {
      name: t.name,
      status: t.status,
      waveIndex: t.waveIndex,
      attempt: t.attempt,
    };
    nodes.push({
      id: t.name,
      type: "task",
      position: { x: 0, y: 0 },
      data: data as unknown as Record<string, unknown>,
      width: TASK_WIDTH,
      height: TASK_HEIGHT,
    });
    for (const dep of t.dependsOn) {
      edges.push({
        id: `${dep}->${t.name}`,
        source: dep,
        target: t.name,
        style: EDGE_STYLE,
        markerEnd: EDGE_MARKER,
      });
    }
  }
  return { nodes, edges, waveMap };
}

/**
 * Assigns smoothstep to cross-wave edges; intra-wave edges keep the
 * default (straight) edge type — same logic as ExecutionDAGView.
 */
function annotateEdges(
  edges: Edge[],
  waveMap: Map<string, number>,
): Edge[] {
  return edges.map((e) => {
    const srcWave = waveMap.get(e.source);
    const tgtWave = waveMap.get(e.target);
    if (
      srcWave !== undefined &&
      tgtWave !== undefined &&
      srcWave !== tgtWave
    ) {
      return { ...e, type: "smoothstep" };
    }
    return e;
  });
}

type WaveBand = {
  waveIndex: number;
  bounds: { x: number; y: number; width: number; height: number };
  taskCount: number;
  failedCount: number;
};

/** Status family that triggers the WaveBackground "failed band" signal. */
const FAILED_TASK_STATUSES: ReadonlySet<string> = new Set([
  "Failed",
  "PushLeaseFailed",
  "PushLeakBlocked",
  "Rejected",
]);

function computeBands(
  nodes: Node[],
  waveMap: Map<string, number>,
): WaveBand[] {
  const grouped = new Map<number, Node[]>();
  for (const n of nodes) {
    const w = waveMap.get(n.id);
    if (w === undefined) continue;
    if (!grouped.has(w)) grouped.set(w, []);
    grouped.get(w)!.push(n);
  }
  const bands: WaveBand[] = [];
  for (const [w, ns] of grouped.entries()) {
    const xs = ns.map((n) => n.position.x);
    const ys = ns.map((n) => n.position.y);
    const ws = ns.map((n) => (n.width ?? TASK_WIDTH) + n.position.x);
    const hs = ns.map((n) => (n.height ?? TASK_HEIGHT) + n.position.y);
    const minX = Math.min(...xs) - PADDING;
    const minY = Math.min(...ys) - PADDING;
    const maxX = Math.max(...ws) + PADDING;
    const maxY = Math.max(...hs) + PADDING;
    let failedCount = 0;
    for (const n of ns) {
      const data = n.data as TaskNodeData | undefined;
      if (data && FAILED_TASK_STATUSES.has(data.status)) {
        failedCount += 1;
      }
    }
    bands.push({
      waveIndex: w,
      bounds: {
        x: minX,
        y: minY,
        width: maxX - minX,
        height: maxY - minY,
      },
      taskCount: ns.length,
      failedCount,
    });
  }
  return bands.sort((a, b) => a.waveIndex - b.waveIndex);
}

const executionNodeTypes = {
  task: TaskNode,
} as const;

function GlobalExecutionDAGViewInner({
  projectName: _projectName,
  project,
  onTaskClick,
  fetchError,
}: GlobalExecutionDAGViewProps) {
  void _projectName; // SSE seam will key off projectName
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const [waveMap, setWaveMap] = useState<Map<string, number>>(new Map());
  const [bands, setBands] = useState<WaveBand[]>([]);
  const [flickerReady, setFlickerReady] = useState(false);
  const ready = useNodesInitialized();
  const layoutBatchRef = useRef(0);
  const lastPositionedBatchRef = useRef(-1);
  const { fitView } = useReactFlow();

  // Build graph from project prop.
  useEffect(() => {
    if (!project) return;
    const { nodes: ns, edges: es, waveMap: wm } = buildExecutionGraph(project);
    layoutBatchRef.current += 1;
    setNodes(
      ns.map((n) => ({ ...n, style: { ...n.style, opacity: 0 } })),
    );
    setEdges(annotateEdges(es, wm));
    setWaveMap(wm);
    setBands([]);
    setFlickerReady(false);
  }, [project, setNodes, setEdges]);

  // Run dagre LR layout once useNodesInitialized fires; compute wave bands.
  useEffect(() => {
    if (!ready) return;
    if (nodes.length === 0) return;
    if (lastPositionedBatchRef.current === layoutBatchRef.current) return;
    const positioned = applyDagreLayout(nodes, edges, "LR");
    lastPositionedBatchRef.current = layoutBatchRef.current;
    const computedBands = computeBands(positioned, waveMap);
    setNodes(
      positioned.map((n) => ({
        ...n,
        style: { ...n.style, opacity: 1 },
      })),
    );
    setBands(computedBands);
    setFlickerReady(true);
  }, [ready, nodes, edges, waveMap, setNodes]);

  // Re-fit after dagre-positioned nodes paint.
  useEffect(() => {
    if (!flickerReady) return;
    const id = requestAnimationFrame(() =>
      fitView({ padding: 0.2, maxZoom: 1 }),
    );
    return () => cancelAnimationFrame(id);
  }, [flickerReady, fitView]);

  const nodeTypes = useMemo(() => executionNodeTypes, []);

  // View states per UI-SPEC §View States.
  if (fetchError) {
    return <EmptyState variant="global-dag-fetch-error" />;
  }

  if (project === null) {
    return (
      <div className="flex h-full w-full items-center justify-center">
        <Loader2
          size={24}
          className="animate-spin"
          style={{ color: "var(--color-text-muted)" }}
        />
      </div>
    );
  }

  if (project.tasks.length === 0) {
    return <EmptyState variant="global-dag-no-tasks" />;
  }

  return (
    <NodeClickContext.Provider value={onTaskClick}>
      <div
        data-testid="global-execution-dag-view"
        data-dagre-direction="LR"
        data-flicker-ready={flickerReady ? "true" : "false"}
        className="h-full w-full relative"
      >
        {/*
          Hidden edge metadata for tests — DOM markers carrying the edge
          type so test assertions can inspect cross-wave routing without
          digging into @xyflow internals. aria-hidden, display:none.
        */}
        <div
          aria-hidden="true"
          style={{ display: "none" }}
          data-testid="global-execution-edges-meta"
        >
          {edges.map((e) => (
            <span
              key={e.id}
              data-edge-meta
              data-edge-id={e.id}
              data-edge-type={e.type ?? "default"}
            />
          ))}
        </div>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          nodeTypes={nodeTypes}
          fitView
          fitViewOptions={{ padding: 0.2, maxZoom: 1 }}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable={true}
          panOnDrag
        >
          {/*
            Wave bands render INSIDE the React Flow viewport via
            <ViewportPortal> so they share the pan/zoom transform and sit
            in flow coordinates — aligned with the dagre-laid-out task nodes.
          */}
          <ViewportPortal>
            <div data-z-layer="wave-background">
              {bands.map((b) => (
                <WaveBackground
                  key={b.waveIndex}
                  waveIndex={b.waveIndex}
                  bounds={b.bounds}
                  taskCount={b.taskCount}
                  isActiveDispatch={project?.activeDispatchWave === b.waveIndex}
                  failedCount={b.failedCount}
                />
              ))}
            </div>
          </ViewportPortal>
        </ReactFlow>
      </div>
    </NodeClickContext.Provider>
  );
}

export default function GlobalExecutionDAGView(
  props: GlobalExecutionDAGViewProps,
) {
  return (
    <ReactFlowProvider>
      <GlobalExecutionDAGViewInner {...props} />
    </ReactFlowProvider>
  );
}
