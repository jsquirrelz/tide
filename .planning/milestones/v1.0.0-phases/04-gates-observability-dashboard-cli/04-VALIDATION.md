---
phase: 04
slug: gates-observability-dashboard-cli
status: approved
nyquist_compliant: true
wave_0_complete: true
created: 2026-05-16
approved: 2026-05-17
---

# Phase 04 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded from RESEARCH.md `## Validation Architecture` section. Per-task rows
> are filled in by the planner as PLAN.md files are authored.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework — Go** | Ginkgo v2.28 + Gomega (existing controller pattern); standard `go test` for `pkg/otelai`, `internal/gates`, `internal/metrics`, `cmd/tide-lint` analyzers |
| **Framework — Frontend** | Vitest 1.x + @testing-library/react for `dashboard/web/`; Playwright deferred (heavy, optional v1.x) |
| **Envtest** | `setup-envtest` for reconciler integration tests; same fake-apiserver pattern as Phase 1+2+3 |
| **OTel test harness** | `go.opentelemetry.io/otel/sdk/trace/tracetest` — `SpanRecorder` + `NewTracerProvider(WithSyncer(rec))` for span-attr assertions |
| **Prometheus test harness** | `testutil.GatherAndCount` + `testutil.ToFloat64` from `github.com/prometheus/client_golang/prometheus/testutil` |
| **Config file (Go)** | `.golangci.yml` (existing); per-package `*_test.go` collocation |
| **Config file (frontend)** | `dashboard/web/vitest.config.ts`, `dashboard/web/tsconfig.json` (added Wave 4) |
| **Quick run command (Go)** | `go test ./pkg/otelai/... ./internal/gates/... ./internal/metrics/... ./cmd/tide/... ./cmd/tide-lint/...` |
| **Quick run command (controller)** | `make test` (existing envtest-driven Ginkgo run from Phase 1+2+3) |
| **Quick run command (frontend)** | `cd dashboard/web && npm run test:unit` |
| **Full suite command** | `make test && cd dashboard/web && npm run test && npm run build` |
| **E2E command (kind)** | `make test-e2e` (existing kind harness from Phase 3 — extended by Phase 4 to include `tide` CLI smoke + dashboard `/healthz` ping) |
| **Estimated quick-run runtime** | ~45s (Go unit) + ~20s (frontend Vitest) |
| **Estimated full-suite runtime** | ~6–9 min (full envtest matrix + frontend build + kind smoke) |

---

## Sampling Rate

- **After every task commit:** Run the relevant package's `go test ./<pkg>/...` (frontend changes also run `npm run test:unit`)
- **After every plan wave:** Run `make test` (controller envtest matrix)
- **After Wave 7 (PodLogStreamer + bundle gate) commits:** Add `npm run build` to confirm bundle target (<500KB gzipped per D-D5)
- **After Wave 8 (Helm + integration) commits:** Run `make test-e2e` against the kind cluster
- **Before `/gsd-verify-work` for Phase 4:** Full suite must be green AND `make test-e2e` green
- **Max feedback latency (single task):** ~60s (Go unit + collocated package tests)

---

## Per-Task Verification Map

> Plan/Wave/Task IDs map every requirement family to its concrete verification
> command in the 16-plan inventory below. The 16-plan structure is:
> Wave 1 = 04-01, 04-02, 04-03; Wave 2 = 04-04, 04-07, 04-10; Wave 3 = 04-05, 04-08, 04-09, 04-11;
> Wave 4 = 04-06, 04-12; Wave 5 = 04-15; Wave 6 = 04-13; Wave 7 = 04-16; Wave 8 = 04-14.

### Gates (GATE-01..03)

| Requirement | Plan/Wave/Task | Threat Ref | Secure Behavior | Test Type | Automated Command | Status |
|-------------|----------------|------------|-----------------|-----------|-------------------|--------|
| GATE-01 (per-level policy on Project CRD) | 04-04 / W2 / T1 | T-04-G1 (gate bypass) | CEL `enum: [auto, approve, pause]` blocks invalid values at apply-time; EvaluatePolicy returns correct defaults | unit + envtest | `go test ./internal/gates/... -run Policy && make test -run TestGatePolicy` | ⬜ pending |
| GATE-02 (reconciler consults policy) | 04-05 / W3 / T1-T4 | T-04-G2 (uninspected dispatch) | On `approve`, reconciler sets `Status.Phase=AwaitingApproval` + `WaveOrLevelPaused` Condition AND does NOT dispatch child | envtest | `make test -run TestReconciler_GateApprove` | ⬜ pending |
| GATE-03 (slack-tide between-wave review) | 04-05 / W3 / T5 + 04-08 / W3 / T1 | T-04-G3 (wave skip) | Wave-N pause clears only when `tideproject.k8s/approve-wave-N` annotation present | envtest | `make test -run TestPlanReconciler_WavePause` | ⬜ pending |

