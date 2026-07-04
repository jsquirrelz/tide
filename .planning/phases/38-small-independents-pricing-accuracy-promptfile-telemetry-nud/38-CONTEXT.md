# Phase 38: Small Independents — Pricing Accuracy, promptFile, Telemetry Nudge, Tech-Debt Carry - Context

**Gathered:** 2026-07-03
**Status:** Ready for planning

<domain>
## Phase Boundary

Four order-free paper-cut clusters from the first external-repo run (2026-07-03), fully independent of Phases 34–37:

1. **Pricing accuracy (COST-01..03):** the budget tally matches the Anthropic console on Claude 5 family models — missing `claude-sonnet-5` row, `-YYYYMMDD` normalizer, empirically-verified cache-write multiplier, observable unknown-model fallback, and chart-configurable pricing overrides.
2. **promptFile (PROMPT-01):** `tide apply --prompt-file <path>` inlines a file into `spec.outcomePrompt` — CLI-side only, no CRD change.
3. **Telemetry nudge triple (TELEM-01..03):** INSTALL.md enable-telemetry step, net-new chart NOTES.txt warning, dashboard "telemetry disabled" banner.
4. **v1.0.6 tech-debt carry (DEBT-01..03):** W1 RetryOnConflict hardening, W2 configmap `default 4` fix, heavy-envtest tier split.

Not in this phase: run integrity (34), baseRef (35), signing (36), dashboard artifact/project/log-drawer views (37), verify-tier LLM subagents, `subagent.levels` rename, CACHE-F1.

</domain>

<decisions>
## Implementation Decisions

### Pricing rows + fallback observability (COST-01, COST-02)
- **D-01 Normalizer:** exact-ID lookup first; on miss, strip a trailing `-YYYYMMDD` date suffix and retry once. Anything still unmatched falls to the conservative tier. No family prefix-matching — a genuinely unknown model must never be silently priced at an old cheaper rate.
- **D-02 Fallback surface (COST-02):** both a durable condition AND a metric. The envelope carries an unknown-model-fallback flag → reporter rolls it up → a Project `.status` condition (e.g. `PricingFallbackActive`, naming the unmatched model) that survives pod GC and shows on Prometheus-less installs, plus a manager-side Prometheus counter where telemetry is enabled. The stderr line stays but is no longer the only signal.
- **D-03 Chart-configurable pricing table: IN SCOPE.** Wire chart values → subagent env → the existing per-instance `Options.PricingOverrides` seam so new models don't require an image rebuild. This is an additive implementation choice beyond the literal COST requirements (the folded todo's follow-up, pulled in by the operator). Its values.yaml surface batches into the single milestone chart bump (D-13).
- **D-04 Proof shape (COST-01):** unit tests pin every new row and the normalizer, PLUS a run-mix fixture replaying the recorded first-run token usage through the new table asserting the tally lands near the console's $3.84 (within rounding) — locking the 2.8× overcount as a regression. First-run model mix: 2 Sonnet-5 + 1 Fable-5 planner dispatches, 3 Haiku-4.5 task executions; TIDE tallied $10.86 vs console $3.84. If per-dispatch token counts aren't recoverable (they live on the perishable minikube PVC — export before cleanup), reconstruct the fixture from whatever envelope evidence the operator can export, and say so in the fixture comment.

