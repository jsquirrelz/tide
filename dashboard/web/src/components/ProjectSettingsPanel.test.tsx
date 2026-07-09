/*
 * ProjectSettingsPanel.test.tsx (plan 37-08 Task 1)
 *
 *   Covers the four behaviors from the plan:
 *     1. status strip — label "Status" (never a Phase-column header), the
 *        lifecycle chip, the budget line `spent $X.XX of $Y.YY cap`, and
 *        ConditionBadges when conditions exist.
 *     2. D-10 cards in order — outcome prompt (verbatim pre-wrap, NOT
 *        markdown), repository (baseRef "HEAD (default)" when empty),
 *        models per level + the locked effort note, budget, gates, secrets
 *        (each name suffixed with the locked "(name only …)" string).
 *     3. raw-spec disclosure collapsed by default, expands + toggles label.
 *     4. fetch failure → error surface with a retry affordance (never a
 *        silent empty panel).
 *
 *   fetchProjectSettings is mocked so each test drives the settings payload
 *   deterministically without a real backend.
 */
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react";

vi.mock("../lib/api", () => ({
  fetchProjectSettings: (...args: unknown[]) => mockFetch(...args),
}));

const mockFetch = vi.fn();

import ProjectSettingsPanel from "./ProjectSettingsPanel";
import type { ProjectSettings } from "../lib/api";
import type { ProjectBlockingCondition } from "./ConditionBadge";

const SETTINGS: ProjectSettings = {
  outcomePrompt: "Build a thing\n  keep indented lines verbatim",
  repo: { repoURL: "https://github.com/acme/repo", baseRef: "", branchName: "tide/run-1" },
  models: { milestone: "opus", phase: "opus", plan: "sonnet", task: "sonnet" },
  budget: { absoluteCapCents: 10000, rollingWindowCapCents: 5000, costSpentCents: 2500 },
  gates: {
    milestone: "manual",
    phase: "auto",
    plan: "auto",
    task: "auto",
    pauseBetweenWaves: true,
  },
  secrets: [
    { purpose: "anthropic", name: "anthropic-api-key" },
    { purpose: "git", name: "git-token" },
  ],
  rawSpecYAML: "apiVersion: tide.dev/v1\nkind: Project\n",
};

const BUDGET_BLOCKED: ProjectBlockingCondition = {
  type: "BudgetBlocked",
  reason: "BudgetCapReached",
  message: "Cost spent exceeds cap; dispatch halted project-wide",
  age: "4m 12s",
};

afterEach(() => {
  cleanup();
  mockFetch.mockReset();
  vi.restoreAllMocks();
});

describe("ProjectSettingsPanel (Test 1) — status strip", () => {
  it("renders the 'Status' label (D-11), lifecycle chip, budget line, and no Phase-column header", async () => {
    mockFetch.mockResolvedValue(SETTINGS);
    render(
      <ProjectSettingsPanel
        projectName="p1"
        statusPhase="Running"
        budgetSpentCents={101}
        budgetCapCents={10000}
        conditions={[]}
      />,
    );

    const strip = await screen.findByTestId("settings-status-strip");
    // D-11: the strip is labeled "Status", never "Phase".
    expect(within(strip).getByText("Status")).toBeInTheDocument();
    expect(within(strip).queryByText("Phase")).toBeNull();
    // Lifecycle chip renders the phase.
    expect(within(strip).getByText("Running")).toBeInTheDocument();
    // Budget line: cents → dollars, two decimals.
    expect(
      within(strip).getByText("spent $1.01 of $100.00 cap"),
    ).toBeInTheDocument();
  });

  it("renders ConditionBadges when conditions exist", async () => {
    mockFetch.mockResolvedValue(SETTINGS);
    render(
      <ProjectSettingsPanel
        projectName="p1"
        statusPhase="Running"
        budgetSpentCents={0}
        budgetCapCents={10000}
        conditions={[BUDGET_BLOCKED]}
      />,
    );
    await screen.findByTestId("settings-status-strip");
    expect(
      screen.getByTestId("condition-badge-BudgetBlocked"),
    ).toBeInTheDocument();
  });
});

