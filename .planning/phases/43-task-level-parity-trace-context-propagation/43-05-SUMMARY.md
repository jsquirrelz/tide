---
phase: 43-task-level-parity-trace-context-propagation
plan: 05
subsystem: observability
tags: [opentelemetry, otelai, trace-context, span-parenting, controller, task-reconciler]

# Dependency graph
requires:
  - phase: 43-01
    provides: TaskSpanEmittedUID/TaskTraceSpanID durable status fields on TaskStatus this plan gates emission on and persists to
  - phase: 43-03
    provides: parenting-aware synthesizePlannerSpan(ctx, level, project, helmDefaults, completedJob, out, envReadOK, parentSpanID) (trace.SpanID, bool), spanIDFromHexOrZero, traceparentForLevel — the fixed interfaces this plan compiles against
provides:
  - "emitTaskSpanOnce — Task's marker-gated (TaskSpanEmittedUID), mark-then-emit span-emission method, with two call sites in handleJobCompletion covering all four Task terminal paths (generalized Option B)"
  - "Task's dispatch-prep TRACEPARENT: createDispatchJob fetches the parent Plan and threads its persisted span ID through traceparentForLevel into podjob.BuildOptions.TraceParent"
  - "Fifth 'SpanEmission — Task level' envtest Describe block proving parenting, deterministic TraceID, idempotency, degraded-envelope, and PROP-02 persistence for Task"
  - "task_dispatch_traceparent_test.go proving the real Task dispatch Job carries TRACEPARENT sourced from the parent Plan's persisted span"
affects: [44-llm-message-array-spans]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Generalized Option B: two emitTaskSpanOnce call sites cover Task's FOUR terminal paths (vs. the four planner levels' one) — inside EnvelopeReadFailed (envReadOK=false) and immediately after a successful envelope read, BEFORE the OutputValidationError/OutputPathsViolation/standard-result branch divergence (envReadOK=true) — so a single post-read call site covers three downstream branches"
    - "Task's Plan fetch (for both dispatch-prep TRACEPARENT and completion-time parentSpanID) mirrors Plan's own Phase fetch: resolveProject's label fast-path never touches Plan, so it is a genuinely new client.Get at each of the two call sites, degrading to an empty/zero value on miss rather than blocking"

key-files:
  created:
    - internal/controller/task_dispatch_traceparent_test.go
  modified:
    - internal/controller/task_controller.go
    - internal/controller/span_emission_test.go

key-decisions:
  - "Generalized Option B (43-05-PLAN.md's explicit decision, not left implicit): two call sites in handleJobCompletion, not one. Call site 1 sits inside the EnvelopeReadFailed branch (Task's only envReadOK=false path); call site 2 sits immediately after a successful ReadOut, before the OutputValidationError/OutputPathsViolation/standard-result branches diverge — covering all three post-read paths with one line."
  - "Output-path-validation test (spec 5) cannot force a genuine OutputValidationError/OutputPathsViolation in this environment: task_controller.go hardcodes taskWorkspaceRoot to /workspaces/<project.UID>/workspace, and this sandbox's root filesystem is read-only outside /tmp and the repo checkout (verified: `mkdir /workspaces/probe` → 'Read-only file system'). validateControllerOutputPaths treats a missing root as fs.ErrNotExist → skipped=true, never reaching vErr or violations — the same limit the pre-existing TestTaskReconciler_OnJobSucceeded_FlagsOutputPathsViolation test already hedges on ('Either way, task moves to terminal state'). The spec instead proves what IS provable: call site 2 already emitted the span before this block runs, so entering and falling through the skipped-validation fallback neither duplicates nor loses it."
  - "Fixed a pre-existing intermittent flake discovered while stabilizing this plan's new tests: all four planner-level blocks' (Milestone/Phase/Plan) TRACE-02 parent-linkage seed steps patch the parent's status via the direct k8sClient, then immediately invoke the handler, which reads the parent back via the reconciler's cached mgrClient — an unsynced informer watch could return the pre-patch (zero) value, intermittently zeroing the observed parent SpanID. Verified pre-existing and independent of this plan via git-stash-and-rerun (removed the new Task block entirely; the Milestone spec failed identically). Fixed by Eventually-waiting for mgrClient to observe the seeded value before invoking the handler, at all four now-five seed sites in the file (NO FLAKE TOLERANCE, CLAUDE.md)."