### Cache-TTL empirical check (COST-03)
- **D-05 Environment:** the teed-credproxy probe runs on the operator's live minikube cluster with the real key — the CACHE-01 spike precedent. It must observe TIDE's actual dispatch surface (the subagent image's `claude` CLI + `--bare` flags), not a local lookalike.
- **D-06 Execution:** scripted recipe, operator runs it. The phase (research stage) authors a one-shot probe script + exact instructions (tee config, dispatch, which request field/header reveals the TTL); the operator executes and reports the observation, which is recorded in phase artifacts.
- **D-07 Sequencing:** the probe happens DURING the research stage of `/gsd-plan-phase 38`, before planning — the researcher produces the recipe, the operator runs it, and the planner already knows the verified multiplier. The pricing-row work must not ship ahead of this result.
- **D-08 Encoding:** a single named `cacheWriteMultiplier` constant (value set from the probe: 5m TTL → 1.25×, 1h TTL → 2×) from which every model's `cacheWriteCentsPerMTok` derives, with a comment citing the probe evidence. One place to change on the next TTL shift; per-model fields remain for the overrides path.

### promptFile flag semantics (PROMPT-01)
- **D-09 Conflict:** `--prompt-file` given while the manifest already sets `spec.outcomePrompt` → error with a clear message. No silent override; matches apply.go's existing fail-loud style.
- **D-10 Targeting:** the manifest must contain exactly one Project document; zero or multiple Projects → error naming the count. No injection into multiple docs.
- **D-11 Content:** inline bytes verbatim (trim only a single trailing newline); reject files above a sane cap (~256 KiB — planner picks the exact number with a comment) with a clear CLI error before the apiserver; reject empty/whitespace-only files at the CLI.

### Telemetry nudge + chart coordination (TELEM-01..03, DEBT-02)
- **D-12 NOTES.txt (net-new file):** short post-install summary (release installed, how to reach the dashboard/docs) plus the conditional `prometheus.enabled=false` warning that run telemetry beyond budget is unavailable. Not warning-only.
- **D-13 Chart bump batching:** ALL v1.0.7 chart changes ship under one chart version bump. Phase 38's chart edits (NOTES.txt, configmap `default 4`, pricing-overrides values) land on main when 38 executes; the version bump itself is a single event coordinated with Phases 35/36 at release. `values.yaml` remains the FIXED contract — binary catches up to chart, never reverse.
- **D-14 Banner signal (TELEM-03):** the chart passes `prometheus.enabled` as an env var on the dashboard deployment; the dashboard server exposes it to the UI via its existing config surface. Disabled-by-config = the env var; no-data = queries return empty while enabled. The banner must distinguish the two.
- **D-15 INSTALL.md step (TELEM-01):** full copy-paste kube-prometheus-stack walkthrough — install, the `release:` label fix on the ServiceMonitor match, `prometheus.enabled=true`, ending at a Prometheus Targets-page verification showing TIDE scraped — plus a short variant note for operators pointing at an existing Prometheus (what the ServiceMonitor needs to match).

### Claude's Discretion
- **DEBT-01:** retrofit the project-level `PlannerRolledUpUID` stamp (`internal/controller/project_controller.go:1370-1379`) to the hardened `retry.RetryOnConflict` + `MergeFromWithOptimisticLock` pattern Phases 31/32 applied to the child-level markers; return the error instead of swallowing. Pattern-following — no user decision needed.
- **DEBT-02:** `charts/tide/templates/configmap.yaml:22` `| default 16` → `| default 4` (one-character fix; chart edit batches per D-13).
- **DEBT-03:** identify which `internal/controller` envtest specs are "heavy" (the 167-spec suite; the CTRL-03 leader-election split at `Makefile:93` is the existing precedent), move them to the integration tier (`test/integration/envtest/`, label-filtered), and verify total spec count is conserved across the split. Researcher/planner pick the threshold and mechanism.
- Condition naming, metric naming, exact size-cap value, banner copy/placement: planner's choice, consistent with existing conventions (water metaphor where natural).

### Folded Todos
- **Update subagent pricing table for Claude 5 family models** (`.planning/todos/pending/2026-07-03-pricing-table-claude-5-family.md`) — the COST-01/02 source: unknown `claude-sonnet-5` billed at the fable-5 conservative tier, 2.8× overcount vs console. Its "chart-configurable table" follow-up is pulled into scope per D-03.
- **Add a Prometheus setup step so run telemetry beyond budget is present** (`.planning/todos/pending/2026-07-03-prometheus-setup-step-for-run-telemetry.md`) — the TELEM-01..03 source: all three candidate surfaces (INSTALL.md, NOTES.txt, dashboard banner) ship.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap
- `.planning/ROADMAP.md` §Phase 38 — goal, success criteria, COST/PROMPT/TELEM/DEBT requirement mapping, the COST-03 research flag
- `.planning/REQUIREMENTS.md` — COST-01..03, PROMPT-01, TELEM-01..03, DEBT-01..03 exact wording; Out of Scope table (ConfigMap-ref union, log archiving)
- `.planning/STATE.md` §Accumulated Context — v1.0.7 binding constraints (FIXED chart contract, dashboard read-only), the COST-03 empirical-gate blocker, the perishable-PVC warning

### Source todos (folded)
- `.planning/todos/pending/2026-07-03-pricing-table-claude-5-family.md` — run evidence: model mix, $10.86 vs $3.84, the stderr anchor line
- `.planning/todos/pending/2026-07-03-prometheus-setup-step-for-run-telemetry.md` — why telemetry was dark; the three candidate surfaces

### Tech-debt provenance
- `.planning/milestones/v1.0.6-MILESTONE-AUDIT.md` — W1 (exact asymmetry description + fix recipe) and W2 (configmap fallback) verbatim; DEBT-03 origin

### Precedents
- `.planning/PROJECT.md` §CACHE-01 decision record — the teed-credproxy probe method COST-03 reuses (tee request bodies, observe cache fields per dispatch)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/subagent/anthropic/pricing.go` — the price table. Already has `claude-fable-5`, `claude-opus-4-8` (+4-7, 4-6), `claude-sonnet-4-6`, `claude-haiku-4-5`; **missing `claude-sonnet-5`**, and lookup is exact-ID (no date-suffix normalizer). `conservativeTier = priceTable["claude-fable-5"]`. Cache rates currently derive from the 1.25×/0.10× rule in comments but are stored as per-model literals.
- `Options.PricingOverrides` (same package) — per-instance override merge already implemented (`maps.Clone(priceTable)` merged in `New()`, T-14-02); D-03 only needs chart → env plumbing into it.
- `internal/subagent/anthropic/pricing_test.go` — existing fixtures pin the conservative tier at fable-5 rates; extend, don't replace.
- `cmd/tide/apply.go` — `newApplyCmd`/`runApply`: unstructured YAML → server-side apply, FieldManager `tide-cli`, field-by-field `apierrors.IsInvalid` surfacing. `--prompt-file` hooks in here; `cmd/tide/cmd_test.go` carries the CLI test conventions.
- `internal/controller/project_controller.go:1355-1380` — the W1 stamp site; the hardened pattern to copy lives in the Phase 31/32 child-level rollup markers (`MilestoneRolledUpUID`/`PhaseRolledUpUID`/`PlanRolledUpUID` sites).
- `Makefile:85-155` — the tier split terrain: `test` (TEST-01 unit tier), `test-leader-election` (existing heavy-spec extraction precedent), `test-int-fast`/`test-int` (integration tier, `test/integration/envtest/` with `--ginkgo.label-filter='envtest'`).

### Established Patterns
- Envelope → reporter → CRD `.status` rollup is the sanctioned path for anything a subagent pod needs to persist (D-02's fallback flag rides it); per-object status stays small.
- Conditions are typed and sticky where the operator must see them (`integration-incomplete` in Phase 34, `BillingHalt` from v1.0.1) — `PricingFallbackActive` follows that family.
- `charts/tide/templates/` has NO NOTES.txt today — TELEM-02 creates the file; `servicemonitor.yaml` + `dashboard-deployment.yaml` are the templates the telemetry work touches.
- Dashboard React app lives at `dashboard/web/`; server binary at `cmd/dashboard`. Chart → env → server config → UI is the existing config path D-14 extends (researcher: map the dashboard's current config endpoint).
- `hack/helm/` is canonical for chart sources per the v1.0.6 audit W2 note — check it before editing rendered templates directly.

### Integration Points
- Pricing changes are entirely behind the `Subagent` interface in `internal/subagent/anthropic/` — no orchestrator/provider leakage (constraint).
- COST-02's metric lands on the manager (`prometheus/client_golang`, existing metrics service/ServiceMonitor); the condition lands on Project `.status` via the reporter path.
- credproxy (`cmd/credproxy`) is the tee point for the COST-03 probe — CACHE-01's teed-request tooling is the precedent to crib.

</code_context>

<specifics>
## Specific Ideas

- The run-mix fixture (D-04) should read like the CACHE-01 evidence style: cite the run date (2026-07-03), the model mix, and both dollar figures in the test comment so the regression is self-explaining.
- The probe recipe (D-06) is operator-facing: exact `kubectl` commands against the minikube cluster, and it must say which JSON field in the teed request/response distinguishes 5m from 1h cache-write TTL.
- NOTES.txt tone: match the tight, declarative docs voice — a few useful lines, not a wall of text.

</specifics>

<deferred>
## Deferred Ideas

- **ConfigMap-ref promptFile union (`outcomePromptFrom`)** — explicitly out of scope (REQUIREMENTS.md Out of Scope); the CLI inlining keeps it a compatible later addition.
- **Per-model TTL/pricing auto-discovery from the provider API** — not discussed as a requirement; D-03's overrides + D-08's constant cover the near term.

### Reviewed Todos (not folded)
- `2026-07-03-wave-parallel-integration-miss.md` → Phase 34; `2026-07-03-git-baseref-run-branch.md` → Phase 35; `2026-07-03-signed-commits-verified-badge.md` → Phase 36; `2026-07-03-dashboard-planning-dag-artifact-view.md` + `2026-07-03-dashboard-log-stream-drawer-empty.md` → Phase 37 — all tagged `resolves_phase` accordingly.
- `2026-07-03-project-level-subagent-override-slot.md` (`subagent.levels` rename) — deferred to its own milestone (breaking, SchemaRevision/v1alpha3).
- `cache-f1-direct-sdk-cross-pod-caching.md` — deferred (vNext or later).

</deferred>

---

*Phase: 38-Small Independents — Pricing Accuracy, promptFile, Telemetry Nudge, Tech-Debt Carry*
*Context gathered: 2026-07-03*
