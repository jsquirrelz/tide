---
phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary
plan: 05
subsystem: observability
tags: [kubernetes, task-controller, reporter, opentelemetry, trace-only, envtest]

# Dependency graph
requires:
  - phase: 44 (plan 44-02)
    provides: ReporterOptions.TraceOnly/TraceOnlyJobKey/OTLPEndpoint + BuildReporterJob's trace-only Job shape
  - phase: 44 (plan 44-04)
    provides: the --trace-only flag registered on the tide-reporter binary
  - phase: 43
    provides: emitTaskSpanOnce (Task's own AGENT span + TaskTraceSpanID mirror) and traceparentForLevel
provides:
  - TaskReconcilerDeps.ReporterImage + OTLPEndpoint fields, wired in cmd/manager/main.go
  - spawnTaskTraceReporterIfNeeded — D-06-gated, idempotent, log-and-continue trace-only reporter spawn keyed on the completed dispatch Job's UID
  - Both handleJobCompletion terminal call sites (EnvelopeReadFailed + post-ReadOut) now attempt the spawn, covering all four Task terminal paths (success and failure, D-02/D-05)
  - envtest proof: shape assertions (Args/Env/labels/ownerRef), D-06 absence proof, non-interference proof
affects: [phase-45-adapter-seam, phase-46-span-enrichment]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Observability spawn helpers return void and log-and-continue on every failure — never gate the caller's terminal-state machinery (mirrors Phase 42 D-04's envelope-degraded precedent, now extended to Job-spawn errors)"
    - "Trace-only reporter Jobs are keyed on the COMPLETED dispatch Job's UID, not the parent's UID — a retried attempt gets its own trace-only spawn without colliding with a prior attempt's"

key-files:
  created:
    - internal/controller/task_traceonly_reporter_test.go
  modified:
    - internal/controller/task_controller.go
    - cmd/manager/main.go

key-decisions:
  - "spawnTaskTraceReporterIfNeeded reads task.Status.TaskTraceSpanID immediately after emitTaskSpanOnce in the same reconcile — correct because emitTaskSpanOnce mirrors the freshly-minted span ID onto the in-memory object before its own persistence patch lands (43-05 precedent)"
  - "D-06 gate (OTLPEndpoint == \"\") checked before any API call, ahead of the ReporterImage check — zero Job churn on plain clusters is the priority ordering"

requirements-completed: [MSG-01]

# Metrics
duration: 24min
completed: 2026-07-16
---

# Phase 44 Plan 05: Task-Level Trace-Only Reporter Spawn Summary

**Task completions (success and failure) now spawn a D-06-gated, idempotent trace-only tide-reporter Job carrying the Task's own span as `--traceparent`, closing MSG-01's manager-side wiring.**

## Performance

- **Duration:** 24 min
- **Started:** 2026-07-16T19:22:42-04:00
- **Completed:** 2026-07-16T19:46:06-04:00
- **Tasks:** 2 completed
- **Files modified:** 3 (2 modified, 1 created)

## Accomplishments
- `TaskReconcilerDeps` gained `ReporterImage`/`OTLPEndpoint`, wired in `cmd/manager/main.go`'s `TaskReconcilerDeps` literal alongside the existing `plannerDeps` wiring
- `spawnTaskTraceReporterIfNeeded` — a new void-returning, log-and-continue method — spawns the trace-only reporter Job at both of `handleJobCompletion`'s terminal call sites (the `EnvelopeReadFailed` branch and the post-`ReadOut` site), covering all four Task terminal paths uniformly
- envtest proof (`internal/controller/task_traceonly_reporter_test.go`, 3 specs): exact shape assertions (Args, Env, role label, ownerRef) when an OTLP endpoint is configured; hard absence proof when it isn't (D-06); non-interference proof that the spawn never perturbs Task's own terminal `Phase`

## Task Commits

Each task was committed atomically:

1. **Task 1: spawnTaskTraceReporterIfNeeded + deps + manager wiring** - `ea5b563` (feat)
2. **Task 2: envtest proof — spawn-with-endpoint, skip-without-endpoint, shape assertions** - `ab7093d` (test)

_No plan-metadata commit yet — this SUMMARY.md commit is that final commit (worktree mode; orchestrator merges and updates STATE.md/ROADMAP.md centrally)._

## Files Created/Modified
- `internal/controller/task_controller.go` - Adds `TaskReconcilerDeps.ReporterImage`/`OTLPEndpoint` fields; adds `spawnTaskTraceReporterIfNeeded` method; wires it into both `handleJobCompletion` terminal call sites
- `cmd/manager/main.go` - Wires `ReporterImage: reporterImage` and `OTLPEndpoint: otlpEndpoint` into the `TaskReconcilerDeps` literal
- `internal/controller/task_traceonly_reporter_test.go` - New envtest file: 3 specs proving spawn shape, D-06 absence, and non-interference

