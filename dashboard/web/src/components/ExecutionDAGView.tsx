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

import TaskNode, { type TaskNodeData } from "./TaskNode";
import WaveBackground from "./WaveBackground";
import { NodeClickContext } from "./NodeClickContext";
import { applyDagreLayout } from "../lib/layout";
import type { StatusValue } from "./StatusBadge";

/**
 * <ExecutionDAGView> (UI-SPEC §4).
 *
 *   Renders the Execution DAG for the selected Plan:
 *     TaskNode × N grouped into wave bands; cross-wave edges visible.
 *
 *   Layout: dagre left-right (rankdir LR). Wave bands are *presentational*
 *   — tasks belong to waves by `pkg/dag.ComputeWaves`, not by @xyflow
 *   parent containment. We render each wave's <WaveBackground> SVG band
 *   at z-index 0 with a computed bounds rect over the wave's task column;
 *   task nodes overlay above.
 *
 *   Cross-wave edges use type "smoothstep" per RESEARCH §610 — intra-wave
 *   edges fall through to React Flow's default (default).
 *
 *   For v1.0 the plan data is passed in as a prop (the SSE-driven `useTasks`
 *   hook lands in plan 04-16). The component's interface is shaped so the
 *   hook can plug in without refactor.
 */
export type ExecutionTaskData = {
  name: string;
  status: StatusValue;
  waveIndex: number;
  attempt: number;
  dependsOn: string[];
};

export type ExecutionPlanData = {
  planName: string;
  tasks: ExecutionTaskData[];
  /** Wave currently dispatching Jobs (gets accent dash stroke). */
  activeDispatchWave?: number;
};

export type ExecutionDAGViewProps = {
  planName: string;
  /** Inlined plan data for v1.0 — plan 04-16 swaps for `useTasks(planName)`. */
  plan: ExecutionPlanData | null;
  onTaskClick: (taskName: string) => void;
};

const PADDING = 24;
const TASK_WIDTH = 160;
const TASK_HEIGHT = 48;

function buildExecutionGraph(plan: ExecutionPlanData): {
  nodes: Node[];
  edges: Edge[];
  waveMap: Map<string, number>;
} {
  const nodes: Node[] = [];
  const edges: Edge[] = [];
  const waveMap = new Map<string, number>();

  for (const t of plan.tasks) {
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
      });
    }
  }
  return { nodes, edges, waveMap };
}

/**
 * Assigns smoothstep to cross-wave edges; intra-wave edges keep the
 * default (straight) edge type per RESEARCH §610.
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

/**
 * Compute wave-band bounds from positioned task nodes. Each band wraps
 * its wave's tasks with `PADDING` slack on all sides. Used for the
 * <WaveBackground> SVG render at z-index 0.
 */
type WaveBand = {
  waveIndex: number;
  bounds: { x: number; y: number; width: number; height: number };
  taskCount: number;
  /** WR-13 fix: count of member tasks in the failed family — drives
   * WaveBackground's blocked-color fill (UI-SPEC §6). */
  failedCount: number;
};

/**
 * The status family that triggers the WaveBackground "failed band" signal
 * per UI-SPEC §6. Matches the FAILED_STATUSES set in TideNodeShell — kept
 * locally to avoid a circular component import; if the list drifts in
 * either file, the visual signal breaks. (Status strings come from the
 * CRD enum so the set is bounded.)
 */
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
    // WR-13 fix: count member tasks whose status is in the failed family
    // so WaveBackground can render the failed-band UI-SPEC §6 signal.
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

function ExecutionDAGViewInner({
  planName: _planName,
  plan,
  onTaskClick,
}: ExecutionDAGViewProps) {
  void _planName; // future SSE seam will key off planName
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const [waveMap, setWaveMap] = useState<Map<string, number>>(new Map());
  const [bands, setBands] = useState<WaveBand[]>([]);
  const [flickerReady, setFlickerReady] = useState(false);
  const ready = useNodesInitialized();
  const layoutBatchRef = useRef(0);
  const lastPositionedBatchRef = useRef(-1);

  // Build graph from plan prop. Two distinct effect ticks (mount + layout)
  // give Pitfall 26 mitigation its transitional state.
  useEffect(() => {
    if (!plan) return;
    const { nodes: ns, edges: es, waveMap: wm } = buildExecutionGraph(plan);
    layoutBatchRef.current += 1;
    setNodes(
      ns.map((n) => ({ ...n, style: { ...n.style, opacity: 0 } })),
    );
    setEdges(annotateEdges(es, wm));
    setWaveMap(wm);
    setBands([]);
    setFlickerReady(false);
  }, [plan, setNodes, setEdges]);

  // Run dagre LR layout once useNodesInitialized fires; compute wave bands
  // from the positioned coords; flip opacity to 1.
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

  const nodeTypes = useMemo(() => executionNodeTypes, []);

  return (
    <NodeClickContext.Provider value={onTaskClick}>
      <div
        data-testid="execution-dag-view"
        data-dagre-direction="LR"
        data-flicker-ready={flickerReady ? "true" : "false"}
        className="h-full w-full relative"
      >
        {/*
          Wave bands render in a z=0 SVG layer behind the @xyflow nodes
          (z=10+ inside the ReactFlow renderer). Per RESEARCH §609 we
          ship raw SVG rectangles rather than @xyflow `parentNode`
          containment, because tasks belong to a wave by computation,
          not by parent-child.
        */}
        <svg
          data-z-layer="wave-background"
          style={{
            position: "absolute",
            inset: 0,
            pointerEvents: "none",
            zIndex: 0,
          }}
          aria-hidden="true"
        >
          {bands.map((b) => (
            <WaveBackground
              key={b.waveIndex}
              waveIndex={b.waveIndex}
              bounds={b.bounds}
              taskCount={b.taskCount}
              isActiveDispatch={plan?.activeDispatchWave === b.waveIndex}
              /* WR-13 fix: drive failed-band styling from member task statuses. */
              failedCount={b.failedCount}
            />
          ))}
        </svg>
        {/*
          Hidden edge metadata for tests — DOM markers carrying the edge
          type so test assertions can inspect cross-wave routing without
          digging into @xyflow internals. aria-hidden, display:none.
        */}
        <div
          aria-hidden="true"
          style={{ display: "none" }}
          data-testid="execution-edges-meta"
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
          fitViewOptions={{ padding: 0.2 }}
          nodesDraggable={false}
          nodesConnectable={false}
          elementsSelectable={true}
          panOnDrag
        />
      </div>
    </NodeClickContext.Provider>
  );
}

export default function ExecutionDAGView(props: ExecutionDAGViewProps) {
  return (
    <ReactFlowProvider>
      <ExecutionDAGViewInner {...props} />
    </ReactFlowProvider>
  );
}
