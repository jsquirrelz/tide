---
phase: 46-observability-enrichment-dashboard-deep-link
plan: 03
subsystem: api
tags: [go, dashboard, otel, opentelemetry, phoenix, trace-identity, chi]

# Dependency graph
requires:
  - phase: 43-task-level-trace-parity-propagation
    provides: "{Level}TraceSpanID status fields on all five api/v1alpha3 types (PROP-02)"
  - phase: 42-trace-context-foundation-planner-spans
    provides: "otelai.TraceIDFromUID (pkg/otelai/tracecontext.go) — deterministic, K8s-import-free"
provides:
  - "GET /api/v1/config additive phoenixBaseURL field (raw PHOENIX_BASE_URL env passthrough)"
  - "projectDetail.traceId/traceSpanId + childRef.traceSpanId on GET /api/v1/projects/{name}"
  - "taskDetail.traceId/traceSpanId on GET /api/v1/tasks/{name} with graceful chain degradation"
affects: [46-05-spa-phoenix-deep-link]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Config-chain env passthrough (telemetryEnabled precedent applied verbatim to phoenixBaseURL): main.go resolves raw env -> router.go Dependencies field -> handler struct field -> JSON response, zero server-side normalization"
    - "Graceful trace-identity degradation: any break in a resolution chain (missing ref, Get error, TraceIDFromUID derive error) yields empty-string fields via omitempty, never a 500 — extends the existing 'resolution-chain breaks degrade gracefully' contract from tasks.go"

key-files:
  created: []
  modified:
    - cmd/dashboard/main.go
    - cmd/dashboard/main_test.go
    - cmd/dashboard/router.go
    - cmd/dashboard/api/config.go
    - cmd/dashboard/api/config_test.go
    - cmd/dashboard/api/projects.go
    - cmd/dashboard/api/projects_test.go
    - cmd/dashboard/api/tasks.go
    - cmd/dashboard/api/tasks_test.go

key-decisions:
  - "taskDetail.TraceSpanID is coupled to TraceID resolution success, not read unconditionally from Status.TaskTraceSpanID — per the plan's explicit instruction, a broken Task->Plan->Phase->Milestone->Project chain degrades BOTH trace fields to empty, since a spanId with no traceId to anchor it cannot build a usable Phoenix link"
  - "projectDetail.TraceID/childRef.TraceSpanID have no such coupling — each is derived independently (own UID / own status field) since buildDetail already holds the Project object directly, no chain-walk risk exists at that level"

patterns-established:
  - "Trace-identity JSON fields are all `omitempty` string — absence is the contract, never a fabricated placeholder ID"

requirements-completed: [OBS-04]

# Metrics
duration: ~15min
completed: 2026-07-17
---

# Phase 46 Plan 03: Dashboard Backend Trace Identity + Phoenix Config Chain Summary

**Wired PHOENIX_BASE_URL through the dashboard's config chain and added server-derived traceId/traceSpanId fields to projectDetail, childRef, and taskDetail — the D-11 plumbing plan 46-05's SPA deep-link components consume.**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-07-17T05:15:00Z (approx.)
- **Completed:** 2026-07-17T05:28:53Z
- **Tasks:** 2 completed
- **Files modified:** 9

## Accomplishments