patterns-established:
  - "Seed-then-Eventually-confirm before invoking a reconciler method directly in an envtest spec: when a test seeds a parent object's status via the direct client, it must Eventually-wait for the SAME cached client the reconciler reads through to reflect that write, not just Expect a single Get, to avoid an informer-lag race."

requirements-completed: [TRACE-01, TRACE-02, PROP-01, PROP-02]

# Metrics
duration: 55min
completed: 2026-07-16
---

# Phase 43 Plan 05: Task-Level Parity + Dispatch-Hop TRACEPARENT Summary

**Closed the last dispatch-chain gap: Task's `handleJobCompletion` now emits idempotent, Plan-parented, deterministic-TraceID AGENT spans across all four terminal paths (including the degraded-envelope EnvelopeReadFailed path unique to Task), persists its own span ID to `TaskTraceSpanID`, and Task's subagent dispatch Job carries a real TRACEPARENT sourced from the parent Plan — completing the five-level trace tree and, along the way, fixing a pre-existing intermittent cache-sync flake in all four planner-level span-emission specs.**

## Performance

- **Duration:** ~55 min
- **Started:** 2026-07-16 (worktree base commit `982bd95`)
- **Completed:** 2026-07-16
- **Tasks:** 2 completed
- **Files modified:** 3 (2 modified, 1 created)

## Accomplishments

