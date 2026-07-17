---
phase: 47-self-hosted-phoenix-install-end-to-end-proof
plan: 01
subsystem: infra
tags: [opentelemetry, otlp, reporter, controller-runtime, phoenix]

# Dependency graph
requires:
  - phase: 44-llm-message-array-spans-d-o5-redaction-size-boundary
    provides: reporter Job's own TracerProvider bootstrap (OTLPEndpoint threading precedent)
  - phase: 46-observability-enrichment-dashboard-deep-link
    provides: enriched ReporterOptions literal (SessionID/MetadataJSON/Tags) at all five spawn sites
provides:
  - ReporterOptions.OTLPHeaders field + guarded env construction on the reporter Job spec
  - OTLPHeaders threaded through PlannerReconcilerDeps and TaskReconcilerDeps
  - cmd/manager/main.go reads OTEL_EXPORTER_OTLP_HEADERS and forwards it to both Deps structs
  - All five reconciler spawn sites (milestone/phase/plan/project/task) forward r.Deps.OTLPHeaders
affects: [47-02-render-gate, 47-03-observability-doc, phoenix-auth-on-recipe]

# Tech tracking
tech-stack:
  added: []
  patterns: ["Guarded-env forwarding: a Deps field mirrors an existing Deps field 1:1 across Deps struct -> main.go read -> reconciler spawn site -> ReporterOptions -> Job env, gated by the SAME outer non-empty check as its precedent"]

key-files:
  created: []
  modified:
    - internal/controller/reporter_jobspec.go
    - internal/controller/reporter_jobspec_test.go
    - internal/controller/dispatch_helpers.go
    - internal/controller/task_controller.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - cmd/manager/main.go

key-decisions:
  - "OTLPHeaders is only emitted onto the reporter Job env when OTLPEndpoint is also non-empty — headers without an endpoint are meaningless; pinned by TestBuildReporterJob_OTLPHeadersWithoutEndpointNoEnv"
  - "Carried as Env (not Args), mirroring OTLPEndpoint precedent — targets the reporter's own otlptracegrpc TracerProvider bootstrap which honors OTEL_EXPORTER_OTLP_HEADERS automatically at v1.43.0 without a reporter-binary change"
  - "T-47-02 accepted: header value is a literal on the Job spec in the project namespace — Job-read RBAC there already implies access to the strictly more sensitive tide-secrets ANTHROPIC_API_KEY"

patterns-established:
  - "Mechanical 9-site threading for a new OTel env value: 2 Deps struct fields, 1 main.go env read, 2 main.go Deps-literal assignments, 5 reconciler ReporterOptions-literal assignments — same shape as OTLPEndpoint's precedent, safe to replicate for future OTLP env forwards"

requirements-completed: [PHX-02]

# Metrics
duration: ~15min
completed: 2026-07-17
---

# Phase 47 Plan 01: OTLP Headers Threading Summary

**Threaded `OTLPHeaders` end-to-end (manager env → both Deps structs → all five reconciler spawn sites → reporter Job env) so the reporter's own `otlptracegrpc` TracerProvider can authenticate to an auth-enabled Phoenix, making D-08's auth-ON recipe functional at the Go layer.**

## Performance

- **Tasks:** 2 completed
- **Files modified:** 9
- **Commits:** 3 (test → feat → feat)

## Accomplishments
- `ReporterOptions.OTLPHeaders` field + guarded env construction on `BuildReporterJob`, with a paired unit test (presence with endpoint, absence without endpoint) proving the outer `OTLPEndpoint` guard is preserved and the zero-entry posture stays byte-identical
- `OTLPHeaders` threaded through `PlannerReconcilerDeps` and `TaskReconcilerDeps`, `cmd/manager/main.go`'s env read + both Deps literals, and all five reconciler `ReporterOptions` literals (milestone, phase, plan, project, task)
- Full existing `TestBuildReporterJob*` suite stays green — zero regressions to the pre-existing `OTLPEndpoint` env pair or any Args-based fields

## Task Commits

Each task was committed atomically:

1. **Task 1: ReporterOptions.OTLPHeaders field + guarded env construction + unit test pair** - `64f2a35` (test, RED) → `697a670` (feat, GREEN)
2. **Task 2: Thread OTLPHeaders through main.go, both Deps structs, and all five reconciler spawn sites** - `c7d82aa` (feat)