## Decisions Made
- Confirmed via source read that `spawnTaskTraceReporterIfNeeded` must read `task.Status.TaskTraceSpanID` in the SAME reconcile immediately after `emitTaskSpanOnce` runs (not a separately-seeded/persisted value) — the Task's own span for this attempt is minted moments earlier in the identical call, and `emitTaskSpanOnce` mirrors it in-memory before its own persistence patch. The envtest spec captures the emitted span's ID from the OTel in-memory exporter rather than pre-seeding a constant, since this reconcile is what mints it (this differs from 43-05's dispatch-hop test, which legitimately pre-seeds a PARENT level's already-persisted span ID from an earlier, separate reconcile).
- Gate ordering inside `spawnTaskTraceReporterIfNeeded`: nil-object guards first, then `OTLPEndpoint == ""` (D-06, before any API call), then `ReporterImage == ""` — matches the acceptance criterion "D-06 gate... BEFORE any API call."

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Test fixture Job UID exceeded the 63-byte Kubernetes label-value limit**
- **Found during:** Task 2 (envtest proof) — first `make test-heavy` run
- **Issue:** The initial test fixture built the synthetic completed dispatch Job's name/UID as `"tide-task-" + task.UID + "-1"` (mirroring `span_emission_test.go`'s Task-level pattern, which never actually creates a real K8s Job from that name). Because `spawnTaskTraceReporterIfNeeded` DOES create a real Job named `"tide-reporter-trace-" + completedJob.UID`, the long synthetic UID pushed the resulting Job name past 63 characters. Kubernetes auto-injects a `job-name` label (value = the Job's own name) onto the pod template, and label values are capped at 63 bytes — Create failed with `spec.template.labels: ... must be no more than 63 bytes`. In production this never fires: real dispatch Jobs get a server-assigned 36-character UUID, well under the cap after the `tide-reporter-trace-` prefix.
- **Fix:** Changed the test fixture to use short, deterministic Job names (`"ttr-shape-job"`, `"ttr-noendpoint-job"`, `"ttr-noninterfere-job"`) decoupled from `task.UID`, with an inline comment explaining the constraint.
- **Files modified:** `internal/controller/task_traceonly_reporter_test.go`
- **Verification:** `make test-heavy` green (MAKE_EXIT=0, zero FAIL lines); 5 consecutive focused runs of the new Describe block all passed
- **Committed in:** `ab7093d` (Task 2 commit — the fixture fix landed before the first commit of this file; no separate commit needed)

**2. [Rule 1 - Bug] Test incorrectly pre-seeded a constant span ID instead of capturing the freshly-minted one**
- **Found during:** Task 2 (envtest proof) — second `make test-heavy` run
- **Issue:** The first draft of spec 1 pre-seeded `task.Status.TaskTraceSpanID = seededParentSpanIDHex` before calling `handleJobCompletion`, expecting `spawnTaskTraceReporterIfNeeded` to read that value. In fact `emitTaskSpanOnce` — called immediately before, in the same code path — mints a brand-new span for this attempt (the fixture's completed Job has real Start/Completion timestamps, satisfying `plannerSpanResolvable`) and overwrites the in-memory field with its own fresh span ID, clobbering the seeded constant before the read.
- **Fix:** Swapped in a `tracetest.InMemoryExporter` (mirroring `span_emission_test.go`'s Task-level `BeforeEach`/`AfterEach`), captured the actually-emitted span's `SpanID` from the exporter after `handleJobCompletion` returns, and asserted the spawned Job's `--traceparent` Arg against that captured value instead of a pre-seeded constant.
- **Files modified:** `internal/controller/task_traceonly_reporter_test.go`
- **Verification:** `make test-heavy` green (MAKE_EXIT=0, zero FAIL lines); `make test-int-fast` green (56/56 Layer A + full heavy suite)
- **Committed in:** `ab7093d` (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (both Rule 1 — test-fixture bugs discovered while proving the envtest spec, not production-code bugs)
**Impact on plan:** Both fixes were necessary to make the envtest proof correct and green; neither touched production code (`task_controller.go`/`cmd/manager/main.go` were correct as written in Task 1). No scope creep.

## Issues Encountered
None beyond the two auto-fixed test-fixture issues documented above.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- MSG-01 closes end-to-end at the manager side: every completed Task (success and failure) attempts a trace-only reporter spawn when an OTLP endpoint is configured; zero spawns on plain clusters (D-06 proven); Task's own terminal-state machinery is unaffected (proven).
- This plan only wires the SPAWN side. The trace-only reporter binary's own behavior (reading `events.jsonl`, redaction, size-bounding, actual span synthesis) is other 44-series plans' territory (`tracesynth.go`, `internal/harness/redact.String`, `pkg/otelai` tool-call helpers) — this plan does not touch those files and makes no claims about them.
- No blockers for Phase 45 (adapter seam) or Phase 46 (span enrichment) — both consume the `traceparent`/`--traceparent` propagation contract this plan (and 43/44-02/44-04) already established.

## Self-Check: PASSED

- FOUND: internal/controller/task_controller.go (modified, contains spawnTaskTraceReporterIfNeeded)
- FOUND: cmd/manager/main.go (modified, ReporterImage/OTLPEndpoint wired in TaskReconcilerDeps literal)
- FOUND: internal/controller/task_traceonly_reporter_test.go (created)
- FOUND commit ea5b563 (git log --oneline --all)
- FOUND commit ab7093d (git log --oneline --all)

---
*Phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary*
*Completed: 2026-07-16*
