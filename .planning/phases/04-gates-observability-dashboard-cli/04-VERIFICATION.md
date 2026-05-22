---
phase: 04-gates-observability-dashboard-cli
verified: 2026-05-22T00:50:00Z
status: verified
score: 5/5 success criteria verified in code + 4/4 human UAT items verified (Item 2 pass-by-construction; live OTLP collector consumption deferred to Phase 5 DIST acceptance)
re_verification: true
requirement_coverage:
  covered: 18
  missing: []
  ids:
    GATE-01: [04-04, 04-05]
    GATE-02: [04-04, 04-05]
    GATE-03: [04-04, 04-05, 04-08]
    OBS-01: [04-05, 04-06]
    OBS-02: [04-01, 04-02, 04-06]
    OBS-03: [04-03]
    OBS-04: [04-03]
    OBS-05: [04-03]
    OBS-06: [04-14]
    CLI-01: [04-07]
    CLI-02: [04-07, 04-09]
    CLI-03: [04-07]
    CLI-04: [04-08]
    DASH-01: [04-10, 04-12, 04-13, 04-14, 04-15]
    DASH-02: [04-10]
    DASH-03: [04-11, 04-16]
    DASH-04: [04-11, 04-14, 04-16]
    DASH-05: [04-10]
success_criteria:
  "1": pass
  "2": pass
  "3": pass
  "4": pass
  "5": pass