### Observability (OBS-01..06)

| Requirement | Plan/Wave/Task | Threat Ref | Secure Behavior | Test Type | Automated Command | Status |
|-------------|----------------|------------|-----------------|-----------|-------------------|--------|
| OBS-01 (structured JSON logs) | 04-05 / W3 / T1-T4 + 04-06 / W4 | T-04-O1 (log injection) | `ctrl.Log.Info()` calls emit JSON with required fields `{project, phase, plan, task?}` | unit + log-capture | `go test ./internal/logging/... -run TestStructuredFields` | ⬜ pending |
| OBS-02 (bounded-cardinality metrics) | 04-01 / W1 + 04-02 / W1 | T-04-O2 (cardinality explosion via task label) | `cmd/tide-lint metric-cardinality` fails on any `prometheus.New*Vec` with `"task"` in labels; CI run blocks merge | unit (AST) + CI gate | `go test ./cmd/tide-lint/... && tide-lint ./...` | ⬜ pending |
| OBS-03 (OTel trace coverage) | 04-03 / W1 | T-04-O3 (missing audit trail) | `SpanRecorder` captures spans named `tide.dispatch.{milestone,phase,plan,task}` with parent-child link | unit (tracetest) | `go test ./pkg/otelai/... -run TestSpanChain` | ⬜ pending |
| OBS-04 (OpenInference attrs on spans) | 04-03 / W1 | T-04-O4 (PII in attrs) | Span attrs include `llm.input_messages.0.message.role`, `llm.token_count.prompt`, `llm.token_count.completion`; NO message content as attr values | unit (tracetest) | `go test ./pkg/otelai/... -run TestOpenInferenceAttrs` | ⬜ pending |
| OBS-05 (artifact path, not payload) | 04-03 / W1 | T-04-O5 (etcd bloat / payload leak) | Span attr `llm.input_messages.0.message.content` is OMITTED; only `gen_ai.artifact_path` (string) present | unit (tracetest) | `go test ./pkg/otelai/... -run TestArtifactPathOnly` | ⬜ pending |
| OBS-06 (ServiceMonitor opt-in) | 04-14 / W8 | T-04-O6 (CRD-not-found on plain clusters) | `helm template ... --set prometheus.serviceMonitor.enabled=false` produces zero ServiceMonitor objects | helm template diff | `make helm-lint-validate` | ⬜ pending |

### CLI (CLI-01..04)

| Requirement | Plan/Wave/Task | Threat Ref | Secure Behavior | Test Type | Automated Command | Status |
|-------------|----------------|------------|-----------------|-----------|-------------------|--------|
| CLI-01 (stateless cobra binary) | 04-07 / W2 / T1 | T-04-C1 (local cache exfil) | `cmd/tide` reads only kubeconfig + flags; zero filesystem writes outside `os.UserCacheDir() + /tide/`(none) | unit | `go test ./cmd/tide/... -run TestNoLocalCache` | ⬜ pending |
| CLI-02 (10 verbs + completion) | 04-07 / W2 / T1 + 04-09 / W3 | T-04-C2 (verb spoofing) | `tide --help` lists every D-C3 verb; each verb returns clear error on missing arg | unit (cobra `ExecuteContextC`) | `go test ./cmd/tide/... -run TestVerbsRegistered` | ⬜ pending |
| CLI-03 (`tide inspect-wave`) | 04-07 / W2 / T2 | T-04-C3 (info-leak in output) | Output columns `NAME STATUS AGE ATTEMPT SCHEDULED-IN-WAVE`; no secret values in output; uses canonical `tideproject.k8s/project` + `tideproject.k8s/wave-index` labels | unit + integration | `go test ./cmd/tide/... -run TestInspectWaveOutput` | ⬜ pending |
| CLI-04 (`tide tail` via pods/log) | 04-08 / W3 / T1 + 04-14 / W8 | T-04-C4 (stream hang/leak) | `tide tail <task>` streams via `pods/log` subresource; client-side Ctrl-C cancels context → stream closes within 1s | integration (kind) | `make test-e2e -run TestTideTailCancelClose` | ⬜ pending |

### Dashboard (DASH-01..05)

