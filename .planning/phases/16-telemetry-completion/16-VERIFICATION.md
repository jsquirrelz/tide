---
phase: 16-telemetry-completion
verified: 2026-06-12T22:05:00Z
status: gaps_found
score: 5/6 must-haves verified
overrides_applied: 0
gaps:
  - truth: "The All projects toggle switches to by (project) aggregates and renders a per-project series for each project"
    status: partial
    reason: "Queries and the budget-card grid are correct, but in all-projects scope every project's Prometheus matrix collapses onto a single fixed legend key, so the Cost / Dispatch Counts / Failure Rate charts silently render one arbitrary project's values mislabeled as the cluster aggregate (CR-01). Project-scope charts are unaffected."
    artifacts:
      - path: "dashboard/web/src/components/TelemetryView.tsx"
        issue: "Line 329: `const key = sd.key ?? matrix.metric[\"project\"] ?? ...` — every SeriesDef in PANELS sets `key`, so the `matrix.metric[\"project\"]` fallback is dead. matrixToPoints (line 347) then overwrites: last project in the result array wins for the whole panel."
    missing:
      - "Derive the series key from matrix.metric[\"project\"] when scope === \"all\" and the query is project-grouped (pass `scope` into fetchPanel's merge step)"
      - "Add a Vitest case: all-projects scope + multi-result success payload (result: [{metric:{project:\"p1\"},...},{metric:{project:\"p2\"},...}]) asserting two distinct series keys render"
deferred: []
---

# Phase 16: Telemetry Completion Verification Report

**Phase Goal:** The merged telemetry foundation is functional end-to-end — PROM_ENDPOINT drives the PromQL proxy, TelemetryView is mounted in AppShell, the six locked metrics emit with correct labels, PromQL panel names match, the Makefile gate is wired, and the proxy client is hardened
**Verified:** 2026-06-12T22:05:00Z
**Status:** gaps_found
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| #   | Truth (Roadmap Success Criterion)                                                                                          | Status      | Evidence                                                                                                                                                                                                                                              |
| --- | -------------------------------------------------------------------------------------------------------------------------- | ----------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | Dashboard reads `PROM_ENDPOINT` from injected env and passes it to the PromQL proxy; helm value change ⇒ endpoint change (TELEM-01) | ✓ VERIFIED  | `cmd/dashboard/main.go:156` `PrometheusEndpoint: os.Getenv("PROM_ENDPOINT")` → `Dependencies` → `router.go:162-163` `PrometheusHandler{Endpoint: deps.PrometheusEndpoint}`. Full chain wired, no code change needed to repoint.                          |
| 2   | AppShell renders a Telemetry tab that mounts TelemetryView; Vitest covers both degradation shapes (200 unavailable / 502 error) (TELEM-02) | ✓ VERIFIED  | `App.tsx:291-296` mounts `<TelemetryView projects={projects} selectedProject={selectedProject}/>` on `activeView==="telemetry"`; `ViewSwitcher` tablist at :145-165, `Header.viewSwitcher` slot. TelemetryView.test.tsx asserts `getAllByTestId("telemetry-unavailable-notice")` length 4 for both 200-sentinel and 502 shapes. |
| 3   | TaskReconciler terminal branches emit all six locked metrics with `{project, phase, wave}` labels matching MILESTONE.md (TELEM-03) | ✓ VERIFIED  | `registry.go:185-227` six metrics, `[]string{"project","phase","plan","wave"}` on all six (superset of SC — `plan` added per D-10), single MustRegister. `emitTaskMetrics` (task_controller.go:1011) emits all six at the 3 RollUpUsage seams (:864/:896/:952); wave from OwnerRef, phase from PlanRef→PhaseRef, "unknown" sentinel. |
| 4   | All four TelemetryView PromQL panels query the locked metric names — dead names replaced (TELEM-04)                          | ✓ VERIFIED  | `grep -rc tide_tasks_dispatched_total\|tide_tokens_used_total dashboard/web/src/` = 0 tree-wide. PANELS (TelemetryView.tsx:127-227) query only registered names in both scope modes. `tide_waves_dispatched_total` present in both project + all-projects builders. |
| 5   | `make helm-rbac-assert` + telemetry gate scripts execute and pass; Makefile targets wired and documented (TELEM-05)         | ✓ VERIFIED  | `make helm-assert` run live → exit 0; helm-rbac-assert + both assert-prometheus-env.py permutations + all 4 EC-7 render permutations PASS. `helm-telemetry-assert`/`helm-assert` targets present; `make helm-assert` step in ci.yaml helm-lint job; false docstring claims removed from hack/helm scripts. |
| 6   | PrometheusHandler uses a bounded HTTP client (timeout + ctx propagation) and preserves base paths (TELEM-06)                | ✓ VERIFIED  | `prometheus.go:43,49` `proxyTimeout = 30s` + `proxyClient = &http.Client{Timeout: proxyTimeout}`; :99 `NewRequestWithContext(r.Context(), ...)`; :96 `strings.TrimRight(upstream.Path, "/") + path`. No `http.DefaultClient`, no `nolint:noctx`. Three-shape degradation contract intact (:80-136). |

