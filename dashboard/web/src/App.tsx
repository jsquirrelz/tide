import { useCallback, useEffect, useMemo, useState } from "react";

import AppShell from "./components/AppShell";
import Header from "./components/Header";
import ConnectionStatusIndicator, {
  type ConnectionState,
} from "./components/ConnectionStatusIndicator";
import { ToastProvider } from "./components/ToastContainer";
import PlanningDAGView from "./components/PlanningDAGView";
import ExecutionDAGView from "./components/ExecutionDAGView";
import TaskDetailDrawer from "./components/TaskDetailDrawer";
import PodLogStreamer from "./components/PodLogStreamer";
import ProjectPicker from "./components/ProjectPicker";
import EmptyState from "./components/EmptyState";
import LoadingState from "./components/LoadingState";
import ErrorState from "./components/ErrorState";
import { useProjects } from "./lib/projects";
import { useTaskDetail, useTasks } from "./lib/tasks";
import type { SSEState } from "./lib/sse";

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
 */
function PaneHeader({ label }: { label: string }) {
  return (
    <div
      className="flex h-7 shrink-0 items-center border-b border-[var(--color-border-subtle)] px-3 text-[var(--color-text-muted)]"
      style={{
        fontFamily: "var(--font-mono)",
        fontSize: "12px",
        fontWeight: 600,
      }}
    >
      {label}
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
  // Real project-events SSE connection state, driven up from PlanningDAGView.
  // Starts "connecting" so the header pill shows activity before the first
  // project resolves.
  const [connState, setConnState] = useState<SSEState>("connecting");

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

  // PlanNode click → swap right pane + update URL hash (deep-link).
  const onPlanClick = useCallback((name: string) => {
    setSelectedPlan(name);
    window.location.hash = `#/plan/${encodeURIComponent(name)}`;
  }, []);

  const onTaskClick = useCallback((name: string) => {
    setSelectedTask(name);
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
              onConnectionStateChange={setConnState}
            />
          </div>
        </div>
        <div className="flex flex-col overflow-hidden rounded border border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)]">
          <PaneHeader label="EXECUTION" />
          <div className="min-h-0 flex-1">
            {selectedPlan ? (
              <ExecutionDAGView
                planName={selectedPlan}
                plan={executionPlan}
                onTaskClick={onTaskClick}
              />
            ) : (
              <div
                className="flex h-full items-center justify-center text-[var(--color-text-muted)]"
                style={{ fontFamily: "var(--font-mono)", fontSize: "13px" }}
              >
                Select a plan to view its execution DAG
              </div>
            )}
          </div>
        </div>
      </div>
    );
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
          />
        }
      >
        {body}
        <TaskDetailDrawer
          taskName={selectedTask}
          task={taskDetail}
          onClose={onCloseDrawer}
          onOpenLogStream={onOpenLogStream}
        />
        {streamingTask !== null && (
          <div
            data-testid="streaming-task-panel"
            className="fixed inset-x-0 bottom-0 z-50 border-t border-[var(--color-border-subtle)]"
            style={{ height: "240px", background: "var(--color-surface-raised)" }}
          >
            <PodLogStreamer
              taskName={streamingTask}
              onClose={onCloseLogStream}
            />
          </div>
        )}
      </AppShell>
    </ToastProvider>
  );
}