## Files Created/Modified
- `internal/controller/reporter_jobspec.go` - `OTLPHeaders string` field on `ReporterOptions` + `OTEL_EXPORTER_OTLP_HEADERS` appended to the container env only when both `OTLPEndpoint` and `OTLPHeaders` are non-empty
- `internal/controller/reporter_jobspec_test.go` - `TestBuildReporterJob_OTLPHeadersEnv` (3 env entries) and `TestBuildReporterJob_OTLPHeadersWithoutEndpointNoEnv` (0 env entries) added, mirroring the existing `OTLPEndpoint` test pair's map-comparison idiom
- `internal/controller/dispatch_helpers.go` - `OTLPHeaders string` added to `PlannerReconcilerDeps`
- `internal/controller/task_controller.go` - `OTLPHeaders string` added to `TaskReconcilerDeps`; spawn site forwards `r.Deps.OTLPHeaders`
- `internal/controller/milestone_controller.go`, `phase_controller.go`, `plan_controller.go`, `project_controller.go` - each `ReporterOptions` literal now includes `OTLPHeaders: r.Deps.OTLPHeaders`
- `cmd/manager/main.go` - reads `OTEL_EXPORTER_OTLP_HEADERS` via `os.Getenv` (sibling to the existing `otlpEndpoint` read); forwards into `plannerDeps` and the `TaskReconcilerDeps` literal

## Decisions Made
- No deviations from the plan's exact interface shape — every field name, comment voice, and guard condition matched 47-01-PLAN.md's `<interfaces>` block and the plan's own resolution of the "headers without endpoint" open question (Test 2 pins zero-entry behavior).

## Deviations from Plan

None - plan executed exactly as written. The plan referenced `47-PATTERNS.md` for exact line numbers, but that file does not exist in this worktree; the plan's own `<interfaces>` section (lines 64-92) and `<action>` blocks in each task contained everything needed, so this had no effect on execution. `task_controller.go`'s spawn-site edit landed at line 1112/1113 instead of the plan's cited 1106 — a pre-existing +6-line drift from Phase 46's enrichment fields added after the plan was authored (comment/field additions), not a Task 1/Task 2 side effect. Located via grep, not assumption.

## Issues Encountered
- `go build ./...` fails on `cmd/tide-demo-init/main.go:112` (`pattern all:fixture: no matching files found`) — confirmed pre-existing via `git show HEAD~2:cmd/tide-demo-init/main.go` (unrelated to this plan's files) and out of scope per the plan's `<verification>` section, which scopes to `./cmd/...` and `./internal/controller/...` for vet, and this plan's own packages for build. `go build ./cmd/manager/...` and `go build ./internal/controller/...` both exit 0.
- `go test ./internal/controller/...` (full package, not the `-run` filter) fails at `[BeforeSuite]` with `fork/exec /usr/local/kubebuilder/bin/etcd: no such file or directory` — confirmed environmental: `KUBEBUILDER_ASSETS` unset and no envtest binaries installed in this worktree sandbox, not caused by this plan's diff (no envtest/suite_test.go files touched). The plan's actual verification command, `go test ./internal/controller/... -run 'TestBuildReporterJob' -v`, is a plain-Go test (no envtest control plane) and passes clean.

## User Setup Required

None - no external service configuration required. This plan only threads a Go value; wiring the Helm chart's `OTEL_EXPORTER_OTLP_HEADERS` value and the render-gate mitigation for T-47-01 land in Plan 47-02.

## Next Phase Readiness

- The Go half of D-08's auth-ON Phoenix recipe is functional: any caller that sets `OTEL_EXPORTER_OTLP_HEADERS` on the manager Deployment env will now see it reach every reporter Job's own TracerProvider bootstrap.
- Plan 47-02 (chart render gate enforcing `valueFrom: secretKeyRef` for the header, per T-47-01) and Plan 47-03 (observability.md recipe documentation) can now build on a real, tested code path rather than an aspirational one.
- No blockers.

---
*Phase: 47-self-hosted-phoenix-install-end-to-end-proof*
*Completed: 2026-07-17*
