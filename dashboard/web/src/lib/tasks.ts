/*
 * tasks.ts (plan 04-17)
 *
 *   `useTasks(projectName, namespace, planName)` and
 *   `useTaskDetail(projectName, namespace, taskName)` — the two hooks
 *   App.tsx wires into ExecutionDAGView and TaskDetailDrawer respectively.
 *
 *   Both follow the same composition pattern:
 *     1. fetch the rich detail via the new lib/api.ts helpers (fetchPlan
 *        / fetchTask) on initial mount + on name change. The selected
 *        project's `namespace` is forwarded to the REST call so plans/tasks
 *        in a non-`default` namespace resolve (debug #14: omitting it let
 *        the backend default to "default" → 404 → empty render).
 *     2. compose useSSEStream against the project-events SSE URL with an
 *        `onMessage` callback that filters by `kind` + `planRef` / `name`
 *        and schedules a debounced (250ms) re-fetch.
 *
 *   Namespace note (debug #14): the REST endpoints (/plans/{name},
 *   /tasks/{name}) default a missing `namespace` query param to "default"
 *   server-side, so the namespace MUST be forwarded for non-default
 *   projects. The SSE events endpoint (/projects/{name}/events) does NOT
 *   share this footgun — its handler lists across all namespaces and
 *   matches by name when namespace is empty, and the event hub is keyed by
 *   Project name only. So only fetchPlan/fetchTask need the namespace.
 *
 *   SSE composition note (plan 04-17 deferral): useSSEStream is invoked
 *   UNCONDITIONALLY with a synthetic non-cluster URL ("/dev/null/no-project")
 *   when projectName is null. The EventSource constructor will hit the
 *   chi SPA fallback (HTTP 200 + text/html); EventSource rejects
 *   non-text/event-stream and fires onerror, which triggers useSSEStream's
 *   exponential reconnect-backoff (capped at 30s per sse.ts:186-198). The
 *   steady-state cost is ≤1 EventSource construction per 30s while no
 *   project is selected — bounded but not free. The cleaner fix (a
 *   `disabled?: boolean` option on useSSEStream) is deferred to a future
 *   plan; modifying sse.ts would force re-running plan 04-16's test
 *   matrix and re-threat-modeling its surface.
 */
import { useCallback, useEffect, useRef, useState } from "react";

import {
  fetchPlan,
  fetchTask,
  type PlanDetail,
  type PlanTaskCard,
  type TaskDetailJSON,
} from "./api";
import { useSSEStream } from "./sse";
import type { ExecutionPlanData } from "../components/ExecutionDAGView";
import type { TaskDetailData } from "../components/TaskDetailDrawer";
import { STATUS_TABLE, type StatusValue } from "../components/StatusBadge";

// Re-exports so consumers can refer to the hook results without
// re-importing the underlying component types directly.
export type TasksResult = ExecutionPlanData | null;
export type TaskDetailResult = TaskDetailData | null;

// KNOWN_STATUSES is derived from the canonical STATUS_TABLE in
// StatusBadge.tsx (plan 04-15's single source of truth). Any phase value
// not in this set coerces to "Pending" — keeps the UI safe against
// backend phase additions.
const KNOWN_STATUSES = new Set<StatusValue>(
  Object.keys(STATUS_TABLE) as StatusValue[],
);

function coerceStatus(phase: string | undefined): StatusValue {
  return phase && KNOWN_STATUSES.has(phase as StatusValue)
    ? (phase as StatusValue)
    : "Pending";
}

// Debounce window for SSE-triggered refetches. The SSE bus can burst on
// rapid status churn (Job creation → Pod start → container ready in <250ms);
// debouncing collapses N events to a single fetch.
const REFETCH_DEBOUNCE_MS = 250;

// Synthetic URL passed to useSSEStream when projectName is null. See the
// file-level deferral note above.
const NO_PROJECT_SYNTHETIC_URL = "/dev/null/no-project";

export function projectEventsURL(projectName: string | null): string {
  if (!projectName) return NO_PROJECT_SYNTHETIC_URL;
  return `/api/v1/projects/${encodeURIComponent(projectName)}/events`;
}

// Transform the wire-shape PlanDetail into the React-layer ExecutionPlanData.
// The phase string coerces to StatusValue via KNOWN_STATUSES; the nullable
// activeDispatchWave folds to undefined (ExecutionPlanData uses
// `activeDispatchWave?: number`).
function planDetailToExecutionPlan(p: PlanDetail): ExecutionPlanData {
  return {
    planName: p.name,
    tasks: p.tasks.map((t: PlanTaskCard) => ({
      name: t.name,
      status: coerceStatus(t.phase),
      waveIndex: t.waveIndex,
      attempt: t.attempt,
      dependsOn: t.dependsOn ?? [],
    })),
    activeDispatchWave: p.activeDispatchWave ?? undefined,
  };
}

