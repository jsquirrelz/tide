/*
 * projects.test.ts (plan 04-17)
 *
 *   Covers the useProjects() hook's contract:
 *     1. happy path — fetchProjects resolves; result.current.projects
 *        populated; loading flips false.
 *     2. empty cluster — fetchProjects resolves []; loading false, no
 *        error.
 *     3. fetch error — fetchProjects rejects (HTTP 500); error captured.
 *     4. refetch — calling result.current.refetch() re-triggers the
 *        fetch.
 *
 *   The hook re-uses the existing fetchProjects helper in lib/api.ts. We
 *   stub the global fetch instead of the helper so we exercise the full
 *   URL-construction path; consistent with api.test.ts's pattern.
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor, act } from "@testing-library/react";

import { useProjects } from "./projects";

afterEach(() => {
  vi.restoreAllMocks();
});

function stubFetchOK<T>(payload: T) {
  const fn = vi.fn().mockResolvedValue({
    ok: true,
    status: 200,
    json: async () => payload,
  });
  vi.stubGlobal("fetch", fn as unknown as typeof fetch);
  return fn;
}

function stubFetchHTTPError(status = 500) {
  const fn = vi.fn().mockResolvedValue({
    ok: false,
    status,
    json: async () => ({ error: "boom" }),
  });
  vi.stubGlobal("fetch", fn as unknown as typeof fetch);
  return fn;
}

describe("useProjects (plan 04-17)", () => {
  it("happy path: populates projects + loading flips false", async () => {
    stubFetchOK([
      {
        name: "p1",
        namespace: "default",
        phase: "Running",
        activeMilestoneCount: 1,
        budget: { capCents: 0, currentSpend: 0, withinBudget: true },
      },
    ]);
    const { result } = renderHook(() => useProjects());
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    expect(result.current.projects.length).toBe(1);
    expect(result.current.projects[0].name).toBe("p1");
    expect(result.current.error).toBeNull();
  });

  it("empty cluster: projects=[] loading=false (NOT an error)", async () => {
    stubFetchOK([]);
    const { result } = renderHook(() => useProjects());
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    expect(result.current.projects).toEqual([]);
    expect(result.current.error).toBeNull();
  });

  it("fetch error: error is populated, loading flips false", async () => {
    stubFetchHTTPError(500);
    const { result } = renderHook(() => useProjects());
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    expect(result.current.error).toBeInstanceOf(Error);
    expect(result.current.error?.message).toMatch(/fetchProjects failed/);
  });

  it("refetch triggers a second fetch call", async () => {
    const fn = stubFetchOK([
      {
        name: "p1",
        namespace: "default",
        phase: "Running",
        activeMilestoneCount: 0,
        budget: { capCents: 0, currentSpend: 0, withinBudget: true },
      },
    ]);
    const { result } = renderHook(() => useProjects());
    await waitFor(() => {
      expect(result.current.loading).toBe(false);
    });
    expect(fn).toHaveBeenCalledTimes(1);

    act(() => {
      result.current.refetch();
    });

    await waitFor(() => {
      expect(fn).toHaveBeenCalledTimes(2);
    });
    expect(result.current.loading).toBe(false);
  });
});
