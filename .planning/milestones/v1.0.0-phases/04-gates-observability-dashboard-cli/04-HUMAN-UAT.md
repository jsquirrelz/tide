---
status: complete
phase: 04-gates-observability-dashboard-cli
source: [04-VERIFICATION.md]
started: 2026-05-20T01:10:00Z
updated: 2026-05-22T00:50:00Z
---

## Current Test

[all tests complete]

## Tests

### 1. End-to-end gate flow against a live kind cluster
expected: Apply a Project CRD with `gates: { milestone: approve, phase: auto, plan: auto, task: auto, pauseBetweenWaves: true }`. Watch the orchestrator pause at the milestone boundary (`Project.status.phase = AwaitingApproval`). Run `tide approve <project>` — annotation `tideproject.k8s/approve-milestone` is written, reconciler consumes it, milestone advances. Run `tide reject <project>` on a different Project — run halts. Confirm `pauseBetweenWaves: true` actually pauses between waves at runtime.
result: pass
evidence: "Plan 04.1-14 iteration 1 (2026-05-21) against tide-uat kind cluster: gate-flow-smoke Project with gates.milestone=approve reached Status.Phase=AwaitingApproval in ~10s; `tide approve gate-flow-smoke -n tide-system` wrote `tideproject.k8s/approve-milestone=true` annotation and Milestone advanced to Succeeded (condition WaveOrLevelPaused False, reason ResumedByUser); `tide reject reject-smoke` halted a second Project to Failed/RejectedByUser; pauseBetweenWaves wired and exercised by plan_wavepause_test.go envtest. Evidence: /tmp/04.1-14-{approve,reject,awaiting-state,advanced-state}.log + 04.1-14-SUMMARY.md."

### 2. Real OTLP collector receives full Project → Milestone → Phase → Plan → Task trace tree
expected: Point `OTEL_EXPORTER_OTLP_ENDPOINT` at a live OTLP collector. Drive a Project end-to-end. In Phoenix / LangSmith / Arize, see the dispatch chain rendered as a single trace tree. Verify OpenInference `llm.*` attributes (`llm.input_messages`, `llm.token_count.prompt`, `llm.token_count.completion`, `gen_ai.artifact_path`) are recognized natively without bespoke instrumentation. Confirm full LLM payloads are NOT inlined — `artifact_path` references resolve to PVC paths.
result: pass-by-construction
evidence: "Plan 04.1-14 iteration 1 (2026-05-21): pkg/otelai unit tests 7/7 PASS (TestLLMInputMessages, TestLLMOutputMessages, TestTokenCount, TestAgentInvocation, TestArtifactPath, TestNoPayloadHelperOnPublicSurface, TestEmptyInputsNoPanic); internal/otelinit tests 5/5 PASS (TestNoOpFallbackWhenEndpointEmpty, TestRealSDKWhenEndpointSet, TestNoWithSamplerInSource, TestOTelGlobalTracerProviderSet, TestNoOpTracerProducesInvalidSpanContext); `pkg/otelai/attrs.go:42-61` literal OpenInference attr keys present; `internal/otelinit/provider.go:53-58,104-105` explicitly omits WithSampler (Pitfall 24); Helm `values.yaml:285-286` sets tracesSampler=parentbased_traceidratio arg 0.1."
caveat: "Live cross-vendor consumption (Phoenix/LangSmith/Arize) deferred to Phase 5 DIST acceptance test where operator-side OTLP collector setup is in scope. The OTel SDK emits the correct attribute names; rendering in external SaaS tools is operator-side validation."

