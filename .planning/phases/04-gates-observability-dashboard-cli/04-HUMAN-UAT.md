---
status: partial
phase: 04-gates-observability-dashboard-cli
source: [04-VERIFICATION.md]
started: 2026-05-20T01:10:00Z
updated: 2026-05-20T01:10:00Z
---

## Current Test

[awaiting human testing]

## Tests

### 1. End-to-end gate flow against a live kind cluster
expected: Apply a Project CRD with `gates: { milestone: approve, phase: auto, plan: auto, task: auto, pauseBetweenWaves: true }`. Watch the orchestrator pause at the milestone boundary (`Project.status.phase = AwaitingApproval`). Run `tide approve <project>` — annotation `tideproject.k8s/approve-milestone` is written, reconciler consumes it, milestone advances. Run `tide reject <project>` on a different Project — run halts. Confirm `pauseBetweenWaves: true` actually pauses between waves at runtime.
result: [pending]

### 2. Real OTLP collector receives full Project → Milestone → Phase → Plan → Task trace tree
expected: Point `OTEL_EXPORTER_OTLP_ENDPOINT` at a live OTLP collector. Drive a Project end-to-end. In Phoenix / LangSmith / Arize, see the dispatch chain rendered as a single trace tree. Verify OpenInference `llm.*` attributes (`llm.input_messages`, `llm.token_count.prompt`, `llm.token_count.completion`, `gen_ai.artifact_path`) are recognized natively without bespoke instrumentation. Confirm full LLM payloads are NOT inlined — `artifact_path` references resolve to PVC paths.
result: [pending]

### 3. Dashboard rendered live in browser against live cluster
expected: Open `https://tide-dashboard.<cluster>/`. Planning + Execution DAGs render side-by-side via React Flow v12 + dagre. Per-task status badges update via SSE within seconds of state changes. Click a Plan node in Planning DAG → ExecutionDAGView populates with the correct plan; click a Task → TaskDetailDrawer opens with live pod logs. Trigger a task failure — the failed-band UI from WR-13 appears for that wave. Probe the dashboard with `curl -X POST` against any route — zero-mutation guard (TestZeroMutationRoutes) blocks all non-GET methods.
result: [pending]

### 4. `tide` CLI ergonomics against live cluster
expected: Exercise all 10 verbs (`apply`, `watch`, `tail`, `approve`, `reject`, `cancel`, `resume`, `inspect-wave`, `artifact-get`, `describe-budget`). Confirm friendly output and error messages. `tide tail` streams pod logs without buffering or visible lag. `tide cancel --dry-run` surfaces List errors correctly (WR-06 fix). DNS-1123 validation error from `tide approve --wave bad..name/1` is friendly (WR-07). `tide tail` with a bogus container name produces the cross-checked error from WR-12 ("container X not found in pod Y; available: [...]"). `tide cancel my-project` without `--force` exits 1 with destructive-warning.
result: [pending]

## Summary

total: 4
passed: 0
issues: 0
pending: 4
skipped: 0
blocked: 0

## Gaps
