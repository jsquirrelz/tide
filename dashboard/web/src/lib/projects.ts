/*
 * projects.ts (plan 04-17)
 *
 *   `useProjects(namespace?)` — wraps the existing `fetchProjects()` helper
 *   in lib/api.ts with React state + a `refetch` callback. The project
 *   list is stable during a session; we deliberately do NOT compose
 *   useSSEStream against it. Operators add/remove projects via
 *   `tide apply` on a longer cadence than the dashboard's per-second
 *   render budget — a manual refetch button suffices.
 *
 *   Re-renders on:
 *     - mount (initial fetch)
 *     - `namespace` change (re-fetch with the new filter)
 *     - `refetch()` call (bumps an internal tick that re-runs the effect)
 *
 *   Plan 04-15's ProjectPicker mounts in the header and consumes the
 *   `projects` field of this hook's result, mapped to its ProjectEntry
 *   shape (subset of ProjectSummary) in App.tsx.
 */
import { useCallback, useEffect, useState } from "react";

import { fetchProjects, type ProjectSummary } from "./api";

export type ProjectsResult = {
  projects: ProjectSummary[];
  loading: boolean;
  error: Error | null;
  refetch: () => void;
};

export function useProjects(namespace?: string): ProjectsResult {
  const [projects, setProjects] = useState<ProjectSummary[]>([]);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<Error | null>(null);
  // Tick bumps on refetch() to retrigger the effect without depending on
  // the consumer providing a stable namespace value.
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(null);
    fetchProjects(namespace)
      .then((data) => {
        if (cancelled) return;
        setProjects(data);
        setLoading(false);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof Error ? err : new Error(String(err)));
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [namespace, tick]);

  const refetch = useCallback(() => {
    setTick((t) => t + 1);
  }, []);

  return { projects, loading, error, refetch };
}