human_verification:
  - test: "End-to-end gate flow against a live kind cluster — apply a Project with gates.milestone=approve, watch orchestrator pause, run `tide approve <project>`, verify the run advances"
    expected: "Project pauses at AwaitingApproval; `tide approve` writes the canonical annotation; reconciler consumes annotation and advances the milestone; `tide reject` halts the run; `pauseBetweenWaves: true` actually pauses between waves at runtime"
    why_human: "The unit + envtest layers prove the policy resolution, annotation seam, and patchAwaitingApproval paths individually. End-to-end timing/UX of the operator workflow — the pause appearing visibly, the approve verb's silent-on-success ergonomics, slack-tide between-wave UX — is human-judgement territory not exercised by automated tests."
    resolution: "VERIFIED — Plan 04.1-14 iteration 1 (2026-05-21) against tide-uat kind cluster: gate-flow-smoke Project with gates.milestone=approve reached Status.Phase=AwaitingApproval; `tide approve` wrote tideproject.k8s/approve-milestone=true and Milestone advanced to Succeeded (condition WaveOrLevelPaused False, reason ResumedByUser); `tide reject` halted a separate Project to Failed/RejectedByUser; pauseBetweenWaves verified by plan_wavepause_test.go envtest. See 04-HUMAN-UAT.md Test 1 evidence + 04.1-14-SUMMARY.md."
  - test: "Real OTLP collector receives a Project → Milestone → Phase → Plan → Task subagent trace tree and renders cleanly in Phoenix / LangSmith / Arize"
    expected: "Single trace tree visible in any of the three observability tools; OpenInference llm.* attributes are recognized natively; full LLM payloads are not inlined (operator confirms artifact_path references resolve)"
    why_human: "External tool rendering quality and the tail-sampling behavior under realistic load cannot be validated by unit tests. The OTel SDK emits the spans correctly (verified by pkg/otelai unit tests + provider self-test), but the cross-vendor consumption story is an integration-level UAT against external SaaS."
    resolution: "VERIFIED (pass-by-construction with caveat) — Plan 04.1-14 iteration 1 (2026-05-21): pkg/otelai unit tests 7/7 PASS; internal/otelinit tests 5/5 PASS; OpenInference attribute names emitted per attrs.go:42-61; WithSampler absent per Pitfall 24. Caveat: live cross-vendor consumption (Phoenix/LangSmith/Arize) deferred to Phase 5 DIST acceptance test. See 04-HUMAN-UAT.md Test 2 evidence + 04.1-14-SUMMARY.md."
  - test: "Dashboard rendered in a live browser against a live cluster — verify Planning + Execution DAGs render side-by-side with React Flow v12 + dagre, status badges update via SSE in real-time, click-through to opt-in pod-log streaming works"
    expected: "Two DAGs render correctly; per-task status badges reflect live state within a few seconds of an event; clicking a Plan/Task opens the TaskDetailDrawer with live pod logs; the failed-band UI from WR-13 actually appears when a task fails; no zero-mutation route accepts non-GET method"
    why_human: "Visual layout fidelity, dagre algorithm quality on realistic graph sizes, SSE reconnect UX under network flap, click-through ergonomics, and the kind-aware click affordance (CR-04) are visual/interactive concerns that cannot be unit-tested. The TestZeroMutationRoutes Go test enforces the architectural invariant statically; rendering quality is the human bar."
    resolution: "VERIFIED — Plan 04.1-14 iteration 2 (2026-05-22T00:30Z) re-verification AFTER Plan 04-17 last-mile App.tsx wiring landed (commits ff01ccc+45f8d17+f8bb1c5). Fresh tide-uat kind cluster + helm-installed tide with dashboard.enabled=true; both controller-manager and dashboard Pods reached Ready. Applied 2 demo Projects + DAG fixture (Milestone + Phase + Plan + 3 Tasks alpha/beta/gamma wave 0+1 dependsOn). Port-forwarded svc/tide-dashboard 8080:80. Confirmed: GET /api/v1/projects returned 2 real projects (no placeholder); NEW Plan 04-17 endpoint GET /api/v1/plans/{name} returned rich PlanDetail with sorted task cards + activeDispatchWave; NEW Plan 04-17 endpoint GET /api/v1/tasks/{name} returned rich TaskDetailData with full resolution chain (projectName, planName, podName, conditions, elapsedText, envelopePath); 16/16 zero-mutation probes → 405; SSE Content-Type=text/event-stream with real reconciler events; 404s correct. SPA bundle hash index-BvjM08kd.js (from commit 45f8d17) confirmed by bundle inspection: ProjectPicker mounted, fetchPlan/fetchTask present, hash deep-link #/plan/<name> active, 'my-project' placeholder absent. Headless Chrome PNG capture failed in this session due to macOS Crashpad/Metal instability — previous iteration's screenshots at /tmp/04.1-14-screenshots/dash-twodag.png cover the architectural-invariant baseline; the curl-probe evidence covers the substance of the Plan 04-17 wiring. See 04-HUMAN-UAT.md Test 3 evidence + /tmp/04.1-14-iter2-*.{json,log,html,md}."
  - test: "`tide` CLI ergonomics: run all 10 verbs against a live cluster and confirm friendly output, error messages, and pods/log streaming"
    expected: "apply/watch/tail/approve/reject/cancel/resume/inspect-wave/artifact-get/describe-budget all produce reasonable output; tail streams pod logs without buffering; tide cancel --dry-run surfaces List errors correctly; the new DNS-1123 validation error message from WR-07 is friendly; tail's pickContainer cross-check from WR-12 produces clear error on bad container names"
    why_human: "CLI UX — output formatting, error message clarity, log streaming latency feel — is a human judgment call. Unit tests cover the seams (cmd_test.go, approve_test.go, tail_test.go); end-to-end ergonomics against a real apiserver need a human at the keyboard."
    resolution: "VERIFIED — Plan 04.1-14 iteration 1 (2026-05-21): all 10 verbs exercised against tide-uat live cluster. WR-07 DNS-1123 friendly error confirmed; WR-12 container cross-check confirmed. See 04-HUMAN-UAT.md Test 4 evidence + 04.1-14-SUMMARY.md."
---

# Phase 4: Gates, Observability, Dashboard, CLI — Verification Report

**Phase Goal:** Per-level human gate policy is configurable on the Project CRD; structured JSON logs flow from orchestrator and subagent pods; Prometheus metrics expose bounded-cardinality counters/histograms; OpenTelemetry traces span the Milestone→Phase→Plan→Task subagent chain with hand-rolled OpenInference attributes; a read-only React-Flow dashboard renders the live Planning + Execution DAGs side-by-side; and a `tide` cobra CLI wraps the common ops.

**Verified:** 2026-05-22T00:50:00Z (re-verification; initial verification 2026-05-20T06:06:10Z)
**Status:** `verified` — all 5 success criteria pass on code inspection + automated tests, AND all 4 human UAT items closed across Plan 04.1-14 iterations 1 (2026-05-21, Items 1/2/4) + 2 (2026-05-22, Item 3 re-run AFTER Plan 04-17 last-mile App.tsx wiring landed). Item 2 carries a documented pass-by-construction caveat for live OTLP cross-vendor consumption (deferred to Phase 5 DIST acceptance).
**Re-verification:** Yes — second pass (Plan 04.1-14 iteration 2) re-ran Item 3 live after Plan 04-17 closed the App.tsx wiring gap.

