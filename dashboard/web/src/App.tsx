import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ChevronDown, ChevronUp } from "lucide-react";

import AppShell from "./components/AppShell";
import Header from "./components/Header";
import ConnectionStatusIndicator, {
  type ConnectionState,
} from "./components/ConnectionStatusIndicator";
import { ToastProvider } from "./components/ToastContainer";
import PlanningDAGView from "./components/PlanningDAGView";
import ExecutionDAGView from "./components/ExecutionDAGView";
import GlobalExecutionDAGView, {
  type ProjectExecutionDAGData,
} from "./components/GlobalExecutionDAGView";
import TaskDetailDrawer, { VERDICT_COLOR } from "./components/TaskDetailDrawer";
import PodLogStreamer from "./components/PodLogStreamer";
import ProjectPicker from "./components/ProjectPicker";
import EmptyState from "./components/EmptyState";
import RunningWavesView from "./components/RunningWavesView";
import TelemetryView from "./components/TelemetryView";
import LoadingState from "./components/LoadingState";
import ErrorState from "./components/ErrorState";
import NodeDetailPanel, {
  type PlanningNodeKind,
} from "./components/NodeDetailPanel";
import ProjectSettingsPanel from "./components/ProjectSettingsPanel";
import ArtifactViewer from "./components/ArtifactViewer";
import ApproveStrip from "./components/ApproveStrip";
import PhoenixTraceLink from "./components/PhoenixTraceLink";
import { ResizeHandle, usePersistedSize } from "./components/ResizeHandle";
import { useProjects } from "./lib/projects";
import { useTaskDetail, useTasks } from "./lib/tasks";
import {
  fetchProject,
  fetchPlan,
  type ProjectDetail,
  type DashboardConfig,
  type PlanDetail,
} from "./lib/api";
import type { SSEState } from "./lib/sse";
import type { TideNodeKind } from "./components/TideNodeShell";
import { STATUS_TABLE, type StatusValue } from "./components/StatusBadge";

// Log-area (D-06 second instance) clamps + chrome sizing.
const LOG_HEIGHT_KEY = "tide.dashboard.log-height";
const LOG_DEFAULT_PX = 240;
const LOG_MIN_PX = 120;
const LOG_BAR_PX = 36; // slim collapse/expand control bar height

/**
 * Top-level dashboard component (plan 04-13 + plan 04-17 wiring).
 *
 *   Hooks composed (plan 04-17 — last-mile wiring):
 *     useProjects()                       — fetches /api/v1/projects once + on refetch
 *     useTasks(project, ns, plan)         — fetches /api/v1/plans/{plan}?namespace={ns}
 *                                           on planName change; SSE on
 *                                           /projects/{project}/events triggers debounced refetch
 *     useTaskDetail(project, ns, task)    — fetches /api/v1/tasks/{task}?namespace={ns}
 *                                           on taskName change; same SSE refresh-trigger pattern.
 *   The placeholder defaults from the pre-04-17 wiring are gone; selectedProject
 *   is null until either useProjects defaults it to projects[0].name or the
 *   operator picks one from <ProjectPicker>.
 *
 *   Namespace threading (debug #14): the plan/task REST endpoints default a
 *   missing `namespace` query param to "default" server-side. Without
 *   forwarding the selected project's namespace, plans/tasks in a non-default
 *   namespace (tide-sample-medium, tide-sample-small, …) 404 and render as 0
 *   tasks / 0 waves. We derive the namespace from the projects list (each
 *   ProjectSummary carries {name, namespace, phase}) and pass it into both
 *   hooks. The SSE events endpoint is namespace-agnostic (lists all namespaces
 *   by name), so it needs no namespace.
 *
 *   Renders:
 *     - AppShell chrome (plan 04-12)
 *     - Header projectPicker slot populated by ProjectPicker (plan 04-15 + 04-17)
 *     - PlanningDAGView (left pane) — projectName={selectedProject}
 *     - ExecutionDAGView (right pane) — planName + plan props
 *     - TaskDetailDrawer (overlay) — taskName + task props
 *
 *   State (this plan):
 *     - selectedProject: which project the picker chose (header). Null until
 *       useProjects defaults it to projects[0].name or the operator picks
 *       one from the header dropdown.
 *     - selectedPlan: PlanNode click swaps the right pane.
 *     - selectedTask: TaskNode click opens the drawer.
 *     - streamingTask: drawer "Open log stream" click — the PodLogStreamer
 *       mount lives below the drawer body.
 *
 *   URL hash deep-link support: #/plan/<plan-name> drives selectedPlan via
 *   a window.location.hash watcher (browser-native History API per UI-SPEC
 *   §Plan-click-swaps-right-pane — no router library).
 */
