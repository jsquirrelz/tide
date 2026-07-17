/*
 * node-panel-integration.test.tsx (plan 37-08 Task 3; extended plan 46-05)
 *
 *   App-level composition smoke: verifies the exact NodeDetailPanel + content
 *   assembly App.tsx mounts renders correctly with real content — the D9
 *   concern 37-04 flagged for this integration ("needs a human/visual pass
 *   when the panel is mounted with real content"). Deterministic (no
 *   ReactFlow layout), so it complements — not replaces — the 37-10 checkpoint
 *   visual pass.
 *
 *     1. project  → NodeDetailPanel hosts ProjectSettingsPanel (dialog chrome
 *                   + status strip render together).
 *     2. milestone (gate-parked) → NodeDetailPanel hosts ArtifactViewer with
 *                   the ApproveStrip pinned below it (D-08).
 *
 *   Plan 46-05 (OBS-04) adds the <PhoenixTraceLink> mount-1 render/hide
 *   cases — App.tsx renders it as the first child inside NodeDetailPanel,
 *   above ProjectSettingsPanel/ArtifactViewer (UI-SPEC §Mount points).
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";

vi.mock("../../lib/api", () => ({
  fetchProjectSettings: (...args: unknown[]) => mockSettings(...args),
  fetchNodeArtifacts: (...args: unknown[]) => mockArtifacts(...args),
}));

const mockSettings = vi.fn();
const mockArtifacts = vi.fn();

import NodeDetailPanel from "../NodeDetailPanel";
import ProjectSettingsPanel from "../ProjectSettingsPanel";
import ArtifactViewer from "../ArtifactViewer";
import ApproveStrip from "../ApproveStrip";
import PhoenixTraceLink from "../PhoenixTraceLink";
import { ToastProvider } from "../ToastContainer";
import type { ProjectSettings } from "../../lib/api";
import { phoenixSpanURL } from "../../lib/phoenixLink";

const SETTINGS: ProjectSettings = {
  outcomePrompt: "Ship the thing",
  repo: { repoURL: "https://github.com/acme/repo", baseRef: "", branchName: "" },
  models: { milestone: "opus", phase: "opus", plan: "sonnet", task: "sonnet" },
  budget: { absoluteCapCents: 10000, rollingWindowCapCents: 5000, costSpentCents: 0 },
  gates: { milestone: "manual", phase: "auto", plan: "auto", task: "auto", pauseBetweenWaves: false },
  secrets: [],
  rawSpecYAML: "kind: Project\n",
};

afterEach(() => {
  cleanup();
  mockSettings.mockReset();
  mockArtifacts.mockReset();
  vi.restoreAllMocks();
});

describe("NodeDetailPanel composition (37-08 App assembly)", () => {
  it("project → hosts ProjectSettingsPanel inside the dialog chrome", async () => {
    mockSettings.mockResolvedValue(SETTINGS);
    render(
      <ToastProvider>
        <NodeDetailPanel open kind="project" name="my-project" onClose={() => undefined}>
          <ProjectSettingsPanel
            projectName="my-project"
            statusPhase="Running"
            budgetSpentCents={0}
            budgetCapCents={10000}
            conditions={[]}
          />
        </NodeDetailPanel>
      </ToastProvider>,
    );

    const panel = await screen.findByTestId("node-detail-panel");
    // Dialog chrome + settings content are mounted together.
    expect(panel.getAttribute("role")).toBe("dialog");
    expect(within(panel).getByText("project/my-project")).toBeInTheDocument();
    expect(await within(panel).findByTestId("settings-status-strip")).toBeInTheDocument();
    expect(within(panel).getByTestId("settings-card-outcome")).toBeInTheDocument();
  });

  it("gate-parked milestone → ArtifactViewer with ApproveStrip pinned below (D-08)", async () => {
    mockArtifacts.mockResolvedValue({
      state: "available",
      branch: "tide/run",
      commitSHA: "abc",
      files: [{ name: "PHASE.md", path: "PHASE.md", content: "# Phase brief", sizeBytes: 13 }],
    });

    render(
      <ToastProvider>
        <NodeDetailPanel open kind="milestone" name="ship-v1" onClose={() => undefined}>
          <div className="min-h-0 flex-1">
            <ArtifactViewer kind="milestone" name="ship-v1" project="my-project" gateParked />
          </div>
          <ApproveStrip projectName="my-project" />
        </NodeDetailPanel>
      </ToastProvider>,
    );

    const panel = await screen.findByTestId("node-detail-panel");
    // Artifact content renders...
    expect(await within(panel).findByTestId("artifact-state-available")).toBeInTheDocument();
    expect(within(panel).getByRole("heading", { name: "Phase brief" })).toBeInTheDocument();
    // ...with the copy-only ApproveStrip pinned below it (D-08).
    const strip = within(panel).getByTestId("approve-strip");
    expect(within(strip).getByText("Approve")).toBeInTheDocument();
    expect(within(strip).getByText("Reject")).toBeInTheDocument();
  });

  it("project node: PhoenixTraceLink renders with correct href/target/rel when baseURL+spanId present (OBS-04)", async () => {
    mockSettings.mockResolvedValue(SETTINGS);
    render(
      <ToastProvider>
        <NodeDetailPanel open kind="project" name="my-project" onClose={() => undefined}>
          <PhoenixTraceLink
            baseURL="http://phoenix:6006"
            traceId="trace123"
            spanId="span456"
            edge="bottom"
          />
          <ProjectSettingsPanel
            projectName="my-project"
            statusPhase="Running"
            budgetSpentCents={0}
            budgetCapCents={10000}
            conditions={[]}
          />
        </NodeDetailPanel>
      </ToastProvider>,
    );

    const panel = await screen.findByTestId("node-detail-panel");
    const link = within(panel).getByTestId("phoenix-trace-link");
    expect(link).toHaveAttribute(
      "href",
      phoenixSpanURL("http://phoenix:6006", "span456"),
    );
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noopener noreferrer");
    expect(link).toHaveTextContent("View trace in Phoenix");
  });

  it("project node: PhoenixTraceLink renders nothing when baseURL is empty (OBS-04)", async () => {
    mockSettings.mockResolvedValue(SETTINGS);
    render(
      <ToastProvider>
        <NodeDetailPanel open kind="project" name="my-project" onClose={() => undefined}>
          <PhoenixTraceLink baseURL="" traceId="trace123" spanId="span456" edge="bottom" />
          <ProjectSettingsPanel
            projectName="my-project"
            statusPhase="Running"
            budgetSpentCents={0}
            budgetCapCents={10000}
            conditions={[]}
          />
        </NodeDetailPanel>
      </ToastProvider>,
    );

    const panel = await screen.findByTestId("node-detail-panel");
    expect(within(panel).queryByTestId("phoenix-trace-link-row")).toBeNull();
  });

  it("project node: PhoenixTraceLink renders nothing for an all-zero spanId (46-REVIEW WR-01)", async () => {
    // Pre-upgrade CRs from tracing-dark runs persist "0000000000000000" in
    // {Level}TraceSpanID — "no span", never a linkable Phoenix target.
    mockSettings.mockResolvedValue(SETTINGS);
    render(
      <ToastProvider>
        <NodeDetailPanel open kind="project" name="my-project" onClose={() => undefined}>
          <PhoenixTraceLink
            baseURL="http://phoenix:6006"
            traceId="trace123"
            spanId="0000000000000000"
            edge="bottom"
          />
          <ProjectSettingsPanel
            projectName="my-project"
            statusPhase="Running"
            budgetSpentCents={0}
            budgetCapCents={10000}
            conditions={[]}
          />
        </NodeDetailPanel>
      </ToastProvider>,
    );

    const panel = await screen.findByTestId("node-detail-panel");
    expect(within(panel).queryByTestId("phoenix-trace-link-row")).toBeNull();
  });

  it("project node: PhoenixTraceLink renders nothing when spanId is absent (OBS-04)", async () => {
    mockSettings.mockResolvedValue(SETTINGS);
    render(
      <ToastProvider>
        <NodeDetailPanel open kind="project" name="my-project" onClose={() => undefined}>
          <PhoenixTraceLink
            baseURL="http://phoenix:6006"
            traceId="trace123"
            edge="bottom"
          />
          <ProjectSettingsPanel
            projectName="my-project"
            statusPhase="Running"
            budgetSpentCents={0}
            budgetCapCents={10000}
            conditions={[]}
          />
        </NodeDetailPanel>
      </ToastProvider>,
    );

    const panel = await screen.findByTestId("node-detail-panel");
    expect(within(panel).queryByTestId("phoenix-trace-link-row")).toBeNull();
  });
});
