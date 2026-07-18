---
phase: 43-task-level-parity-trace-context-propagation
plan: 04
subsystem: observability
tags: [opentelemetry, otelai, trace-context, traceparent, controller, envtest]

# Dependency graph
requires:
  - phase: 43-01
    provides: podjob.BuildOptions.TraceParent / ReporterOptions.TraceParent carrier fields (inert until this plan wires callers)
  - phase: 43-02
    provides: conditional TRACEPARENT env append in BuildJobSpec + --traceparent Arg append in BuildReporterJob + cmd/tide-reporter flag registration
  - phase: 43-03
    provides: "synthesizePlannerSpan(..., parentSpanID) (SpanID, bool), spanIDFromHexOrZero, traceparentForLevel — the exact helpers this plan calls; all four planner completion handlers already resolve+persist their own {Level}TraceSpanID"
provides:
  - "Three planner dispatch-prep BuildOptions sites (Milestone/Phase/Plan) now carry TraceParent sourced from the immediate parent's persisted span ID; Project's carries none (root, D-02)"
  - "All four reporter-spawn sites (2 via spawnReporterIfNeeded, 2 inline BuildReporterJob) now carry TraceParent sourced from the level's OWN persisted span ID"
  - "internal/controller/dispatch_traceparent_test.go — envtest proof of both hops with exact W3C strings, including the absent-parent omission case"
affects: [44-llm-message-array-spans, 46-dashboard-deep-link]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Dispatch-prep hop reads the PARENT's persisted {Level}TraceSpanID (a new client.Get for Phase/Plan, free for Milestone); reporter-spawn hop reads THIS level's OWN persisted {Level}TraceSpanID — never conflated (RESEARCH Pattern 4)"
    - "traceparentForLevel(project, hex) is the single call-site idiom at both hops; empty/invalid input degrades to '' which BuildJobSpec/BuildReporterJob already turn into omission, never a malformed value"

key-files:
  created:
    - internal/controller/dispatch_traceparent_test.go
  modified:
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/dispatch_helpers.go

key-decisions:
  - "Confirmed and root-caused a PRE-EXISTING intermittent failure in span_emission_test.go (untouched by this plan): 'emits one attribute-complete AGENT span and is idempotent' patches proj.Status.ProjectTraceSpanID via the direct k8sClient then immediately calls handleJobCompletion, whose internal project resolution is a cache-backed r.Get with no sync guard — a real race, reproduced identically at commit 982bd95 (before any plan 43-04 change). Not fixed here (out of this task's file scope); documented for the record."
  - "Self-review caught the same race class latent in my own new specs before it could flake in CI: dispatch-hop spec 1 (direct-patch parent Phase, then cache-backed dispatch-prep read) and reporter-hop spec 3 (cache-backed read of a just-written status field) both needed explicit Eventually guards, not bare Gets. Fixed proactively; verified 8/8 clean isolated runs after the fix."

requirements-completed: [PROP-01]

# Metrics
duration: ~50min
completed: 2026-07-16
---

# Phase 43 Plan 04: Traceparent Propagation at Both Dispatch Hops Summary