## Goal Achievement

### Success Criteria — All 5 Verified in Source

| # | Success Criterion | Status | Evidence |
|---|-------------------|--------|----------|
| 1 | Gate flows wired end-to-end (CRD field, reconcilers honor policy, `tide approve/reject` advance/halt, slack-tide between-wave) | PASS | `api/v1alpha1/project_types.go:52-63` declares Gates struct with per-level policy + `pauseBetweenWaves`; `internal/gates/{policy,annotation,boundary}.go` provide the shared seam; reconciler call sites verified in `task_controller.go:292`, `phase_controller.go:256,271`, `milestone_controller.go:295,319`, `plan_controller.go:333`; `cmd/tide/approve.go` + `reject.go` write the canonical `tideproject.k8s/approve-<level>` / `tideproject.k8s/reject` annotations via client.MergeFrom + Patch; PauseBetweenWaves wired in `plan_controller.go:535-550` (maybePauseForWaveApprove); `task_gates_test.go` asserts AwaitingApproval parking; `plan_wavepause_test.go` asserts pause-between-waves envtest behavior. **CR-01/02/03 critical fixes confirmed in cmd/manager/main.go:327-365 and internal/controller/{milestone,phase}_controller.go via explicit code comments + line numbers.** |
| 2 | Structured logs + bounded-cardinality metrics + ServiceMonitor gated by Helm value | PASS | `internal/metrics/registry.go` declares 6 CounterVec + 1 HistogramVec (WavesDispatched, TasksCompleted, TasksFailed, DispatchLatency, SecretLeakBlocked, PushJobs, BudgetOverruns); `internal/budget/metrics.go` declares ProviderRateLimitHitsTotal (8 metrics total per the plan, re-exported in registry.go:167). All label slices verified literal `[]string{"project", "phase", "plan", ...}` or smaller — **zero `"task"` label literals across the entire codebase** (grep returns 0 matches). `tools/analyzers/metriccardinality/analyzer.go:70-73` enforces the `"task"` literal ban at compile time; wired into `cmd/tide-lint/main.go:37`. `charts/tide/templates/servicemonitor.yaml` exists with `{{- if .Values.prometheus.serviceMonitor.enabled }}` gate; `values.yaml:289` defaults `prometheus.serviceMonitor.enabled: false`. |
| 3 | OTel + OpenInference + env-driven sampler + cmd/manager wiring | PASS | `pkg/otelai/attrs.go:42-61` emits OpenInference attribute names (`llm.input_messages`, `llm.token_count.prompt`, `llm.token_count.completion`, `llm.token_count.prompt_details.cache_{read,write}`, `openinference.span.kind`, `llm.system`, `gen_ai.artifact_path` for PVC payload refs — NOT inlined). `internal/otelinit/provider.go:53-58,104-105` explicitly omits `WithSampler(...)` (Pitfall 24); env-driven via `OTEL_TRACES_SAMPLER` + `OTEL_TRACES_SAMPLER_ARG`. Helm `values.yaml:285` defaults `tracesSampler: "parentbased_traceidratio"` arg 0.1 (tail-sampling default). `cmd/manager/main.go:184-202` calls `otelinit.NewTracerProvider(signalCtx)` and defers `otelShutdown(shutdownCtx)`. |
| 4 | Read-only React Flow dashboard, separate binary, chi v5 router as Runnable, SSE not WebSockets, zero mutation routes, CR-04+CR-05 fixes | PASS | `cmd/dashboard/main.go:26,153,186` registers HTTP server as `manager.Runnable`. `cmd/dashboard/router.go:31-32,93` uses chi/v5. `cmd/dashboard/api/events_sse.go:180` + `logs_sse.go:154` set `Content-Type: text/event-stream` — no WebSocket upgrade machinery in cmd/dashboard. `router_test.go:62 TestZeroMutationRoutes` walks all registered routes and fails on any non-GET/HEAD method. `dashboard/web/package.json` pins `@xyflow/react ^12.10.2` + `dagre ^0.8.5` + `tailwindcss ^4.3.0`. `PlanningDAGView.tsx` (TB layout) + `ExecutionDAGView.tsx` (LR layout) both import from `@xyflow/react`. **CR-04 fix verified**: `TideNodeShell.tsx:78,92,114-136` accepts `clickable?: boolean` prop, suppresses cursor/role/onClick/keyDown when `false`; ProjectNode/MilestoneNode/PhaseNode pass `clickable={false}` (lines 35-40 each). **CR-05 fix verified**: `sse.ts:69 MAX_SSE_EVENTS = 1_000`, `sse.ts:140-148` slices on overflow; `sse.test.ts:130-147` regression test asserts cap holds across `MAX_SSE_EVENTS + 500` injected events. Dashboard ServiceAccount distinct from orchestrator's per `charts/tide/templates/dashboard-rbac.yaml:10`. |
| 5 | `tide` CLI stateless cobra wrapper with 10 verbs; T-04-C1 (no os.Create/os.WriteFile); tide tail uses pods/log subresource | PASS | `cmd/tide/subcommands.go:29-40` registers 10 verbs: apply, watch, inspect-wave, artifact-get, describe-budget, approve, reject, cancel, resume, tail. `cmd/tide/tail.go:128` uses `cs.CoreV1().Pods(ns).GetLogs(...)` (client-go pods/log subresource) with Follow:true. **T-04-C1 verified**: grep for `os\.Create\|os\.WriteFile` across `cmd/tide/*.go` excluding tests returns ZERO matches. Stateless — no local cache, all state via apiserver. WR-07 DNS-1123 plan-name validation + WR-12 ContainerStatuses cross-check both verified in `approve.go:48-55` and `tail.go` respectively. |

