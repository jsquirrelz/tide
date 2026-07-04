# Phase 38: Small Independents — Pattern Map

**Mapped:** 2026-07-04
**Files analyzed:** 18 new/modified files across 4 independent clusters (COST, PROMPT, TELEM, DEBT)
**Analogs found:** 17 / 18 (INSTALL.md telemetry step is doc-prose only)

**Assumptions:** honors RESEARCH.md's headless Q2 ruling (A6) — a new `prometheus.enabled` values key + dashboard env var is added per D-14's letter. All analog line numbers verified against the worktree this session.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/subagent/anthropic/pricing.go` (sonnet-5 row, normalizer, `cacheWriteMultiplier`) | provider utility | transform (pure) | itself — extend in place | exact |
| `internal/subagent/anthropic/pricing_test.go` + `cost_parity_test.go` (rows, normalizer, run-mix fixture) | test | — | existing tests in same files | exact |
| `pkg/dispatch/envelope.go` (`Usage.PricingFallbackModel`) | model (wire type) | transform | `Usage.CacheSavingsCents` field (lines 308-313) | exact |
| `api/v1alpha2/shared_types.go` (`ConditionPricingFallbackActive` + reason const) | model | — | `ConditionBillingHalt` (line 220) | exact |
| `internal/controller/pricing_fallback.go` (NEW — condition helper) | controller helper | event-driven (envelope→status) | `internal/controller/billing_halt.go` | exact |
| controller rollup call sites (project/milestone/phase/plan/task `_controller.go`) | controller | event-driven | milestone rollup site (`milestone_controller.go:600-636`) | exact |
| `internal/metrics/registry.go` (`tide_pricing_fallback_total`) | config (metric registry) | — | `BudgetOverrunsTotal` registration (registry.go:181-188) | exact |
| `cmd/tide/apply.go` (`--prompt-file`) | CLI command | request-response (CLI→apiserver) | itself — `newApplyCmd`/`runApply` | exact |
| `cmd/tide/cmd_test.go` (`TestApply*` additions) | test | — | `TestApplyRequiresFFlag` (line 153) | exact |
| `hack/helm/augment-tide-chart.sh` (NOTES.txt heredoc, `default 4`) | config source | — | its own configmap.yaml heredoc (lines 80-100) | exact |
| `charts/tide/templates/NOTES.txt` (NET-NEW) | config | — | RESEARCH.md sketch; conditional style from configmap.yaml heredoc | role-match |
| `charts/tide/templates/configmap.yaml:22` (`default 16`→`4`) | config | — | itself (one-token edit, BOTH copies) | exact |
| `charts/tide/values.yaml` + `hack/helm/tide-values.yaml` (`prometheus.enabled`, pricing comment refresh) | config | — | existing `prometheus:` block (values.yaml:327-348) | exact |
| `charts/tide/templates/dashboard-deployment.yaml` (`PROMETHEUS_ENABLED` env) | config | — | `PROM_ENDPOINT` env block (lines 58-67) | exact |
| `cmd/dashboard/main.go` + `cmd/dashboard/api/*` (expose enabled flag) | service (server config) | request-response | `PrometheusEndpoint: os.Getenv("PROM_ENDPOINT")` (main.go:156) + `api/prometheus.go` sentinel | exact |
| `dashboard/web/src/components/TelemetryView.tsx` (+ banner, no-data branch) | component | request-response (fetch→state) | its own `kind` state machine (lines 79-82, 267-315) + `TelemetryUnavailableNotice.tsx` | exact |
| `docs/INSTALL.md` (telemetry walkthrough) | docs | — | existing INSTALL.md section voice (insert after "Verifying the install", ~line 156) | role-match |
| `internal/controller/project_controller.go:1370-1381` (DEBT-01 hardening) | controller | event-driven | `milestone_controller.go:620-633` (WR-02/WR-03 pattern) | exact |
| `Makefile` + heavy-spec `Label("heavy")` edits (DEBT-03) | build config / test | — | `test-leader-election` target (Makefile:93-95) + `test-int-fast` label-filter line | exact |

## Pattern Assignments

### `internal/subagent/anthropic/pricing.go` — sonnet-5 row + normalizer + multiplier

**Analog:** the file itself. Row shape to copy (pricing.go:96-101, the sonnet-4-6 row — same $3/$15 rates as sonnet-5 sticker):

```go
	"claude-sonnet-4-6": {
		inputCentsPerMTok:      300,
		outputCentsPerMTok:     1500,
		cacheReadCentsPerMTok:  30,
		cacheWriteCentsPerMTok: 375,
	},
```

**Fallback pattern to extend, not replace** (pricing.go:165-172): `estimatedCostCents` does `price, ok := a.prices[model]`; on miss it `fmt.Fprintf(os.Stderr, "pricing: unknown model %q, ...")` and uses `conservativeTier` (pricing.go:117: `var conservativeTier = priceTable["claude-fable-5"]`). The D-01 normalizer inserts one date-suffix-stripped retry between exact lookup and fallback; RESEARCH.md's `lookupPrice` sketch (RESEARCH §Code Examples) is the shape. `cacheSavingsCents` (pricing.go:133-150) has the same lookup and must share the normalizer.

**Firewall rules baked into the file header** (pricing.go:19-28): stays behind the Subagent interface; always read `a.prices`, never package-level `priceTable` (T-14-02, pricing.go:155-157).

**D-08 constant:** derive every row's `cacheWriteCentsPerMTok` from `cacheWriteMultNum/Den = 125/100` (RESEARCH sketch) — or keep literals and add a consistency unit test asserting `write == input*125/100` per row. Comment must cite probe evidence (pending; encode 1.25× pre-probe).

### `pkg/dispatch/envelope.go` — `PricingFallbackModel` field

**Analog:** `Usage.CacheSavingsCents` (envelope.go:308-313) — provider-computed, provider-neutral, `omitempty`:

```go
	// CacheSavingsCents is the realized savings in US cents from cache reads
	// ... Computed by the provider (provider firewall D-C1, Phase 21 OBSV-02) ...
	CacheSavingsCents int64 `json:"cacheSavingsCents,omitempty"`
```

Copy this exact doc-comment style: what, who computes it (provider firewall citation), why omitempty. New field: `PricingFallbackModel string \`json:"pricingFallbackModel,omitempty"\`` — provider-neutral name (not "anthropic*").

### `internal/controller/pricing_fallback.go` (new) + condition constant

**Analog:** `internal/controller/billing_halt.go` (whole file — read it before writing). Key excerpt (billing_halt.go:125-135):

```go
	patch := client.MergeFrom(project.DeepCopy())
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:   tideprojectv1alpha2.ConditionBillingHalt,
		Status: metav1.ConditionTrue,
		Reason: tideprojectv1alpha2.ReasonCreditBalanceTooLow,
		Message: "Provider billing 400: credit balance too low. ...",
		LastTransitionTime: metav1.Now(),
	})
	return c.Status().Patch(ctx, project, patch)
```

Copy: file-header doc block explaining lifecycle + provider-firewall note; nil-project safe no-op; `set*IfNeeded(ctx, client, project, ...)` signature returning the patch error for non-fatal logging by callers. Constant lives beside `ConditionBillingHalt = "BillingHalt"` (api/v1alpha2/shared_types.go:220). Difference from analog: `PricingFallbackActive` is informational — no `check*` dispatch gate, no halting (RESEARCH anti-pattern list). Message names the unmatched model ID from `Usage.PricingFallbackModel`.

**Call sites:** all five rollup sites, hooked where `out.Usage` is in hand — e.g. milestone_controller.go:609-611 (`if isFirstCompletion && envReadOK && project != nil { budget.RollUpUsage(...) }`). A shared helper keeps it one implementation (RESEARCH Pitfall 5).

### `internal/metrics/registry.go` — `tide_pricing_fallback_total`

**Analog:** `BudgetOverrunsTotal` (registry.go:181-188):

```go
	BudgetOverrunsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tide_budget_overruns_total",
			Help: "Count of times a Project exceeded its absolute cost cap. Tracked at the Project granularity (Phase 4 D-O2).",
		},
		[]string{"project"},
	)
```

Copy: `tide_*_total` naming, Help string citing the phase/decision ID ("Phase 38 COST-02"), registration inside the same `init()`, labels `{project, model}` (A5: drop `model` if cardinality worries materialize). Add to the file's `MustRegister` block alongside the others.

### `cmd/tide/apply.go` — `--prompt-file`

**Analog:** the file itself. Flag wiring (apply.go:41-58): `c.Flags().StringVarP(&file, "file", "f", ...)`; add `--prompt-file` beside it and thread through `runApply` (the "testable seam", apply.go:61-63).

**Decode-loop change** (apply.go:69-76 — currently single-doc):

```go
	obj := &unstructured.Unstructured{}
	dec := yaml.NewYAMLOrJSONDecoder(bytesReader(raw), 4096)
	if err := dec.Decode(obj); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
```

D-10: loop `dec.Decode` to `io.EOF`, count `Kind == "Project"` docs, error naming the count unless exactly 1. Inject via `unstructured.NestedString`/`SetNestedField` on `spec.outcomePrompt` after the D-09 conflict check.

**Error style to match** (apply.go:107-121, `formatApplyError`): plain `fmt.Errorf` with the offending path/kind/name inline; field-by-field causes for `apierrors.IsInvalid`. Prompt-file errors (conflict, doc count, size cap ~256 KiB, empty file) follow this fail-loud one-line style, checked BEFORE the apiserver call.

### `cmd/tide/cmd_test.go` — `TestApply*` additions

**Analog:** `TestApplyRequiresFFlag` (cmd_test.go:153-158):

```go
	root.SetArgs([]string{"apply"})
```

Convention: build root cmd, `SetArgs`, assert error text — no cluster needed for flag/validation paths. Use `t.TempDir()` temp files for prompt-file content cases (size cap, empty, trailing-newline trim). Table-driven where multiple content cases share shape.

### Chart cluster — NOTES.txt, configmap default, `prometheus.enabled`, dashboard env

**Canonical-source rule (Pitfall 3):** every chart edit lands in BOTH `hack/helm/` (heredoc/source) and rendered `charts/tide/` — the augment script reverts rendered-only edits.

**Analog for heredoc style:** `hack/helm/augment-tide-chart.sh:80-100` (configmap heredoc). The DEBT-02 edit is line 90 (`| default 16` → `| default 4`) mirrored at `charts/tide/templates/configmap.yaml:22`:

```
    plannerConcurrency: {{ .Values.plannerConcurrency | default 16 }}
```

**NOTES.txt:** net-new in both places; content sketch in RESEARCH §Code Examples (conditional on `not .Values.prometheus.enabled`, tight declarative voice, few lines).

**`prometheus.enabled` key:** add under the existing `prometheus:` block (charts/tide/values.yaml:332-348, mirrored in `hack/helm/tide-values.yaml`), default `false`, comment style matching the block's existing ServiceMonitor rationale comment (values.yaml:327-331). Do NOT touch `prometheus.serviceMonitor.enabled` default (binding constraint) and do NOT bump Chart.yaml (D-13).

**Dashboard env:** copy the `PROM_ENDPOINT` block shape at `charts/tide/templates/dashboard-deployment.yaml:58-67` for `PROMETHEUS_ENABLED`. Caution: the file's comment references an EC-7 gate that greps the default render for PROM_ENDPOINT text — check that gate before changing render conditions. Extend `hack/helm/assert-telemetry-render.sh` / `assert-prometheus-env.py` for the new env + NOTES.txt.

### `cmd/dashboard` + `TelemetryView.tsx` — banner (TELEM-03)

**Server analog:** `cmd/dashboard/main.go:156` (`PrometheusEndpoint: os.Getenv("PROM_ENDPOINT")`) — read `PROMETHEUS_ENABLED` the same way and expose via the existing config surface. Sentinel shape to preserve (api/prometheus.go proxy):

```go
	if h.Endpoint == "" {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "unavailable"})
		return
	}
```

**UI analog:** `TelemetryView.tsx` discriminated-union state machine (lines 79-82):

```ts
  | { kind: "loading" }
  | { kind: "data"; series: SeriesPoint[] }
  | { kind: "unavailable" }
  | { kind: "unreachable"; message: string };
```

and the classifier at lines 267-315 (fetch → `kind`). The TELEM-03 work adds a branch: `status:"success"` with empty `result` while enabled → new "no-data" kind, vs env-var-false → disabled-by-config banner. Banner component copies `TelemetryUnavailableNotice.tsx`'s shape/copy tone. Tests: extend existing TelemetryView vitest tests (`npx vitest run TelemetryView` — runs on host node 22).

### `internal/controller/project_controller.go:1370-1381` — DEBT-01 hardening

**Analog (copy verbatim, adapted to Project/PlannerRolledUpUID):** `milestone_controller.go:620-633`:

```go
	if mErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &tideprojectv1alpha2.Milestone{}
		if err := r.Get(ctx, client.ObjectKeyFromObject(ms), latest); err != nil {
			return err
		}
		if latest.Status.MilestoneRolledUpUID == milestoneJobName {
			return nil // already set by a concurrent reconcile — idempotent
		}
		markerPatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
		latest.Status.MilestoneRolledUpUID = milestoneJobName
		return r.Status().Patch(ctx, latest, markerPatch)
	}); mErr != nil {
		return ctrl.Result{}, fmt.Errorf("patch MilestoneRolledUpUID: %w", mErr)
	}
```

Also copy the WR-02/WR-03 comment block above it (lines 614-619) explaining why: durable stamp vs concurrent writers, return-don't-swallow. Preserve the "marker only after successful rollup" ordering (comment at project_controller.go:1374). Sibling copies at phase_controller.go:537-563, plan_controller.go:611-638. Test analog: `child_rollup_idempotency_test.go` family.

### Makefile + heavy labels — DEBT-03

**Analogs:**
- Extraction precedent (Makefile:93-95): `test-leader-election` runs `go test ./internal/controller/... -ginkgo.focus="Leader Election"`; unit tier excludes it via `testing.Short()` guard (leader_election_test.go:54) + `-short` on the umbrella run (Makefile:85-86).
- Label-filter precedent (test-int-fast target): `go test ./test/integration/envtest/... -ginkgo.v --ginkgo.label-filter='envtest'`.

**Constraint (RESEARCH Open Q4):** the umbrella `make test` invocation covers non-Ginkgo packages — `-ginkgo.label-filter` there fails with "flag provided but not defined". Pick (a) split `internal/controller` out of the umbrella `go list` into its own invocation carrying `-ginkgo.label-filter='!heavy'`, or (b) `testing.Short()`-style guards. Heavy tier runs from `test-int-fast`/`test-int` with `-ginkgo.label-filter='heavy'`. Conservation check per Pitfall 6 (Ginkgo `Ran X of Y` sums, not grep counts).

## Shared Patterns

### Provider firewall
**Source:** pricing.go header comment (lines 19-24) + billing_halt.go firewall note (lines 36-39).
**Apply to:** all COST files — pricing logic stays in `internal/subagent/anthropic/`; envelope fields and controller helpers are provider-neutral.

### Sticky typed conditions
**Source:** `billing_halt.go` + `api/v1alpha2/shared_types.go:220`.
**Apply to:** `PricingFallbackActive` (informational, non-gating).

### Chart dual-source edits
**Source:** `hack/helm/augment-tide-chart.sh` ↔ `charts/tide/templates/*`; `hack/helm/tide-values.yaml` ↔ `charts/tide/values.yaml`.
**Apply to:** NOTES.txt, configmap default, `prometheus.enabled`, dashboard env — every chart file, both copies, no Chart.yaml bump.

### Fail-loud CLI errors
**Source:** `cmd/tide/apply.go` `formatApplyError` + inline `fmt.Errorf` style.
**Apply to:** all `--prompt-file` validation errors (before the apiserver).

### Test-environment reality
**Source:** RESEARCH Pitfall 7 / Environment Availability. Go toolchain absent on host — Go tests run in the dev VM (or `docker run golang:1.26`); vitest runs on host.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `docs/INSTALL.md` telemetry step | docs | — | Pure prose; no code analog. Follow the doc's existing section voice; content spec is RESEARCH §"TELEM-01 `release:` label fix" (insert ~line 156, verify A1 against kube-prometheus-stack README while authoring). |

## Metadata

**Analog search scope:** `internal/subagent/anthropic/`, `internal/controller/`, `internal/metrics/`, `pkg/dispatch/`, `cmd/tide/`, `cmd/dashboard/`, `dashboard/web/src/components/`, `charts/tide/`, `hack/helm/`, `Makefile`
**Files scanned:** 14 read/grepped directly this session (RESEARCH.md had pre-verified the rest)
**Pattern extraction date:** 2026-07-04
