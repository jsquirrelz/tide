# Phase 38: Small Independents — Pricing Accuracy, promptFile, Telemetry Nudge, Tech-Debt Carry - Research

**Researched:** 2026-07-04
**Domain:** TIDE internals — subagent pricing table, tide CLI, Helm chart/telemetry surfaces, controller status patterns, test tiering
**Confidence:** HIGH (codebase-verified) / MEDIUM on the one empirical gate (COST-03 TTL — pending operator probe)

## Summary

This phase is four independent paper-cut clusters, and the codebase investigation shrinks two of them substantially. **D-03 (chart-configurable pricing) is already fully implemented end-to-end** — `values.yaml pricing.overrides` → `--pricing-overrides-json` manager flag → `TIDE_PRICING_OVERRIDES_JSON` env on subagent Jobs → `Options.PricingOverrides` merge in `New()` (landed Phase 14, T-14-02/14-05). The COST work reduces to: one new `claude-sonnet-5` price row, a `-YYYYMMDD` suffix normalizer, a single `cacheWriteMultiplier` refactor (D-08), the run-mix regression fixture, and the fallback observability (condition + metric). **TELEM-03 is also half-built**: the dashboard already distinguishes "unavailable" (PROM_ENDPOINT unset → proxy returns the `{"status":"unavailable"}` sentinel → `TelemetryUnavailableNotice`) from live data; the remaining work is elevating that to a banner that also distinguishes empty-result "no-data", plus copy.

Two findings need planner attention. First, **there is no `prometheus.enabled` chart key today** — the operative keys are `prometheus.serviceMonitor.enabled` (scrape) and `prometheus.endpoint` (dashboard PROM_ENDPOINT env). D-14 (locked) says the chart passes `prometheus.enabled` as a dashboard env var; the Q2 ruling (headless, recorded in Open Questions + Assumptions A6) honors D-14's letter: add the `prometheus.enabled` umbrella key under D-13 and wire it as the dashboard env var, keeping the existing keys in their current roles. Second, **`claude-sonnet-5` has introductory pricing ($2/$10 per MTok through 2026-08-31; $3/$15 sticker)** — the 2026-07-03 console figure of $3.84 was billed at intro rates, so a sticker-rate table will not reproduce $3.84 exactly. The fixture must pin the exact expected tally at the new table's rates and assert it is far below the $10.86 overcount (see Pitfall 1).

The COST-03 empirical gate has a ready-made vehicle: `cmd/tide-spike` (CACHE-01 precedent) already dispatches real `claude -p --bare` calls through a teed credproxy (`--tee-body-dir`). The probe recipe (below, § COST-03 Probe Recipe) tells the operator exactly which JSON field discriminates 5m from 1h TTL: the presence/absence of `"ttl":"1h"` inside `cache_control` blocks in the teed request body. **The pricing rows must not ship before the operator reports this observation** (D-07) — the planner should front-load the probe as a `checkpoint:human-verify` before the pricing-row task.

**Primary recommendation:** Plan four independent work streams (COST, PROMPT, TELEM, DEBT) with no cross-dependencies except (a) the COST-03 probe checkpoint gating the pricing rows and (b) all chart-file edits landing via `hack/helm/` sources (canonical per v1.0.6 audit W2) under the single batched v1.0.7 chart bump (D-13).

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Pricing rows + fallback observability (COST-01, COST-02)
- **D-01 Normalizer:** exact-ID lookup first; on miss, strip a trailing `-YYYYMMDD` date suffix and retry once. Anything still unmatched falls to the conservative tier. No family prefix-matching — a genuinely unknown model must never be silently priced at an old cheaper rate.
- **D-02 Fallback surface (COST-02):** both a durable condition AND a metric. The envelope carries an unknown-model-fallback flag → reporter rolls it up → a Project `.status` condition (e.g. `PricingFallbackActive`, naming the unmatched model) that survives pod GC and shows on Prometheus-less installs, plus a manager-side Prometheus counter where telemetry is enabled. The stderr line stays but is no longer the only signal.
- **D-03 Chart-configurable pricing table: IN SCOPE.** Wire chart values → subagent env → the existing per-instance `Options.PricingOverrides` seam so new models don't require an image rebuild. This is an additive implementation choice beyond the literal COST requirements (the folded todo's follow-up, pulled in by the operator). Its values.yaml surface batches into the single milestone chart bump (D-13).
- **D-04 Proof shape (COST-01):** unit tests pin every new row and the normalizer, PLUS a run-mix fixture replaying the recorded first-run token usage through the new table asserting the tally lands near the console's $3.84 (within rounding) — locking the 2.8× overcount as a regression. First-run model mix: 2 Sonnet-5 + 1 Fable-5 planner dispatches, 3 Haiku-4.5 task executions; TIDE tallied $10.86 vs console $3.84. If per-dispatch token counts aren't recoverable (they live on the perishable minikube PVC — export before cleanup), reconstruct the fixture from whatever envelope evidence the operator can export, and say so in the fixture comment.