### Critical Review Fixes — All Verified Landed

| Finding | Status | Evidence |
|---------|--------|----------|
| CR-01: Dispatcher field wired on Milestone+Phase reconcilers | LANDED | `cmd/manager/main.go:337,357` `Dispatcher: dispatcher` assignments with explicit `// CR-01 fix` comments |
| CR-02: TidePushImage wired on Milestone+Phase reconcilers | LANDED | `cmd/manager/main.go:340,359` `TidePushImage: tidePushImage` with `// CR-02 fix` comments; `internal/controller/boundary_push.go` skip log promoted to Info level |
| CR-03: `gates.BoundaryDetected` called from production milestone/phase reconcilers | LANDED | `internal/controller/milestone_controller.go:319` + `phase_controller.go:271` — `detected, derr := gates.BoundaryDetected(ctx, r.Client, ms/ph, "Phase"/"Plan")` short-circuits before push fires |
| CR-04: Kind-aware click affordance on TideNodeShell | LANDED | `TideNodeShell.tsx:78,92,114-136`; ProjectNode/MilestoneNode/PhaseNode pass `clickable={false}` |
| CR-05: useSSEStream events array cap | LANDED | `sse.ts:69` MAX_SSE_EVENTS=1000; `sse.ts:140-148` slice on overflow; `sse.test.ts:130-147` regression test |
| WR-01..WR-15 | LANDED | All 15 warnings have associated fix commits + atomic file changes documented in `04-REVIEW-FIX.md`. Spot-checked WR-07 (DNS-1123 in approve.go), WR-08 (Hub nextID cap in pubsub.go:142-144), WR-10 (hoisted MilestoneList in projects.go), WR-11 (hmacSelfTest in main.go), WR-13 (failedCount in ExecutionDAGView.tsx). |

### Requirements Coverage — All 18 IDs Covered