/**
 * A 28px header strip labelling each DAG pane (UI-SPEC — pane labels). Mono
 * 12px semibold muted text over a subtle bottom border.
 *
 * UI-SPEC C4 (15-07-PLAN.md): optional `action` slot for right-aligned
 * affordances (e.g. the EXECUTION pane's "All waves" return button). The
 * PLANNING pane call is untouched and passes no action.
 */
function PaneHeader({ label, action }: { label: string; action?: React.ReactNode }) {
  return (
    <div
      className="flex h-7 shrink-0 items-center justify-between border-b border-[var(--color-border-subtle)] px-3 text-[var(--color-text-muted)]"
      style={{
        fontFamily: "var(--font-mono)",
        fontSize: "12px",
        fontWeight: 600,
      }}
    >
      <span>{label}</span>
      {action}
    </div>
  );
}

type ActiveView = "dags" | "telemetry";

/**
 * Segmented view switcher rendered in the Header left cluster (UI-SPEC §C2, plan 16-05).
 *
 * Segments: DAGs (primary surface) → Telemetry. Container uses role="tablist" /
 * role="tab" semantics; ArrowLeft/ArrowRight move selection with roving focus
 * (accessibility — UI-SPEC C2). Transient state only — no localStorage.
 */
function ViewSwitcher({
  activeView,
  onChange,
}: {
  activeView: ActiveView;
  onChange: (v: ActiveView) => void;
}) {
  const dagsRef = useRef<HTMLButtonElement>(null);
  const telemetryRef = useRef<HTMLButtonElement>(null);

  const handleKeyDown = (e: React.KeyboardEvent<HTMLButtonElement>) => {
    if (e.key === "ArrowRight") {
      e.preventDefault();
      onChange("telemetry");
      telemetryRef.current?.focus();
    } else if (e.key === "ArrowLeft") {
      e.preventDefault();
      onChange("dags");
      dagsRef.current?.focus();
    }
  };

  const segmentStyle = (active: boolean): React.CSSProperties => ({
    background: active ? "var(--color-surface-overlay)" : "transparent",
    color: active ? "var(--color-text-primary)" : "var(--color-text-muted)",
    fontFamily: "var(--font-sans)",
    fontSize: "var(--text-label)",
    fontWeight: 600,
    border: "none",
    padding: "var(--spacing-1) var(--spacing-2)",
    cursor: "pointer",
    outline: "none",
  });

  return (
    <div
      role="tablist"
      aria-label="Dashboard view"
      data-testid="view-switcher"
      style={{
        display: "inline-flex",
        borderRadius: "var(--radius-sm, 4px)",
        border: "1px solid var(--color-border-subtle)",
        background: "transparent",
      }}
    >
      <button
        ref={dagsRef}
        type="button"
        role="tab"
        aria-selected={activeView === "dags"}
        data-testid="view-tab-dags"
        style={segmentStyle(activeView === "dags")}
        onClick={() => onChange("dags")}
        onKeyDown={handleKeyDown}
      >
        DAGs
      </button>
      <button
        ref={telemetryRef}
        type="button"
        role="tab"
        aria-selected={activeView === "telemetry"}
        data-testid="view-tab-telemetry"
        style={segmentStyle(activeView === "telemetry")}
        onClick={() => onChange("telemetry")}
        onKeyDown={handleKeyDown}
      >
        Telemetry
      </button>
    </div>
  );
}