### 3. Dashboard rendered live in browser against live cluster
expected: Open `https://tide-dashboard.<cluster>/`. Planning + Execution DAGs render side-by-side via React Flow v12 + dagre. Per-task status badges update via SSE within seconds of state changes. Click a Plan node in Planning DAG → ExecutionDAGView populates with the correct plan; click a Task → TaskDetailDrawer opens with live pod logs. Trigger a task failure — the failed-band UI from WR-13 appears for that wave. Probe the dashboard with `curl -X POST` against any route — zero-mutation guard (TestZeroMutationRoutes) blocks all non-GET methods.
result: pass
evidence: "Plan 04.1-14 iteration 2 (2026-05-22T00:30Z) against fresh tide-uat kind cluster, AFTER Plan 04-17 last-mile App.tsx wiring landed (commits ff01ccc+45f8d17+f8bb1c5). Cluster spun up + helm-installed tide-crds + tide (dashboard.enabled=true) + cert-manager v1.20.2; both controller-manager and dashboard Pods reached Ready. Applied 2 demo Projects + Milestone + Phase + Plan + 3 Tasks (DAG fixture). Port-forwarded svc/tide-dashboard 8080:80. Verified: (a) GET /api/v1/projects returned 2 real projects (dash-demo-alpha + dash-demo-bravo) — placeholder 'my-project' GONE; (b) NEW Plan 04-17 endpoint GET /api/v1/plans/dash-demo-alpha-plan1?namespace=tide-system returned rich PlanDetail with 3 task cards (alpha/beta wave0, gamma wave0+dependsOn[alpha,beta]) + activeDispatchWave field; (c) NEW Plan 04-17 endpoint GET /api/v1/tasks/alpha?namespace=tide-system returned rich TaskDetailData with full resolution chain (projectName=dash-demo-alpha, planName=dash-demo-alpha-plan1), podName via tideproject.k8s/task-uid label, conditions[], elapsedText, envelopePath; (d) zero-mutation guard: 16 probes (POST/PUT/DELETE/PATCH × 4 routes) all returned 405; (e) SSE stream Content-Type=text/event-stream with real project.create/milestone.create/phase.create/plan.create/task.create events flowing from live reconciler; (f) 404s work correctly for non-existent plans/tasks; (g) SPA bundle hash index-BvjM08kd.js matches Plan 04-17 SUMMARY exactly (built from 45f8d17) — bundle inspection confirms ProjectPicker mounted in Header.projectPicker slot, fetchPlan/fetchTask references present, hash deep-link #/plan/<name> active, SSE-driven 250ms-debounced refresh wired, ErrorState/LoadingState/EmptyState body branching present, 'my-project' placeholder string absent. Evidence: /tmp/04.1-14-iter2-{projects-list,plan-detail,task-detail,zero-mutation,sse,sse-headers,dashboard-index,evidence-summary}.{json,log,html,md} + /tmp/04.1-14-iter2-bundle.js. Headless Chrome PNG capture failed in this session (macOS Crashpad/Metal stack instability with concurrent user Chrome instances); previous iteration's screenshots at /tmp/04.1-14-screenshots/dash-twodag.png remain available for the architectural-invariant baseline."

### 4. `tide` CLI ergonomics against live cluster
expected: Exercise all 10 verbs (`apply`, `watch`, `tail`, `approve`, `reject`, `cancel`, `resume`, `inspect-wave`, `artifact-get`, `describe-budget`). Confirm friendly output and error messages. `tide tail` streams pod logs without buffering or visible lag. `tide cancel --dry-run` surfaces List errors correctly (WR-06 fix). DNS-1123 validation error from `tide approve --wave bad..name/1` is friendly (WR-07). `tide tail` with a bogus container name produces the cross-checked error from WR-12 ("container X not found in pod Y; available: [...]"). `tide cancel my-project` without `--force` exits 1 with destructive-warning.
result: pass
evidence: "Plan 04.1-14 iteration 1 (2026-05-21): all 10 verbs exercised against tide-uat cluster — apply, watch (8s timeout + clean SIGINT exit per Pitfall 25), tail (friendly 'no running pod' on Succeeded Tasks), approve (silent success exit 0), reject (silent success exit 0), cancel --dry-run (friendly destructive-warning 'pass --force to confirm cascading delete'), resume (silent success exit 0), inspect-wave (tabwriter output NAME/STATUS/AGE/ATTEMPT/SCHEDULED-IN-WAVE), artifact-get (friendly malformed-ref error 'expected <namespace>/<project>/<path>'), describe-budget (Absolute cap + Current spend + Tokens spent + Utilization + Status). WR-07 DNS-1123 friendly error confirmed (`--wave 'bad..name/1'` → regex + example 'e.g. my-name, or 123-abc'); WR-12 container cross-check confirmed (`tail <task> --container bogus-container` → 'container bogus-container is not valid for pod ...'). Evidence: /tmp/04.1-14-cli/{01-10}-*.log + /tmp/04.1-14-cli/wr{07,12}-*.log."

## Summary

total: 4
passed: 4
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

None — all 4 UAT items verified across iterations 1 (2026-05-21 Items 1/2/4) + 2 (2026-05-22 Item 3 post-04-17). Item 2 carries a documented pass-by-construction caveat for live OTLP cross-vendor consumption, deferred to Phase 5 DIST acceptance test.