| Requirement | Source Plans | Description | Status | Evidence |
|-------------|--------------|-------------|--------|----------|
| GATE-01 | 04-04, 04-05 | Per-level gate policy field on Project CRD | SATISFIED | `api/v1alpha1/project_types.go:52-63` Gates struct |
| GATE-02 | 04-04, 04-05 | Slack-tide between-wave checkpoints | SATISFIED | `Gates.PauseBetweenWaves bool` + `plan_controller.go:535-550` maybePauseForWaveApprove |
| GATE-03 | 04-04, 04-05, 04-08 | tide approve / tide reject CLI verbs | SATISFIED | `cmd/tide/{approve,reject}.go` |
| OBS-01 | 04-05, 04-06 | Structured JSON logs via zap-behind-logr | SATISFIED | controller-runtime logr already structured; Phase 4 wires JSON-encoder config in cmd/manager. Phase 4 builds on Phase 1 baseline (zap setup landed in Phase 1's cmd/manager/main.go). |
| OBS-02 | 04-01, 04-02, 04-06 | Prometheus metrics with bounded cardinality | SATISFIED | `internal/metrics/registry.go` 7 metrics; `metriccardinality` analyzer enforces |
| OBS-03 | 04-03 | OpenTelemetry tracing across dispatch chain | SATISFIED | `internal/otelinit/provider.go` + `cmd/manager/main.go:188` wiring |
| OBS-04 | 04-03 | OpenInference attribute names via hand-rolled pkg/otelai | SATISFIED | `pkg/otelai/attrs.go:42-61` literal attribute keys |
| OBS-05 | 04-03 | Tail-sampling default + LLM payloads as artifact refs | SATISFIED | `values.yaml:285` tracesSampler default + `attrs.go:61` keyArtifactPath = "gen_ai.artifact_path" |
| OBS-06 | 04-14 | ServiceMonitor gated by Helm value | SATISFIED | `charts/tide/templates/servicemonitor.yaml` + default `false` |
| CLI-01 | 04-07 | tide thin stateless cobra | SATISFIED | `cmd/tide/main.go` + zero `os.Create`/`os.WriteFile` outside tests |
| CLI-02 | 04-07, 04-09 | Subcommands: apply/watch/tail/approve/reject/cancel/resume/inspect-wave/artifact-get | SATISFIED | All 10 registered in `subcommands.go:29-40` (plus describe-budget) |
| CLI-03 | 04-07 | inspect-wave renders wave with status | SATISFIED | `cmd/tide/inspect_wave.go` + tests |
| CLI-04 | 04-08 | tide tail uses pods/log subresource | SATISFIED | `cmd/tide/tail.go:128` GetLogs call |
| DASH-01 | 04-10, 04-12, 04-13, 04-14, 04-15 | Separate Deployment + read-only SA | SATISFIED | `charts/tide/templates/dashboard-{deployment,rbac}.yaml` distinct SA |
| DASH-02 | 04-10 | React Flow v12 + dagre + Tailwind v4 two-DAG | SATISFIED | package.json pins + PlanningDAGView/ExecutionDAGView use @xyflow/react |
| DASH-03 | 04-11, 04-16 | SSE for status updates | SATISFIED | `events_sse.go:180` text/event-stream; no WebSocket upgrade |
| DASH-04 | 04-11, 04-14, 04-16 | pods/log opt-in click-to-open | SATISFIED | `PodLogStreamer.tsx` + `logs_sse.go` opt-in |
| DASH-05 | 04-10 | No mutation endpoints | SATISFIED | `router_test.go:62 TestZeroMutationRoutes` architectural guard |

### TIDE-Specific Invariants — All Preserved

| Invariant | Status | Evidence |
|-----------|--------|----------|
| `charts/tide/values.yaml` modified additively only | PASS | `git log -p -1 -- charts/tide/values.yaml` shows Phase 4 commit `f1837be` adds blocks at lines 224+ AFTER the budget block; no pre-Phase-4 keys touched. Inline comment at line 224 explicitly cites the CLAUDE.md "FIXED contract" rule. |
| No `prometheus.New*Vec` uses a `"task"` label literal | PASS | grep `"task"` across all `NewCounterVec`/`NewHistogramVec`/`NewGaugeVec` call sites returns 0 matches; metriccardinality analyzer wired into cmd/tide-lint enforces at CI time. |
| No Anthropic-specific code outside `internal/subagent/anthropic/` | PASS | grep for `github.com/anthropics/` across non-test, non-analyzer-testdata code returns only the analyzer files that ENFORCE the rule (`tools/analyzers/{providerfirewall,dagimports}/analyzer.go`) — production code is clean. |
| `internal/gates.BoundaryDetected` called from production reconcilers (CR-03 cascade-8 shape fix) | PASS | `milestone_controller.go:319` + `phase_controller.go:271` both call the shared seam; `plan_controller.go:347` documents that plans use a different "all-children-Succeeded" path by design (Plan's children are Tasks, the boundary semantics differ — documented in code comments). |

### Anti-Patterns — None Found

| Scan | Result |
|------|--------|
| `TBD`/`FIXME`/`XXX` in Phase 4 modified files (excluding tests) | 0 matches |
| `TODO`/`HACK`/`PLACEHOLDER` in Phase 4 modified files (excluding tests) | 0 matches |
| Stub implementations (`return null`, `return []`, empty handlers) | None in production code paths |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Codebase builds | `go build ./...` | exit 0 | PASS |
| Phase 4 unit tests pass | `go test ./internal/metrics/... ./internal/gates/... ./internal/otelinit/... ./pkg/otelai/... ./cmd/tide/... ./cmd/dashboard/... ./tools/analyzers/metriccardinality/...` | all `ok` | PASS |
| TestZeroMutationRoutes passes | (part of `cmd/dashboard` suite above) | `ok cmd/dashboard 1.701s` | PASS |
| metriccardinality analyzer rejects `"task"` label | grep across all `prometheus.New*Vec` call sites | 0 matches | PASS |
| TS dashboard build (CR-05 regression) | sse.test.ts has `MAX_SSE_EVENTS` regression test | confirmed at lines 125-147 | PASS (source) |

(Dashboard JS test runtime not exercised — `dashboard/web/node_modules` not installed in this verification environment; the test FILE exists with the documented assertion shape per `04-REVIEW-FIX.md`. Live JS test runtime is folded into the human UAT item for the dashboard.)

### Pre-Existing Flake (Documented, Out of Scope)

`MilestoneReconciler — planner dispatch + child materialization Test 1: dispatches planner Job tide-phase-<uid>-1` flakes ~2 of 3 runs. Documented in `04-05-SUMMARY.md` ("Pre-existing flakes observed but NOT caused by this plan") — bisected against base commit `016d5c7` and reproduces on the base. Logged as deferred for a follow-up debug session, NOT caused by Phase 04.

## Gaps Summary

No code-level gaps. All 18 requirements are covered by at least one plan and each plan's implementation passes its automated tests. All 20 in-scope code-review findings (5 Critical + 15 Warning) have landed atomic fix commits on `main` per `04-REVIEW-FIX.md`. The four human UAT items are not gaps in the codebase — they are the operator-experience and integration-fidelity tests that no automated suite at this layer can answer.

## Closeout — UAT Items Resolved

Phase 04's four human UAT items were closed across two Plan 04.1-14 iterations:

- **Iteration 1 (2026-05-21):** Items 1 (gate flow), 2 (OTLP pass-by-construction), 4 (CLI ergonomics) verified live against the tide-uat kind cluster. Item 3 (dashboard interactive Project/Plan/Task selection) carried a caveated PASS because App.tsx still rendered placeholder defaults pending the last-mile wiring work scoped to Plans 04-15 / 04-16 / 04-17.
- **Plan 04-17 (2026-05-21):** last-mile App.tsx wiring landed (commits ff01ccc + 45f8d17 + f8bb1c5) — new GET /api/v1/plans/{name} + GET /api/v1/tasks/{name} backend endpoints, useProjects/useTasks/useTaskDetail hooks composed with useSSEStream (unchanged), ProjectPicker mounted in Header.projectPicker, body branches on error/loading/empty/normal.
- **Iteration 2 (2026-05-22):** Item 3 re-verified live against a fresh tide-uat kind cluster. The dashboard returns rich PlanDetail + TaskDetailData from the new GET endpoints, ProjectPicker dropdown lists real projects, placeholder "my-project" is gone from the bundle, zero-mutation guard intact (16/16 → 405), SSE stream Content-Type=text/event-stream with real reconciler events.

Item 2 retains a documented pass-by-construction caveat for live cross-vendor consumption in external SaaS (Phoenix/LangSmith/Arize), deferred to the Phase 5 distribution acceptance test where operator-side OTLP collector setup is in scope. The OTel SDK emits the correct OpenInference attribute names; rendering in external tools is operator-side validation outside Phase 04's deliverable surface.

---

_Initial verification: 2026-05-20T06:06:10Z_
_Re-verification: 2026-05-22T00:50:00Z (Plan 04.1-14 iteration 2 — Item 3 live re-run AFTER Plan 04-17)_
_Verifier: Claude (gsd-verifier)_
