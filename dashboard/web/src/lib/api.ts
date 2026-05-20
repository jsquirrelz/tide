/**
 * Typed REST client for the dashboard backend (cmd/dashboard, plan 04-10).
 *
 * All endpoints are GET-only (DASH-05 zero-mutation contract). Both helpers
 * throw on non-2xx HTTP status — callers should surface the error via
 * <ErrorBoundary> or the toast emitter; the full-screen ERR1/ERR2 takeover
 * surfaces land in plan 04-16.
 *
 * Wire shape matches cmd/dashboard/api/projects.go (D-D2): plain JSON, no
 * envelope. The TypeScript types here mirror the Go struct fields verbatim
 * so any backend rename surfaces as a compile-time error in this file.
 */

export type BudgetSummary = {
  capCents: number;
  currentSpend: number;
  withinBudget: boolean;
};

/** Mirrors cmd/dashboard/api/projects.go::projectSummary. */
export type ProjectSummary = {
  name: string;
  namespace: string;
  phase: string;
  activeMilestoneCount: number;
  budget: BudgetSummary;
};

/** Mirrors cmd/dashboard/api/projects.go::childRef. */
export type ChildRef = {
  name: string;
  namespace: string;
  phase: string;
  parent: string;
};

/** Mirrors cmd/dashboard/api/projects.go::projectDetail. */
export type ProjectDetail = ProjectSummary & {
  milestones: ChildRef[];
  phases: ChildRef[];
  plans: ChildRef[];
};

type APIErrorBody = { error?: string };

async function readError(res: Response): Promise<string> {
  try {
    const body = (await res.json()) as APIErrorBody;
    if (body.error) return body.error;
  } catch {
    // body wasn't JSON; fall through to status-text only.
  }
  return `HTTP ${res.status}`;
}

function withNamespace(url: string, namespace?: string): string {
  if (!namespace) return url;
  const sep = url.includes("?") ? "&" : "?";
  return `${url}${sep}namespace=${encodeURIComponent(namespace)}`;
}

/**
 * GET /api/v1/projects[?namespace=foo]
 *
 * Returns an empty array (not null / 404) when no projects exist —
 * matches the backend's UI-SPEC §13 E1 empty-state contract.
 */
export async function fetchProjects(
  namespace?: string,
): Promise<ProjectSummary[]> {
  const url = withNamespace("/api/v1/projects", namespace);
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`fetchProjects failed: ${await readError(res)}`);
  }
  return (await res.json()) as ProjectSummary[];
}

/**
 * GET /api/v1/projects/{name}[?namespace=foo]
 *
 * Returns the project + embedded planning-DAG children (Milestones, Phases,
 * Plans). Throws on 404 / 500.
 */
export async function fetchProject(
  name: string,
  namespace?: string,
): Promise<ProjectDetail> {
  const url = withNamespace(
    `/api/v1/projects/${encodeURIComponent(name)}`,
    namespace,
  );
  const res = await fetch(url);
  if (!res.ok) {
    throw new Error(`fetchProject(${name}) failed: ${await readError(res)}`);
  }
  return (await res.json()) as ProjectDetail;
}