**Score:** 5/6 truths verified (truth #2 mount/degradation verified; the all-projects sub-behavior of the TelemetryView is the partial — see Gaps)

> Note: All six ROADMAP success criteria for the named requirements pass at the criterion level. The single gap (CR-01) is a correctness defect inside the all-projects scope of the now-mounted TelemetryView — deeper than what SC #4's "query the locked names" wording literally requires, but it falsifies the plan 16-04 truth "the All projects toggle switches to by (project) aggregates [with per-project series]" at the rendering layer.

### Required Artifacts

| Artifact                                              | Expected                                                           | Status     | Details                                                                                  |
| ----------------------------------------------------- | ----------------------------------------------------------------- | ---------- | ---------------------------------------------------------------------------------------- |
| `cmd/dashboard/api/prometheus.go`                     | Bounded proxy client, ctx propagation, base-path join             | ✓ VERIFIED | All three fixes substantive; degradation contract preserved (138 lines).                 |
| `internal/config/config.go`                           | PrometheusEndpoint field + PROM_ENDPOINT override                 | ⚠️ ORPHANED | Field + Load override + 3 tests exist, but no binary consumes `cfg.PrometheusEndpoint` (dashboard reads env directly; manager never reads it). Dead config — WR-03. Working mechanism (env) is unaffected. |
| `cmd/dashboard/main.go`                               | PROM_ENDPOINT → Dependencies.PrometheusEndpoint                   | ✓ VERIFIED | :156 wired and threads to the handler.                                                   |
| `internal/metrics/registry.go`                        | Six locked metrics, 4-label arity, D-11 buckets                   | ✓ VERIFIED | :185-227 all six; single MustRegister; taskDurationBuckets 30s–2h.                       |
| `internal/controller/task_controller.go`              | resolveWave + emitTaskMetrics + 3 emission sites                  | ✓ VERIFIED | emitTaskMetrics:1011, 3 call sites at RollUpUsage seams.                                  |
| `dashboard/web/src/components/TelemetryView.tsx`      | D-06 queries, recharts panels, scope/range toolbar, polling, grid | ⚠️ HOLLOW (all-projects) | recharts charts, toolbar, polling, budget grid all present; project-scope correct; **all-projects series collapse (CR-01)**. |
| `dashboard/web/src/components/__tests__/TelemetryView.test.tsx` | Vitest: both degradation shapes, scope/range/grid/chart   | ✓ VERIFIED | Covers both locked shapes (4 notices each), scope queries, range, budget grid, chart svg. Gap: no all-projects multi-result render assertion (why CR-01 slipped). |
| `dashboard/web/package.json`                          | recharts@3.8.1 exact pin                                          | ✓ VERIFIED | `"recharts": "3.8.1"`.                                                                    |
| `dashboard/web/src/App.tsx`                           | activeView state, ViewSwitcher, telemetry body branch            | ✓ VERIFIED | :183 state, :291-296 branch, :385 viewSwitcher prop.                                      |
| `dashboard/web/src/components/Header.tsx`             | viewSwitcher optional slot                                       | ✓ VERIFIED | Slot present and rendered.                                                               |
| `Makefile`                                            | helm-telemetry-assert + helm-assert targets                      | ✓ VERIFIED | Both targets; `make helm-assert` exit 0 live.                                            |
| `.github/workflows/ci.yaml`                           | make helm-assert in helm-lint job                                | ✓ VERIFIED | One `make helm-assert` step in helm-lint.                                                |

### Key Link Verification

| From                          | To                                  | Via                                          | Status    | Details                                                       |
| ----------------------------- | ----------------------------------- | -------------------------------------------- | --------- | ------------------------------------------------------------ |
| main.go                       | PrometheusHandler.Endpoint          | os.Getenv → Dependencies → router            | ✓ WIRED   | main.go:156 → router.go:162-163.                             |
| task_controller.go            | tidemetrics registry vars           | WithLabelValues at 3 RollUpUsage seams       | ✓ WIRED   | emitTaskMetrics emits all six.                              |
| task_controller resolveWave   | Wave CRD name                       | OwnerReferences Kind=="Wave"                 | ✓ WIRED   | :1 occurrence, "unknown" sentinel on miss.                  |
| App.tsx                       | TelemetryView                       | telemetry body branch w/ props contract      | ✓ WIRED   | :296 props match 16-04 contract.                            |
| Makefile helm-telemetry-assert | assert-prometheus-env.py            | two python3 invocations                      | ✓ WIRED   | --expect-absent + --expect-endpoint both PASS.             |
| ci.yaml helm-lint             | Makefile helm-assert                | run: make helm-assert                        | ✓ WIRED   | step present.                                                |

### Data-Flow Trace (Level 4)

| Artifact            | Data Variable        | Source                              | Produces Real Data | Status      |
| ------------------- | -------------------- | ----------------------------------- | ------------------ | ----------- |
| TelemetryView (Cost / Token panels, project scope) | panelStates ← fetchQueryRange → /api/v1/query_range → Prometheus | tide_cost_cents_total / tide_tokens_*_total (emitted by emitTaskMetrics) | ✓ FLOWING | Metrics are emitted at terminal seams; queries reference them. |
| TelemetryView (Dispatch Counts, Failure Rate panels) | same | tide_waves_dispatched_total / tide_tasks_completed_total / tide_tasks_failed_total | ✗ STATIC | Metrics are **registered but never incremented** in production code (CR-02). Both panels will render "No data in range" on every cluster, permanently. Knowingly declared out-of-scope in plan 16-04 context. |
| TelemetryView (all-projects Cost/Dispatch/Failure) | merged series keys | line 329 key derivation | ✗ HOLLOW | Multi-project series collapse onto one key — wrong numbers rendered (CR-01). |

### Behavioral Spot-Checks

| Behavior                                   | Command                          | Result   | Status  |
| ------------------------------------------ | -------------------------------- | -------- | ------- |
| go build compiles all packages             | `go build ./...`                 | exit 0   | ✓ PASS  |
| Helm render gate suite executes and passes | `make helm-assert`               | exit 0, all RBAC + 2 prom-env + 4 EC-7 permutations PASS | ✓ PASS  |
| Dead metric names absent tree-wide         | `grep -rc tide_tasks_dispatched_total\|tide_tokens_used_total dashboard/web/src/` | 0 | ✓ PASS  |
| Dashboard Vitest (per evidence)            | 26 files / 196 tests             | passed   | ✓ PASS (orchestrator evidence) |
| `make test` (per evidence)                 | /tmp/16-postmerge-wave2-test.log | exit 0   | ✓ PASS (orchestrator evidence) |

### Probe Execution

No probes declared for this phase (non-migration, render-gate based). Render gate executed via `make helm-assert` — see Behavioral Spot-Checks.

### Requirements Coverage

| Requirement | Source Plan        | Description                                              | Status      | Evidence                                                       |
| ----------- | ------------------ | ------------------------------------------------------- | ----------- | ------------------------------------------------------------- |
| TELEM-01    | 16-01              | Dashboard reads PROM_ENDPOINT into the proxy            | ✓ SATISFIED | main.go:156 → router → handler wired.                         |
| TELEM-02    | 16-04, 16-05       | TelemetryView mounted as tab; both degradation shapes covered | ✓ SATISFIED | App.tsx:291-296 mount; Vitest 4-notice assertions for 200+502. |
| TELEM-03    | 16-02              | Six locked metrics emitted with labels                  | ✓ SATISFIED | registry.go:185-227 + emitTaskMetrics 3 seams.               |
| TELEM-04    | 16-04              | Panels query locked names; dead names replaced          | ✓ SATISFIED | 0 dead names tree-wide; correct names in both scope modes.    |
| TELEM-05    | 16-03              | hack/helm gates wired into Makefile + CI                | ✓ SATISFIED | `make helm-assert` exit 0; ci.yaml step present.             |
| TELEM-06    | 16-01              | Bounded client + ctx propagation + base-path            | ✓ SATISFIED | prometheus.go:43/49/96/99.                                   |

All six declared requirement IDs accounted for; no orphaned requirement IDs map to Phase 16 in REQUIREMENTS.md. All criterion-level requirements pass; CR-01 is a within-criterion correctness defect surfaced below.

### Anti-Patterns Found

| File                              | Line | Pattern                                  | Severity   | Impact                                                                 |
| --------------------------------- | ---- | ---------------------------------------- | ---------- | --------------------------------------------------------------------- |
| TelemetryView.tsx                 | 329  | Dead-fallback key derivation             | 🛑 Blocker | All-projects series collapse — wrong numbers rendered (CR-01).        |
| TelemetryView.tsx                 | 151,158,174 | Panels query never-emitted metrics | ⚠️ Warning | Dispatch Counts + Failure Rate render "No data" forever (CR-02). Knowingly deferred in plan 16-04 context; no later phase exists to land emission (Phase 16 is last in milestone). |
| internal/config/config.go         | 48-51,116-123 | Dead config field                  | ⚠️ Warning | `prometheusEndpoint` YAML key consumed by no binary (WR-03). Misleading operator surface; working env mechanism unaffected. |
| internal/controller/task_controller.go | 1040-1043 | Unguarded negative duration         | ⚠️ Warning | Observe(negative) can drag histogram _sum negative on stale envelope / clock skew (WR-04). |

No `TBD`/`FIXME`/`XXX` debt markers in any modified file.

### Human Verification Required

None deferred via planner `<human-check>` blocks. The all-projects rendering defect (CR-01) is decided by code inspection, not human-uncertain — it is reported as a gap, not a human item.

### Gaps Summary

Five of six requirement criteria are fully and substantively verified, with live gate evidence (`make helm-assert` exit 0) and a clean `go build`. The phase goal — telemetry functional end-to-end — is largely achieved: the proxy is hardened and endpoint-driven, the six metrics emit with correct labels at the terminal seams, the panels query only registered names, the gate is wired into CI, and the Telemetry tab mounts with working degradation handling and correct project-scope charts.

One real correctness gap blocks a clean PASS:

- **CR-01 — all-projects series collapse.** In all-projects scope, `fetchPanel` (TelemetryView.tsx:329) keys every project's Prometheus matrix with the panel's fixed `sd.key`, so `matrixToPoints` overwrites all but the last project. The Cost Over Time / Dispatch Counts / Failure Rate panels then display one arbitrary project's data labeled as the cluster aggregate. The multi-series legend machinery can never activate. No test covers the all-projects multi-result success path, which is why it slipped. This falsifies the "All projects toggle switches to by (project) aggregates [rendering per-project series]" truth at the rendering layer. Project-scope charts are unaffected.

Two warnings do not block but should be tracked (no later milestone phase exists to defer to — Phase 16 is the last v1.0.1 phase):

- **CR-02 — dispatch-count panels query never-emitted metrics.** `tide_waves_dispatched_total` / `tide_tasks_completed_total` / `tide_tasks_failed_total` are registered but never `.Inc()`/`.Add()`-ed in production code, so the Dispatch Counts and Failure Rate panels render "No data in range" on every cluster forever. This was knowingly declared out-of-scope in plan 16-04's context ("emission lands in a later phase"), but no later phase exists. If accepted as intentional for v1.0.1, record an override; otherwise emission belongs in this phase's gap-closure.

**This looks partially intentional (CR-02).** To accept the never-emitted dispatch panels as a deliberate v1.0.1 deferral, add to this VERIFICATION.md frontmatter:

```yaml
overrides:
  - must_have: "All four TelemetryView PromQL panels query the locked metric names"
    reason: "Dispatch Counts / Failure Rate panels query Phase-4 counters registered but not yet emitted; emission deliberately deferred per plan 16-04 context. SC #4 requires query-name correctness, which is satisfied."
    accepted_by: "<name>"
    accepted_at: "<ISO timestamp>"
```

---

_Verified: 2026-06-12T22:05:00Z_
_Verifier: Claude (gsd-verifier)_