| Requirement | Plan/Wave/Task | Threat Ref | Secure Behavior | Test Type | Automated Command | Status |
|-------------|----------------|------------|-----------------|-----------|-------------------|--------|
| DASH-01 (side-by-side two-DAG render) | 04-12 / W4 + 04-15 / W5 + 04-13 / W6 | T-04-D1 (XSS via task name) | `<TaskNode>` renders user-supplied names via React text node (escaped); no `dangerouslySetInnerHTML` anywhere in `dashboard/web/src` | Vitest + AST-grep | `npm run test:unit && rg -n "dangerouslySetInnerHTML" dashboard/web/src` (must exit 1) | ⬜ pending |
| DASH-02 (own read-only SA) | 04-14 / W8 | T-04-D2 (write-RBAC escalation) | `charts/tide/templates/dashboard-rbac.yaml` ClusterRole has ONLY `get/list/watch` verbs across all rules | helm template assertion | `make helm-rbac-assert` | ⬜ pending |
| DASH-03 (SSE event stream) | 04-11 / W3 + 04-16 / W7 / T1 | T-04-D3 (event flood / fan-out leak) | SSE handler closes on client-disconnect within 1s; goroutine count steady after disconnect | unit (httptest) + race | `go test -race ./cmd/dashboard/... -run TestSSEFanoutCleanup` | ⬜ pending |
| DASH-04 (opt-in pod-log stream) | 04-11 / W3 + 04-16 / W7 / T2 | T-04-D4 (websocket leak, Pitfall 22) | 5-min idle timeout closes stream; client-disconnect-cleanup defer fires | unit (httptest) + race | `go test -race ./cmd/dashboard/... -run TestPodLogIdleTimeout` | ⬜ pending |
| DASH-05 (zero mutation endpoints) | 04-10 / W2 | T-04-D5 (CSRF / cross-origin write) | Backend route registry contains ZERO POST/PUT/PATCH/DELETE handlers under `/api/v1/` | unit (handler registry walk) | `go test ./cmd/dashboard/... -run TestZeroMutationRoutes` | ⬜ pending |

### W-1 / W-2 (Phase 3 catch-up folded here)

| Requirement | Plan/Wave/Task | Threat Ref | Secure Behavior | Test Type | Automated Command | Status |
|-------------|----------------|------------|-----------------|-----------|-------------------|--------|
| W-1 (`tide_secret_leak_blocked_total` counter) | 04-06 / W4 | T-04-W1 (leak-not-counted) | On push-Job exit-10 + `reason="leak-detected"`, counter increments AND `Project.Status.Phase=PushLeakBlocked` | envtest | `make test -run TestProject_LeakBlocked` | ⬜ pending |
| W-2 (mid-stack boundary push) | 04-04 / W2 / T3 + 04-06 / W4 | T-04-W2 (silent commit gap) | Milestone/Phase/Plan reconcilers each fire a `tide-push` Job at all-children-Succeeded with correct D-B2 commit-message shape | envtest | `make test -run TestBoundaryPush_AllLevels` | ⬜ pending |

---

## Wave 0 Requirements

- [x] `internal/gates/policy_test.go` — stub tests for D-G1 policy schema + default-application (created in 04-04 Task 1)
- [x] `internal/metrics/registry_test.go` — stub for metric registration + cardinality assertion (created in 04-01)
- [x] `pkg/otelai/attrs_test.go` — stub for OpenInference attribute shape assertions (created in 04-03)
- [x] `tools/analyzers/metriccardinality/analyzer_test.go` — testdata fixtures (created in 04-02)
- [x] `cmd/tide/cmd_test.go` — stub for verb registry (created in 04-07 Task 1)
- [x] `cmd/dashboard/handlers_test.go` — stub for handler registry walk (created in 04-10)
- [x] `dashboard/web/vitest.config.ts` — Vitest config (created in 04-12 Wave 4)
- [x] `dashboard/web/src/__tests__/setup.ts` — testing-library setup (created in 04-12 Wave 4)
- [x] `Makefile` targets `helm-lint-validate`, `helm-rbac-assert`, `make test-e2e -run TestTideTailCancelClose` — added in 04-14 Wave 8

*Wave 0 work is integrated into the existing Phase 1+2+3 test infrastructure. No new framework installs needed for Go (Ginkgo, envtest, tracetest, testutil/prometheus all already vendored). Vitest + @testing-library bootstrap is in-scope for Wave 4's frontend plan (04-12).*

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| OTLP traces visible end-to-end in Phoenix UI | OBS-03/04 | Requires running Phoenix instance + manual visual inspection of trace tree | Run `make deploy-kind`, port-forward Phoenix, point `OTEL_EXPORTER_OTLP_ENDPOINT` at it, apply demo Project, observe `tide.dispatch.*` span tree in Phoenix UI |
| Dashboard renders Planning + Execution DAGs side-by-side | DASH-01 | Visual layout correctness | `make deploy-kind && kubectl port-forward svc/tide-dashboard 8080:80`; open `http://localhost:8080`; apply demo Project; verify split pane + dagre layout + wave bands |
| `tide tail` keeps streaming across pod restart | CLI-04 | Requires real pod lifecycle | In kind: `kubectl delete pod <task-pod>`; observe `tide tail` reconnects within 5s without manual intervention |
| Krew install produces working `kubectl tide` symlink | CLI-02 distribution | Needs real Krew index | After GH release: `kubectl krew install --manifest-url <url> tide`; verify `kubectl tide --help` works |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies (filled by planner)
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 60s (quick-run target)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** approved 2026-05-17
