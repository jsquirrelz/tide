import { useCallback, useEffect, useState } from "react";

import AppShell from "./components/AppShell";
import Header from "./components/Header";
import ConnectionStatusIndicator from "./components/ConnectionStatusIndicator";
import { ToastProvider } from "./components/ToastContainer";
import PlanningDAGView from "./components/PlanningDAGView";
import ExecutionDAGView, {
  type ExecutionPlanData,
} from "./components/ExecutionDAGView";
import TaskDetailDrawer, {
  type TaskDetailData,
} from "./components/TaskDetailDrawer";

/**
 * Top-level dashboard component (plan 04-13 wiring).
 *
 *   Renders:
 *     - <AppShell> chrome (plan 04-12)
 *     - <PlanningDAGView projectName={selectedProject} /> (left pane)
 *     - <ExecutionDAGView planName={selectedPlan} /> (right pane)
 *     - <TaskDetailDrawer taskName={selectedTask} /> (overlay)
 *
 *   State (this plan):
 *     - selectedProject: which project the picker chose (header — wired in
 *       plan 04-15's <ProjectPicker> slot, defaults to a placeholder until
 *       the SSE bootstrap lands).
 *     - selectedPlan: PlanNode click swaps the right pane.
 *     - selectedTask: TaskNode click opens the drawer.
 *     - streamingTask: drawer "Open log stream" click — the PodLogStreamer
 *       mount itself lands in plan 04-16.
 *
 *   URL hash deep-link support: #/plan/<plan-name> drives selectedPlan via
 *   a window.location.hash watcher (browser-native History API per UI-SPEC
 *   §Plan-click-swaps-right-pane — no router library).
 */
export default function App() {
  // Placeholder defaults — plan 04-15's ProjectPicker + plan 04-16's SSE
  // bootstrap populate these from the live cluster. v1.0 ships with a
  // sane default so a freshly-deployed cluster boots straight into the
  // empty-state pane.
  const [selectedProject, setSelectedProject] = useState<string>("my-project");
  const [selectedPlan, setSelectedPlan] = useState<string | null>(null);
  const [selectedTask, setSelectedTask] = useState<string | null>(null);
  const [, setStreamingTask] = useState<string | null>(null);

  // setSelectedProject reserved for plan 04-15's ProjectPicker wiring.
  void setSelectedProject;

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
    // Plan 04-16 mounts <PodLogStreamer> inline below the drawer body
    // when streamingTask is non-null. For this plan we just hold the
    // state — the streamer component renders nothing yet.
    setStreamingTask(name);
  }, []);

  // Empty plan data for the right pane until a Plan is selected. Plan
  // 04-16's useTasks(planName) hook replaces this with live SSE data.
  const executionPlan: ExecutionPlanData | null = selectedPlan
    ? { planName: selectedPlan, tasks: [] }
    : null;

  // Resolved task detail data is null until a fetch hook is wired in
  // plan 04-16. The drawer renders nothing when task is null (UI-SPEC §7).
  const taskDetail: TaskDetailData | null = null;

  return (
    <ToastProvider>
      <AppShell
        header={
          <Header
            connectionStatus={<ConnectionStatusIndicator state="connected" />}
          />
        }
      >
        <div className="grid h-full grid-cols-2 gap-2 p-4">
          <div className="rounded border border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)]">
            <PlanningDAGView
              projectName={selectedProject}
              onPlanClick={onPlanClick}
            />
          </div>
          <div className="rounded border border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)]">
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
        <TaskDetailDrawer
          taskName={selectedTask}
          task={taskDetail}
          onClose={onCloseDrawer}
          onOpenLogStream={onOpenLogStream}
        />
      </AppShell>
    </ToastProvider>
  );
}