- `GET /api/v1/config` now returns `phoenixBaseURL` beside `telemetryEnabled` — additive JSON field, raw `PHOENIX_BASE_URL` env passthrough (no server-side normalization; that lives SPA-side per D-11), `telemetryEnabled` semantics byte-identical.
- `projectDetail` carries `traceId` (deterministic via `otelai.TraceIDFromUID` on the Project's own UID) and `traceSpanId` (`Status.ProjectTraceSpanID`); every `childRef` (Milestone/Phase/Plan) carries its own `traceSpanId` from the matching `{Level}TraceSpanID` status field.
- `taskDetail` carries `traceId`/`traceSpanId`, resolved by walking the existing `resolveTaskParents` chain to the Project and deriving the TraceID from its UID; any break in that chain (or a derive error) degrades both fields to `""` rather than 500ing the request.
- Handler tests prove: populated fixtures yield the expected hex (and same-UID-twice determinism), empty fixtures omit the JSON keys entirely (`omitempty`), and the taskDetail broken-chain fixture still returns HTTP 200.

## Task Commits

1. **Task 1: phoenixBaseURL config chain (telemetryEnabled precedent, applied verbatim)** - `cf9c950` (feat)
2. **Task 2: Trace identity on projectDetail/childRef/taskDetail payloads** - `82273b5` (feat)

_No plan-metadata commit yet — SUMMARY.md commit follows this one per worktree protocol._

## Files Created/Modified

- `cmd/dashboard/main.go` - `phoenixBaseURLFromEnv()` (bare `os.Getenv` passthrough), wired into the `Dependencies` literal
- `cmd/dashboard/main_test.go` - `TestPhoenixBaseURLFromEnv`; extended `TestConfigRouteRegistered` for the new wire-contract body
- `cmd/dashboard/router.go` - `Dependencies.PhoenixBaseURL` field; wired into `ConfigHandler`
- `cmd/dashboard/api/config.go` - `ConfigHandler.PhoenixBaseURL` + `configResponse.PhoenixBaseURL` (`json:"phoenixBaseURL"`)
- `cmd/dashboard/api/config_test.go` - extended `TestConfigHandlerGet` with phoenix-set/unset x telemetry-enabled/disabled cases
- `cmd/dashboard/api/projects.go` - `projectDetail.TraceID/TraceSpanID`, `childRef.TraceSpanID`; `buildDetail` computes/populates them; imports `pkg/otelai`
- `cmd/dashboard/api/projects_test.go` - `TestGetProjectTraceIdentityPopulated`, `TestGetProjectTraceIdentityEmptyOmitted`
- `cmd/dashboard/api/tasks.go` - `taskDetail.TraceID/TraceSpanID`; new `resolveTaskTraceIdentity` helper; imports `pkg/otelai`
- `cmd/dashboard/api/tasks_test.go` - `TestTasksHandlerTraceIdentity`; extended `TestTasksHandlerResolutionChainBreak` with empty-trace-field assertions

## Decisions Made

- **taskDetail trace-field coupling:** the plan's action text explicitly directs "ANY failure... degrades to empty strings on both fields" for the task-level trace identity. Implemented `resolveTaskTraceIdentity` so a broken parent-chain resolution (or a `TraceIDFromUID` derive error) zeroes both `TraceID` and `TraceSpanID` together, even though `TaskTraceSpanID` is otherwise a direct status-field read. Rationale carried into the code comment: a span ID with no trace ID to anchor it is not a usable Phoenix link.
- **projectDetail/childRef stay independent:** no equivalent coupling — `buildDetail` already holds the `Project` object directly (no chain-walk risk), so `TraceID` derives from the Project's own UID and each `childRef.TraceSpanID` reads its own CR's status field, both independently.
- **main_test.go touched despite not being in the plan's `files_modified` frontmatter list:** the Task 1 wire-contract change (`{"telemetryEnabled":true}` -> `{"telemetryEnabled":true,"phoenixBaseURL":...}`) broke `TestConfigRouteRegistered`'s exact-body assertion; fixed in place (Rule 1) and added `TestPhoenixBaseURLFromEnv` alongside the existing `TestTelemetryEnabledFromEnv` for symmetric coverage of the new resolver function (Rule 2).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed a wire-contract test broken by the additive phoenixBaseURL field**
- **Found during:** Task 1
- **Issue:** `cmd/dashboard/main_test.go`'s `TestConfigRouteRegistered` asserted the exact body `{"telemetryEnabled":true}`; adding `phoenixBaseURL` to `configResponse` (as instructed) changed the serialized body and would fail this pre-existing test.
- **Fix:** Updated the `Dependencies` literal in the test to set `PhoenixBaseURL`, and updated the expected body string to include the new field.
- **Files modified:** cmd/dashboard/main_test.go
- **Verification:** `go test ./cmd/dashboard/... -run TestConfigRouteRegistered -v` passes.
- **Committed in:** cf9c950 (Task 1 commit)

**2. [Rule 2 - Missing Critical] Added symmetric unit coverage for phoenixBaseURLFromEnv**
- **Found during:** Task 1
- **Issue:** The plan's action item only specified extending `config_test.go`; the new `phoenixBaseURLFromEnv()` resolver function in `main.go` (parallel to `telemetryEnabledFromEnv()`, which has its own dedicated `TestTelemetryEnabledFromEnv`) had no direct unit test.
- **Fix:** Added `TestPhoenixBaseURLFromEnv` in main_test.go covering unset/set/trailing-slash-preserved cases, mirroring the existing `TestTelemetryEnabledFromEnv` structure.
- **Files modified:** cmd/dashboard/main_test.go
- **Verification:** `go test ./cmd/dashboard/... -run TestPhoenixBaseURLFromEnv -v` passes (3/3 subtests).
- **Committed in:** cf9c950 (Task 1 commit)

---

**Total deviations:** 2 auto-fixed (1 bug fix, 1 missing-critical-coverage addition)
**Impact on plan:** Both fixes are direct, necessary consequences of the Task 1 wire-contract change in files already touched by the plan. No scope creep — no unrelated files modified.

## Issues Encountered

- `go build ./cmd/dashboard/` from the repo root failed with "build output \"dashboard\" already exists and is a directory" — a pre-existing, unrelated `dashboard/web` frontend source directory collides with the default build-output name when no `-o` flag is given. Not a code defect; verified clean with `go build -o /tmp/tide-dashboard-bin ./cmd/dashboard/`.
- `go build ./...` at the repo root fails on `cmd/tide-demo-init/main.go:112` ("pattern all:fixture: no matching files found") — pre-existing and unrelated to this plan (confirmed via `git log --oneline -1 -- cmd/tide-demo-init/main.go`, last touched by an unrelated commit). Out of scope per the deviation-rules scope boundary; not fixed.

## User Setup Required

None - no external service configuration required. (The Phoenix self-host recipe and `PHOENIX_BASE_URL` Helm wiring are Phase 47's responsibility; this plan only wires the dashboard's read side of the env var.)

## Next Phase Readiness

- The exact wire-contract fields (`phoenixBaseURL`, `traceId`, `traceSpanId`) plan 46-05's SPA TypeScript mirrors will consume are now live and test-proven — `lib/phoenixLink.ts`, `PhoenixTraceLink.tsx`, and the config-fetch hook can be built against this contract without further backend changes.
- No blockers. `go test ./cmd/dashboard/...` green, `go vet ./cmd/dashboard/...` clean, zero new non-GET routes (DASH-05 zero-mutation contract preserved, verified by the existing `TestZeroMutationRoutes`).

---
*Phase: 46-observability-enrichment-dashboard-deep-link*
*Completed: 2026-07-17*

## Self-Check: PASSED

All 9 modified files + SUMMARY.md verified present on disk; all 3 commits (`cf9c950`, `82273b5`, plan-summary commit) verified present in `git log --oneline --all`.