**Threaded real W3C `traceparent` values into the carriers plan 43-02 built: each of the four planner levels' own subagent dispatch Job now carries its immediate parent's persisted span ID as `TRACEPARENT` env (Project's carries none, the trace root), and each level's reporter Job carries that level's OWN just-synthesized span ID as a `--traceparent` Arg — proven end-to-end by three new envtest specs with exact W3C strings.**

## Performance

- **Duration:** ~50 min
- **Completed:** 2026-07-16T17:47:27Z
- **Tasks:** 3 completed
- **Files modified:** 6 (1 created, 5 modified)

## Accomplishments

- **Task 1 (dispatch-prep hop):** `milestone_controller.go`, `phase_controller.go`, `plan_controller.go` each gained a `TraceParent:` field on their `podjob.BuildOptions` literal, sourced via `traceparentForLevel(project, <parent's persisted span-ID field>)`. Milestone's parent (Project) was already resolved — free. Phase and Plan each gained a genuinely new `client.Get` on their immediate parent (Milestone, Phase respectively), matching the exact shape their sibling completion handlers already used (43-03 precedent). Project's dispatch-prep gained a one-line comment documenting the deliberate omission (D-02) — no `TraceParent` field, since Project is the trace root.
- **Task 2 (reporter hop):** `spawnReporterIfNeeded` (`dispatch_helpers.go`) gained a trailing `traceParent string` parameter threaded into its internal `ReporterOptions` construction. All four reporter-spawn call sites now pass the level's OWN persisted span ID — never the parent's: Milestone/Phase via the updated helper call, Plan/Project via their inline `BuildReporterJob` literals (these two never routed through the shared helper, confirmed via a repo-wide grep before editing).
- **Task 3 (envtest proof):** new `internal/controller/dispatch_traceparent_test.go` with three Ginkgo specs (`Label("envtest", "heavy")`):
  1. Dispatch hop, value present (Plan level) — seeds the parent Phase's `PhaseTraceSpanID`, drives real Plan dispatch via `Reconcile`, asserts the created Job's subagent container carries the exact `00-<traceID>-<parentSpanID>-01` string.
  2. Dispatch hop, value absent — same chain with the parent's span ID never persisted; asserts the Job has NO `TRACEPARENT` env var at all (never a malformed value).
  3. Reporter hop (Milestone level) — invokes `handleJobCompletion` directly with a synthetic Job, re-fetches the persisted `MilestoneTraceSpanID` after the handler returns, and asserts the spawned reporter Job's Args carry the matching `--traceparent=...` string — proving emit → persist → mirror → reporter threading in one reconcile.

## Task Commits

Each task was committed atomically:

1. **Task 1: Dispatch-prep TRACEPARENT at the four planner BuildOptions sites** - `e56272e` (feat)
2. **Task 2: Reporter traceparent — spawnReporterIfNeeded param + all four ReporterOptions sites** - `ca54b71` (feat)
3. **Task 3: Envtest proof — dispatch-hop env and reporter-hop Arg** - `00239b5` (test)
4. **Follow-up fix (self-review): harden new specs against cache-lag races** - `889e1f8` (fix)

**Plan metadata:** SUMMARY.md commit follows this file (docs: complete plan)

## Files Created/Modified

- `internal/controller/milestone_controller.go` - dispatch-prep `TraceParent` (parent=Project, already resolved); reporter-spawn call now passes `traceparentForLevel(project, ms.Status.MilestoneTraceSpanID)`
- `internal/controller/phase_controller.go` - new parent-Milestone `client.Get` at dispatch-prep + `TraceParent` field; reporter-spawn call passes own `PhaseTraceSpanID`
- `internal/controller/plan_controller.go` - new parent-Phase `client.Get` at dispatch-prep + `TraceParent` field; inline `ReporterOptions` literal gains own `PlanTraceSpanID`
- `internal/controller/project_controller.go` - deliberate-omission comment at dispatch-prep (no `TraceParent`, D-02 root); inline `ReporterOptions` literal gains own `ProjectTraceSpanID`
- `internal/controller/dispatch_helpers.go` - `spawnReporterIfNeeded` gains trailing `traceParent string` param, threaded into `ReporterOptions`
- `internal/controller/dispatch_traceparent_test.go` (new) - three envtest specs proving both hops with exact W3C strings

## Decisions Made

**Dedicated `client.Get` over restructuring `resolveProject`/`resolveProjectForPlan`:** matches 43-03's precedent exactly — both helpers have multiple existing call sites, so a single dedicated fetch at the dispatch-prep site (mirroring the identical fetch already added at the completion-handler site) has lower blast radius than changing either helper's return shape.

**Pre-existing flake root-caused, not fixed (out of scope):** `span_emission_test.go`'s "emits one attribute-complete AGENT span and is idempotent" spec (Milestone level) fails intermittently (~1-in-2 to 1-in-3 across repeated runs) with `Expected <string>: 0000000000000000 to equal <string>: b7ad6b7169203331`. Root cause confirmed by direct code reading: the test patches `proj.Status.ProjectTraceSpanID` via the direct `k8sClient` then immediately calls `handleJobCompletion`, whose internal Project resolution (`r.Get`) reads through the cache-backed `mgrClient` with no sync guard between the write and the read — a genuine cache-lag race in the test itself. Verified pre-existing and unrelated to this plan by reverting all four of this plan's modified files to their commit-982bd95 (pre-Task-1) state and reproducing the identical failure 3 of 4 runs. Not fixed here: the file is outside this task's declared file scope (`internal/controller/dispatch_traceparent_test.go` only) and fixing another plan's test is a separate concern from this plan's PROP-01 delivery.

**Self-review caught (and fixed) the same race class in my own new specs before they could flake in CI:** dispatch-hop spec 1 patched the parent Phase's status via the direct client then immediately drove Plan dispatch (cache-backed read) with no sync guard — added an `Eventually` waiting for the manager cache to observe the specific patched field before triggering dispatch, since the Job is created exactly once (a stale first read would have baked in an empty parent span ID permanently, with no self-healing on retry). Reporter-hop spec 3 read back `refreshed.Status.MilestoneTraceSpanID` with a bare `Get` immediately after `handleJobCompletion`'s own status-persistence write (same cache-backed client) to build the expected Arg string — wrapped in `Eventually`, matching `span_emission_test.go`'s own (correctly guarded) precedent for the identical assertion. Verified 8/8 clean isolated runs of the three new specs after the fix, plus two clean full `make test-heavy` runs.

## Deviations from Plan

None beyond the self-review fix documented above (which is itself the deviation-handling process working as intended — Rule 1/2 auto-fix, tracked, verified).

**Total deviations:** 1 auto-fixed (test-hardening, caught in self-review before any CI exposure). **Impact:** strictly improves test robustness; no production code path touched by the fix.

## Issues Encountered

`go build ./...` (full repo) still fails on the pre-existing, unrelated `cmd/tide-demo-init/main.go:112` (`pattern all:fixture: no matching files found`) — identical to the environmental gap documented in plans 43-01/43-02/43-03's summaries; confirmed untouched by this plan's diff (`git diff --stat cmd/tide-demo-init/main.go` empty). `go build ./internal/...` and `go vet ./internal/controller/...` are both clean.

`make test-heavy` and `make test-int-fast` both intermittently fail on the pre-existing `span_emission_test.go:270` cache-lag race described above (not this plan's file, not this plan's diff). Both targets achieved clean (`MAKE_EXIT=0`) runs after 1-3 retries in this session: `make test-heavy` clean at 29/29 Specs; `make test-int-fast` clean at Layer A 56/56 + Layer A2 (heavy) 29/29 in the same process.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- PROP-01 is complete for the four planner levels: dispatch Jobs carry the parent's span as `TRACEPARENT` env, reporter Jobs carry the level's own span as `--traceparent` Arg, the two recipients are never conflated, and missing data degrades to omission — all proven by envtest with exact W3C strings.
- Task-level parity (Task's own dispatch/reporter wiring, sibling plan 43-05) is unaffected by this plan — no shared files were touched with `task_controller.go`.
- The pre-existing `span_emission_test.go:270` flake remains open; it is not gated on this plan and should be tracked as a separate quick-fix (add a `waitForCacheSync`-equivalent guard after the `proj.Status.ProjectTraceSpanID` patch, before calling `handleJobCompletion`) if the orchestrator wants it closed.
- No blockers for wave completion.

## Self-Check: PASSED

- `internal/controller/milestone_controller.go` — FOUND, `TraceParent:` count 1
- `internal/controller/phase_controller.go` — FOUND, `TraceParent:` count 1
- `internal/controller/plan_controller.go` — FOUND, `TraceParent:` count 2
- `internal/controller/project_controller.go` — FOUND, `TraceParent:` count 1
- `internal/controller/dispatch_helpers.go` — FOUND, `TraceParent:` count 1
- `internal/controller/dispatch_traceparent_test.go` — FOUND
- `e56272e`, `ca54b71`, `00239b5`, `889e1f8` — FOUND (`git log --oneline 982bd95..HEAD`)
- `go build ./internal/...` — PASS
- `go vet ./internal/controller/...` — PASS
- `go test ./internal/controller/ -run TestControllers -ginkgo.focus="PROP-01" -ginkgo.label-filter="heavy" -count=1` — PASS (8/8 consecutive clean runs)
- `make test-heavy` — PASS (29 Passed, 0 Failed, 192 Skipped, `MAKE_EXIT=0`, clean run obtained)
- `make test-int-fast` — PASS (Layer A 56/56, Layer A2 29/29, `MAKE_EXIT=0`, clean run obtained)

---
*Phase: 43-task-level-parity-trace-context-propagation*
*Completed: 2026-07-16*
