# Phase 14: Budget Enforcement + Pricing - Context

**Gathered:** 2026-06-11
**Status:** Ready for planning

<domain>
## Phase Boundary

The pricing table resolves current model IDs with CORRECT prices (the existing opus-4-7 entry is provably wrong), budget-cap exhaustion is loudly visible on the Project and dashboard, and in-flight overshoot past the cap is bounded by dispatch-time reservation. Requirements: BUDGET-01, BUDGET-02, BUDGET-03. Every fix carries a regression test reproducing the run-1 symptom (cap hit at $100, ~$40 overshoot, silent Project).

</domain>

<decisions>
## Implementation Decisions

### Pricing table (BUDGET-01)
- **D-01: Correct AND extend the compiled table from verified ground truth (cached 2026-06-04, per-MTok input/output):** `claude-fable-5` $10/$50, `claude-opus-4-8` $5/$25, `claude-opus-4-7` $5/$25 (**fixing the existing $15/$75 entry — that was Opus 4.1-era pricing; the comment's "Anthropic pricing page 2026-06" citation was wrong**), `claude-opus-4-6` $5/$25, `claude-sonnet-4-6` $3/$15 (already correct), `claude-haiku-4-5` $1/$5 (already correct). Include cache rates in the model price struct: cache-read = 0.1× input rate; cache-write = 1.25× input (5-minute TTL; 2× for 1h — TIDE sessions use the default 5m). Keep the conservative-default fallback for unknown IDs (run 1 proved it sane).
- **D-02: Helm-values pricing overrides.** A `pricing.overrides` chart value (map of model-ID → {inputCentsPerMTok, outputCentsPerMTok, …}) merged OVER the compiled table at controller startup — operators correct price drift without waiting for a TIDE release. Additive chart change; document in values.yaml.
- **D-03: Pricing-drift process.** (a) A scheduled (weekly) GitHub Action fetches the published pricing docs page (platform.claude.com/docs/en/pricing.md), diffs against the compiled table, and opens/updates a deduped, labeled GitHub issue on drift — no auto-PR; a human reviews billing-math changes. (b) A release-checklist line: verify the pricing table against the live page before tagging. The fetch-diff script lives in hack/ and is runnable locally.

### Cap surfacing (BUDGET-02)
- **D-04: `BudgetBlocked` condition on Project.Status.Conditions** — mirroring phase 13's BillingHalt shape (kubectl + dashboard visible, no new chip states) — set the moment the cap first blocks any dispatch. The existing `Project.Status.Phase=BudgetExceeded` machinery STAYS for whatever path legitimately sets it; research must root-cause why run 1 saw neither (the cap halted task dispatch with per-Task conditions only). Regression test reproduces the run-1 silence: cap trips → Project carries BudgetBlocked.

### Overshoot bounding (BUDGET-03)
- **D-05: Reservation at dispatch.** Pre-charge each session's ESTIMATED cost against the budget at Job creation; dispatch blocks when spent + reserved ≥ cap; the reservation settles to actual cost on completion (and releases on terminal failure). Overshoot becomes bounded by estimate error rather than wave width (run 1: ~$40 from a wide wave of already-dispatched sessions). The cap is genuinely hard at dispatch time. Estimate sources, settle/expiry semantics, and in-memory-vs-status accounting are research/planner territory — but the resumption constraint stands: reservations must be rederivable (in-process like the indegree map; never persisted aggregates in CRD status per PERSIST-02/no-aggregates guard).

### Claude's Discretion
- Root-cause of run-1's silent BudgetExceeded path (observe first: the budget store, rollup, and phase-flip sites).
- Per-level reservation estimate source (per-level historical average? configured per-level estimate in helm? caps-derived ceiling?) — pick the simplest defensible one and document it.
- Reservation bookkeeping placement (budget.Store is in-process — keep it there, rederive on restart from in-flight Jobs).
- Whether `tide resume` interacts with BudgetBlocked (cap raises via Spec edit / bypass annotation already exist — don't invent a second unlock path without reason).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Pricing
- `internal/subagent/anthropic/pricing.go` — the table to correct/extend (provider-firewalled: Anthropic-specific pricing stays behind the subagent boundary; the helm-override plumbing must respect this — generic map flows in, provider package consumes).
- Verified price ground truth in D-01 (source: claude-api skill model table cached 2026-06-04; drift-check script re-verifies against the live pricing docs page).

### Budget machinery
- `internal/budget/` — Store, RollUpUsage, Limits (reservation lands here).
- `internal/controller/task_controller.go` :348-355 (existing BudgetBlocked per-Task condition + BudgetExceeded check), `internal/controller/project_controller.go` :267-272, :1175-1180 (BudgetExceeded phase + bypass machinery) — the silent-path root-cause starts here.
- Phase 13's BillingHalt condition shape (`internal/controller/billing_halt.go`) — the D-04 condition mirrors it; keep the two halts composable (they're different conditions: BillingHalt = provider can't bill; BudgetBlocked = operator's cap).

### Constraints
- CLAUDE.md/PROJECT.md: CRD `.status` carries no derived aggregates (`make verify-no-aggregates`); resumption state = indegree map + completed-task set shape — reservations follow the same in-process-rederivable pattern.
- Memory file `project_dogfood_run1_findings.md` — run-1 numbers for regression assertions ($148.55 metered vs $140.64 real = +5.6%; ~$40 overshoot; cap $100).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `budget.Store` (in-process accumulator) + `RollUpUsage` — extend with reserve/settle.
- Phase 13's `setBillingHalt`/`checkBillingHalt` + dispatch-entry hold pattern — fourth hold instance if needed, or reuse the cap check already at the five dispatch sites.
- Helm → controller config plumbing (flags/env → main.go) — the pricing.overrides path mirrors existing tunables.

### Established Patterns
- Provider firewall: pricing specifics stay in internal/subagent/anthropic/; overrides flow through a provider-agnostic config shape.
- Conditions via Reason* constants + MergeFrom status patches; no new Status.Phase enum values.
- make verify-no-aggregates greps api types — reservation state must NOT land in CRD status.

### Integration Points
- Five dispatch sites already gate on budget (task_controller :348) — reservation check joins/extends that gate.
- Dashboard reads Project conditions (BudgetBlocked visible without dashboard changes; chip mapping is Phase 15).
- .github/workflows/ for the scheduled drift-check action.

</code_context>

<specifics>
## Specific Ideas

- Run-1 regression scenario for BUDGET-02/03: budget cap $100, wide wave dispatching; assert (a) Project carries BudgetBlocked condition when the cap first blocks, (b) with reservation accounting, total committed (settled + reserved) never exceeds cap + one estimate error, vs run 1's ~$40 overshoot.
- The drift-check issue should quote the diff (model, table value, live value) so the human fix is mechanical.

</specifics>

<deferred>
## Deferred Ideas

- Provider-key/org credit balance on dashboard (COST-02, Future Requirements).
- Cache-aware cost optimization strategy (COST-01 prompt-caching design).
- Per-namespace/per-level budget sub-caps — new capability, not in v1.0.1.

</deferred>

---

*Phase: 14-Budget Enforcement + Pricing*
*Context gathered: 2026-06-11*