#### Cache-TTL empirical check (COST-03)
- **D-05 Environment:** the teed-credproxy probe runs on the operator's live minikube cluster with the real key — the CACHE-01 spike precedent. It must observe TIDE's actual dispatch surface (the subagent image's `claude` CLI + `--bare` flags), not a local lookalike.
- **D-06 Execution:** scripted recipe, operator runs it. The phase (research stage) authors a one-shot probe script + exact instructions (tee config, dispatch, which request field/header reveals the TTL); the operator executes and reports the observation, which is recorded in phase artifacts.
- **D-07 Sequencing:** the probe happens DURING the research stage of `/gsd-plan-phase 38`, before planning — the researcher produces the recipe, the operator runs it, and the planner already knows the verified multiplier. The pricing-row work must not ship ahead of this result.
- **D-08 Encoding:** a single named `cacheWriteMultiplier` constant (value set from the probe: 5m TTL → 1.25×, 1h TTL → 2×) from which every model's `cacheWriteCentsPerMTok` derives, with a comment citing the probe evidence. One place to change on the next TTL shift; per-model fields remain for the overrides path.

#### promptFile flag semantics (PROMPT-01)
- **D-09 Conflict:** `--prompt-file` given while the manifest already sets `spec.outcomePrompt` → error with a clear message. No silent override; matches apply.go's existing fail-loud style.
- **D-10 Targeting:** the manifest must contain exactly one Project document; zero or multiple Projects → error naming the count. No injection into multiple docs.
- **D-11 Content:** inline bytes verbatim (trim only a single trailing newline); reject files above a sane cap (~256 KiB — planner picks the exact number with a comment) with a clear CLI error before the apiserver; reject empty/whitespace-only files at the CLI.

#### Telemetry nudge + chart coordination (TELEM-01..03, DEBT-02)
- **D-12 NOTES.txt (net-new file):** short post-install summary (release installed, how to reach the dashboard/docs) plus the conditional `prometheus.enabled=false` warning that run telemetry beyond budget is unavailable. Not warning-only.
- **D-13 Chart bump batching:** ALL v1.0.7 chart changes ship under one chart version bump. Phase 38's chart edits (NOTES.txt, configmap `default 4`, pricing-overrides values) land on main when 38 executes; the version bump itself is a single event coordinated with Phases 35/36 at release. `values.yaml` remains the FIXED contract — binary catches up to chart, never reverse.
- **D-14 Banner signal (TELEM-03):** the chart passes `prometheus.enabled` as an env var on the dashboard deployment; the dashboard server exposes it to the UI via its existing config surface. Disabled-by-config = the env var; no-data = queries return empty while enabled. The banner must distinguish the two.
- **D-15 INSTALL.md step (TELEM-01):** full copy-paste kube-prometheus-stack walkthrough — install, the `release:` label fix on the ServiceMonitor match, `prometheus.enabled=true`, ending at a Prometheus Targets-page verification showing TIDE scraped — plus a short variant note for operators pointing at an existing Prometheus (what the ServiceMonitor needs to match).

### Claude's Discretion
- **DEBT-01:** retrofit the project-level `PlannerRolledUpUID` stamp (`internal/controller/project_controller.go:1370-1379`) to the hardened `retry.RetryOnConflict` + `MergeFromWithOptimisticLock` pattern Phases 31/32 applied to the child-level markers; return the error instead of swallowing. Pattern-following — no user decision needed.
- **DEBT-02:** `charts/tide/templates/configmap.yaml:22` `| default 16` → `| default 4` (one-character fix; chart edit batches per D-13).
- **DEBT-03:** identify which `internal/controller` envtest specs are "heavy" (the 167-spec suite; the CTRL-03 leader-election split at `Makefile:93` is the existing precedent), move them to the integration tier (`test/integration/envtest/`, label-filtered), and verify total spec count is conserved across the split. Researcher/planner pick the threshold and mechanism.
- Condition naming, metric naming, exact size-cap value, banner copy/placement: planner's choice, consistent with existing conventions (water metaphor where natural).

### Deferred Ideas (OUT OF SCOPE)
- **ConfigMap-ref promptFile union (`outcomePromptFrom`)** — explicitly out of scope (REQUIREMENTS.md Out of Scope); the CLI inlining keeps it a compatible later addition.
- **Per-model TTL/pricing auto-discovery from the provider API** — not discussed as a requirement; D-03's overrides + D-08's constant cover the near term.
- Reviewed todos routed elsewhere: run integrity → 34, baseRef → 35, signing → 36, dashboard views → 37; `subagent.levels` rename and CACHE-F1 deferred to future milestones.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| COST-01 | Claude 5 family models price at real per-MTok rates via exact-ID lookup with `-YYYYMMDD` normalizer | Verified current rates via claude-api skill (table below); `pricing.go` already has fable-5/opus-4-8/haiku-4-5 rows at correct rates — only `claude-sonnet-5` row + normalizer are new. Test conventions in `pricing_test.go`/`cost_parity_test.go`. |
| COST-02 | Unknown-model fallback observable as metric/condition, not only a GC'd pod log | Envelope path mapped: `Usage`/`EnvelopeOut` (pkg/dispatch/envelope.go) → controller rollup sites → `budget.RollUpUsage`. Condition precedent: `billing_halt.go` (`ConditionBillingHalt`, `meta.SetStatusCondition`). Metric registry: `internal/metrics/registry.go` (`tide_*_total` naming). |
| COST-03 | Cache-write TTL multiplier verified empirically before pricing table ships | Probe recipe authored (§ COST-03 Probe Recipe); discriminator = `cache_control` `ttl` field in teed request bodies; vehicle = `cmd/tide-spike` + credproxy `--tee-body-dir` (CACHE-01 precedent). Result PENDING operator run — planner must gate pricing rows behind checkpoint. |
| PROMPT-01 | `tide apply --prompt-file <path>` inlines file into `spec.outcomePrompt` | `cmd/tide/apply.go` mapped (`newApplyCmd`/`runApply`, single-doc decode today, fail-loud `formatApplyError` style); `OutcomePrompt` has no CRD MaxLength (CLI cap is the only guard); CLI test conventions in `cmd/tide/cmd_test.go`. |
| TELEM-01 | INSTALL.md enable-telemetry step incl. `release:` label fix, ends at Targets page | `docs/INSTALL.md` structure mapped (insert after "Verifying the install"/before "Provider Secret"); ServiceMonitor labels today are `control-plane` + `tide.labels` — no `release:` label (that's why kube-prometheus-stack ignores it). |
| TELEM-02 | Chart NOTES.txt warns when telemetry off | Confirmed NO NOTES.txt exists; chart templates are generated by `hack/helm/augment-tide-chart.sh` (canonical source) — NOTES.txt must be added there, not only in `charts/tide/templates/`. |
| TELEM-03 | Dashboard "telemetry disabled" banner distinguishing disabled-by-config from no-data | Existing surface mapped: `PROM_ENDPOINT` env → `cmd/dashboard/api/prometheus.go` `{"status":"unavailable"}` sentinel → `TelemetryView.tsx` state machine (`data`/`unavailable`/`unreachable`) + `TelemetryUnavailableNotice.tsx`. |
| DEBT-01 | Project-level `PlannerRolledUpUID` uses hardened RetryOnConflict pattern | W1 site confirmed at `project_controller.go:1370-1381` (plain `MergeFrom` + swallow); hardened template at `milestone_controller.go:606-633` / `phase_controller.go:537-563` / `plan_controller.go:611-638`. |
| DEBT-02 | Chart configmap `plannerConcurrency` default 4 | Confirmed `| default 16` at `charts/tide/templates/configmap.yaml:22` AND at its canonical source `hack/helm/augment-tide-chart.sh:90`; `values.yaml:89` is `plannerConcurrency: 4`. Both files need the edit. |
| DEBT-03 | Heavy controller envtest specs move to integration tier, spec count conserved | 167 `It(` specs counted in `internal/controller`; existing precedents: `-short` + `testing.Short()` skip (leader_election_test.go:54), `make test-leader-election` (ginkgo.focus), integration tier = `test/integration/envtest/` with `--ginkgo.label-filter='envtest'`. Mechanism recommendation below. |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

- **GSD workflow enforcement:** all edits land via GSD plans (this phase's PLAN.md files).
- **`charts/tide/values.yaml` is a FIXED contract** — binary catches up to chart, never reverse. Chart edits require explicit care; Phase 38's chart edits batch into the single v1.0.7 chart bump (D-13).
- **All Anthropic-specific code stays behind the `Subagent` interface in `internal/subagent/anthropic/`** — pricing changes must not leak provider detail into orchestrator/controllers. The fallback *flag* on the provider-neutral envelope must be provider-neutral (a model-ID string + boolean, not Anthropic-specific structure).
- **CRD `.status` only; per-object status stays small** — the fallback condition is one `metav1.Condition`, not a per-dispatch log.
- **Don't vendor GSD Markdown; compiled-in Go templates** — N/A this phase.
- **Metrics on prometheus/client_golang (not OTel metrics)** — the COST-02 counter goes in `internal/metrics/registry.go`.
- **Default chart ServiceMonitor to disabled** (avoid CRD-not-found on plain clusters) — TELEM work must keep `prometheus.serviceMonitor.enabled: false` as the default; NOTES.txt/banner compensate with guidance, not by flipping the default.
- **Water metaphor naming where natural** (vocabulary conventions).
- **Verification discipline:** `make test-int` exit ≠ Ginkgo green — read `MAKE_EXIT` and grep `^--- FAIL`; subagent "pre-existing" dismissals are claims, not verification.
- **Execute, Don't Ask exceptions:** chart values edits and anything outside an approved plan need explicit routing.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Price table + normalizer + multiplier (COST-01/03) | Subagent provider pkg (`internal/subagent/anthropic/pricing.go`) | — | Provider firewall: all Anthropic pricing lives behind the Subagent interface |
| Unknown-model fallback flag transport (COST-02) | Envelope (`pkg/dispatch/envelope.go`) | Subagent (writes it) | Envelope is the sanctioned subagent→controller channel; field must be provider-neutral |
| `PricingFallbackActive` condition (COST-02) | Controller (per-level rollup sites → Project `.status`) | — | Conditions are controller-owned; survives pod GC; follows BillingHalt family |
| Fallback Prometheus counter (COST-02) | Manager metrics (`internal/metrics/registry.go`) | — | client_golang registry on the manager; scraped via existing metrics service |
| `--prompt-file` (PROMPT-01) | CLI (`cmd/tide/apply.go`) | — | Explicitly CLI-side only; no CRD change |
| NOTES.txt + configmap default + pricing values docs (TELEM-02, DEBT-02, D-03) | Chart sources (`hack/helm/` → `charts/tide/`) | — | hack/helm is canonical; augment scripts regenerate charts/tide |
| INSTALL.md telemetry step (TELEM-01) | Docs (`docs/INSTALL.md`) | — | Pure docs |
| Telemetry banner (TELEM-03) | Dashboard UI (`dashboard/web/src`) | Dashboard server (existing PROM_ENDPOINT → sentinel) | Server signal already exists; UI renders the distinction |
| W1 stamp hardening (DEBT-01) | Controller (`project_controller.go`) | — | Pattern copy from sibling controllers |
| Envtest tier split (DEBT-03) | Build/test infra (Makefile + Ginkgo labels) | — | Tiering is a Makefile/label concern, not a code move (recommended) |

## Standard Stack

No new libraries. Everything uses the pinned stack (Go 1.26, controller-runtime v0.24.x, Ginkgo v2.28/Gomega, client_golang v1.23, cobra, chi, React 18 + TS).

### Verified Claude pricing (for the COST-01 rows)

Source: claude-api skill model table (cached 2026-06-24) — [VERIFIED: claude-api skill / platform.claude.com pricing]. Existing `pricing.go` rows for fable-5 ($10/$50), opus-4-8/4-7/4-6 ($5/$25), sonnet-4-6 ($3/$15), haiku-4-5 ($1/$5) all **match current published rates** — no corrections needed to existing rows.

| Model | Input ¢/MTok | Output ¢/MTok | Cache read (0.10×) | Cache write @1.25× | Cache write @2× | Status in table |
|-------|-------------:|--------------:|-------------------:|-------------------:|----------------:|-----------------|
| claude-fable-5 | 1000 | 5000 | 100 | 1250 | 2000 | present ✓ |
| claude-opus-4-8 | 500 | 2500 | 50 | 625 | 1000 | present ✓ |
| **claude-sonnet-5** | **300** | **1500** | **30** | **375** | **600** | **MISSING — add** |
| claude-haiku-4-5 | 100 | 500 | 10 | 125 | 200 | present ✓ |

⚠️ `claude-sonnet-5` carries **introductory pricing $2/$10 per MTok through 2026-08-31** ($3/$15 sticker) [VERIFIED: claude-api skill]. Recommendation: compile the **sticker** rates (300/1500) — conservative (never under-counts), durable past the intro window, and consistent with `hack/check-pricing-drift.sh` which diffs against the published pricing page. See Pitfall 1 for the fixture implication.

Cache multipliers [CITED: platform.claude.com/docs prompt-caching via claude-api skill]: reads ≈ 0.1× base input; writes **1.25× for 5-minute TTL, 2× for 1-hour TTL**. Which one the `claude` CLI dispatch surface uses is exactly the COST-03 probe question — do not assume.

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Sticker rates for sonnet-5 | Intro rates ($2/$10) | Intro matches the 2026-07-03 console bill but silently under-counts after 2026-08-31; drift script would then flag it. Sticker is the conservative choice. |
| Ginkgo `Label("heavy")` + Makefile filter (DEBT-03) | Physically moving spec files to `test/integration/envtest/` | File moves break `internal/controller` package access (unexported reconciler internals) and require duplicating the envtest suite scaffolding; labels conserve spec count trivially and follow the `envtest` label-filter precedent already in `test-int-fast`. |
| One `Usage.PricingFallbackModel` string field | New EnvelopeOut top-level struct | Usage already rides every rollup path; a single `omitempty` string keeps JSON clean and per-object status small. |

**Installation:** none — no new packages.

## Package Legitimacy Audit

**No new external packages are installed by this phase.** All work uses modules already in `go.mod` and npm packages already in `dashboard/web/package.json`. Audit table: empty. Packages removed due to [SLOP]: none. Packages flagged [SUS]: none.

## Architecture Patterns

### System Architecture Diagram (COST-01/02 data flow)

```
                     subagent Pod (per dispatch Job)
  ┌──────────────────────────────────────────────────────────┐
  │ claude CLI ──stream-json──► stream_parser ──► Usage      │
  │  estimatedCostCents(model, usage)                        │
  │    ├─ exact-ID hit ────────────► priced row              │
  │    ├─ miss → strip -YYYYMMDD, retry once (NEW, D-01)     │
  │    └─ still miss → conservativeTier + stderr warn        │
  │                    └─► Usage.PricingFallbackModel (NEW)  │
  │ EnvelopeOut{Usage} written to PVC eventsDir              │
  └───────────────┬──────────────────────────────────────────┘
                  │ envelope read (EnvReader.ReadOut — podjob.EnvelopeReader, per-level controller)
                  ▼
  controller rollup site (project/milestone/phase/plan/task)
      ├─► budget.RollUpUsage → Project.status.budget.costSpentCents
      ├─► if Usage.PricingFallbackModel != "":
      │     ├─ meta.SetStatusCondition(Project, PricingFallbackActive,
      │     │    Reason=UnknownModel, Message names the model)  (NEW, D-02)
      │     └─ metrics: tide_pricing_fallback_total{model=...}.Inc()  (NEW)
      ▼
  Project .status  ──scrape──► /metrics ──ServiceMonitor──► Prometheus
```

### Pricing overrides plumbing (D-03 — ALREADY BUILT, verify only)

```
values.yaml pricing.overrides (map)                       [charts/tide/values.yaml:135-149]
  → deployment.yaml renders --pricing-overrides-json={{ toJson }}   [deployment.yaml:35-36]
  → cmd/manager/main.go flag parse + fail-fast validation           [main.go:146,176,235-240]
  → controllers Deps.PricingOverridesJSON                           [task_controller.go:117 etc.]
  → jobspec.go stamps TIDE_PRICING_OVERRIDES_JSON env               [jobspec.go:370-377]
  → cmd/claude-subagent parsePricingOverridesFromEnv                [main.go:40-51]
  → anthropic.Options.PricingOverrides → maps.Clone merge in New()  [subagent.go:138-167]
```
[VERIFIED: codebase grep, all files listed]. The D-03 task should be a **render/round-trip verification test** (e.g. helm template asserts the flag renders from a set override; an existing-style unit test proves an override row beats the compiled row), plus values.yaml comment refresh naming the Claude-5 use case — not new plumbing.

### Telemetry signal chain (TELEM-02/03 terrain)

```
values.yaml prometheus.endpoint ("" default)
  → dashboard-deployment.yaml env PROM_ENDPOINT             [dashboard-deployment.yaml:66-67]
  → cmd/dashboard/main.go os.Getenv("PROM_ENDPOINT")        [main.go:156]
  → api/prometheus.go: empty endpoint → HTTP 200 {"status":"unavailable"}
  → TelemetryView.tsx: kind ∈ {data | unavailable | unreachable}
  → TelemetryUnavailableNotice.tsx ("Telemetry unavailable — Prometheus not configured")

values.yaml prometheus.serviceMonitor.enabled (false default)
  → servicemonitor.yaml (labels: control-plane + tide.labels — NO `release:` label)
```

- **Disabled-by-config** = the `unavailable` sentinel (PROM_ENDPOINT unset) — already detected end-to-end. [VERIFIED: cmd/dashboard/api/prometheus.go:22,79; TelemetryView.tsx:66-314]
- **No-data** = `status:"success"` with empty `result` arrays while endpoint configured — UI must add this branch + banner copy.
- The `unavailable` sentinel already senses endpoint-unset; per the Q2 ruling, D-14's letter additionally adds a `prometheus.enabled` key + dashboard env var (e.g. `PROMETHEUS_ENABLED`) as the authoritative disabled-by-config signal, with the sentinel as defensive fallback. See Open Questions Q2 (resolved) and Assumptions A6.
- Existing chart render assertions for telemetry: `hack/helm/assert-telemetry-render.sh`, `assert-prometheus-env.py` — extend these for NOTES.txt / any new env. Note dashboard-deployment.yaml's comment mentions an EC-7 gate that greps the default render for PROM_ENDPOINT text — check that gate before changing how the env renders.

### Chart source-of-truth pattern (TELEM-02, DEBT-02, D-13)

`hack/helm/` is canonical (v1.0.6 audit W2). `augment-tide-chart.sh` heredoc-writes `templates/configmap.yaml` (the `| default 16` bug is at **`augment-tide-chart.sh:90`**, mirrored at `charts/tide/templates/configmap.yaml:22`). Any Phase 38 chart edit must land in BOTH the hack/helm source and the rendered charts/tide copy (or regenerate via the augment script). NOTES.txt is net-new: add its heredoc to `augment-tide-chart.sh` alongside configmap.yaml, and the rendered `charts/tide/templates/NOTES.txt`. Do NOT bump `Chart.yaml` version in this phase — the bump is a single release-time event (D-13).

### Pattern 1: Hardened idempotent status stamp (DEBT-01)

**What:** re-fetch + `retry.RetryOnConflict` + `MergeFromWithOptimisticLock`, return error (don't swallow).
**Copy from:** `internal/controller/milestone_controller.go:606-633` (identical siblings at phase_controller.go:537-563, plan_controller.go:611-638).

```go
// Source: internal/controller/milestone_controller.go:620-632 (WR-02 pattern)
if mErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
    var latest tideprojectv1alpha2.Project
    if gErr := r.Get(ctx, client.ObjectKeyFromObject(project), &latest); gErr != nil {
        return gErr
    }
    if latest.Status.Budget.PlannerRolledUpUID == plannerJobName {
        return nil // another writer already stamped it
    }
    markerPatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
    latest.Status.Budget.PlannerRolledUpUID = plannerJobName
    return r.Status().Patch(ctx, &latest, markerPatch)
}); mErr != nil {
    return ctrl.Result{}, fmt.Errorf("patch PlannerRolledUpUID: %w", mErr)
}
```

The W1 site to replace is `project_controller.go:1370-1381` (plain `client.MergeFrom` + non-fatal log swallow). Keep the "marker only after successful rollup" ordering (Pitfall 2 comment at :1374).

### Pattern 2: Sticky Project condition (COST-02)

**Copy from:** `internal/controller/billing_halt.go` (ConditionBillingHalt + `meta.SetStatusCondition` + a `check*` reader). `PricingFallbackActive` follows the same family: typed constant in `api/v1alpha2`, `Status: True`, `Reason` like `UnknownModelPriced`, `Message` naming the unmatched model ID(s). Unlike BillingHalt it does NOT halt anything — informational, sticky until an operator or a later table fix clears it (planner's choice whether anything ever clears it; recommend leave sticky for the run's lifetime).

### Pattern 3: Metric registration (COST-02)

`internal/metrics/registry.go` — namespaced `tide_*` names via client_golang (e.g. `tide_budget_overruns_total`, `tide_tokens_input_total`). Add `tide_pricing_fallback_total` with a bounded `model` label (unknown model IDs are operator-config-bounded; cardinality is safe). [VERIFIED: registry.go:134-200]

### Pattern 4: promptFile CLI shape (PROMPT-01)

`runApply` today decodes **only the first YAML document** (`yaml.NewYAMLOrJSONDecoder(...).Decode(obj)` called once — apply.go:69-76). D-10 requires counting Project docs: loop `dec.Decode` until `io.EOF`, count docs with `Kind == "Project"`, error unless exactly 1. Inject via `unstructured.NestedString`/`SetNestedField` on `spec.outcomePrompt` after the D-09 conflict check. `OutcomePrompt` has **no CRD MaxLength** (api/v1alpha2/project_types.go:339-345) — the CLI size cap is the only guard; ~256 KiB is comfortably under etcd's 1.5 MiB object ceiling with headroom for the rest of the spec. Tests follow `cmd/tide/cmd_test.go` conventions (table-driven `TestApply*`, exercising `newApplyCmd` with temp files).

### Anti-Patterns to Avoid
- **Editing `charts/tide/templates/*` without the `hack/helm/` source** — the next augment run reverts it (W2's own lesson).
- **Bumping the chart version in this phase** — D-13 batches the bump at release.
- **Prefix/family model matching in the normalizer** — D-01 explicitly forbids it (a new expensive model must not inherit an old cheaper family rate).
- **Reading the package-level `priceTable` in cost paths** — always `a.prices` (T-14-02; pricing.go:157).
- **Anthropic-specific naming in the envelope field** — envelope is provider-neutral (`pricingFallbackModel`, not `anthropicUnknownModel`).
- **Making the fallback condition halt dispatch** — it's observability, not a gate (BillingHalt already owns halting).

## COST-03 Probe Recipe (operator-facing deliverable, D-05/D-06)

**Status: NOT YET RUN — result required before the pricing-row plan executes (D-07).** This is a headless research run; the operator must execute this recipe and report the observation. The planner MUST place a `checkpoint:human-verify` task carrying this recipe ahead of the pricing-row task, and record the observed TTL in the plan/SUMMARY artifacts.

**What discriminates 5m from 1h:** in the teed `/v1/messages` request body, every prompt-cache breakpoint is a `"cache_control"` JSON object:
- `"cache_control":{"type":"ephemeral"}` (no `ttl` key) → **5-minute TTL → cacheWriteMultiplier = 1.25×**
- `"cache_control":{"type":"ephemeral","ttl":"1h"}` → **1-hour TTL → cacheWriteMultiplier = 2×**

[CITED: platform.claude.com prompt-caching docs via claude-api skill — `{"type": "ephemeral"}` = 5m default; `{"type": "ephemeral", "ttl": "1h"}` = 1h; writes bill 1.25×/2× respectively]

**Vehicle:** `cmd/tide-spike` (build tag `spike`) already dispatches real `claude -p --bare` calls through a credproxy that supports `--tee-body-dir` (internal/credproxy/server.go:75-88 writes `req-N.json` per `/v1/messages` request) — the CACHE-01 precedent. One caveat vs D-05: `tide-spike` runs the **host** `claude` binary (main.go:142-144). To observe the *subagent image's* CLI exactly, run the dispatch from inside the claude-subagent image (step 3 alternative below).

**Recipe (operator, on the live minikube cluster with the real key):**

```bash
# 0. Context: minikube cluster with the tide-system install and real provider secret.
kubectl config use-context minikube

# 1. Start a teed credproxy reachable from the dispatch environment.
#    (Reuse the CACHE-01 spike flow: credproxy binary or image with the tee flag.)
TEE_DIR=$(mktemp -d) && chmod 0700 "$TEE_DIR"
# If running credproxy locally against the real key (simplest):
ANTHROPIC_API_KEY=<real-key-from-secret> TIDE_SIGNING_KEY=<signing-key> \
  ./bin/credproxy --tee-body-dir="$TEE_DIR" &   # note listen addr/port it logs

# 2. EITHER: run the spike (host claude CLI — same -p/--bare flag set TIDE uses):
TIDE_PROXY_ENDPOINT=<credproxy-url> TIDE_SIGNED_TOKEN=<signed-token> \
TIDE_TEE_BODY_DIR="$TEE_DIR" \
  go run -tags spike ./cmd/tide-spike/ -model claude-haiku-4-5

# 3. OR (faithful to D-05 — the subagent IMAGE's pinned CLI): run one dispatch
#    from inside the claude-subagent image, pointing at the teed credproxy:
docker run --rm --network host \
  -e ANTHROPIC_BASE_URL=<credproxy-url> \
  -e ANTHROPIC_API_KEY=<signed-token> \
  ghcr.io/jsquirrelz/tide-claude-subagent:<tag> \
  sh -c 'mkdir -p /tmp/events && echo "Reply with the single word: tide" | \
    claude -p --model claude-haiku-4-5 --output-format stream-json --verbose \
      --include-partial-messages --permission-mode acceptEdits \
      --add-dir /tmp/events --bare'
#    (flag set copied verbatim from internal/subagent/anthropic/subagent.go:285-294)

# 4. Read the verdict from the teed request body:
grep -o '"cache_control":{[^}]*}' "$TEE_DIR"/req-*.json | sort -u
#    → every match is {"type":"ephemeral"}            ⇒ 5m  ⇒ multiplier 1.25
#    → any match contains "ttl":"1h"                  ⇒ 1h  ⇒ multiplier 2
#    Record ALL distinct values (mixed TTLs are possible in principle; if mixed,
#    report the full set — the planner treats "any 1h present" as 2× for those writes
#    and should surface the mix for a decision).

# 5. Report: the distinct cache_control values, CLI version (`claude --version`
#    inside the image), model used, and date. This goes into the phase artifacts
#    verbatim (D-08's constant comment cites it).
```

**Fallback if the probe cannot run before planning completes:** encode `cacheWriteMultiplier` at the current 1.25× (matches every existing row's stored literals — zero numeric change) and keep the pricing-row plan gated on the checkpoint; flipping the single constant to 2× post-probe is a one-line diff by design (D-08).

## COST-01 Envelope Export Recipe (operator-facing deliverable, D-04)

**Purpose:** ground-truth token counts for the run-mix regression fixture live in the 2026-07-03 run's `EnvelopeOut` JSONs on the perishable minikube `tide-projects` PVC (Pitfall 2). The plan's **first COST task** carries this recipe verbatim so the operator exports them before any cleanup. No busybox mounter is needed — **the manager Pod already mounts the PVC at `/workspaces` with no subPath** [VERIFIED: charts/tide/templates/deployment.yaml:144-145; charts/tide/values.yaml:389-391]. Envelope layout: `/workspaces/{project-uid}/workspace/envelopes/{uid}/out.json` (one `{uid}/` dir per dispatch, `in.json` + `out.json` each) [VERIFIED: internal/dispatch/podjob/backend.go:83-95; jobspec.go:256-266].

```bash
# 0. Context: the minikube cluster; chart in tide-system; the CB-1605 run's Project
#    lives in namespace tide-cashboard, manifest cashboard-api ops/tide/log-levels-project.yaml.
kubectl config use-context minikube

# 1. Find the manager pod (it mounts the tide-projects PVC at /workspaces):
MGR=$(kubectl -n tide-system get pod -l control-plane=controller-manager \
      -o jsonpath='{.items[0].metadata.name}')

# 2. The project UID keys the PVC layout. If the Project CRD still exists:
PUID=$(kubectl -n tide-cashboard get project -o jsonpath='{.items[0].metadata.uid}')
#    If it was deleted, recover the UID by listing the PVC root:
#    kubectl -n tide-system exec "$MGR" -- ls /workspaces

# 3. List the envelopes — expect one {uid}/ dir per dispatch of the 2026-07-03 run
#    (planner + task dispatches; the six Usage blocks D-04 needs are in the out.json files):
kubectl -n tide-system exec "$MGR" -- \
  find /workspaces/$PUID/workspace/envelopes -name out.json

# 4. Export the whole envelopes tree to the host:
kubectl -n tide-system cp "$MGR":/workspaces/$PUID/workspace/envelopes ./cb1605-envelopes

# 5. Verify: each out.json's usage block carries the per-dispatch token counts;
#    map dispatch → model from the envelope contents (don't trust the prose model
#    mix — the folded todo and CONTEXT D-04 disagree; the envelopes are ground truth).
ls -R ./cb1605-envelopes && grep -l '"usage"' ./cb1605-envelopes/*/out.json
```

The fixture task consumes the exported `usage` blocks; if some envelopes are missing (partial GC), D-04 pre-authorizes reconstructing from whatever is exported, with a fixture comment disclosing provenance.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Conflict-safe status stamp | Custom retry loop | `retry.RetryOnConflict` + `MergeFromWithOptimisticLock` (existing WR-02 pattern) | Three verbatim precedents in sibling controllers; audited in v1.0.6 |
| Condition management | Manual conditions slice surgery | `meta.SetStatusCondition` (apimachinery) | Already the codebase-wide pattern |
| YAML multi-doc handling in CLI | Custom `---` splitting | `yaml.NewYAMLOrJSONDecoder` loop to `io.EOF` | Already imported in apply.go; handles JSON too |
| Request-body capture for the probe | New proxy/tcpdump tooling | credproxy `--tee-body-dir` + tide-spike | Built for exactly this in CACHE-01 (Phase 20) |
| Chart render verification | Ad-hoc helm template greps | Extend `hack/helm/assert-telemetry-render.sh` / `assert-prometheus-env.py` | Existing telemetry render-assertion harness |
| Pricing drift detection | New checker | `hack/check-pricing-drift.sh` (weekly CI) | Already diffs compiled table vs published pricing page; will validate the new sonnet-5 row |

**Key insight:** almost every deliverable in this phase has a working precedent in-tree; the tasks are pattern-copies and small deltas, not new machinery.

## Common Pitfalls

### Pitfall 1: The $3.84 console figure includes sonnet-5 INTRO pricing
**What goes wrong:** D-04 says the fixture asserts the tally "lands near the console's $3.84 (within rounding)". The 2026-07-03 run was billed while `claude-sonnet-5` intro pricing ($2/$10) was live; a sticker-rate ($3/$15) table replaying the same tokens will exceed $3.84 on the sonnet-5 lines by up to 1.5×.
**Why it happens:** intro pricing runs through 2026-08-31 [VERIFIED: claude-api skill].
**How to avoid:** compile sticker rates (conservative, durable); have the fixture (a) pin the **exact expected cents** computed from the recorded token counts at the NEW table's rates, and (b) assert that value is `< ~1.6×` the console's 384¢ and `≪` the old 1086¢ — locking the 2.8× overcount regression without pretending the sticker table reproduces an intro-priced bill. Document both dollar figures and the intro-pricing caveat in the fixture comment (CONTEXT `<specifics>` asks for the CACHE-01 evidence style).
**Warning signs:** a fixture asserting `== 384` cents will be red on day one or forces intro rates into the table.

### Pitfall 2: Fixture token counts live on a perishable PVC
**What goes wrong:** the per-dispatch envelopes (with exact token counts) are on the minikube `tide-projects` PVC; namespace/cluster cleanup destroys them.
**How to avoid:** the plan's first COST task carries the § COST-01 Envelope Export Recipe verbatim (manager pod mounts the PVC at `/workspaces`; `kubectl exec` + `kubectl cp` of `/workspaces/{project-uid}/workspace/envelopes/`) and the operator runs it BEFORE anything else; D-04 pre-authorizes reconstructing from partial evidence with a fixture comment saying so. Note: the folded todo's model-mix phrasing differs slightly from CONTEXT D-04 ("1 Sonnet-5 + 1 Fable-5 + 2 Sonnet-5" vs "2 Sonnet-5 + 1 Fable-5") — the exported envelopes are the ground truth; trust them over either prose.

### Pitfall 3: Editing the rendered chart but not `hack/helm/`
**What goes wrong:** DEBT-02's `default 16` lives in TWO places — `hack/helm/augment-tide-chart.sh:90` (canonical heredoc) and `charts/tide/templates/configmap.yaml:22` (rendered). Fixing only the rendered copy is undone by the next augment run.
**How to avoid:** edit both (or edit the script and re-run it); add a render assertion. Same rule for NOTES.txt (net-new in both places) and any values.yaml comment edits (`hack/helm/tide-values.yaml` mirrors `charts/tide/values.yaml`).

### Pitfall 4: Assuming a `prometheus.enabled` key exists
**What goes wrong:** requirements/CONTEXT phrase the toggle as `prometheus.enabled`, but the chart today has only `prometheus.serviceMonitor.enabled` and `prometheus.endpoint`. NOTES.txt conditionals and banner logic written against `.Values.prometheus.enabled` would always be falsy/nil — until the key exists.
**How to avoid:** per the Q2 ruling (see Open Questions), the plan ADDS `prometheus.enabled` (default `false`) under D-13 and NOTES.txt/banner target it; the key must land in BOTH `hack/helm/tide-values.yaml` and `charts/tide/values.yaml` (Pitfall 3) in the same task that references it, and the render assertion must prove the conditional fires. If the operator overrides the ruling at plan approval, conditions target the existing keys instead.

### Pitfall 5: Envelope fallback flag placed Anthropic-side of the firewall
**What goes wrong:** putting the unknown-model signal only in Anthropic-internal state (or stderr) breaks D-02; putting Anthropic-specific structure into `pkg/dispatch` breaks the provider firewall.
**How to avoid:** one provider-neutral field on `Usage` (e.g. `pricingFallbackModel string, omitempty`) set by the anthropic package when `estimatedCostCents` (or `cacheSavingsCents`) misses the table post-normalizer. Note `estimatedCostCents` is a value-returning method — the planner must pick where the flag is recorded (e.g. a field on the `Anthropic` struct captured when building the envelope, or change the method signature internally). All rollup call sites (project/milestone/phase/plan/task controllers) then need the condition+metric hook — a shared helper keeps it to one implementation.

### Pitfall 6: DEBT-03 spec-count conservation measured wrong
**What goes wrong:** "167 specs" counts `It(` occurrences; Ginkgo's `Ran X of Y Specs` counts runtime specs (table entries expand, `-short` skips count as skipped). Comparing grep counts to Ginkgo counts gives false alarms.
**How to avoid:** measure conservation with the same instrument before/after: run the unit-tier filter and the new heavy filter and assert `X_unit + X_heavy == Y_total` from Ginkgo's own summary lines (or `ginkgo --dry-run` per filter). Also remember `make test` passes `-short` — the leader-election spec is `testing.Short()`-skipped, not label-skipped; converting it to the new label scheme is optional but keep `make test-leader-election` working if you do.

### Pitfall 7: `make test` cannot run on this host as-is
**What goes wrong:** the Go toolchain is not installed on the host Mac (verified — see Environment Availability); `go test`/`make test` fail with command-not-found if executed directly.
**How to avoid:** execution follows the established dev-VM/Docker workflow (CLAUDE.md's constrained-VM recipe); plans should verify with the environment the operator actually uses, or wrap in `docker run golang:1.26` as fallback. Dashboard UI tests (vitest via node 22) DO run on the host.

## Code Examples

### Normalizer + multiplier shape (COST-01 / D-01 / D-08)

```go
// Source: pattern derived from internal/subagent/anthropic/pricing.go (existing code) + D-01/D-08
// cacheWriteMultiplier: set from the COST-03 probe (probe evidence: <date, CLI ver, observed cache_control>).
//   5m TTL → write = 1.25× input  → num/den = 125/100
//   1h TTL → write = 2× input     → num/den = 200/100
const (
    cacheWriteMultNum = 125 // ← flip to 200 if the probe observes "ttl":"1h"
    cacheWriteMultDen = 100
)

func cacheWriteCents(inputCentsPerMTok int64) int64 {
    return inputCentsPerMTok * cacheWriteMultNum / cacheWriteMultDen
}

var dateSuffixRe = regexp.MustCompile(`-\d{8}$`)

// lookupPrice: exact-ID first; one date-suffix-stripped retry; NO family matching (D-01).
func (a *Anthropic) lookupPrice(model string) (modelPrice, bool) {
    if p, ok := a.prices[model]; ok {
        return p, true
    }
    if stripped := dateSuffixRe.ReplaceAllString(model, ""); stripped != model {
        if p, ok := a.prices[stripped]; ok {
            return p, true
        }
    }
    return conservativeTier, false
}
```
(Existing per-model `cacheWriteCentsPerMTok` fields stay — required by the overrides path (D-08); derive their literals from the constant at table construction or assert consistency in a unit test.)

### NOTES.txt sketch (TELEM-02 / D-12 — tight declarative voice)

```
{{- /* charts/tide/templates/NOTES.txt — rendered post-install (net-new, TELEM-02) */ -}}
TIDE {{ .Chart.AppVersion }} installed in {{ .Release.Namespace }}.

Dashboard:  kubectl -n {{ .Release.Namespace }} port-forward svc/{{ include "tide.fullname" . }}-dashboard 8080:80
Docs:       https://github.com/jsquirrelz/tide/blob/main/docs/INSTALL.md

{{- if not .Values.prometheus.enabled }}

WARNING: run telemetry beyond the budget tally is unavailable —
prometheus.enabled is false.
Token spend over time, dispatch counts, and per-level durations will be dark.
Enable: see the "Enable telemetry" step in docs/INSTALL.md.
{{- end }}
```
(Condition targets the new `prometheus.enabled` umbrella key per the Q2 ruling — the key must exist in values.yaml before this template lands; keep it a few useful lines, not a wall.)

### TELEM-01 `release:` label fix — what INSTALL.md must say

kube-prometheus-stack's Prometheus selects ServiceMonitors by `release: <helm-release-name>` label by default; TIDE's ServiceMonitor carries only `control-plane: controller-manager` + `tide.labels` [VERIFIED: charts/tide/templates/servicemonitor.yaml], so a default kube-prometheus-stack install ignores it. Two documented fixes (pick one in the walkthrough, mention the other) [ASSUMED — standard kube-prometheus-stack behavior, verify the exact values key when writing the doc]:
1. `kubectl label servicemonitor <name> release=<kps-release> -n tide-system` (or a chart `additionalLabels` value if the planner adds one under D-13), or
2. Set `prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false` on the kube-prometheus-stack install so it selects ALL ServiceMonitors.
End state: Prometheus UI → Status → Targets shows the `tide-...-metrics` endpoint UP. Insert the step after "Verifying the install" (docs/INSTALL.md ~line 156) with the existing-Prometheus variant note (what the ServiceMonitor must match: namespace + label selectors).

## State of the Art / Key Deltas

| Assumed by CONTEXT | Actually in tree | Impact on plan |
|--------------------|------------------|----------------|
| D-03 needs chart→env plumbing built | Fully built (Phase 14): values → flag → env → overrides merge | D-03 task = verification test + docs comment only |
| `prometheus.enabled` key exists | Only `prometheus.serviceMonitor.enabled` + `prometheus.endpoint` today | Q2 ruling: ADD the key under D-13; NOTES.txt/banner target it (A6) |
| Dashboard needs a new env var for disabled-by-config (D-14) | PROM_ENDPOINT + `{"status":"unavailable"}` sentinel already distinguish it end-to-end | Per Q2 ruling, D-14's env var is still added (locked decision's letter); the sentinel stays as defensive fallback |
| Sonnet-5 priced $3/$15 flat | $2/$10 intro through 2026-08-31, $3/$15 sticker | Fixture tolerance + comment (Pitfall 1) |
| "167-spec suite" | Confirmed: 167 `It(` in internal/controller; envtest integration tier + label filter already exist | DEBT-03 = label + Makefile mechanics, precedents in place |
| Probe needs new tooling | tide-spike + credproxy `--tee-body-dir` exist (CACHE-01) | Recipe reuses them; only the in-image variant is new |

**Deprecated/outdated:** nothing removed this phase; `plannerConcurrency` default-16 remnant is the W2 cosmetic being fixed.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | kube-prometheus-stack default ServiceMonitor selection requires the `release:` label (or `serviceMonitorSelectorNilUsesHelmValues=false`) | TELEM-01 example | INSTALL.md step wouldn't end at a green Target; verify against the kube-prometheus-stack chart README while authoring the doc |
| A2 | The `claude` CLI's cache-write TTL is observable purely from request-body `cache_control` objects (no header-side TTL channel) | COST-03 recipe | Probe could be inconclusive; fallback: also capture response `usage.cache_creation` breakdown (`ephemeral_5m_input_tokens` / `ephemeral_1h_input_tokens`) which would require teeing responses — a small credproxy tweak |
| A3 | The subagent image tag layout allows `docker run ... claude ...` as sketched (entrypoint permits shell exec) | COST-03 recipe step 3 | Operator falls back to step 2 (host CLI at the pinned version) — still evidence-grade if `claude --version` matches the image's |
| A4 | Mixed cache TTLs within one request do not occur on the CLI surface (single multiplier suffices, per D-08's single-constant design) | COST-03 / D-08 | If probe shows a mix, planner surfaces it — D-08's one-constant encoding needs a decision |
| A5 | `tide_pricing_fallback_total{model=...}` label cardinality stays bounded (unknown models are rare, operator-driven) | COST-02 metric | If unbounded in practice, drop the label and put the model only in the condition message |
| A6 | Q2 ruling (headless): D-14's letter governs — add a `prometheus.enabled` umbrella key + dashboard env var, batched under D-13. Made without operator confirmation because this run cannot ask; chosen as the conservative reading of a locked decision plus REQUIREMENTS.md's literal `prometheus.enabled=false` wording | Open Question 2, Pitfall 4, NOTES.txt sketch | If the operator prefers existing-keys-only, the plan drops the new key: NOTES.txt targets `serviceMonitor.enabled`/`endpoint`, banner keys off the existing sentinel — a small, contained change |

## Open Questions

1. **COST-03 probe result (BLOCKING for the pricing-row task)**
   - What we know: writes bill 1.25× (5m) or 2× (1h); the discriminator field; the recipe is ready; existing table literals encode 1.25×.
   - What's unclear: which TTL the pinned `claude` CLI (≥ v2.1.139) actually requests under `--bare`.
   - Recommendation: planner front-loads a `checkpoint:human-verify` task carrying the recipe; encode 1.25× as the pre-probe default (no numeric change) and make the constant flip the only post-probe edit.

2. **How to express "telemetry enabled" for NOTES.txt + banner (TELEM-02/03, D-14) — RESOLVED (headless ruling)**
   - What we know: no `prometheus.enabled` key exists today; `serviceMonitor.enabled` gates scraping, `endpoint` gates the dashboard's data path; the dashboard already senses endpoint-unset via the sentinel.
   - The tension: D-14 is a **locked** decision ("the chart passes `prometheus.enabled` as an env var on the dashboard deployment"), and success criterion 3 / TELEM-02's REQUIREMENTS.md wording literally say `prometheus.enabled=false` — but the research initially recommended honoring the intent with existing keys. Left open, a planner would have to either contradict a locked decision or add a key the research argued against.
   - **Ruling (headless run — cannot ask the operator; conservative choice = honor the locked decision's letter):** add a new `prometheus.enabled` umbrella key to values.yaml (default `false`), batched under D-13 like every other Phase 38 chart edit. The chart passes it as an env var (e.g. `PROMETHEUS_ENABLED`) on the dashboard deployment per D-14's wording; the dashboard server exposes it via its existing config surface; the banner distinguishes disabled-by-config (env var false) from no-data (enabled, queries return empty). NOTES.txt conditions on `not .Values.prometheus.enabled`. The existing keys keep their roles — `prometheus.endpoint` still supplies the URL, `prometheus.serviceMonitor.enabled` still gates scraping (its `false` default is a binding constraint) — and the existing `unavailable` sentinel remains as a defensive secondary signal. The operator may override this ruling at plan approval; if overridden to "existing keys only," the plan drops the new key and targets `serviceMonitor.enabled`/`endpoint` in NOTES.txt and keys the banner off the sentinel. See Assumptions Log A6.

3. **Fixture ground truth availability (D-04)**
   - What we know: envelopes with exact token counts live on the perishable minikube PVC at `/workspaces/{project-uid}/workspace/envelopes/{uid}/out.json`, readable via the manager pod's existing mount — full copy-paste export recipe authored (§ COST-01 Envelope Export Recipe).
   - What's unclear: whether the operator has exported them, and whether all six dispatch envelopes survived.
   - Recommendation: first COST task carries the export recipe verbatim; fixture reconstructs from whatever is provided (D-04 pre-authorizes), comment discloses provenance.

4. **DEBT-03 heaviness threshold + unit-tier filter mechanism**
   - What we know: 167 specs; the suite is envtest-backed; leader-election (~60s) is already split out via `testing.Short()`. **Mechanism constraint:** `make test` (Makefile:85-90) runs ONE umbrella `go test -short ... $$(go list ./... | grep -v /e2e | grep -v /test/integration)` across all packages, and `-ginkgo.*` flags are only registered in Ginkgo-importing test binaries — passing `-ginkgo.label-filter='!heavy'` to the umbrella invocation fails non-Ginkgo packages with `flag provided but not defined`. In-tree, label-filter is only ever used on single-package invocations (Makefile:94, 149, 155); the only unit-tier exclusion precedent is `testing.Short()` (leader_election_test.go).
   - What's unclear: per-spec timings (not measured this session — requires a full suite run in the dev VM).
   - Recommendation: planner's first DEBT-03 task generates a Ginkgo JSON report (`--ginkgo.json-report`) from one unit-tier run in the dev environment, labels the top slow specs (suggested threshold: specs/files > ~2s, or the envtest-apiserver-heavy files like `billing_halt_regression_test.go`, `project_controller_test.go`), adds `Label("heavy")`. For the unit tier, the plan must pick one of: **(a)** split `internal/controller` out of the umbrella `go list` (add `| grep -v internal/controller`) into its own `go test` invocation carrying `-ginkgo.label-filter='!heavy'`, or **(b)** gate heavy specs with a `testing.Short()`-style guard so the existing `-short` umbrella run skips them unmodified. Either way the heavy tier runs `./internal/controller/...` with `-ginkgo.label-filter='heavy'` from `test-int-fast`/`test-int` (precedent: the existing Layer A line). Conservation check per Pitfall 6.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test everything Go | ✗ (host Mac) | — | Dev Docker VM (established workflow) or `docker run golang:1.26` |
| Docker | image builds, dev VM, probe step 3 | ✓ | running | — |
| minikube (operator cluster) | COST-03 probe, PVC evidence export | ✓ | host Running / apiserver Running | — |
| helm | chart render assertions | ✓ | v4.2.0 (note: stack pins Helm 3 — verify `helm template` parity or run in dev VM) | dev VM helm 3 |
| kubectl | probe, verification | ✓ | client v1.36.1 | — |
| node | dashboard UI build/tests (vitest) | ✓ | v22.22.3 | — |
| kind | integration Layer B | ✓ (host) | v0.32.0 (go1.26.3 darwin/arm64) | dev VM also available (per CLAUDE.md constrained-VM recipe) |
| `claude` CLI + real ANTHROPIC key | COST-03 probe | operator-side | ≥ v2.1.139 pinned in image | probe recipe step 2 vs 3 |

**Missing with no fallback:** none (everything routes through the operator's established dev VM / minikube workflow).
**Missing with fallback:** Go on the host — use the dev VM (or `docker run golang:1.26`); plans must not assume host-native `make test`. Note: kind v0.32.0 and Docker ARE present on the host — host-native `make test-int` is blocked solely by the missing Go toolchain, not by kind.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go test + Ginkgo v2.28/Gomega (+ envtest); vitest for dashboard/web |
| Config file | Makefile targets (test/test-only/test-int-fast/test-int); dashboard/web vitest config |
| Quick run command | `go test ./internal/subagent/anthropic/ ./cmd/tide/ ./pkg/dispatch/` (in dev VM) |
| Full suite command | `make test` (unit tier) → `make test-int` (integration; kind required) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| COST-01 | New rows + normalizer + run-mix fixture | unit | `go test ./internal/subagent/anthropic/ -run 'TestEstimatedCostCents|TestCostParity|Normalizer|RunMix'` | extend `pricing_test.go` / `cost_parity_test.go` (exists) |
| COST-02 | Envelope flag round-trip; condition set; metric registered | unit + envtest | `go test ./pkg/dispatch/ -run TestUsage` ; controller spec in internal/controller | Wave 0: new condition spec ❌ |
| COST-03 | Multiplier constant matches probe | unit (pins constant) + human-verify checkpoint | `go test ./internal/subagent/anthropic/ -run CacheWrite` | Wave 0 ❌ |
| PROMPT-01 | flag inlining, conflict, doc-count, size/empty caps | unit | `go test ./cmd/tide/ -run TestApply` | extend `cmd_test.go` (exists) |
| TELEM-01 | Doc walkthrough | manual-only (operator follows doc to Targets page) — justification: requires live kube-prometheus-stack | — | — |
| TELEM-02 | NOTES.txt renders + conditional warning | contract test | `hack/helm/assert-telemetry-render.sh` (extend) / `helm template` grep | extend (exists) |
| TELEM-03 | banner states (disabled-by-config vs no-data) | vitest component test | `cd dashboard/web && npx vitest run TelemetryView` | extend TelemetryView tests (exists) |
| DEBT-01 | hardened stamp survives conflict | envtest spec | `go test ./internal/controller/... -run TestControllers -ginkgo.focus='PlannerRolledUpUID'` | extend `child_rollup_idempotency_test.go` family (exists) |
| DEBT-02 | rendered configmap defaults 4 | contract test | `helm template charts/tide --set plannerConcurrency=null \| grep 'plannerConcurrency: 4'` | Wave 0: add render assertion ❌ |
| DEBT-03 | spec-count conservation across tiers | meta (Ginkgo summary arithmetic) | unit-tier + heavy-tier `Ran X of Y` sums equal pre-split total | Wave 0: report step ❌ |

### Sampling Rate
- **Per task commit:** package-scoped `go test` for the touched package (dev VM)
- **Per wave merge:** `make test` (unit tier)
- **Phase gate:** `make test` green AND `make test-int` green (read MAKE_EXIT + grep `^--- FAIL`, per CLAUDE.md) before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] Condition/metric spec for `PricingFallbackActive` + `tide_pricing_fallback_total` — covers COST-02
- [ ] `cacheWriteMultiplier` consistency test (derives every row's write rate) — covers COST-03 encoding
- [ ] Helm render assertion for configmap `default 4` and NOTES.txt — covers DEBT-02/TELEM-02
- [ ] Ginkgo JSON timing report task — feeds DEBT-03 threshold choice

## Security Domain

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | — (no auth surface changes) |
| V3 Session Management | no | — |
| V4 Access Control | no | dashboard stays read-only (binding constraint) |
| V5 Input Validation | yes | `--prompt-file`: size cap before apiserver, empty/whitespace reject, verbatim inlining (no template/shell interpolation of file content); pricing-overrides JSON already fail-fast validated at manager startup (T-14-01) |
| V6 Cryptography | no | — (signing untouched; credproxy tee dir must be 0700 per existing server.go contract — the probe recipe honors it) |

### Known Threat Patterns for this stack
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Oversized CLI file → apiserver/etcd pressure | DoS | CLI size cap (~256 KiB) before the API call (D-11) |
| Teed request bodies contain prompts + billing headers | Information disclosure | 0700 tee dir, operator-local tmpdir, delete after probe (recipe notes) |
| Metric label cardinality abuse via crafted model IDs | DoS (metrics) | model IDs come from operator-authored specs/chart defaults, bounded; fall back to unlabeled counter if needed (A5) |
| stderr-only failure signals lost to pod GC | Repudiation/observability | exactly what COST-02 fixes (condition + metric) |

## Sources

### Primary (HIGH confidence)
- Codebase [VERIFIED: direct reads/greps this session]: `internal/subagent/anthropic/{pricing.go,subagent.go,pricing_test.go,cost_parity_test.go}`, `cmd/tide/apply.go`, `cmd/tide/cmd_test.go`, `internal/controller/{project,milestone,phase,plan}_controller.go`, `internal/controller/billing_halt.go`, `internal/metrics/registry.go`, `pkg/dispatch/envelope.go`, `internal/dispatch/podjob/{jobspec.go,backend.go}`, `cmd/manager/main.go`, `cmd/claude-subagent/main.go`, `cmd/credproxy/main.go`, `internal/credproxy/server.go`, `cmd/tide-spike/main.go`, `charts/tide/{values.yaml,templates/*}`, `hack/helm/*`, `hack/check-pricing-drift.sh`, `Makefile:75-165`, `cmd/dashboard/{main.go,router.go,api/prometheus.go}`, `dashboard/web/src/components/{TelemetryView,TelemetryUnavailableNotice}.tsx`, `docs/INSTALL.md`, `api/v1alpha2/project_types.go`
- claude-api skill (cached 2026-06-24) [VERIFIED]: model pricing table incl. sonnet-5 intro pricing; prompt-caching TTL semantics (`cache_control` shapes, 1.25×/2× write economics)
- Planning artifacts [VERIFIED]: `38-CONTEXT.md`, `REQUIREMENTS.md`, `STATE.md`, `PROJECT.md` §CACHE-01 decision record, `v1.0.6-MILESTONE-AUDIT.md` (W1/W2 verbatim), both folded todos

### Secondary (MEDIUM confidence)
- Anthropic prompt-caching docs as relayed by the skill's `shared/prompt-caching.md` [CITED: platform.claude.com/docs/en/build-with-claude/prompt-caching.md]

### Tertiary (LOW confidence)
- kube-prometheus-stack `release:` label selection behavior [ASSUMED — A1; verify while authoring INSTALL.md]

## Metadata

**Confidence breakdown:**
- Standard stack / pricing rates: HIGH — skill-verified against current published table; existing rows cross-checked
- Architecture (all four clusters): HIGH — every integration point read directly in-tree this session
- COST-03 multiplier: LOW until probe runs — by design (D-07); recipe HIGH
- Pitfalls: HIGH (1–3, 5–7 codebase-verified), MEDIUM (4 — Q2 resolved via headless ruling A6; operator may override at plan approval)

**Research date:** 2026-07-04
**Valid until:** 2026-08-03 (30 days — stable internal codebase), EXCEPT the sonnet-5 intro-pricing note which expires 2026-08-31 and the probe result which is valid per pinned CLI version.
