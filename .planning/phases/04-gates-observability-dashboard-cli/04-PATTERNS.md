# Phase 04 — Gates, Observability, Dashboard, CLI — Pattern Map

**Mapped:** 2026-05-16
**Files analyzed:** ~55 new + ~10 modified
**Analogs found:** 41 / 55 (14 are GREENFIELD — frontend, OTel SDK init, chi+SSE, cobra entrypoints, .goreleaser, Krew manifest)

This map answers "what should each new file copy from?" for the gsd-planner. Concrete excerpts (with file:line spans) are gathered by pattern family below the classification table.

---

## File Classification

### Go: new packages + modified reconcilers

| New / Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `pkg/otelai/attrs.go` | utility (public) | transform (helpers returning slices) | `internal/owner/owner.go` (small public helper pkg, pure-func surface) | role-partial — semantically OTel attrs are bespoke; structural pkg shape only |
| `pkg/otelai/attrs_test.go` | test | unit | `internal/budget/cap_test.go` (table-driven pure-func tests) | role-match |
| `pkg/otelai/doc.go` | doc | n/a | `api/v1alpha1/shared_types.go:17-23` (package-level doc-comment pattern) | role-match |
| `internal/gates/policy.go` | utility (private) | transform (config → decision) | `internal/budget/cap.go` (pure-func reading CRD field, returning bool) | exact |
| `internal/gates/annotation.go` | utility (private) | transform (CRD annotations → decision) | `internal/budget/precharge.go` + `project_controller.go:323-356` bypass-annotation handler | role-match |
| `internal/gates/boundary.go` (D-W2 shared seam) | utility (private) | transform | `internal/controller/push_helpers.go:309-359` (`buildCommitMessage` — pure-func boundary classifier) | role-match |
| `internal/gates/policy_test.go` | test | unit | `internal/budget/cap_test.go` | exact |
| `internal/metrics/registry.go` | metrics registration | startup | `internal/budget/metrics.go` (CounterVec + `metrics.Registry.MustRegister` in `init()`) | **EXACT** |
| `internal/metrics/registry_test.go` | test | unit | none — `internal/budget/` has no metrics test; the registration is asserted indirectly via Pitfall-17 cardinality unit test (new) | partial |
| `internal/otelinit/provider.go` | startup wiring | bootstrap | `cmd/credproxy/main.go:96-135` (provider construct + signal-driven Shutdown) + `cmd/manager/main.go:128-132` (zap-behind-logr init) | role-partial (no current OTel init in repo — see GREENFIELD note) |
| `internal/otelinit/provider_test.go` | test | unit | none in-repo; new pattern | GREENFIELD |
| `tools/analyzers/metriccardinality/analyzer.go` | static analysis | transform (AST → diagnostics) | `tools/analyzers/providerfirewall/analyzer.go` (entire file template; AST.Imports → forbid-list) | **EXACT** |
| `tools/analyzers/metriccardinality/analyzer_test.go` | test | unit | analogous `tools/analyzers/providerfirewall/analyzer_test.go` (analysistest fixtures) | exact (assumed parallel) |
| `tools/analyzers/metriccardinality/testdata/...` | test fixture | n/a | `tools/analyzers/providerfirewall/testdata/` (valid/* + violation/* with `// want` directives) | exact |
| `api/v1alpha1/project_types.go` (modify: add `PhasePushLeakBlocked`) | API const | n/a | same file lines 297-316 (`PhasePushLeaseFailed` definition pattern) | self-referential |
| `api/v1alpha1/shared_types.go` (modify: add `ConditionWaveOrLevelPaused`, `ReasonAwaitingApproval`, `ReasonRejected`, `ReasonResumed`) | API const | n/a | same file lines 74-93 (`ConditionPushLeaseFailed` block pattern) | self-referential |
| `internal/controller/milestone_controller.go` (modify: gate hook + boundary push) | reconciler | request-response | same file lines 91-153 (existing 6-step body) and `handleJobCompletion` lines 241-274 (the seam) | self-referential |
| `internal/controller/phase_controller.go` (modify: same as milestone) | reconciler | request-response | `milestone_controller.go` template | exact |
| `internal/controller/plan_controller.go` (modify: same + wave-pause hook) | reconciler | request-response | `milestone_controller.go` template + `wave_controller.go` annotation-watch (for `PauseBetweenWaves`) | role-match |
| `internal/controller/wave_controller.go` (modify: wave-pause annotation watcher) | reconciler | event-driven | existing `wave_controller.go` `SetupWithManager` predicate block + `project_controller.go:622-645` `AnnotationChangedPredicate` | exact |
| `internal/controller/task_controller.go` (modify: task-level gate hook) | reconciler | request-response | same file lines 121-188 (6-step body) | self-referential |
| `internal/controller/project_controller.go` (modify: exit-10 vs exit-11 mapping for W-1) | reconciler | request-response | same file lines 425-443 (`isJobFailed` → PhasePushLeaseFailed branch) | self-referential |
| `cmd/manager/main.go` (modify: OTel init + metrics-registry import) | cmd entry | bootstrap | same file lines 86-342 (existing wiring) | self-referential |
| `cmd/manager/env.go` (modify: add OTEL_* env helpers if needed) | env helpers | startup | same file lines 33-58 (`envOrDefault` / `atoiOrDefault` template) | self-referential |
| `cmd/tide-lint/main.go` (modify: append `metriccardinality.Analyzer`) | cmd entry | bootstrap | same file lines 31-33 (`multichecker.Main(crosspool.Analyzer, providerfirewall.Analyzer)` — append-only change) | self-referential |
| `cmd/tide-push/main.go` (modify only if exit-10 vs exit-11 needs envelope reason field) | cmd entry | bootstrap | same file `pushResult` JSON envelope (already present in Phase 3 03-06) | likely no change |

### Go: cobra CLI binary (`cmd/tide/`)

| New File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `cmd/tide/main.go` | cmd entry | bootstrap | `cmd/credproxy/main.go:58-135` (flag.Parse → context → signal.NotifyContext → ListenAndServe) — **NOT a cobra analog; one will be authored from scratch** | GREENFIELD (no cobra usage in repo) |
| `cmd/tide/root_flags.go` | cmd entry | flag binding | none — `genericclioptions.ConfigFlags` wiring is new | GREENFIELD |
| `cmd/tide/apply.go` | subcommand | request-response | `cmd/tide-push/main.go` `pushConfig` parse → run() shape | role-partial |
| `cmd/tide/watch.go` | subcommand | streaming (long-running) | `cmd/credproxy/main.go:115-131` (ctx + signal handling for long-running) | role-partial |
| `cmd/tide/tail.go` | subcommand | streaming (pods/log SSE) | none — `clientset.CoreV1().Pods(ns).GetLogs(name, &corev1.PodLogOptions{Follow:true}).Stream(ctx)` is new wiring | GREENFIELD |
| `cmd/tide/approve.go` | subcommand | request-response (annotation patch) | `internal/controller/project_controller.go:323-356` bypass-annotation consumer (mirror — annotation **write** uses `MergeFrom`/`Patch`) | role-match |
| `cmd/tide/reject.go` | subcommand | request-response | analogous to `approve.go` | exact (intra-phase) |
| `cmd/tide/cancel.go` | subcommand | request-response (cascading delete) | `project_controller.go` deletion path lines 122-129 (`finalizer.HandleDeletion`) — read-only reference; CLI side is a `client.Delete` call | role-partial |
| `cmd/tide/resume.go` | subcommand | request-response | `approve.go` mirror (annotation clear) | exact |
| `cmd/tide/inspect_wave.go` | subcommand | transform (list → table) | none — `text/tabwriter` over Task list is new | GREENFIELD |
| `cmd/tide/artifact_get.go` | subcommand | streaming (pod-exec proxy) | none — `pods/exec` subresource proxy is new | GREENFIELD |
| `cmd/tide/describe_budget.go` | subcommand | request-response | reads `Project.Status.Budget` (existing schema lines 220-238 of `project_types.go`) | role-match (data side) |
| `cmd/tide/*_test.go` | tests | unit | `cmd/manager/env_test.go` (table-driven env-var helper tests) | role-partial |

### Go: dashboard backend (`cmd/dashboard/`)

| New File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `cmd/dashboard/main.go` | cmd entry | bootstrap (manager.Runnable composition) | `cmd/manager/main.go:86-342` (Manager construction + multiple `mgr.Add(Runnable)`) | role-match — dashboard uses controller-runtime client + informer cache, **not** the reconcilers |
| `cmd/dashboard/api/projects.go` | HTTP handler | request-response (REST GET) | none — chi router + JSON encode is GREENFIELD | GREENFIELD |
| `cmd/dashboard/api/events_sse.go` | HTTP handler | streaming (SSE fan-out) | none — `http.Flusher` SSE is GREENFIELD | GREENFIELD |
| `cmd/dashboard/api/logs_sse.go` | HTTP handler | streaming (pods/log → SSE) | none — pods/log apiserver proxy with idle-timeout defer is GREENFIELD | GREENFIELD |
| `cmd/dashboard/hub/pubsub.go` | in-process pubsub | event-driven | none — informer-cache fan-out hub is GREENFIELD | GREENFIELD |
| `cmd/dashboard/embed/embed.go` | bundler | n/a | none — `//go:embed dist/*` is GREENFIELD | GREENFIELD |
| `cmd/dashboard/*_test.go` | tests | integration | none — `httptest.NewRecorder` SSE flush tests are GREENFIELD | GREENFIELD |

### Frontend (`dashboard/web/`) — entirely GREENFIELD

No prior `package.json`, no Vite config, no React code, no Tailwind v4 config exists in this repo. Every file listed in `04-UI-SPEC.md` (15 components) is greenfield. The closest authoring reference is the **04-UI-SPEC.md contract itself** plus `04-RESEARCH.md §1011-1060` (Vite + Tailwind v4 + @xyflow/react v12 toolchain layout).

| New File | Role | Reference |
|---|---|---|
| `dashboard/web/package.json` | build config | `04-UI-SPEC.md` "Stack & Bundle Targets" table; `04-RESEARCH.md` Open Question #11 |
| `dashboard/web/vite.config.ts` | build config | `04-RESEARCH.md` Open Question #11 |
| `dashboard/web/tailwind.config.ts` | build config | `04-UI-SPEC.md` design-system tokens block |
| `dashboard/web/tsconfig.json` | build config | standard React 18 + TS 5.x |
| `dashboard/web/vitest.config.ts` | test config | new |
| `dashboard/web/src/main.tsx`, `App.tsx` | SPA entry | `04-UI-SPEC.md` §1 `<AppShell>` |
| `dashboard/web/src/components/PlanningDAG.tsx` | React component | `04-UI-SPEC.md` §3 |
| `dashboard/web/src/components/ExecutionDAG.tsx` | React component | `04-UI-SPEC.md` §4 |
| `dashboard/web/src/components/{Project,Milestone,Phase,Plan,Task}Node.tsx` | React component | `04-UI-SPEC.md` §5 |
| `dashboard/web/src/components/WaveBandBackground.tsx` | React component | `04-UI-SPEC.md` §6 |
| `dashboard/web/src/components/TaskDetailDrawer.tsx` | React component | `04-UI-SPEC.md` §7 |
| `dashboard/web/src/components/PodLogStreamer.tsx` | React component | `04-UI-SPEC.md` §8 |
| `dashboard/web/src/components/ProjectPicker.tsx` | React component | `04-UI-SPEC.md` §9 |
| `dashboard/web/src/components/CopyCommandButton.tsx` | React component | `04-UI-SPEC.md` §10 |
| `dashboard/web/src/components/{Toast,ToastContainer}.tsx` | React component | `04-UI-SPEC.md` §11 |
| `dashboard/web/src/components/ConnectionStatusIndicator.tsx` | React component | `04-UI-SPEC.md` §12 |
| `dashboard/web/src/components/{StatusBadge,EmptyState,ErrorState,LoadingState}.tsx` | React component | `04-UI-SPEC.md` §13-15 + Status Vocabulary table |
| `dashboard/web/src/lib/sse.ts` | hook / util | `04-UI-SPEC.md` §8 EventSource cleanup + `04-RESEARCH.md` §757-786 SSE-through-ingress pitfall |
| `dashboard/web/src/lib/layout.ts` | util | `04-RESEARCH.md` §595-618 React Flow + dagre two-DAG layouts |
| `dashboard/web/src/lib/ansi.ts` | util | `04-UI-SPEC.md` §8 — 80-line SGR parser, scope locked |

### Helm chart

| New File | Role | Closest Analog | Match Quality |
|---|---|---|---|
| `charts/tide/templates/dashboard-deployment.yaml` | Helm template | `charts/tide/templates/deployment.yaml` (controller-manager Deployment — full Pod spec template) | **EXACT** |
| `charts/tide/templates/dashboard-rbac.yaml` | Helm template (read-only ClusterRole + Binding + SA) | `charts/tide/templates/manager-rbac.yaml` (ClusterRole structure) + `charts/tide/templates/project-viewer-rbac.yaml` (read-only verb set `get/list/watch`) + `charts/tide/templates/serviceaccount.yaml` | exact (composition of three) |
| `charts/tide/templates/servicemonitor.yaml` | Helm template (CRD-bearing, gated) | None — gated-template idiom is new. Closest existing gate: `charts/tide/values.yaml:118-122` rateLimits block guarded by flag (only structural similarity). | GREENFIELD (gated `{{ if }}` block is straightforward Helm) |
| `charts/tide/values.yaml` (modify: add `dashboard.*`, `prometheus.serviceMonitor.*`, `otel.*` blocks) | Helm values | same file lines 137-167 (`images:` block — multi-key Helm hierarchy with comment-as-spec pattern) | self-referential |

### Release pipeline (`.goreleaser.yaml`, `.github/workflows/release.yaml`, `krew-plugins/tide.yaml`)

All GREENFIELD. No prior goreleaser config, no release.yaml workflow (only ci.yaml + lint.yml + test-e2e.yml + test.yml exist), no Krew manifest. Authoring reference is **04-RESEARCH.md §830-895 (cobra + Krew distribution)**.

| New File | Role | Reference |
|---|---|---|
| `.goreleaser.yaml` | release config | RESEARCH.md §830-895; goreleaser v2 docs (LOW confidence per A4) |
| `.github/workflows/release.yaml` | CI workflow | `.github/workflows/ci.yaml` job-shape skeleton |
| `krew-plugins/tide.yaml` | plugin manifest | RESEARCH.md §830-895 Krew v1alpha2 schema |

### Docs (Phase 4 stubs)

`docs/cli.md`, `docs/dashboard.md`, `docs/observability.md`, `docs/gates.md` — likely GREENFIELD (no `docs/` dir today). Planner can stub one-paragraph-per-decision; Phase 5 finalizes.

### Integration tests (`test/integration/envtest/`)

| New File | Role | Closest Analog |
|---|---|---|
| `test/integration/envtest/gates_test.go` | envtest | existing envtest controller suite_test pattern (in `internal/controller/suite_test.go`) |
| `test/integration/envtest/observability_test.go` | envtest | same |
| `test/integration/envtest/sse_test.go` | envtest | none — SSE test via `httptest` is new |
| `test/integration/envtest/boundary_push_test.go` | envtest | existing `internal/controller/project_phase3_test.go` push-Job assertion pattern |

---

## Pattern Assignments — Concrete Excerpts

### A. Prometheus counter registration (exact analog — copy as-is)

**Apply to:** `internal/metrics/registry.go` (all 8 counters + 1 histogram), `internal/budget/metrics.go` (existing — for reference)

**Source:** `internal/budget/metrics.go` (entire file, 38 lines)

```go
// Source: internal/budget/metrics.go:1-39
package budget

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

// ProviderRateLimitHitsTotal counts the number of times the per-Secret-UID
// rate-limit bucket was exhausted, surfaced at the Project granularity.
//
// Cardinality discipline (Pitfall 17): the label set is {project} only.
// Per-Secret-UID dimension lives in the in-process sync.Map (bucket.go),
// NOT in a metric label. Adding a {secret_uid} label would produce O(project ×
// secret) cardinality — unacceptable on long-lived clusters.
var ProviderRateLimitHitsTotal *prometheus.CounterVec

func init() {
	ProviderRateLimitHitsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "tide_provider_rate_limit_hits_total",
			Help: "Count of times the per-Secret-UID rate-limit bucket was exhausted, surfaced by Project.",
		},
		[]string{"project"},
	)
	metrics.Registry.MustRegister(ProviderRateLimitHitsTotal)
}
```

**Planner instruction:** `internal/metrics/registry.go` declares all 8 counters + 1 histogram exactly like the above (one `init()` function — D-O2 / D-W1):

- `tide_waves_dispatched_total{project, phase, plan}`
- `tide_tasks_completed_total{project, phase, plan}`
- `tide_tasks_failed_total{project, phase, plan, reason}`
- `tide_dispatch_latency_seconds{level}` — `prometheus.NewHistogramVec` with custom buckets `[0.1, 0.5, 1, 5, 10, 30, 60, 300, 1800]` (CONTEXT.md specifics block §195)
- `tide_provider_rate_limit_hits_total{project, vendor}` — **NOTE:** existing version has only `{project}`; Phase 4 either extends label set or leaves as-is. Planner must decide which (D-O2 lists `{project, vendor}`; existing code has `{project}`).
- `tide_secret_leak_blocked_total{project, phase, plan}` — **W-1 lands here**
- `tide_push_jobs_total{project, outcome}`
- `tide_budget_overruns_total{project}`

**Critical:** `task` label is forbidden in any registration (Pitfall 17 / D-X4 — the new `metriccardinality` analyzer enforces this at compile time).

---

### B. tide-lint analyzer (exact analog — copy template)

**Apply to:** `tools/analyzers/metriccardinality/analyzer.go` (full file structure copied from providerfirewall)

**Source:** `tools/analyzers/providerfirewall/analyzer.go` (entire file, 108 lines)

```go
// Source: tools/analyzers/providerfirewall/analyzer.go:26-72 (selected excerpts)
package providerfirewall

import (
	"strings"
	"golang.org/x/tools/go/analysis"
)

var forbiddenPrefixes = []string{
	"github.com/anthropics/",
	"github.com/openai/",
	"github.com/sashabaranov/go-openai",
	"github.com/google/generative-ai-go",
}

var forbiddenScopes = []struct {
	contains  string
	hasSuffix string
}{
	{"/pkg/controller/", "pkg/controller"},
	// ...
}

var Analyzer = &analysis.Analyzer{
	Name: "providerfirewall",
	Doc:  "rejects github.com/anthropics/*, github.com/openai/*, etc. imports inside the orchestrator-side firewall boundary (SUB-05 / Pitfall 14)",
	Run:  run,
}

func run(pass *analysis.Pass) (interface{}, error) {
	path := pass.Pkg.Path()
	if !inFirewalledScope(path) {
		return nil, nil
	}
	for _, f := range pass.Files {
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, "\"")
			for _, prefix := range forbiddenPrefixes {
				if strings.HasPrefix(importPath, prefix) {
					pass.Reportf(imp.Pos(),
						"SUB-05 violation: forbidden LLM SDK import %q in %s (Pitfall 14: vendor lock-in creep)",
						importPath, path)
				}
			}
		}
	}
	return nil, nil
}
```

**Planner instruction:** `metriccardinality/analyzer.go` walks `pass.Files` looking for **call expressions** (not imports — different AST node), specifically `prometheus.NewCounterVec` / `NewHistogramVec` / `NewGaugeVec` / `NewSummaryVec`, examines the `[]string{...}` literal argument, and reports any element matching `"task"`. The shape:

- `Analyzer = &analysis.Analyzer{Name: "metriccardinality", Doc: "...", Run: run}`
- `run(pass)` iterates `pass.Files` → `ast.CallExpr` whose `Fun` is a `prometheus.New*Vec` selector → inspect 2nd arg (label list).
- Fixture pair under `testdata/`: `valid/registry.go` (no `task` label, must produce 0 diagnostics) and `violation/registry.go` (has `task` label, expects `// want "metriccardinality:.*task"` directive).

**Update** `cmd/tide-lint/main.go`:

```go
// Source: cmd/tide-lint/main.go:31-33 (existing)
func main() {
	multichecker.Main(crosspool.Analyzer, providerfirewall.Analyzer)
}
// Phase 4 change:
//	multichecker.Main(crosspool.Analyzer, providerfirewall.Analyzer, metriccardinality.Analyzer)
```

---

### C. Gate-policy seam in the 6-step reconciler body

**Apply to:** All four up-stack reconcilers (`milestone_controller.go`, `phase_controller.go`, `plan_controller.go`, `task_controller.go`) — gate hook insertion. Plus `plan_controller.go` for `PauseBetweenWaves`.

**Source:** `internal/controller/milestone_controller.go:241-274` (`handleJobCompletion` — the seam where gate-policy + boundary-push lands per RESEARCH.md §365-397)

```go
// Source: internal/controller/milestone_controller.go:241-274
func (r *MilestoneReconciler) handleJobCompletion(ctx context.Context, ms *tideprojectv1alpha1.Milestone, _ *batchv1.Job) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	var project *tideprojectv1alpha1.Project
	if ms.Spec.ProjectRef != "" {
		var p tideprojectv1alpha1.Project
		if err := r.Get(ctx, client.ObjectKey{Namespace: ms.Namespace, Name: ms.Spec.ProjectRef}, &p); err == nil {
			project = &p
		}
	}
	projectUID := ""
	if project != nil {
		projectUID = string(project.UID)
	}

	if r.EnvReader == nil {
		logger.Info("no env reader; marking Milestone Succeeded without ChildCRD materialization")
		return r.patchMilestoneSucceeded(ctx, ms)
	}

	envOut, err := r.EnvReader.ReadOut(ctx, projectUID, string(ms.UID))
	if err != nil {
		return r.patchMilestoneFailed(ctx, ms, "EnvelopeReadFailed", err.Error())
	}

	if len(envOut.ChildCRDs) > 0 {
		if mErr := MaterializeChildCRDs(ctx, r.Client, r.Scheme, ms, envOut.ChildCRDs); mErr != nil {
			return r.patchMilestoneFailed(ctx, ms, "ChildCRDMaterializationFailed", mErr.Error())
		}
	}

	return r.patchMilestoneSucceeded(ctx, ms)  // ← THIS is where gate hook + push trigger inserts
}
```

**Planner instruction:** Insert two NEW steps **before** the final `r.patchMilestoneSucceeded(ctx, ms)` call. Pattern from RESEARCH.md §371-397:

```go
// NEW (Phase 4) — gate-policy check (D-G2) using internal/gates
policy := gates.EvaluatePolicy(project.Spec.Gates, "milestone")
if policy == "approve" || policy == "pause" {
	if !gates.CheckApprove(ms, "milestone") {
		return r.patchMilestoneAwaitingApproval(ctx, ms, policy)
	}
}
if gates.CheckReject(project) {
	return r.patchMilestoneFailed(ctx, ms, "RejectedByUser", "...")
}

// NEW (Phase 4) — mid-stack push trigger (D-W2) using internal/gates.BoundaryDetected
if err := r.maybeTriggerBoundaryPush(ctx, ms, project); err != nil {
	return ctrl.Result{}, err
}

return r.patchMilestoneSucceeded(ctx, ms)
```

`patchMilestoneAwaitingApproval` is a new helper alongside `patchMilestoneSucceeded` / `patchMilestoneFailed` (`milestone_controller.go:276-306`) that sets `Status.Phase=AwaitingApproval` + emits `Condition WaveOrLevelPaused=True`. Reuses the exact `meta.SetStatusCondition + Status().Patch` shape from lines 276-289.

---

### D. Annotation-driven approve/reject pattern

**Apply to:** `internal/gates/annotation.go` (CheckApprove/CheckReject helpers) AND `cmd/tide/approve.go` / `reject.go` / `resume.go` (annotation writers).

**Source:** `internal/controller/project_controller.go:323-356` — the existing `bypassPushLeaseAnnotation` consumer (the mirror pattern for both reading and clearing)

```go
// Source: internal/controller/project_controller.go:323-356
const bypassPushLeaseAnnotation = "tideproject.k8s/bypass-push-lease"

// Step 2: Bypass-annotation handling (D-B6 / D-D4 mirror).
if project.Status.Phase == tideprojectv1alpha1.PhasePushLeaseFailed {
	if v, ok := project.Annotations[bypassPushLeaseAnnotation]; ok && v == "true" {
		logger.Info("push-lease bypass annotation present; clearing PushLeaseFailed", "project", project.Name)
		// Consume the annotation.
		annotPatch := client.MergeFrom(project.DeepCopy())
		newAnnotations := make(map[string]string, len(project.Annotations))
		for k, v := range project.Annotations {
			if k != bypassPushLeaseAnnotation {
				newAnnotations[k] = v
			}
		}
		project.Annotations = newAnnotations
		if err := r.Patch(ctx, project, annotPatch); err != nil {
			return ctrl.Result{}, fmt.Errorf("consume bypass annotation: %w", err)
		}
		// Clear PushLeaseFailed phase.
		statusPatch := client.MergeFrom(project.DeepCopy())
		project.Status.Phase = tideprojectv1alpha1.PhaseRunning
		// ... SetStatusCondition + Status().Patch
		return ctrl.Result{Requeue: true}, nil
	}
}
```

**Planner instruction:** `internal/gates/annotation.go` exposes:

- `CheckApprove(obj client.Object, level string) bool` — reads `tideproject.k8s/approve-<level>` (or `approve-wave-<N>` per D-G3).
- `CheckReject(project *v1alpha1.Project) bool` — reads `tideproject.k8s/reject`.
- `ConsumeApprove(obj client.Object, level string) map[string]string` — returns new annotation map with key removed (callers do the `Patch`). Mirrors the loop at lines 328-334.

`cmd/tide/approve.go` writes the annotation via client-go (RESEARCH.md Open Question #2 RESOLVED — direct client-go, no shelling to kubectl):

```go
// Planner writes (NEW):
patch := client.MergeFrom(project.DeepCopy())
if project.Annotations == nil {
	project.Annotations = map[string]string{}
}
project.Annotations["tideproject.k8s/approve-"+level] = "true"
if err := c.Patch(ctx, project, patch); err != nil { ... }
```

---

### E. SetupWithManager + AnnotationChangedPredicate (existing — extend, don't reinvent)

**Apply to:** `internal/controller/wave_controller.go` (modify to watch annotation `tideproject.k8s/approve-wave-<N>`), `milestone_controller.go` / `phase_controller.go` / `plan_controller.go` (already match this shape — extend predicate set).

**Source:** `internal/controller/project_controller.go:622-645`

```go
// Source: internal/controller/project_controller.go:619-645
func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Recorder == nil {
		r.Recorder = mgr.GetEventRecorderFor("project-controller")
	}
	nsPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.WatchNamespace == "" {
			return true
		}
		return obj.GetNamespace() == r.WatchNamespace
	})
	return ctrl.NewControllerManagedBy(mgr).
		For(&tideprojectv1alpha1.Project{},
			builder.WithPredicates(predicate.Or(
				predicate.GenerationChangedPredicate{},
				predicate.AnnotationChangedPredicate{},  // ← this is the seam
			)),
		).
		Owns(&batchv1.Job{}).
		Owns(&tideprojectv1alpha1.Milestone{}).
		WithEventFilter(nsPred).
		WithOptions(controller.Options{MaxConcurrentReconciles: r.MaxConcurrentReconciles}).
		Named("project").
		Complete(r)
}
```

**Planner instruction:** `milestone_controller.go:335-350`, `phase_controller.go`, `plan_controller.go`, `wave_controller.go` SetupWithManager methods need the same `builder.WithPredicates(predicate.Or(GenerationChanged, AnnotationChanged))` block on their `For()` calls. Currently they only set predicates via `WithEventFilter(nsPred)`.

---

### F. CRD constant additions

**Apply to:** `api/v1alpha1/project_types.go` (add `PhasePushLeakBlocked`), `api/v1alpha1/shared_types.go` (add condition + reasons).

**Source A — Phase constants:** `api/v1alpha1/project_types.go:297-316`

```go
// Source: api/v1alpha1/project_types.go:297-316
const (
	PhasePending = "Pending"
	PhaseInitialized = "Initialized"
	PhaseInitFailed = "InitFailed"
	PhaseBudgetExceeded = "BudgetExceeded"
	PhaseRunning = "Running"
	// PhasePushLeaseFailed is set when a push Job rejects due to --force-with-lease
	// mismatch (Phase 3 D-B6). Recovery: kubectl annotate project
	// tideproject.k8s/bypass-push-lease=true (mirrors D-D4 bypass-budget pattern).
	PhasePushLeaseFailed = "PushLeaseFailed"
	PhaseComplete = "Complete"
)
```

**Planner instruction:** Add (W-1):

```go
// PhasePushLeakBlocked is set when a push Job exits with code 10 (gitleaks
// finding — envelope.reason=leak-detected). Distinct from PhasePushLeaseFailed
// (exit-11) so the reconciler can fire tide_secret_leak_blocked_total
// counter (Phase 4 D-W1). Recovery: operator inspects the diff and
// either drops the leaked secret artifact or rotates the leaked credential.
PhasePushLeakBlocked = "PushLeakBlocked"
```

**Source B — Conditions and Reasons:** `api/v1alpha1/shared_types.go:74-93`

```go
// Source: api/v1alpha1/shared_types.go:74-93
const (
	ConditionCloned = "Cloned"
	ConditionAuthoringPlanner = "AuthoringPlanner"
	ConditionPushLeaseFailed = "PushLeaseFailed"
)
```

**Planner instruction:** Phase 4 adds (D-G2):

```go
// ConditionWaveOrLevelPaused — set when a reconciler observes a gate-policy
// value of "approve" or "pause" at a level boundary OR the
// PauseBetweenWaves wave-boundary check. Cleared by the matching approve/
// resume annotation (D-G3).
ConditionWaveOrLevelPaused = "WaveOrLevelPaused"

// Phase 4 reasons for D-G2/D-G3/D-G4:
ReasonAwaitingApproval = "AwaitingApproval"  // gate=approve, no annotation yet
ReasonPausedAtBoundary = "PausedAtBoundary"  // gate=pause OR PauseBetweenWaves=true
ReasonRejectedByUser   = "RejectedByUser"    // tideproject.k8s/reject annotation set
ReasonResumedByUser    = "ResumedByUser"     // tide resume clears reject
```

---

### G. Helm Deployment template

**Apply to:** `charts/tide/templates/dashboard-deployment.yaml`

**Source:** `charts/tide/templates/deployment.yaml` (entire file, 123 lines) — copy-and-adapt.

Key sections (lines 26-95):

```yaml
# Source: charts/tide/templates/deployment.yaml:26-50
spec:
  containers:
  - args:
      {{- toYaml .Values.controllerManager.manager.args | nindent 10 }}
    command:
    - /manager
    env:
    - name: WATCH_NAMESPACE
      value: {{ quote .Values.controllerManager.manager.env.watchNamespace }}
    image: {{ .Values.controllerManager.manager.image.repository }}:{{ .Values.controllerManager.manager.image.tag | default .Chart.AppVersion }}
    livenessProbe:
      httpGet:
        path: /healthz
        port: 8081
    name: manager
    ports:
    - containerPort: 8080
      name: metrics
    - containerPort: 8081
      name: health
    readinessProbe:
      httpGet:
        path: /readyz
        port: 8081
    resources: {{- toYaml .Values.controllerManager.manager.resources | nindent 10 }}
    securityContext: {{- toYaml .Values.controllerManager.manager.containerSecurityContext | nindent 10 }}
```

**Planner instruction:** `dashboard-deployment.yaml`:

- Replace `{{ include "tide.fullname" . }}-controller-manager` → `{{ include "tide.fullname" . }}-dashboard`.
- Strip webhook ports + cert volume mounts + tide-config + workspaces PVC (dashboard backend is read-only on apiserver, doesn't need any of these).
- Single container `dashboard`, port `8080` for HTTP (chi router), `8081` for `/healthz` + `/readyz`.
- Image: `{{ .Values.dashboard.image.repository }}:{{ .Values.dashboard.image.tag | default .Chart.AppVersion }}` (new Helm value).
- `serviceAccountName: {{ include "tide.fullname" . }}-dashboard` (different SA — see RBAC below).
- Whole template gated `{{- if .Values.dashboard.enabled }}` (default `true` per D-X3).

---

### H. Helm RBAC template (read-only ClusterRole + Binding + SA)

**Apply to:** `charts/tide/templates/dashboard-rbac.yaml`

**Source A — ClusterRole + Binding structure:** `charts/tide/templates/manager-rbac.yaml:1-92`

```yaml
# Source: charts/tide/templates/manager-rbac.yaml:1-92 (selected)
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ include "tide.fullname" . }}-manager-role
  labels:
  {{- include "tide.labels" . | nindent 4 }}
rules:
- apiGroups:
  - tideproject.k8s
  resources:
  - milestones
  - phases
  - plans
  - projects
  - tasks
  - waves
  verbs:
  - create
  - delete
  - get
  - list
  - patch
  - update
  - watch
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ include "tide.fullname" . }}-manager-rolebinding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: '{{ include "tide.fullname" . }}-manager-role'
subjects:
- kind: ServiceAccount
  name: '{{ include "tide.fullname" . }}-controller-manager'
  namespace: '{{ .Release.Namespace }}'
```

**Source B — read-only verb set:** `charts/tide/templates/project-viewer-rbac.yaml` (verb set `get/list/watch` only).

**Source C — SA structure:** `charts/tide/templates/serviceaccount.yaml` (5 lines, simple).

**Planner instruction:** `dashboard-rbac.yaml` composes all three:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ include "tide.fullname" . }}-dashboard
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: {{ include "tide.fullname" . }}-dashboard-readonly
rules:
- apiGroups: [tideproject.k8s]
  resources: [projects, milestones, phases, plans, tasks, waves]
  verbs: [get, list, watch]      # ← read-only — D-D2
- apiGroups: [""]
  resources: [pods]
  verbs: [get, list, watch]      # for the pod-log subresource
- apiGroups: [""]
  resources: [pods/log]
  verbs: [get]                   # log streaming
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: {{ include "tide.fullname" . }}-dashboard
roleRef:
  kind: ClusterRole
  name: '{{ include "tide.fullname" . }}-dashboard-readonly'
subjects:
- kind: ServiceAccount
  name: '{{ include "tide.fullname" . }}-dashboard'
  namespace: '{{ .Release.Namespace }}'
```

Gated by `{{- if .Values.dashboard.enabled }}` at top of file.

---

### I. ServiceMonitor template (gated by default-false)

**Apply to:** `charts/tide/templates/servicemonitor.yaml`

**Source:** No existing ServiceMonitor in repo. Closest gating idiom is **values.yaml comment + flag pattern**.

**Planner instruction:** GREENFIELD but trivial:

```yaml
{{- if .Values.prometheus.serviceMonitor.enabled }}
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: {{ include "tide.fullname" . }}-metrics
  labels:
  {{- include "tide.labels" . | nindent 4 }}
spec:
  selector:
    matchLabels:
      control-plane: controller-manager
  endpoints:
  - port: https
    path: /metrics
    scheme: https
    interval: 30s
    tlsConfig:
      insecureSkipVerify: true  # uses webhook self-signed cert; v1.x adds proper CA
{{- end }}
```

Default `false` per CLAUDE.md anti-pattern ("Default the chart's ServiceMonitor to prometheus.enabled=false to avoid CRD-not-found on plain clusters").

---

### J. Env-driven config helpers (extend existing pattern for OTel)

**Apply to:** `cmd/manager/env.go` (add OTel-env helpers IF needed; RESEARCH.md A2 recommends relying on OTel SDK's native env-var support — likely no new helpers needed)

**Source:** `cmd/manager/env.go:33-58`

```go
// Source: cmd/manager/env.go:33-58
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func atoiOrDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
```

**Planner instruction:** OTel SDK reads `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_TRACES_SAMPLER`, `OTEL_TRACES_SAMPLER_ARG` natively (RESEARCH.md §670-704). `cmd/manager/main.go` only needs to:

1. Import `internal/otelinit`.
2. Call `tp, shutdown, err := otelinit.NewTracerProvider(ctx)` once after `flag.Parse()`.
3. `defer shutdown(ctx)` before `mgr.Start(...)`.
4. Add `_ "github.com/jsquirrelz/tide/internal/metrics"` import for blank-import side-effect registration (mirrors how `internal/budget/metrics.go` self-registers via `init()`).

---

### K. Standalone binary boot template (cobra CLI + dashboard backend)

**Apply to:** `cmd/tide/main.go`, `cmd/dashboard/main.go`

**Source A — context + signal handling:** `cmd/credproxy/main.go:113-135`

```go
// Source: cmd/credproxy/main.go:113-135
ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
defer stop()

log.Info("starting credential proxy",
	"listenAddr", listenAddr,
	"certDir", certDir,
	"upstreamURL", upstreamURL,
	"taskUID", taskUID,
)

if err := p.ListenAndServe(ctx); err != nil {
	log.Error(err, "credential proxy exited with error")
	os.Exit(1)
}
log.Info("credential proxy shut down cleanly")
```

**Source B — zap-behind-logr setup (leaner than cmd/manager):** `cmd/credproxy/main.go:72-78`

```go
zapCore, err := zap.NewProduction()
if err != nil { ... }
defer zapCore.Sync()
log := zapr.NewLogger(zapCore).WithName("credproxy")
```

**Planner instruction:**

- `cmd/tide/main.go`: cobra root command + `cmd.ExecuteContext(ctx)` (Pitfall 25 — RESEARCH.md §555-566). Each subcommand `.go` file declares its own `*cobra.Command` and `func init() { rootCmd.AddCommand(...) }`.
- `cmd/dashboard/main.go`: same context/signal pattern as credproxy, but registers chi router as `mgr.Add(manager.RunnableFunc(...))` so it shares controller-runtime's lifecycle (and benefits from the informer cache). RESEARCH.md §1294 confirms separate image — `cmd/dashboard/main.go` still uses controller-runtime's `Manager` constructor, just with a dashboard-only RBAC.

---

### L. Reconciler dispatch + Job creation pattern (for boundary push trigger)

**Apply to:** `milestone_controller.go` / `phase_controller.go` / `plan_controller.go` — `maybeTriggerBoundaryPush(...)` helper.

**Source:** `internal/controller/project_controller.go:385-413` (existing push-Job creation at Project boundary)

```go
// Source: internal/controller/project_controller.go:385-413
if project.Status.Phase == tideprojectv1alpha1.PhaseComplete && project.Spec.Git != nil && project.Spec.Git.RepoURL != "" {
	pushJobName := fmt.Sprintf("tide-push-%s", project.UID)
	var existingPush batchv1.Job
	pErr := r.Get(ctx, types.NamespacedName{Name: pushJobName, Namespace: project.Namespace}, &existingPush)
	if pErr != nil && !apierrors.IsNotFound(pErr) {
		return ctrl.Result{}, pErr
	}
	if apierrors.IsNotFound(pErr) {
		msg, mErr := buildCommitMessage("project", "")
		if mErr != nil {
			return ctrl.Result{}, fmt.Errorf("build commit message: %w", mErr)
		}
		pushOpts := PushOptions{
			TidePushImage:  r.TidePushImage,
			Branch:         project.Status.Git.BranchName,
			LastPushedSHA:  project.Status.Git.LastPushedSHA,
			CommitMessage:  msg,
			LeaksConfigMap: project.Spec.Git.LeaksConfigRef,
		}
		pushJob := buildPushJob(project, pvcName, pushOpts, r.Scheme)
		if cErr := r.Create(ctx, pushJob); cErr != nil {
			if !apierrors.IsAlreadyExists(cErr) {
				return ctrl.Result{}, fmt.Errorf("create push job: %w", cErr)
			}
			// AlreadyExists: idempotent success (D-B5 serialization).
		}
		logger.Info("created push Job", "job", pushJobName)
	}
}
```

**Planner instruction:** Each up-stack reconciler's `maybeTriggerBoundaryPush(ctx, parent, project)` follows the same shape:

1. Compute `pushJobName := fmt.Sprintf("tide-push-%s-%s", boundary, parent.UID)` (one push Job per boundary — D-W2).
2. `r.Get` to check AlreadyExists.
3. `buildCommitMessage(boundary, parent.Name)` (already exists at `push_helpers.go:335-359` — reuse).
4. `buildPushJob(...)` (already exists at `push_helpers.go` — reuse; **may need to extend** to take Milestone/Phase/Plan owner refs instead of Project).
5. `r.Create(ctx, pushJob)` with AlreadyExists tolerated.

The W-2 shared boundary-detection function lives at `internal/gates/boundary.go` (RESEARCH.md Open Question #3 RESOLVED). It exposes a `BoundaryDetected(ctx, client, parent) (bool, error)` predicate consumed by both the gate hook AND the push-trigger helper.

---

### M. W-1 push-Job exit-code mapping (modify project_controller.go)

**Apply to:** `internal/controller/project_controller.go` (extend `reconcilePhase3Lifecycle`)

**Source:** `internal/controller/project_controller.go:425-443` (current Failed → PushLeaseFailed mapping)

```go
// Source: internal/controller/project_controller.go:425-443
} else if isJobFailed(&existingPush) {
	patch := client.MergeFrom(project.DeepCopy())
	project.Status.Phase = tideprojectv1alpha1.PhasePushLeaseFailed
	project.Status.Git.LeaseFailureCount++
	meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
		Type:               tideprojectv1alpha1.ConditionPushLeaseFailed,
		Status:             metav1.ConditionTrue,
		Reason:             "LeaseRejected",
		Message:            fmt.Sprintf("Push Job %s rejected by --force-with-lease", pushJobName),
		LastTransitionTime: metav1.Now(),
	})
	if err := r.Status().Patch(ctx, project, patch); err != nil {
		return ctrl.Result{}, err
	}
}
```

**Planner instruction:** Phase 4 W-1 splits this into two arms based on the push envelope's `reason` field (already present in Phase 3 03-06 per RESEARCH.md §1259):

```go
} else if isJobFailed(&existingPush) {
	// Phase 4 W-1: distinguish exit-10 (gitleaks finding) from exit-11 (lease rejection)
	// via the push-result envelope's `reason` field.
	envOut, err := r.EnvReader.ReadPushOut(ctx, string(project.UID))
	// ... handle err ...
	switch envOut.Reason {
	case "leak-detected":
		project.Status.Phase = tideprojectv1alpha1.PhasePushLeakBlocked
		metrics.SecretLeakBlockedTotal.WithLabelValues(project.Name, "", "").Inc()  // D-W1
		// ... SetStatusCondition with new reason ...
	case "lease-rejected":
		project.Status.Phase = tideprojectv1alpha1.PhasePushLeaseFailed
		project.Status.Git.LeaseFailureCount++
		// ... existing logic ...
	default:
		// fallback to PushLeaseFailed for unknown reasons
	}
}
```

`metrics.SecretLeakBlockedTotal` is exposed by `internal/metrics/registry.go` per family A above.

---

## Shared Patterns (cross-cutting)

### Authentication / authorization
No auth pattern reused. CLI uses kubeconfig via `genericclioptions.ConfigFlags` (RESEARCH.md §1317). Dashboard uses its own SA via the controller-runtime client (no token in browser — D-D2). Both are new wiring.

### Structured logging
**Apply to:** every new `.go` file.
**Source:** `cmd/manager/main.go:128-133` (zap-behind-logr setup).

```go
opts := zap.Options{Development: false}
opts.BindFlags(flag.CommandLine)
flag.Parse()
ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
setupLog := ctrl.Log.WithName("setup")
```

Reconcilers use `logf.FromContext(ctx)` (e.g. `milestone_controller.go:92`). Standalone binaries (`cmd/tide/`, `cmd/dashboard/`) use the leaner `zapr.NewLogger(zap.NewProduction())` pattern from `cmd/credproxy/main.go:72-78`.

Required structured fields (D-O1 / D-X1): `project`, `phase`, `plan`, `task` — same label set as metrics for grep-correlation. Apply to all `logger.Info`/`Error` calls in the new code.

### Error handling
**Source:** `internal/controller/project_controller.go` (entire file) — pattern of `return ctrl.Result{}, fmt.Errorf("operation: %w", err)` for wrap-and-bubble, `client.IgnoreNotFound(err)` for tolerable NotFound, `apierrors.IsAlreadyExists(err)` for idempotent create.

Apply to all new reconciler code. CLI/dashboard binaries surface errors via cobra's stderr + non-zero exit (Pitfall 25 — RESEARCH.md §555-566 — use `cmd.ExecuteContext` so SIGINT propagates).

### Validation
No new admission webhooks. Existing CEL validation on `Gates` struct (`api/v1alpha1/project_types.go:50` — `+kubebuilder:validation:Enum=auto;approve;pause`) is the only validation surface; Phase 4 Wave 1 plan verifies `helm template` renders the enum correctly (RESEARCH.md §1308).

### Testing
**Source for unit tests:** `internal/budget/cap_test.go` (table-driven pure-func tests).
**Source for envtest:** `internal/controller/suite_test.go` + `internal/controller/project_phase3_test.go` (existing envtest scaffolding).
**Source for analyzer tests:** `tools/analyzers/providerfirewall/analyzer_test.go` + its `testdata/` (analysistest pattern).
**Source for CLI tests:** `cmd/manager/env_test.go` (env-var-driven helper test pattern).

Frontend tests: GREENFIELD with Vitest (no analog in repo).

---

## No Analog Found (GREENFIELD)

Files with no close match in the codebase — planner falls back to RESEARCH.md patterns + UI-SPEC contract:

| File / Family | Role | Why no analog | Reference for planner |
|---|---|---|---|
| `dashboard/web/**` (everything frontend) | React 18 SPA + Vite + Tailwind v4 + @xyflow/react v12 | First frontend in repo | `04-UI-SPEC.md` (entire file — design contract) + `04-RESEARCH.md` Open Question #1 (React Flow v12 + dagre) and #11 (Vite/TS toolchain) |
| `cmd/dashboard/api/events_sse.go` | `http.Flusher` SSE | No SSE in repo | `04-RESEARCH.md` §399-442 Pattern 2 (SSE handler with EventSource fan-out, pseudocode) |
| `cmd/dashboard/api/logs_sse.go` | `pods/log` apiserver-proxy SSE | No pod-log streaming in repo | `04-RESEARCH.md` §787-829 (K8s pods/log subresource streaming) + Pitfall 22 idle-timeout |
| `cmd/dashboard/hub/pubsub.go` | informer-cache pubsub hub | No in-process pubsub in repo | `04-RESEARCH.md` §1320-1325 DASH-03 recommendation |
| `cmd/dashboard/embed/embed.go` | `//go:embed` bundle | No embedded assets in repo | Go stdlib `embed` package |
| `internal/otelinit/provider.go` | OTel TracerProvider with no-op fallback | No OTel SDK code in repo | `04-RESEARCH.md` §443-473 Pattern 3 (OTel TracerProvider with no-op fallback, pseudocode) + Pitfall 24 |
| `pkg/otelai/attrs.go` | OpenInference attribute helpers | No OpenInference SDK in Go (2026 — hand-rolled per D-O4) | `04-RESEARCH.md` §619-669 (current OpenInference attribute names) |
| `cmd/tide/main.go` + all subcommands | cobra CLI | No cobra usage in repo | `04-RESEARCH.md` §830-895 (cobra + Krew distribution) + Pitfall 25 (ExecuteContext) + Pitfall 27 (Krew naming) |
| `.goreleaser.yaml` | release pipeline | No goreleaser in repo | `04-RESEARCH.md` §830-895; assumption A4 flagged for Wave-3 re-verification |
| `krew-plugins/tide.yaml` | Krew plugin manifest | No Krew in repo | `04-RESEARCH.md` §830-895 v1alpha2 schema |
| `charts/tide/templates/servicemonitor.yaml` | Prometheus CRD | First Prometheus CRD template | `04-RESEARCH.md` §955-1006 (Helm chart additions) |

---

## Modified-Files Action Map (concise)

| File | Action | Source reference |
|---|---|---|
| `api/v1alpha1/project_types.go` | Add `PhasePushLeakBlocked` constant | self lines 297-316 (W-1) |
| `api/v1alpha1/shared_types.go` | Add `ConditionWaveOrLevelPaused`, `ReasonAwaitingApproval`, `ReasonPausedAtBoundary`, `ReasonRejectedByUser`, `ReasonResumedByUser` | self lines 74-93 (D-G2/3/4) |
| `internal/controller/project_controller.go` | Split `isJobFailed` arm into leak vs lease via envelope reason; add gate hook before mid-stack push | self lines 425-443 (W-1); RESEARCH.md §371-397 (gate hook) |
| `internal/controller/milestone_controller.go` | Insert gate hook + `maybeTriggerBoundaryPush` in `handleJobCompletion` before `patchMilestoneSucceeded` | self lines 241-274; RESEARCH.md §371-397 |
| `internal/controller/phase_controller.go` | Same as milestone | analogous |
| `internal/controller/plan_controller.go` | Same as milestone + add `PauseBetweenWaves` check before dispatching new wave | analogous + RESEARCH.md §1311 (GATE-02) |
| `internal/controller/wave_controller.go` | Add annotation watch for `tideproject.k8s/approve-wave-<N>` via AnnotationChangedPredicate | self `SetupWithManager` + project_controller.go:622-645 |
| `internal/controller/task_controller.go` | Add task-level gate hook in 6-step body | self lines 121-188 |
| `cmd/manager/main.go` | Add `internal/otelinit` init + `_ "internal/metrics"` blank import | self lines 86-342 (existing wiring shape) |
| `cmd/manager/env.go` | NO change (OTel SDK reads env natively per RESEARCH.md A2) | self lines 33-58 |
| `cmd/tide-lint/main.go` | Append `metriccardinality.Analyzer` to `multichecker.Main(...)` | self lines 31-33 |
| `charts/tide/values.yaml` | Add `dashboard.*` (enabled, image, replicas, resources), `prometheus.serviceMonitor.enabled` (default false), `otel.*` (endpoint, sampler) | self lines 137-167 (images block — model for nested Helm value comment-as-spec) |
| `Makefile` | Add `dashboard-frontend`, `dashboard-build`, `tide-cli`, `release-snapshot` targets | existing target patterns (verify with `grep -nE '^[a-z-]+:' Makefile`) |
| `go.mod` | Add cobra, otel/sdk, otel/trace, otel/exporters/otlp/otlptrace/otlptracegrpc, cli-runtime | n/a — `go mod tidy` |

---

## Metadata

**Analog search scope:** `internal/controller/`, `internal/budget/`, `internal/credproxy/`, `cmd/*/`, `tools/analyzers/`, `api/v1alpha1/`, `charts/tide/templates/`, `charts/tide/values.yaml`, `.github/workflows/`.
**Files scanned:** ~30 (full reads on 9 critical analogs, targeted reads on the rest).
**Pattern extraction date:** 2026-05-16.

Pattern mapping complete. Planner can now reference per-file analogs in PLAN.md files — every Phase 4 production file maps either to a concrete in-repo excerpt or to an explicit GREENFIELD reference in RESEARCH.md / UI-SPEC.md.