export default function App() {
  // Plan 04-17: selectedProject starts null; useProjects defaults it to
  // projects[0].name once the list lands (single-project clusters never
  // need an extra click). The pre-04-17 "my-project" placeholder is gone.
  const [selectedProject, setSelectedProject] = useState<string | null>(null);
  const [selectedPlan, setSelectedPlan] = useState<string | null>(null);
  const [selectedTask, setSelectedTask] = useState<string | null>(null);
  const [streamingTask, setStreamingTask] = useState<string | null>(null);
  // Plan 37-08 (D-05/D-09): the Planning-DAG node whose detail panel is open
  // (project → settings, milestone/phase → artifacts, plan → artifacts +
  // preserved execution-pane swap). Distinct from selectedPlan/selectedTask so
  // the existing plan/task flows are untouched.
  const [selectedNode, setSelectedNode] = useState<{
    kind: PlanningNodeKind;
    name: string;
  } | null>(null);
  // Plan 37-08: full project detail (milestone/phase/plan lifecycle strings)
  // used to derive a node's gate-parked state for ArtifactViewer + ApproveStrip.
  const [projectDetail, setProjectDetail] = useState<ProjectDetail | null>(null);
  // Plan 46-05 (OBS-04): one-shot GET /api/v1/config fetch for phoenixBaseURL,
  // copying TelemetryView.tsx's fetch shape (TelemetryView's own fetch is
  // untouched — this is a separate one-shot call). "" = unset or fetch
  // failed; PhoenixTraceLink treats "" as hidden — no loading affordance.
  const [phoenixBaseURL, setPhoenixBaseURL] = useState<string>("");
  // Plan 26-04 (D-07): global execution DAG pane state.
  const [showGlobalDAG, setShowGlobalDAG] = useState(false);
  const [globalExecutionDAG, setGlobalExecutionDAG] =
    useState<ProjectExecutionDAGData | null>(null);
  const [globalDAGError, setGlobalDAGError] = useState(false);
  // Plan 16-05 (D-01): transient view switcher — DAGs | Telemetry. Default
  // is DAGs; never persisted to localStorage (UI-SPEC §C2).
  const [activeView, setActiveView] = useState<ActiveView>("dags");
  // Real project-events SSE connection state, driven up from PlanningDAGView.
  // Starts "connecting" so the header pill shows activity before the first
  // project resolves.
  const [connState, setConnState] = useState<SSEState>("connecting");

  // Plan 37-08 (D-06 second instance): drag-to-resize + collapsible log area.
  // The 70%-of-viewport ceiling is recomputed at drag time (window resize),
  // mirroring NodeDetailPanel's width clamp — not frozen at mount.
  const [maxLogH, setMaxLogH] = useState<number>(() =>
    Math.round((typeof window !== "undefined" ? window.innerHeight : 800) * 0.7),
  );
  useEffect(() => {
    const onResize = () => setMaxLogH(Math.round(window.innerHeight * 0.7));
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);
  const [logHeight, setLogHeight, commitLogHeight] = usePersistedSize(
    LOG_HEIGHT_KEY,
    LOG_DEFAULT_PX,
    LOG_MIN_PX,
    maxLogH,
  );
  // Collapse ≠ close: the collapsed panel keeps PodLogStreamer mounted so the
  // SSE stream stays alive; only the close button (onCloseLogStream) unmounts.
  const [logCollapsed, setLogCollapsed] = useState(false);

  // Plan 04-17 hook wiring (replaces the pre-04-17 placeholder defaults).
  const {
    projects,
    loading: projectsLoading,
    error: projectsError,
  } = useProjects();

  // Debug #14: resolve the selected project's namespace from the projects
  // list so the plan/task fetches target the right namespace. Null until a
  // project is selected (or the list hasn't landed) — the hooks pass
  // undefined through to fetchPlan/fetchTask in that case.
  const selectedNamespace = useMemo(
    () => projects.find((p) => p.name === selectedProject)?.namespace ?? null,
    [projects, selectedProject],
  );

  const executionPlan = useTasks(
    selectedProject,
    selectedNamespace,
    selectedPlan,
  );
  const taskDetail = useTaskDetail(
    selectedProject,
    selectedNamespace,
    selectedTask,
  );

  // Plan 53-08 (Contract 3): the Planning-DAG plan node's one-line
  // plan-check mirror needs loopIteration/verifyMaxIterations/loopDecision —
  // executionPlan (useTasks above) folds PlanDetail into ExecutionPlanData
  // for the execution-DAG pane and drops those fields in the process, so
  // this is a small targeted fetch of the SAME existing GET plan endpoint
  // (53-04 already landed the payload; no new endpoint, no shape change).
  const [planCheckDetail, setPlanCheckDetail] = useState<PlanDetail | null>(null);
  useEffect(() => {
    if (!selectedNode || selectedNode.kind !== "plan") {
      setPlanCheckDetail(null);
      return;
    }
    let cancelled = false;
    fetchPlan(selectedNode.name, selectedNamespace ?? undefined)
      .then((detail) => {
        if (!cancelled) setPlanCheckDetail(detail);
      })
      .catch(() => {
        if (!cancelled) setPlanCheckDetail(null);
      });
    return () => {
      cancelled = true;
    };
  }, [selectedNode, selectedNamespace]);

  // Plan 26-04 (D-07): fetch the project-scoped global execution DAG when
  // showGlobalDAG is active. Mirrors the fetchPlan hook pattern in lib/tasks.ts.
  // The API shape {projectName, tasks[]{name,phase,waveIndex,attempt,dependsOn}}
  // maps identically to ProjectExecutionDAGData (phase → status coercion).
  useEffect(() => {
    if (!showGlobalDAG || !selectedProject) return;
    let cancelled = false;
    const knownStatuses = new Set<StatusValue>(
      Object.keys(STATUS_TABLE) as StatusValue[],
    );
    const coerce = (phase: string): StatusValue =>
      knownStatuses.has(phase as StatusValue)
        ? (phase as StatusValue)
        : "Pending";
    const ns = selectedNamespace ?? "default";
    fetch(
      `/api/v1/projects/${encodeURIComponent(selectedProject)}/execution-dag?namespace=${encodeURIComponent(ns)}`,
    )
      .then(async (res) => {
        if (cancelled) return;
        if (!res.ok) {
          setGlobalDAGError(true);
          setGlobalExecutionDAG({ projectName: selectedProject, tasks: [] });
          return;
        }
        const body = (await res.json()) as {
          projectName: string;
          tasks: {
            name: string;
            phase: string;
            waveIndex: number;
            attempt: number;
            dependsOn: string[];
          }[];
        };
        if (cancelled) return;
        setGlobalDAGError(false);
        setGlobalExecutionDAG({
          projectName: body.projectName,
          tasks: body.tasks.map((t) => ({
            name: t.name,
            status: coerce(t.phase),
            waveIndex: t.waveIndex,
            attempt: t.attempt,
            dependsOn: t.dependsOn ?? [],
          })),
        });
      })
      .catch(() => {
        if (cancelled) return;
        setGlobalDAGError(true);
        setGlobalExecutionDAG({ projectName: selectedProject ?? "", tasks: [] });
      });
    return () => {
      cancelled = true;
    };
  }, [showGlobalDAG, selectedProject, selectedNamespace]);

  // Default selectedProject to the first project once useProjects resolves.
  // Operators with a single-project cluster never see the picker; multi-
  // project clusters land on the first entry and the picker lets them
  // switch. If selectedProject is already set (deep-link or prior pick),
  // we never clobber it.
  useEffect(() => {
    if (selectedProject === null && projects.length > 0) {
      setSelectedProject(projects[0].name);
    }
  }, [projects, selectedProject]);

  // URL hash deep-link: #/plan/<name> sets selectedPlan.
  useEffect(() => {
    const apply = () => {
      const m = window.location.hash.match(/^#\/plan\/([^/]+)/);
      if (m) setSelectedPlan(decodeURIComponent(m[1]));
    };
    apply();
    window.addEventListener("hashchange", apply);
    return () => window.removeEventListener("hashchange", apply);
  }, []);

  // Plan 37-08: fetch the full project detail so a milestone/phase/plan node's
  // lifecycle string is available to derive gate-parked state. Mirrors the
  // globalExecutionDAG fetch pattern; PlanningDAGView fetches the same payload
  // for the graph, but the App needs it locally to route clicks without a
  // per-kind provider tree (the click callback is (kind, name) only).
  useEffect(() => {
    if (!selectedProject) {
      setProjectDetail(null);
      return;
    }
    let cancelled = false;
    fetchProject(selectedProject, selectedNamespace ?? undefined)
      .then((d) => {
        if (!cancelled) setProjectDetail(d);
      })
      .catch(() => {
        if (!cancelled) setProjectDetail(null);
      });
    return () => {
      cancelled = true;
    };
  }, [selectedProject, selectedNamespace]);

  // Plan 46-05 (OBS-04): one-shot fetch of GET /api/v1/config for
  // phoenixBaseURL, passed down as a prop to both PhoenixTraceLink mounts.
  // Per page load, not per component mount; any failure or missing field
  // degrades to "" (observability never gates — no error surface).
  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const res = await fetch("/api/v1/config");
        if (!res.ok) return;
        const body = (await res.json()) as DashboardConfig;
        if (!cancelled && typeof body.phoenixBaseURL === "string") {
          setPhoenixBaseURL(body.phoenixBaseURL);
        }
      } catch {
        // Config surface unreachable — stay "" (hidden link).
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  // PlanNode click → swap right pane + update URL hash (deep-link). Used by
  // RunningWavesView (wave-card navigation) and as PlanningDAGView's fallback;
  // behavior unchanged from pre-37-08 (does NOT open the artifact panel).
  const onPlanClick = useCallback((name: string) => {
    setSelectedPlan(name);
    window.location.hash = `#/plan/${encodeURIComponent(name)}`;
  }, []);

  const onTaskClick = useCallback((name: string) => {
    setSelectedTask(name);
  }, []);

  // Plan 37-08: kind-aware Planning-DAG click routing. Project/milestone/phase
  // open the NodeDetailPanel; plan additionally preserves the existing
  // execution-pane swap + #/plan/<name> deep link.
  const onNodeClick = useCallback((kind: TideNodeKind, name: string) => {
    if (kind === "task") {
      // Defensive: the Planning DAG has no task nodes, but keep task routing
      // consistent with the Execution DAGs if ever reached.
      setSelectedTask(name);
      return;
    }
    setSelectedNode({ kind, name });
    if (kind === "plan") {
      setSelectedPlan(name);
      window.location.hash = `#/plan/${encodeURIComponent(name)}`;
    }
  }, []);

  const onCloseNodePanel = useCallback(() => {
    setSelectedNode(null);
  }, []);

  const onCloseDrawer = useCallback(() => {
    setSelectedTask(null);
  }, []);

  const onOpenLogStream = useCallback((name: string) => {
    // Drawer "Open log stream" button → mount <PodLogStreamer> inline
    // below the drawer body (UI-SPEC §8). The streamer owns its own
    // SSE EventSource lifecycle via useTaskLog (src/lib/sse.ts).
    setStreamingTask(name);
  }, []);

  const onCloseLogStream = useCallback(() => {
    setStreamingTask(null);
  }, []);

  // Map the 4-state SSE stream onto the indicator's 3-state vocabulary. The
  // pill has no distinct "connecting" — pre-connection and mid-reconnect both
  // read as "reconnecting" (a pulsing dot). With no project selected the
  // stream sits at "connecting"/"reconnecting", which is the intended idle look.
  const connectionState: ConnectionState = useMemo(() => {
    switch (connState) {
      case "connected":
        return "connected";
      case "offline":
        return "offline";
      case "connecting":
      case "reconnecting":
        return "reconnecting";
    }
  }, [connState]);

  // Body branching (UI-SPEC §13/§14/§15):
  //   - projectsError       → ErrorState ERR1 (backend-unreachable)
  //   - initial load        → LoadingState L1 (initial)
  //   - empty cluster       → EmptyState  E1 (no-projects)
  //   - otherwise           → the two-column grid (PlanningDAGView + ExecutionDAGView)
  let body: React.ReactNode;
  if (projectsError) {
    body = <ErrorState variant="backend-unreachable" />;
  } else if (projectsLoading && projects.length === 0) {
    body = <LoadingState variant="initial" />;
  } else if (projects.length === 0) {
    body = <EmptyState variant="no-projects" />;
  } else if (activeView === "telemetry") {
    // Plan 16-05 (D-01): full-width TelemetryView — no two-pane grid, no
    // split-ratio involvement. Receives the same projects array and
    // selectedProject the DAGs view already holds (16-04 props contract).
    body = (
      <TelemetryView projects={projects} selectedProject={selectedProject} />
    );
  } else {
    body = (
      <div className="grid h-full grid-cols-2 gap-2 p-4">
        <div className="flex flex-col overflow-hidden rounded border border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)]">
          <PaneHeader label="PLANNING" />
          {/* selectedProject is guaranteed non-null in this branch: the
              auto-default useEffect picks projects[0].name once
              projects.length > 0, which is the same gate as this branch. */}
          <div className="min-h-0 flex-1">
            <PlanningDAGView
              projectName={selectedProject ?? ""}
              onPlanClick={onPlanClick}
              onNodeClick={onNodeClick}
              onConnectionStateChange={setConnState}
            />
          </div>
        </div>
        <div className="flex flex-col overflow-hidden rounded border border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)]">
          {/* UI-SPEC C4 (15-07-PLAN.md): EXECUTION PaneHeader gains an "All waves"
              return button when a plan is selected — clears selectedPlan + URL hash
              so the right pane returns to the RunningWavesView aggregate (D-13).
              Plan 26-04 (D-07): adds a "Global DAG" button that sets showGlobalDAG;
              the pane label changes to "GLOBAL EXECUTION DAG" when active. */}
          <PaneHeader
            label={showGlobalDAG ? "GLOBAL EXECUTION DAG" : "EXECUTION"}
            action={
              selectedPlan ? (
                <button
                  data-testid="execution-pane-all-waves"
                  aria-label="Show all running waves"
                  onClick={() => {
                    setSelectedPlan(null);
                    // Mirror how onPlanClick writes the hash — clear it symmetrically.
                    history.replaceState(null, "", window.location.pathname + window.location.search);
                  }}
                  style={{
                    fontFamily: "var(--font-mono)",
                    fontSize: "12px",
                    fontWeight: 600,
                    color: "var(--color-text-muted)",
                    background: "none",
                    border: "none",
                    padding: 0,
                    cursor: "pointer",
                  }}
                  onMouseEnter={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.color =
                      "var(--color-text-primary)";
                  }}
                  onMouseLeave={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.color =
                      "var(--color-text-muted)";
                  }}
                >
                  All waves
                </button>
              ) : showGlobalDAG ? (
                <button
                  data-testid="execution-pane-all-waves"
                  aria-label="Show all running waves"
                  onClick={() => setShowGlobalDAG(false)}
                  style={{
                    fontFamily: "var(--font-mono)",
                    fontSize: "12px",
                    fontWeight: 600,
                    color: "var(--color-text-muted)",
                    background: "none",
                    border: "none",
                    padding: 0,
                    cursor: "pointer",
                  }}
                  onMouseEnter={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.color =
                      "var(--color-text-primary)";
                  }}
                  onMouseLeave={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.color =
                      "var(--color-text-muted)";
                  }}
                >
                  All waves
                </button>
              ) : (
                <button
                  data-testid="execution-pane-global-dag"
                  aria-label="Show global execution DAG"
                  onClick={() => {
                    setSelectedPlan(null);
                    setGlobalExecutionDAG(null);
                    setGlobalDAGError(false);
                    setShowGlobalDAG(true);
                  }}
                  style={{
                    fontFamily: "var(--font-mono)",
                    fontSize: "12px",
                    fontWeight: 600,
                    color: "var(--color-text-muted)",
                    background: "none",
                    border: "none",
                    padding: 0,
                    cursor: "pointer",
                  }}
                  onMouseEnter={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.color =
                      "var(--color-text-primary)";
                  }}
                  onMouseLeave={(e) => {
                    (e.currentTarget as HTMLButtonElement).style.color =
                      "var(--color-text-muted)";
                  }}
                >
                  Global DAG
                </button>
              )
            }
          />
          <div className="min-h-0 flex-1">
            {/* UI-SPEC C4 / D-13 (15-07-PLAN.md): replace the "Select a plan" empty
                state with RunningWavesView as the right-pane default. The
                selectedPlan !== null branch (ExecutionDAGView) is unchanged.
                Plan 26-04 (D-07): three-way conditional: selectedPlan →
                ExecutionDAGView; showGlobalDAG → GlobalExecutionDAGView;
                else → RunningWavesView. */}
            {selectedPlan ? (
              <ExecutionDAGView
                planName={selectedPlan}
                plan={executionPlan}
                onTaskClick={onTaskClick}
              />
            ) : showGlobalDAG ? (
              <GlobalExecutionDAGView
                projectName={selectedProject ?? ""}
                project={globalExecutionDAG}
                fetchError={globalDAGError}
                onTaskClick={onTaskClick}
              />
            ) : (
              <RunningWavesView
                projectName={selectedProject ?? ""}
                onPlanClick={onPlanClick}
              />
            )}
          </div>
        </div>
      </div>
    );
  }

  // Plan 37-08: build the NodeDetailPanel content for the selected node.
  //   project           → ProjectSettingsPanel (status-strip props from the
  //                       already-fetched project summary; no refetch)
  //   milestone / phase / plan → ArtifactViewer (+ ApproveStrip when the node
  //                       is gate-parked / AwaitingApproval — D-08)
  // Plan nodes ALSO keep the execution-pane swap (handled in onNodeClick); the
  // artifacts render in this shared panel — the uniform artifact home across
  // all kinds — while the execution DAG remains in the right pane underneath.
  let nodePanelContent: React.ReactNode = null;
  if (selectedNode) {
    if (selectedNode.kind === "project") {
      const summary = projects.find((p) => p.name === selectedNode.name) ?? null;
      nodePanelContent = (
        <>
          {/* Plan 46-05 (OBS-04), UI-SPEC mount 1: first child inside the
              panel content, above ProjectSettingsPanel. */}
          <PhoenixTraceLink
            baseURL={phoenixBaseURL}
            traceId={projectDetail?.traceId ?? ""}
            spanId={projectDetail?.traceSpanId}
            edge="bottom"
          />
          <ProjectSettingsPanel
            projectName={selectedNode.name}
            namespace={selectedNamespace ?? undefined}
            statusPhase={summary?.phase ?? ""}
            budgetSpentCents={summary?.budget.currentSpend ?? 0}
            budgetCapCents={summary?.budget.capCents ?? 0}
            conditions={summary?.blockingConditions ?? []}
          />
        </>
      );
    } else {
      // Derive the node's lifecycle string from the fetched project detail so
      // gate-parked review (materializing state + ApproveStrip) is accurate.
      const refs =
        selectedNode.kind === "milestone"
          ? projectDetail?.milestones
          : selectedNode.kind === "phase"
            ? projectDetail?.phases
            : projectDetail?.plans;
      const nodeRef = refs?.find((r) => r.name === selectedNode.name);
      const gateParked = nodeRef?.phase === "AwaitingApproval";
      // Plan 53-08 (Contract 3): eligibility is all-three-or-nothing — the
      // server emits loopIteration/verifyMaxIterations/loopDecision as an
      // omitempty trio, so any one being undefined means the plan-check
      // loop hasn't run yet (absence renders nothing, D-08 minimal shape).
      const planCheckEligible =
        selectedNode.kind === "plan" &&
        planCheckDetail?.loopIteration !== undefined &&
        planCheckDetail?.verifyMaxIterations !== undefined &&
        planCheckDetail?.loopDecision !== undefined;
      nodePanelContent = (
        <>
          {/* Plan 46-05 (OBS-04), UI-SPEC mount 1: first child inside the
              panel content, above ArtifactViewer. */}
          <PhoenixTraceLink
            baseURL={phoenixBaseURL}
            traceId={projectDetail?.traceId ?? ""}
            spanId={nodeRef?.traceSpanId}
            edge="bottom"
          />
          {planCheckEligible && planCheckDetail && (
            <div className="px-6 pt-4" data-testid="plan-check-mirror">
              <div
                className="text-[var(--color-text-muted)]"
                style={{
                  fontSize: "11px",
                  textTransform: "uppercase",
                  letterSpacing: "0.04em",
                }}
              >
                Plan check
              </div>
              <div className="mt-0.5" style={{ fontSize: "13px" }}>
                iteration {planCheckDetail.loopIteration} of{" "}
                {planCheckDetail.verifyMaxIterations} ·{" "}
                <span
                  style={{
                    fontFamily: "var(--font-mono)",
                    color: VERDICT_COLOR[planCheckDetail.loopDecision as string],
                  }}
                >
                  {planCheckDetail.loopDecision}
                </span>
              </div>
            </div>
          )}
          <div className="min-h-0 flex-1">
            <ArtifactViewer
              kind={selectedNode.kind}
              name={selectedNode.name}
              project={selectedProject ?? ""}
              namespace={selectedNamespace ?? undefined}
              gateParked={gateParked}
            />
          </div>
          {gateParked && <ApproveStrip projectName={selectedProject ?? ""} />}
        </>
      );
    }
  }

  return (
    <ToastProvider>
      <AppShell
        header={
          <Header
            connectionStatus={<ConnectionStatusIndicator state={connectionState} />}
            projectPicker={
              <ProjectPicker projects={projects.map((p) => ({ name: p.name, namespace: p.namespace, phase: p.phase }))} value={selectedProject} onChange={setSelectedProject} />
            }
            viewSwitcher={<ViewSwitcher activeView={activeView} onChange={setActiveView} />}
          />
        }
      >
        {body}
        <TaskDetailDrawer
          taskName={selectedTask}
          task={taskDetail}
          onClose={onCloseDrawer}
          onOpenLogStream={onOpenLogStream}
          phoenixBaseURL={phoenixBaseURL}
        />
        {selectedNode && (
          <NodeDetailPanel
            open
            kind={selectedNode.kind}
            name={selectedNode.name}
            onClose={onCloseNodePanel}
          >
            {nodePanelContent}
          </NodeDetailPanel>
        )}
        {streamingTask !== null && (
          <div
            data-testid="streaming-task-panel"
            className="fixed inset-x-0 bottom-0 z-50 border-t border-[var(--color-border-subtle)]"
            style={{
              height: `${logCollapsed ? LOG_BAR_PX : logHeight}px`,
              background: "var(--color-surface-raised)",
            }}
          >
            {/* Top-edge drag-to-resize (D-06 second instance) — hidden while
                collapsed. Persisted to tide.dashboard.log-height on release. */}
            {!logCollapsed && (
              <div className="absolute inset-x-0 top-0 z-10">
                <ResizeHandle
                  orientation="horizontal"
                  value={logHeight}
                  min={LOG_MIN_PX}
                  max={maxLogH}
                  onChange={setLogHeight}
                  onCommit={commitLogHeight}
                  label="Resize log area"
                />
              </div>
            )}
            <div className="flex h-full w-full flex-col">
              {/* Slim control bar: collapse ⇄ expand. Collapse keeps the stream
                  mounted (≠ close); the PodLogStreamer close button unmounts. */}
              <div
                className="flex shrink-0 items-center border-b border-[var(--color-border-subtle)] px-3"
                style={{ height: `${LOG_BAR_PX}px` }}
              >
                <button
                  type="button"
                  data-testid="log-collapse-toggle"
                  onClick={() => setLogCollapsed((c) => !c)}
                  aria-label={
                    logCollapsed ? "Expand log stream" : "Collapse log stream"
                  }
                  aria-expanded={!logCollapsed}
                  className="inline-flex items-center gap-1 rounded px-2 py-1 text-[var(--color-text-muted)] hover:bg-[var(--color-surface-overlay)]"
                  style={{
                    fontFamily: "var(--font-mono)",
                    fontSize: "12px",
                    fontWeight: 600,
                  }}
                >
                  {logCollapsed ? (
                    <ChevronUp size={14} aria-hidden="true" />
                  ) : (
                    <ChevronDown size={14} aria-hidden="true" />
                  )}
                  {logCollapsed ? "Log stream" : "Collapse"}
                </button>
              </div>
              {/* PodLogStreamer stays mounted while collapsed (hidden) so the
                  SSE stream survives collapse/expand. */}
              <div
                className="min-h-0 flex-1"
                style={{ display: logCollapsed ? "none" : "block" }}
              >
                <PodLogStreamer
                  taskName={streamingTask}
                  namespace={selectedNamespace ?? undefined}
                  onClose={onCloseLogStream}
                />
              </div>
            </div>
          </div>
        )}
      </AppShell>
    </ToastProvider>
  );
}
