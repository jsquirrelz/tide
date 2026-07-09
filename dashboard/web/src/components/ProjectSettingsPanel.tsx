/*
 * ProjectSettingsPanel.tsx (plan 37-08, DASH-03 / D-10 / D-11).
 *
 *   Rendered inside <NodeDetailPanel> on ProjectNode click. Top-to-bottom
 *   (UI-SPEC §4):
 *
 *     1. Live-status strip from PROPS (the App already holds the project
 *        detail — do not refetch): the Project CR lifecycle state as a mono
 *        chip labeled "Status" (D-11 — never "Phase", which names TIDE Phase
 *        CRs), the budget line `spent $X.XX of $Y.YY cap`, and any blocking
 *        conditions via the existing ConditionBadge idiom.
 *     2. Settings cards fetched via fetchProjectSettings: outcome prompt
 *        (verbatim mono pre-wrap — operator input, NOT markdown), repository,
 *        models (+ the locked effort note), budget, gates, secrets (NAMES
 *        only — the server redacts values).
 *     3. Raw-spec disclosure — collapsed by default, expands to the
 *        server-rendered YAML in the standard pre treatment.
 *
 *   Security (T-37-08-01 / T-37-08-02): secret VALUES never cross the wire
 *   (server redacts in 37-07); the outcome prompt is rendered verbatim in a
 *   pre block, never through a markdown/HTML renderer, so LLM/operator input
 *   cannot become active content.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import { Loader2 } from "lucide-react";

import {
  fetchProjectSettings,
  type ProjectSettings,
} from "../lib/api";
import { clsx } from "../lib/clsx";
import ConditionBadge, {
  type ProjectBlockingCondition,
} from "./ConditionBadge";
import { KNOWN_STATUS_VALUES, type StatusValue } from "./StatusBadge";
import StatusBadge from "./StatusBadge";

export type ProjectSettingsPanelProps = {
  projectName: string;
  namespace?: string;
  /** The Project CR lifecycle string — labeled "Status" (D-11). From the
   *  already-fetched project detail; NOT refetched here. */
  statusPhase: string;
  /** Budget spent, in cents — from the already-fetched project detail. */
  budgetSpentCents: number;
  /** Budget cap, in cents — from the already-fetched project detail. */
  budgetCapCents: number;
  /** True blocking conditions on the Project CR (BudgetBlocked, BillingHalt). */
  conditions: ProjectBlockingCondition[];
};

/** cents → `$X.XX` (two decimals). */
function dollars(cents: number): string {
  return "$" + (cents / 100).toFixed(2);
}

function isKnownStatus(s: string): s is StatusValue {
  return (KNOWN_STATUS_VALUES as readonly string[]).includes(s);
}

/** MetaRow `dt` idiom — 11px uppercase muted card header (UI-SPEC §4). */
function CardHeader({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="text-[var(--color-text-muted)]"
      style={{
        fontSize: "11px",
        textTransform: "uppercase",
        letterSpacing: "0.04em",
        fontWeight: 600,
      }}
    >
      {children}
    </div>
  );
}

/** A settings card: raised bg, subtle border, 4px radius, p-4. */
function Card({
  testId,
  title,
  children,
}: {
  testId: string;
  title: string;
  children: React.ReactNode;
}) {
  return (
    <section
      data-testid={testId}
      className="flex flex-col gap-2 rounded border border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)] p-4"
    >
      <CardHeader>{title}</CardHeader>
      {children}
    </section>
  );
}

/** A mono `label: value` row inside a card. */
function ValueRow({
  label,
  value,
  title,
}: {
  label: string;
  value: string;
  title?: string;
}) {
  return (
    <div className="flex items-baseline justify-between gap-3">
      <span
        className="shrink-0 text-[var(--color-text-muted)]"
        style={{ fontSize: "12px" }}
      >
        {label}
      </span>
      <span
        className="min-w-0 truncate text-right text-[var(--color-text-primary)]"
        title={title ?? value}
        style={{ fontFamily: "var(--font-mono)", fontSize: "13px" }}
      >
        {value}
      </span>
    </div>
  );
}

const MODEL_LEVELS: { key: keyof ProjectSettings["models"]; label: string }[] = [
  { key: "milestone", label: "Milestone" },
  { key: "phase", label: "Phase" },
  { key: "plan", label: "Plan" },
  { key: "task", label: "Task" },
];