- `emitTaskSpanOnce` extracted as a shared method: gated by `TaskSpanEmittedUID != completedJob.UID` and `plannerSpanResolvable`, mark-then-emit ordering (marker patch via `retry.RetryOnConflict` + `client.MergeFromWithOptimisticLock`, stamped BEFORE calling `synthesizePlannerSpan`), Plan-parent resolution via a genuinely new `client.Get` on `task.Spec.PlanRef` (degrading to a zero/unnested parentSpanID on miss), and a second, separately-retried post-emission patch persisting `TaskTraceSpanID`.
- Two call sites cover all four Task terminal paths per the generalized Option B decision: inside the `EnvelopeReadFailed` branch (`envReadOK=false`, the only Task path reaching D-07's degraded-envelope span) and immediately after a successful envelope read, before the `OutputValidationError`/`OutputPathsViolation`/standard-result branches diverge (`envReadOK=true`) — one call covers three downstream branches.
- `createDispatchJob` (Task's dispatch-prep) now fetches the parent Plan and threads `traceparentForLevel(project, parentPlan.Status.PlanTraceSpanID)` into `podjob.BuildOptions.TraceParent` — Task's PROP-01 dispatch hop. Task gets no reporter-hop wiring this phase (no reporter Job until Phase 44 MSG-01).
- New fifth `Describe("SpanEmission — Task level", ...)` block (5 specs) mirrors the four existing planner-level blocks: one bundled spec proves succeeded-Job span emission, `tide.role=executor`/`tide.invocation.level=task`/`openinference.span.kind=AGENT` attributes, token counts, deterministic TraceID, real Plan-parenting (Remote SpanContext), PROP-02 `TaskTraceSpanID` persistence (re-fetched from the API), and idempotency (second call, same Job UID, still 1 span); a failed-Job spec; a nil-completedJob spec; an `EnvelopeReadFailed` spec proving the degraded span (no token attributes, `tide.envelope.degraded=true`) AND that the Task still lands `Failed`/`EnvelopeReadFailed` (existing behavior preserved, span emission is additive); and an output-path-validation-block-entered spec.
- New `task_dispatch_traceparent_test.go` (separate file from plan 43-04's `dispatch_traceparent_test.go` per the wave-3 file-disjointness requirement) drives a real `TaskReconciler.Reconcile` dispatch and asserts the created executor Job's subagent container carries `TRACEPARENT` equal to `00-<TraceIDFromUID(project.UID)>-<seeded Plan hex>-01`.
- Along the way, root-caused and fixed a pre-existing intermittent flake affecting all four planner-level `SpanEmission` blocks' TRACE-02 parent-linkage seed steps (see Decisions).

## Task Commits

Each task was committed atomically:

1. **Task 1: emitTaskSpanOnce + two completion call sites + dispatch-prep TRACEPARENT** - `b6c094b` (feat)
2. **Task 2: Fifth envtest Describe block + Task dispatch-hop spec (+ pre-existing flake fix)** - `b7dab0d` (test)

**Plan metadata:** SUMMARY.md commit follows this file (docs: complete plan)

## Files Created/Modified

- `internal/controller/task_controller.go` - `emitTaskSpanOnce` method; two call sites in `handleJobCompletion`; `createDispatchJob` fetches parent Plan and sets `BuildOptions.TraceParent`; `//nolint:gocyclo` added alongside the existing `//nolint:unparam` on `handleJobCompletion`
- `internal/controller/span_emission_test.go` - new fifth "SpanEmission — Task level" Describe block (5 specs); `Eventually`-wait fix for the pre-existing cache-sync race at all four prior levels' + the new Task level's TRACE-02 seed steps; new `"k8s.io/apimachinery/pkg/api/meta"` import for `meta.FindStatusCondition`
- `internal/controller/task_dispatch_traceparent_test.go` (new) - envtest proof of Task's dispatch-hop TRACEPARENT

## Decisions Made

**Generalized Option B, explicit per plan:** two `emitTaskSpanOnce` call sites (EnvelopeReadFailed with `envReadOK=false`; immediately post-successful-read with `envReadOK=true`, positioned before the three-way branch divergence) rather than one call site matching the four planner levels literally — required because Task's `handleJobCompletion` has four terminal paths where the planner levels have one.

**Output-path-validation spec (spec 5) documents an environmental infeasibility rather than forcing it:** `task_controller.go`'s hardcoded `taskWorkspaceRoot = "/workspaces/<project.UID>/workspace"` cannot be created in this sandbox (`mkdir /workspaces/probe` → "Read-only file system", verified directly). `validateControllerOutputPaths` treats a missing root as `fs.ErrNotExist` → `skipped=true`, so neither a genuine `OutputValidationError` (vErr) nor a real `OutputPathsViolation` (violations found) is reachable through `handleJobCompletion` without write access to that literal host path — the same constraint the pre-existing `TestTaskReconciler_OnJobSucceeded_FlagsOutputPathsViolation` test already hedges on ("Either way, task moves to terminal state", `task_controller_test.go` ~944-946). The spec instead proves the load-bearing claim that IS testable: call site 2 sits before this block, so entering and falling through its skipped-validation fallback neither duplicates nor loses the already-emitted span.

**Fixed a pre-existing intermittent flake found while stabilizing this plan's own new tests, in-scope because the affected file (`span_emission_test.go`) is already part of this plan's file manifest:** all four planner-level blocks' TRACE-02 seed steps write the parent's persisted span ID via `k8sClient.Status().Patch` (direct API client) then immediately invoke the reconciler, which reads the parent back via `mgrClient` (the manager's cached client). The informer watch backing that cache syncs asynchronously; under load, the reconciler's `Get` could race the watch and observe the stale (empty) parent, intermittently producing a zero parent SpanID and failing the "real parent linkage" assertion. Root-caused via observation, not guesswork: reproduced 1-in-3 runs, then verified independence from this plan's diff by stashing out the new Task block entirely and re-running — the pre-existing Milestone-level spec failed identically. Fixed at all five seed sites (four pre-existing + the new Task one) by `Eventually`-waiting for `mgrClient` to observe the seeded value before invoking the handler. Consistent with CLAUDE.md's explicit "NO FLAKE TOLERANCE... a non-deterministic spec is a bug to root-cause, not relabeled 'flake'."

## Deviations from Plan

**Spec 5 scope adjustment (documented above):** the plan's task list asked for a spec that "crafts `out` so the handler takes the OutputValidationError branch" — direct code reading showed the branch is gated entirely by a real filesystem walk of a hardcoded host path, never by `out` fields, and that path cannot be created in this sandbox. The spec was adjusted to prove the reachable, load-bearing part of the same claim (call-site-2 placement survives the block regardless of which sub-branch it takes) rather than force an infeasible filesystem precondition. No production code was changed to work around this — it is a test-design accommodation, documented above and in the spec's own comment.

**Flake fix beyond the plan's stated task list:** the plan's task list did not ask for a fix to the four pre-existing planner-level specs. This was done because (a) the affected file was already in this plan's file manifest, (b) the project's own `CLAUDE.md` states flakes are bugs to root-cause, not tolerate, and (c) leaving it in place would make this plan's own `make test-heavy` gate intermittently red for reasons unrelated to this plan's changes, undermining the verification the plan itself requires.

## Issues Encountered

`go build ./...` (full repo) still fails on the pre-existing, unrelated `cmd/tide-demo-init/main.go:112` (`pattern all:fixture: no matching files found`) — identical to the gap documented in plans 43-01/43-02/43-03, untouched by any file this plan modifies. `go build ./internal/... ./api/... ./pkg/...` and `go vet ./internal/controller/...` are both clean.

`make lint` (`golangci-lint run ./internal/controller/...`) surfaces 4 pre-existing issues, none introduced by this plan's diff: 3 ginkgo-linter `HaveLen(0)` suggestions in pre-existing "nil completedJob → zero spans" specs (my own equivalent Task-level spec correctly uses `BeEmpty()`), and 1 goconst finding on `task_controller.go`'s pre-existing `"cap-hit"` literal (same 4 occurrences as `git show HEAD` before this plan's edits — confirmed via direct diff).

`golangci-lint run <single-file-path>.go` (the plan's literal Task 1 verify command) produces spurious `undefined: ...` typecheck errors because golangci-lint v2 loads only the named file, not its containing package, when given an explicit file argument — confirmed this is a tool-invocation artifact, not a real issue, by running `golangci-lint run ./internal/controller/...` instead (clean except the 4 pre-existing items above).

An intermittent cache-sync flake in the pre-existing (Phase 42) `SpanEmission` blocks was found and fixed — see Decisions/Deviations above.

## User Setup Required

None — no external service configuration required.

## Next Phase Readiness

- The five-level trace tree is now complete: Project (root) → Milestone → Phase → Plan → Task, all sharing one deterministic TraceID, each child properly parented under its immediate parent's persisted span ID, each level's own span ID durably persisted to `.status.{Level}TraceSpanID`.
- Task's subagent dispatch Job carries a real `TRACEPARENT`; Task has no reporter Job this phase (Phase 44 MSG-01 adds it, and is the natural consumer of `TRACEPARENT`/`ExtractRemoteParent` for LLM message-array spans).
- `emitTaskSpanOnce`'s generalized Option B pattern (multiple call sites feeding one shared marker-gated emission method) is available as precedent if Phase 44's reporter-side work needs a similar multi-path emission shape.
- No blockers for Phase 44.

## Self-Check: PASSED

- `internal/controller/task_controller.go` — FOUND
- `internal/controller/span_emission_test.go` — FOUND
- `internal/controller/task_dispatch_traceparent_test.go` — FOUND
- `b6c094b` — FOUND (`git log --oneline --all | grep b6c094b`)
- `b7dab0d` — FOUND (`git log --oneline --all | grep b7dab0d`)
- `go build ./internal/... ./api/... ./pkg/...` — PASS
- `go vet ./internal/controller/...` — PASS
- `grep -c 'emitTaskSpanOnce' internal/controller/task_controller.go` — 4 (1 doc-comment mention + 1 definition + 2 call sites)
- `grep -c 'TaskSpanEmittedUID' internal/controller/task_controller.go` — 5 (gate + stamp + doc comments)
- `grep -c 'TraceParent:' internal/controller/task_controller.go` — 1 (BuildOptions literal)
- EnvelopeReadFailed branch's "Deliberate exclusion" comment and return behavior unchanged — confirmed via `git diff` (only the added call line before the existing status patch)
- `make test-heavy` — PASS (32 Passed, 0 Failed, 192 Skipped, `MAKE_EXIT=0`), verified across 5 consecutive focused runs plus 2 full runs with zero flakes post-fix
- `make test-int-fast` — PASS (envtest 56/56, heavy 32/32, `MAKE_EXIT=0`)
- `golangci-lint run ./internal/controller/...` — 4 pre-existing issues, 0 introduced

---
*Phase: 43-task-level-parity-trace-context-propagation*
*Completed: 2026-07-16*
