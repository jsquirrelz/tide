import { describe, it, expect, afterEach, vi } from "vitest";
import { fetchProject, fetchProjects } from "./api";

afterEach(() => {
  vi.restoreAllMocks();
});

function stubFetchOK<T>(payload: T) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => payload,
    }) as unknown as typeof fetch,
  );
}

function stubFetchHTTPError(status = 500, body: unknown = { error: "boom" }) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: false,
      status,
      json: async () => body,
    }) as unknown as typeof fetch,
  );
}

describe("api — Test 8: typed async fetch helpers", () => {
  it("fetchProjects() calls /api/v1/projects and returns the parsed JSON", async () => {
    const payload = [
      {
        name: "p1",
        namespace: "default",
        phase: "Running",
        activeMilestoneCount: 1,
        budget: { capCents: 10000, currentSpend: 500, withinBudget: true },
      },
    ];
    stubFetchOK(payload);
    const res = await fetchProjects();
    expect(res).toEqual(payload);
    const [url] = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(url).toBe("/api/v1/projects");
  });

  it("fetchProjects(namespace) appends ?namespace=foo", async () => {
    stubFetchOK([]);
    await fetchProjects("team-a");
    const [url] = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(url).toBe("/api/v1/projects?namespace=team-a");
  });

  it("fetchProject(name) calls /api/v1/projects/{name} and returns the parsed JSON", async () => {
    const payload = {
      name: "p1",
      namespace: "default",
      phase: "Running",
      activeMilestoneCount: 1,
      budget: { capCents: 10000, currentSpend: 500, withinBudget: true },
      milestones: [],
      phases: [],
      plans: [],
    };
    stubFetchOK(payload);
    const res = await fetchProject("p1");
    expect(res).toEqual(payload);
    const [url] = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(url).toBe("/api/v1/projects/p1");
  });

  it("fetchProject(name, namespace) appends ?namespace=foo", async () => {
    stubFetchOK({
      name: "p1",
      namespace: "team-a",
      phase: "Running",
      activeMilestoneCount: 0,
      budget: { capCents: 0, currentSpend: 0, withinBudget: true },
      milestones: [],
      phases: [],
      plans: [],
    });
    await fetchProject("p1", "team-a");
    const [url] = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0];
    expect(url).toBe("/api/v1/projects/p1?namespace=team-a");
  });

  it("fetchProjects() throws on non-2xx response", async () => {
    stubFetchHTTPError(500);
    await expect(fetchProjects()).rejects.toThrow();
  });

  it("fetchProject() throws on 404 with the error body", async () => {
    stubFetchHTTPError(404, { error: "project nope not found" });
    await expect(fetchProject("nope")).rejects.toThrow(/nope/);
  });
});