const GATE_LEVELS: { key: keyof ProjectSettings["gates"]; label: string }[] = [
  { key: "milestone", label: "Milestone" },
  { key: "phase", label: "Phase" },
  { key: "plan", label: "Plan" },
  { key: "task", label: "Task" },
];

export default function ProjectSettingsPanel({
  projectName,
  namespace,
  statusPhase,
  budgetSpentCents,
  budgetCapCents,
  conditions,
}: ProjectSettingsPanelProps) {
  const [settings, setSettings] = useState<ProjectSettings | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [rawOpen, setRawOpen] = useState(false);
  const mountedRef = useRef(true);

  const load = useCallback(async () => {
    setError(null);
    setSettings(null);
    try {
      const result = await fetchProjectSettings(projectName, namespace);
      if (!mountedRef.current) return;
      setSettings(result);
    } catch (err) {
      if (!mountedRef.current) return;
      setError(err instanceof Error ? err.message : String(err));
    }
  }, [projectName, namespace]);

  useEffect(() => {
    mountedRef.current = true;
    void load();
    return () => {
      mountedRef.current = false;
    };
  }, [load]);

  // Status strip renders from PROPS regardless of the fetch state, so an
  // operator always sees the live lifecycle even while the cards load.
  const strip = (
    <div
      data-testid="settings-status-strip"
      className="flex flex-wrap items-center gap-3 border-b border-[var(--color-border-subtle)] px-6 py-3"
    >
      <span
        className="text-[var(--color-text-muted)]"
        style={{
          fontSize: "11px",
          textTransform: "uppercase",
          letterSpacing: "0.04em",
          fontWeight: 600,
        }}
      >
        Status
      </span>
      {isKnownStatus(statusPhase) ? (
        <StatusBadge status={statusPhase} />
      ) : (
        <span
          className="rounded border border-[var(--color-border-subtle)] px-2 py-0.5 text-[var(--color-text-primary)]"
          style={{ fontFamily: "var(--font-mono)", fontSize: "12px", fontWeight: 600 }}
        >
          {statusPhase}
        </span>
      )}
      <span
        className="text-[var(--color-text-muted)]"
        style={{ fontFamily: "var(--font-mono)", fontSize: "12px" }}
      >
        {`spent ${dollars(budgetSpentCents)} of ${dollars(budgetCapCents)} cap`}
      </span>
      {conditions.map((c) => (
        <ConditionBadge key={c.type} condition={c} />
      ))}
    </div>
  );

  let cards: React.ReactNode;
  if (error !== null) {
    cards = (
      <div
        data-testid="settings-state-error"
        className="flex flex-col items-center justify-center px-6 py-16 text-center"
      >
        <h2 style={{ fontSize: "18px", fontWeight: 600, color: "var(--color-text-primary)" }}>
          Couldn't load project settings
        </h2>
        <p
          style={{
            marginTop: "16px",
            fontSize: "14px",
            color: "var(--color-text-muted)",
            maxWidth: "440px",
          }}
        >
          The dashboard could not read the project spec. Check the dashboard
          logs: kubectl logs deploy/tide-dashboard
        </p>
        <button
          type="button"
          onClick={() => void load()}
          className="rounded border px-3 py-2"
          style={{
            marginTop: "24px",
            fontFamily: "var(--font-mono)",
            fontSize: "13px",
            fontWeight: 600,
            borderColor: "var(--color-border-subtle)",
            color: "var(--color-text-primary)",
            background: "transparent",
          }}
        >
          Retry
        </button>
      </div>
    );
  } else if (settings === null) {
    cards = (
      <div
        data-testid="settings-state-loading"
        className="flex items-center justify-center py-16"
      >
        <Loader2
          size={24}
          aria-label="Loading"
          className="animate-spin"
          style={{ color: "var(--color-text-muted)" }}
        />
      </div>
    );
  } else {
    const remainingCents = settings.budget.absoluteCapCents - settings.budget.costSpentCents;
    cards = (
      <div className="flex flex-col gap-3 p-4">
        {/* Outcome prompt — verbatim mono pre-wrap; NEVER markdown (T-37-08-02). */}
        <Card testId="settings-card-outcome" title="Outcome prompt">
          <pre
            data-testid="settings-outcome-body"
            className="rounded"
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: "13px",
              lineHeight: 1.5,
              whiteSpace: "pre-wrap",
              wordBreak: "break-word",
              background: "var(--color-surface-overlay)",
              padding: "12px",
              margin: 0,
              color: "var(--color-text-primary)",
            }}
          >
            {settings.outcomePrompt}
          </pre>
        </Card>

        {/* Repository. */}
        <Card testId="settings-card-repository" title="Repository">
          <ValueRow label="Repo URL" value={settings.repo.repoURL} />
          <ValueRow
            label="Base ref"
            value={settings.repo.baseRef || "HEAD (default)"}
          />
          {settings.repo.branchName && (
            <ValueRow label="Run branch" value={settings.repo.branchName} />
          )}
        </Card>

        {/* Models — one row per level; effort is not configurable in v1.0.7. */}
        <Card testId="settings-card-models" title="Models">
          {MODEL_LEVELS.map((lvl) => (
            <ValueRow
              key={lvl.key}
              label={lvl.label}
              value={settings.models[lvl.key]}
            />
          ))}
          <div
            className="text-[var(--color-text-muted)]"
            style={{ fontSize: "12px", fontStyle: "italic", marginTop: "4px" }}
          >
            effort: not yet configurable
          </div>
        </Card>

        {/* Budget — cap / spent / remaining. */}
        <Card testId="settings-card-budget" title="Budget">
          <ValueRow label="Cap" value={dollars(settings.budget.absoluteCapCents)} />
          <ValueRow label="Spent" value={dollars(settings.budget.costSpentCents)} />
          <ValueRow label="Remaining" value={dollars(remainingCents)} />
        </Card>

        {/* Gates — approval policy per level + pauseBetweenWaves. */}
        <Card testId="settings-card-gates" title="Gates">
          {GATE_LEVELS.map((lvl) => (
            <ValueRow
              key={lvl.key}
              label={lvl.label}
              value={String(settings.gates[lvl.key])}
            />
          ))}
          <ValueRow
            label="Pause between waves"
            value={settings.gates.pauseBetweenWaves ? "yes" : "no"}
          />
        </Card>

        {/* Secrets — NAMES only; the server never sends values (T-37-08-01). */}
        <Card testId="settings-card-secrets" title="Secrets">
          {settings.secrets.length === 0 ? (
            <span
              className="text-[var(--color-text-muted)]"
              style={{ fontSize: "13px" }}
            >
              No secret references.
            </span>
          ) : (
            settings.secrets.map((s) => (
              <div key={s.name} className="flex flex-col gap-0.5">
                <span
                  className="text-[var(--color-text-muted)]"
                  style={{ fontSize: "12px" }}
                >
                  {s.purpose}
                </span>
                <span
                  className="text-[var(--color-text-primary)]"
                  style={{ fontFamily: "var(--font-mono)", fontSize: "13px" }}
                >
                  {s.name}{" "}
                  <span
                    className="text-[var(--color-text-muted)]"
                    style={{ fontFamily: "var(--font-sans)", fontStyle: "italic" }}
                  >
                    (name only — value not shown)
                  </span>
                </span>
              </div>
            ))
          )}
        </Card>

        {/* Raw spec — collapsed by default. */}
        <section
          data-testid="settings-card-raw-spec"
          className="flex flex-col gap-2 rounded border border-[var(--color-border-subtle)] bg-[var(--color-surface-raised)] p-4"
        >
          <button
            type="button"
            onClick={() => setRawOpen((o) => !o)}
            aria-expanded={rawOpen}
            className={clsx(
              "flex items-center gap-2 text-left text-[var(--color-text-primary)]",
            )}
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: "13px",
              fontWeight: 600,
              background: "none",
              border: "none",
              padding: 0,
              cursor: "pointer",
            }}
          >
            {rawOpen ? "Hide raw spec" : "Show raw spec (YAML)"}
          </button>
          {rawOpen && (
            <pre
              data-testid="settings-raw-spec-body"
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: "13px",
                lineHeight: 1.5,
                whiteSpace: "pre",
                overflowX: "auto",
                background: "var(--color-surface-overlay)",
                padding: "12px",
                borderRadius: "4px",
                margin: 0,
                color: "var(--color-text-primary)",
              }}
            >
              {settings.rawSpecYAML}
            </pre>
          )}
        </section>
      </div>
    );
  }

  return (
    <div data-testid="project-settings-panel" className="flex flex-1 flex-col">
      {strip}
      {cards}
    </div>
  );
}