// Same fold for TaskDetailJSON → TaskDetailData. Only `status` differs by
// typing (JSON carries raw string; React layer wants StatusValue).
function taskDetailJSONToData(t: TaskDetailJSON): TaskDetailData {
  return {
    name: t.name,
    projectName: t.projectName,
    planName: t.planName,
    status: coerceStatus(t.status),
    namespace: t.namespace,
    attempt: t.attempt,
    attemptMax: t.attemptMax,
    podName: t.podName,
    exitCode: t.exitCode,
    waveIndex: t.waveIndex,
    scheduledAt: t.scheduledAt,
    envelopePath: t.envelopePath,
    elapsedText: t.elapsedText,
    conditions: t.conditions ?? [],
  };
}

export function useTasks(
  projectName: string | null,
  namespace: string | null,
  planName: string | null,
): TasksResult {
  const [plan, setPlan] = useState<ExecutionPlanData | null>(null);
  // Per-render mutable ref for the active debounce timer; cleared on
  // cleanup and on planName change.
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  // Track the latest planName inside the SSE onMessage callback. The
  // callback identity is stable across re-renders (held in a ref by
  // useSSEStream); we update this ref so the filter always reflects the
  // current plan name without re-constructing the EventSource.
  const planNameRef = useRef<string | null>(planName);
  planNameRef.current = planName;
  // Track the latest namespace the same way so the stable runFetch closure
  // always forwards the current project's namespace without re-creating the
  // callback (debug #14).
  const namespaceRef = useRef<string | null>(namespace);
  namespaceRef.current = namespace;

  // Triggers a fetch + state update. Wrapped so the SSE callback can
  // call it without depending on planName through the closure (the
  // planNameRef keeps the filter coherent).
  const runFetch = useCallback((name: string) => {
    fetchPlan(name, namespaceRef.current ?? undefined)
      .then((p) => {
        // Stale-response guard: drop the result if planName has changed
        // since the fetch was kicked off.
        if (planNameRef.current !== name) return;
        setPlan(planDetailToExecutionPlan(p));
      })
      .catch(() => {
        // Errors are swallowed at the hook boundary — the dashboard's
        // ErrorState surfaces ERR1/ERR2 via the top-level fetch failure
        // gate on useProjects. Per-plan fetch failures degrade silently
        // to the previous render until the next SSE refresh retries.
      });
  }, []);

  // Initial fetch + reset on planName / projectName / namespace change.
  useEffect(() => {
    // Clear any pending debounce timer from a previous planName.
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
    if (!planName) {
      setPlan(null);
      return;
    }
    runFetch(planName);
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
        debounceRef.current = null;
      }
    };
  }, [projectName, namespace, planName, runFetch]);

  // SSE: refresh-trigger only. The minimal projection lacks dependsOn/
  // waveIndex; we re-fetch the rich shape via fetchPlan on every relevant
  // event (debounced 250ms).
  useSSEStream(projectEventsURL(projectName), {
    onMessage: (e: MessageEvent) => {
      const currentPlan = planNameRef.current;
      if (!currentPlan) return;
      let parsed: { kind?: string; name?: string; planRef?: string } = {};
      try {
        parsed = JSON.parse(String(e.data));
      } catch {
        return;
      }
      const matches =
        (parsed.kind === "Task" && parsed.planRef === currentPlan) ||
        (parsed.kind === "Plan" && parsed.name === currentPlan) ||
        (parsed.kind === "Wave" && parsed.planRef === currentPlan);
      if (!matches) return;
      if (debounceRef.current) clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(() => {
        debounceRef.current = null;
        runFetch(currentPlan);
      }, REFETCH_DEBOUNCE_MS);
    },
  });

  return plan;
}

export function useTaskDetail(
  projectName: string | null,
  namespace: string | null,
  taskName: string | null,
): TaskDetailResult {
  const [task, setTask] = useState<TaskDetailData | null>(null);
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const taskNameRef = useRef<string | null>(taskName);
  taskNameRef.current = taskName;
  // See useTasks: forward the current project's namespace through a ref so
  // the stable runFetch closure resolves tasks in non-default namespaces
  // (debug #14).
  const namespaceRef = useRef<string | null>(namespace);
  namespaceRef.current = namespace;

  const runFetch = useCallback((name: string) => {
    fetchTask(name, namespaceRef.current ?? undefined)
      .then((t) => {
        // Stale-response guard.
        if (taskNameRef.current !== name) return;
        setTask(taskDetailJSONToData(t));
      })
      .catch(() => {
        // Silent — see useTasks rationale.
      });
  }, []);

  useEffect(() => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
    if (!taskName) {
      setTask(null);
      return;
    }
    runFetch(taskName);
    return () => {
      if (debounceRef.current) {
        clearTimeout(debounceRef.current);
        debounceRef.current = null;
      }
    };
  }, [projectName, namespace, taskName, runFetch]);

  useSSEStream(projectEventsURL(projectName), {
    onMessage: (e: MessageEvent) => {
      const currentTask = taskNameRef.current;
      if (!currentTask) return;
      let parsed: { kind?: string; name?: string } = {};
      try {
        parsed = JSON.parse(String(e.data));
      } catch {
        return;
      }
      if (parsed.kind !== "Task" || parsed.name !== currentTask) return;
      if (debounceRef.current) clearTimeout(debounceRef.current);
      debounceRef.current = setTimeout(() => {
        debounceRef.current = null;
        runFetch(currentTask);
      }, REFETCH_DEBOUNCE_MS);
    },
  });

  return task;
}
