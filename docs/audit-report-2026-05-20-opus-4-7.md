# TIDE codebase audit — verified findings

**Date:** 2026-05-20
**Model:** Claude Opus 4.7 (1M context)
**Method:** Draft → fact-check questions → independent parallel verification → final reconciliation.

> Drafted observations from direct reads of `cmd/manager/main.go`, `internal/controller/*.go`,
> `Makefile`, `.golangci.yml`, `.github/workflows/`, `go.mod`, `charts/tide/values.yaml`,
> `tools/analyzers/*`, then dispatched five independent Explore agents to verify 15 fact-shaped
> questions in parallel. Each agent saw only its own questions, never the draft. Final report
> below reflects the corrections.

---

## Best practices — verified

### 1. Architectural invariants encoded as code, not convention

Three CI gates work together and protect different invariants:

- **Custom static analyzers**, registered in `cmd/tide-lint`, run via `make tide-lint`:
  - `tools/analyzers/crosspool/analyzer.go:1` — rejects `select` statements that wait on both planner and executor pool channels (POOL-03 / Pitfall 6).
  - `tools/analyzers/providerfirewall/analyzer.go:1` — rejects LLM SDK imports from orchestrator-side packages (`pkg/controller/*`, `pkg/dispatch/*`, `pkg/dag/*`, `internal/controller/*`, `internal/webhook/*`, `internal/dispatch/*`).
  - `tools/analyzers/metriccardinality/doc.go:1` — rejects the literal `"task"` label in any `prometheus.New*Vec` call (OBS-02 / Pitfall 17).
- **Shell-based `verify-*` Makefile targets** for invariants that grep can express more cheaply than AST analysis: `verify-dag-imports`, `verify-no-aggregates`, `verify-no-sqlite-dep`, `verify-no-rbac-wildcards`, `verify-rbac-marker-discipline`, `verify-no-blocking`. `.github/workflows/ci.yaml` runs each as a named step (lines 51–66).
- **In-source guard tests** for things that should be runtime-checked, e.g. `internal/controller/rbac_guard_test.go:33,62,89` asserts the wildcard-RBAC regex catches violations and stays silent on clean files.

This layered enforcement (analyzer + shell-grep + go test) means the same invariant can't be silently broken if one detection path fails.

### 2. Test infrastructure

- 121 `_test.go` files vs 133 non-test, non-generated source files (≈0.91:1).
- Eight `test*` Makefile targets stratified by cost: `test` (envtest, <30s budget), `test-int-fast` (Layer A envtest, ~90s), `test-int` (Layer A + Layer B kind, 30 min outer / 20 min inner), `test-e2e` and `test-e2e-kind` (kind, 15 min), `test-e2e-live` (nightly with `ANTHROPIC_API_KEY` + 15 min budget).
- Live-API tests have triple gating: `//go:build live_e2e` tag, BeforeSuite env-var check, and a `$1.00` per-run hard cap via `Project.Spec.budget.absoluteCapCents=100` (`test/e2e/testdata/live-claude-project.yaml`).
- Tests with the `Dispatcher` seam use `internal/controller/task_controller_test.go:48` `stubDispatcher` with a `var _ dispatch.Dispatcher = (*stubDispatcher)(nil)` compile-time assertion (line 54).

### 3. Forbidden patterns: clean

Searches across `cmd/`, `internal/`, `pkg/`, `api/` (excluding tests, generated, vendored, and `.claude/`):

| pattern | hits |
|---|---|
| `TODO` / `FIXME` / `XXX` / `HACK` | 0 |
| bare `panic(` | 0 |
| `time.Sleep(` | 0 |
| `<-time.After(` | 1 — `cmd/stub-subagent/main.go:263` (intentional test-mode handler; not a reconciler) |

### 4. Anthropic SDK boundary tighter than the design document requires

The `providerfirewall` analyzer prevents LLM SDK imports from controllers/dispatch. The actual import surface is even smaller than that:

- **Zero** Anthropic SDK imports in `cmd/`, `internal/`, or `pkg/` Go source.
- The only mentions are doc-comment references in `pkg/dispatch/doc.go` and `pkg/dag/doc.go` (explaining what is *forbidden*).
- The Anthropic client is invoked through `internal/subagent/anthropic/` and surfaced behind the `Subagent` interface in `pkg/dispatch/subagent.go:17`.

### 5. Six-step Reconcile pattern, applied uniformly

All six reconcilers (`Project`, `Milestone`, `Phase`, `Plan`, `Wave`, `Task`) follow: Fetch → DeletionTimestamp → Finalizer → OwnerRef → Dispatcher/business seam → Status update. Divergences are documented and intentional: `ProjectReconciler` skips step 4 (top-level CRD); `Plan`/`Wave`/`Task` log-and-continue on missing parent so dispatch doesn't stall.

### 6. Single, well-leveraged field indexer

`mgr.GetFieldIndexer().IndexField(...)` registers exactly one index — `Task.spec.planRef` at `internal/controller/task_controller.go:931` (constant `taskPlanRefIndexKey` at line 62). It serves five call sites: `TaskReconciler.listSiblingTasks` (line 635), `PlanReconciler.reconcilePlannerDispatch` (line 189) and `.reconcileWaveMaterialization` (line 481), `WaveReconciler.reconcileObservational` (line 150), and `TaskReconciler.siblingsToTaskMapper` (line 820). No full-namespace `List`s on the hot path. (Milestone/Project lookups stay unindexed — defensible given hierarchy shallowness.)

### 7. Manager startup is properly fail-fast

`cmd/manager/main.go` does six things before `mgr.Start()` (line 448), with clear fail-fast vs best-effort intent:

- Fail-fast: config load (212), signing-key length check (274), HMAC self-test round-trip (285), OTel boot (192), both webhooks (419–425).
- Best-effort with logging: planner pool PreCharge (262), executor pool PreCharge (265), budget PreCharge runnable (437–445).
- OTel shutdown is bounded at 5s under `context.Background()` because `signalCtx` is already cancelled when the defer fires (lines 196–206).

### 8. Strict lint baseline

`.golangci.yml` enables 19 linters with `default: none`: `copyloopvar, dupl, errcheck, ginkgolinter, goconst, gocyclo, govet, ineffassign, lll, modernize, misspell, nakedret, prealloc, revive, staticcheck, unconvert, unparam, unused, logcheck` (the last via the k8s `logcheck` module). Path-scoped relaxations: `lll` is exempted under `api/*`; `dupl` + `lll` under `internal/*`. Formatters: `gofmt` + `goimports`.

### 9. Spec → code fidelity

Spot-checks of the spec's stated stack choices:

- Dashboard streaming is SSE (`Content-Type: text/event-stream` at `cmd/dashboard/api/events_sse.go:180` and `logs_sse.go:154`); zero WebSocket imports anywhere.
- No `argoproj` / `tektoncd` references in source.
- `charts/tide/values.yaml:269` defaults `prometheus.serviceMonitor.enabled: false` — matches the named anti-pattern.
- `go.mod` has no SQL driver (no `sqlite`, `pgx`, `lib/pq`, `go-sql-driver/mysql`).

---

## Findings worth attention

### F1 — `tools/analyzers/dagimports/` exists but isn't wired into the lint binary

The analyzer's doc claims it rejects forbidden imports under `/pkg/dag/`, but `cmd/tide-lint` only registers `crosspool`, `providerfirewall`, and `metriccardinality`. The load-bearing CI gate for DAG-05 is the shell-based `verify-dag-imports` Makefile target. The analyzer is effectively dead code or a test fixture. Either delete it, mark it as such in its doc, or wire it into `cmd/tide-lint`.

### F2 — Two `verify-*` Makefile targets aren't named in `ci.yaml`

`verify-dispatch-imports` and `verify-import-firewall` exist as Make targets but don't appear as named steps in `.github/workflows/ci.yaml`. The latter is functionally covered because the providerfirewall analyzer runs via `make tide-lint` (ci.yaml line 69). `verify-dispatch-imports` may be redundant with `tide-lint` too, but the discrepancy is worth a one-line note or a CI step for parity with the others.

### F3 — Reconciler size hotspots

Three files in `internal/controller/` exceed 700 lines with one large method each:

| file | total | longest method | size |
|---|---|---|---|
| `task_controller.go` | 976 | `reconcileDispatch` (line 194) — 12-step dispatch body | 268 lines |
| `plan_controller.go` | 837 | `maybePauseForWaveApprove` (line 569) | 129 lines |
| `project_controller.go` | 760 | `reconcilePhase3Lifecycle` (line 363) | 200 lines |

The lengths are justified by the dispatcher seam pattern (the alternative — six tiny indirection helpers — would obscure the K8s reconcile flow). But `reconcileDispatch` at 268 lines is at the upper edge; extracting the budget gate, indegree compute, gate-policy hook, and Job-create steps into named methods would make the dispatch body readable without changing the reconcile shape.

### F4 — `TaskReconciler` carries 10+ injected fields

`PlannerPool`, `ExecutorPool`, `Dispatcher`, `Budget`, `Defaults`, `SigningKey`, `SubagentImage`, `CredproxyImage`, `EnvReader`, `WatchNamespace`, plus the embedded `client.Client`, `Scheme`, `MaxConcurrentReconciles`. Each is independently necessary today; if more land, consolidate into a `TaskReconcilerDeps` carrier (mirrors the `HelmProviderDefaults` pattern already in use for Milestone/Phase/Plan).

### F5 — Live-E2E workflow exists in docs only

`make test-e2e-live` is fully implemented and gate-protected, but no `.github/workflows/live-e2e.yml` is committed — `docs/live-e2e.md` provides a template for operators to wire it themselves. If the intent is "nightly Anthropic-billed run," wiring the workflow (with `secrets.ANTHROPIC_API_KEY` and a `schedule:` trigger) would close the gap; if the intent is "self-host this when you fork," call that out in the doc.

### F6 — `TIDE_PUSH_IMAGE` and `CLAUDE_SUBAGENT_IMAGE` default to `:v0.1.0-dev` tags in source

`cmd/manager/main.go:164,165`. Fine as Helm-overridden defaults; in production the chart's `values.yaml` should always set them. Worth a `// PROD_OVERRIDE_REQUIRED` comment so future maintainers don't accept the dev tag thinking it's a release-stable placeholder.

---

## Items the draft claimed that verification refuted (removed)

- **".claude/worktrees/ orphan copies of internal/"** — yes there are 12, but `.gitignore` covers `.claude/`, so they never enter the repo. Local dev hygiene, not a code-quality concern.
- **"bin/k8s/1.34.0-* committed binaries"** — `git ls-files bin/k8s/` returns 0; properly ignored.
- **"Strict golangci-lint with 20+ linters"** — actual count is 19. Off by one.

---

## Summary

This codebase shows discipline well above the median Kubernetes operator project. The dominant pattern — *encode every architectural decision as a runnable check (analyzer, Makefile target, or guard test)* — is the single most important best practice on display. The findings worth attention (F1, F2, F5) are housekeeping; F3 and F4 are normal scaling tension in a controller that's accreting features. Nothing here blocks shipping; F1 (dead analyzer) and F5 (missing CI workflow) would each be a 10-line PR.