describe("ProjectSettingsPanel (Test 2) — D-10 cards", () => {
  async function renderCards() {
    mockFetch.mockResolvedValue(SETTINGS);
    render(
      <ProjectSettingsPanel
        projectName="p1"
        statusPhase="Running"
        budgetSpentCents={2500}
        budgetCapCents={10000}
        conditions={[]}
      />,
    );
    await screen.findByTestId("settings-card-outcome");
  }

  it("renders the cards in D-10 order", async () => {
    await renderCards();
    const order = [
      "settings-card-outcome",
      "settings-card-repository",
      "settings-card-models",
      "settings-card-budget",
      "settings-card-gates",
      "settings-card-secrets",
    ];
    const positions = order.map((id) => {
      const el = screen.getByTestId(id);
      return el.compareDocumentPosition(screen.getByTestId(order[0]));
    });
    // Every card after the first must be positioned AFTER the first card.
    for (let i = 1; i < order.length; i++) {
      expect(positions[i] & Node.DOCUMENT_POSITION_PRECEDING).toBeTruthy();
    }
  });

  it("renders the outcome prompt verbatim in a pre-wrap block, not markdown", async () => {
    await renderCards();
    const pre = screen.getByTestId("settings-outcome-body");
    expect(pre.tagName).toBe("PRE");
    // Verbatim: exact text including the indented second line.
    expect(pre.textContent).toBe(SETTINGS.outcomePrompt);
  });

  it("renders repository with 'HEAD (default)' when baseRef is empty and the branch when present", async () => {
    await renderCards();
    const card = screen.getByTestId("settings-card-repository");
    expect(within(card).getByText("https://github.com/acme/repo")).toBeInTheDocument();
    expect(within(card).getByText("HEAD (default)")).toBeInTheDocument();
    expect(within(card).getByText("tide/run-1")).toBeInTheDocument();
  });

  it("renders one row per model level and the locked effort note", async () => {
    await renderCards();
    const card = screen.getByTestId("settings-card-models");
    expect(within(card).getAllByText("opus")).toHaveLength(2);
    expect(within(card).getAllByText("sonnet")).toHaveLength(2);
    expect(within(card).getByText("effort: not yet configurable")).toBeInTheDocument();
  });

  it("renders budget cap/spent/remaining", async () => {
    await renderCards();
    const card = screen.getByTestId("settings-card-budget");
    expect(within(card).getByText("$100.00")).toBeInTheDocument(); // cap
    expect(within(card).getByText("$25.00")).toBeInTheDocument(); // spent
    expect(within(card).getByText("$75.00")).toBeInTheDocument(); // remaining
  });

  it("renders gate policies and pauseBetweenWaves", async () => {
    await renderCards();
    const card = screen.getByTestId("settings-card-gates");
    expect(within(card).getByText("manual")).toBeInTheDocument();
    expect(within(card).getAllByText("auto").length).toBeGreaterThanOrEqual(1);
    // pauseBetweenWaves boolean surfaced.
    expect(within(card).getByText(/pause between waves/i)).toBeInTheDocument();
  });

  it("renders each secret NAME suffixed with the locked names-only string", async () => {
    await renderCards();
    const card = screen.getByTestId("settings-card-secrets");
    expect(within(card).getByText("anthropic-api-key")).toBeInTheDocument();
    expect(within(card).getByText("git-token")).toBeInTheDocument();
    expect(
      within(card).getAllByText("(name only — value not shown)"),
    ).toHaveLength(2);
  });
});

describe("ProjectSettingsPanel (Test 3) — raw-spec disclosure", () => {
  it("is collapsed by default and expands / toggles its label", async () => {
    mockFetch.mockResolvedValue(SETTINGS);
    render(
      <ProjectSettingsPanel
        projectName="p1"
        statusPhase="Running"
        budgetSpentCents={0}
        budgetCapCents={10000}
        conditions={[]}
      />,
    );
    const toggle = await screen.findByText("Show raw spec (YAML)");
    // Collapsed: YAML not rendered yet.
    expect(screen.queryByTestId("settings-raw-spec-body")).toBeNull();

    fireEvent.click(toggle);
    const body = screen.getByTestId("settings-raw-spec-body");
    expect(body.textContent).toContain("apiVersion: tide.dev/v1");
    expect(screen.getByText("Hide raw spec")).toBeInTheDocument();
    expect(screen.queryByText("Show raw spec (YAML)")).toBeNull();
  });
});

describe("ProjectSettingsPanel (Test 4) — fetch failure", () => {
  it("renders an error surface with a retry affordance, never a silent empty panel", async () => {
    mockFetch.mockRejectedValueOnce(new Error("boom"));
    render(
      <ProjectSettingsPanel
        projectName="p1"
        statusPhase="Running"
        budgetSpentCents={0}
        budgetCapCents={10000}
        conditions={[]}
      />,
    );
    const err = await screen.findByTestId("settings-state-error");
    const retry = within(err).getByRole("button", { name: /retry/i });
    expect(mockFetch).toHaveBeenCalledTimes(1);

    mockFetch.mockResolvedValueOnce(SETTINGS);
    fireEvent.click(retry);
    await screen.findByTestId("settings-status-strip");
    expect(mockFetch).toHaveBeenCalledTimes(2);
  });
});
