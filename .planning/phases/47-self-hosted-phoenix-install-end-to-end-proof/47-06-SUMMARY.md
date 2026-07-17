---
phase: 47-self-hosted-phoenix-install-end-to-end-proof
plan: 06
subsystem: infra
tags: [opentelemetry, k8s-secrets, rbac, reporter-job, phoenix, gap-closure]

# Dependency graph
requires:
  - phase: 47-self-hosted-phoenix-install-end-to-end-proof (plans 01, 03)
    provides: Phoenix self-hosted install docs + OTLPHeaders forwarding chain that this plan corrects
provides:
  - "Reporter Job PodSpecs reference the OTLP-headers Secret by NAME via valueFrom.secretKeyRef, never the decoded bearer as a literal EnvVar.Value"
  - "Fixed-name per-project-namespace tide-otlp-headers Secret mirror convention (mirrors tide-signing-key), Optional=true degrade-safe"
  - "Corrected docs/observability.md exposure posture (WR-03 false RBAC-equivalence claim removed)"
  - "docs/INSTALL.md project-namespace mirror step for tide-otlp-headers"
affects: [47-verification, phase-48-if-any-future-otlp-headers-work]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Fixed-name cross-namespace Secret mirror (tide-otlp-headers, mirrors tide-signing-key) referenced via Optional valueFrom.secretKeyRef, never a literal EnvVar.Value"
    - "Manager reads a sensitive env var only to detect presence, threads a Secret NAME (not the decoded value) into Deps/Job-builder options"

key-files:
  created: []
  modified:
    - internal/controller/reporter_jobspec.go
    - internal/controller/reporter_jobspec_test.go
    - internal/controller/dispatch_helpers.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go
    - internal/controller/project_controller.go
    - internal/controller/task_controller.go
    - cmd/manager/main.go
    - docs/observability.md
    - docs/INSTALL.md

key-decisions:
  - "Root-fixed per user's standing preference (fix-shape default): no override recorded for T-47-02's unsound RBAC-equivalence rationale."
  - "ReporterOptions.OTLPHeaders (string, decoded value) renamed to OTLPHeadersSecret (string, Secret NAME only) across the full Deps chain: PlannerReconcilerDeps, TaskReconcilerDeps, ReporterOptions, cmd/manager/main.go."
  - "secretKeyRef.Optional=true: missing per-namespace mirror degrades the reporter to unauthenticated export (dark traces) rather than blocking child-CRD materialization with CreateContainerConfigError — matches the Phase 44 D-10 exit-0/dark-pipe posture."

patterns-established:
  - "When a manager-tier process must forward a secret's PRESENCE (not its value) down to a project-namespace workload, thread the fixed Secret NAME through Deps and resolve via secretKeyRef at the workload, not os.Getenv + literal EnvVar.Value."

requirements-completed: [PHX-01, PHX-02]

# Metrics
duration: 9min
completed: 2026-07-17
---

# Phase 47 Plan 06: Reporter OTLP-Headers Secret Reference (Gap Closure) Summary

**Reporter Job PodSpecs no longer carry the decoded Phoenix bearer token as a plaintext EnvVar.Value — `OTEL_EXPORTER_OTLP_HEADERS` now resolves via `valueFrom.secretKeyRef` against a per-project-namespace `tide-otlp-headers` Secret mirror, with the unsound T-47-02 RBAC-equivalence doc claim corrected in `docs/observability.md`.**

## Performance

- **Duration:** ~9 min (first commit 14:00:47 → last commit 14:03:04 local; excludes read/investigation time)
- **Tasks:** 2 completed
- **Files modified:** 11

## Accomplishments

- Closed verification gap #1 (CR-02, blocker): the decoded Phoenix bearer credential no longer appears anywhere in a Job spec across all 5 reporter-Job spawn sites (Milestone/Phase/Plan/Project/Task).
- `ReporterOptions.OTLPHeaders` (decoded value) → `OTLPHeadersSecret` (Secret NAME only) renamed consistently through `PlannerReconcilerDeps`, `TaskReconcilerDeps`, `ReporterOptions`, and `cmd/manager/main.go`'s wiring — the manager now reads `OTEL_EXPORTER_OTLP_HEADERS` only to detect presence, never forwarding the decoded value out of its own process.
- Discharged WR-03: `docs/observability.md`'s false "Job-read RBAC already implies Secret access" claim replaced with the true exposure posture (secretKeyRef exposes only the Secret NAME; reading the token requires `get` on Secrets) and the Optional-degrade behavior (dark traces, not a blocked materialization).
- `docs/INSTALL.md`'s Phoenix install step now documents mirroring `tide-otlp-headers` into each Project namespace, in the same `--from-literal` / "never paste into YAML" voice as the existing signing-key mirror.

## Task Commits

Each task was committed atomically:

1. **Task 1: Replace literal OTLP-headers forwarding with per-namespace secretKeyRef across the Go chain** - `75fc851` (fix)
2. **Task 2: Correct the exposure-posture docs and add the project-namespace mirror step** - `3e8b876` (docs)

**Follow-up fix (Rule 1 — self-caught formatting bug):** `ec5372f` (style) — `gofmt -w` realignment of four `ReporterOptions` struct literals whose field-name column width shifted when `OTLPHeaders` widened to `OTLPHeadersSecret`; no logic change, caught by a post-commit `gofmt -l` check before declaring the plan done.

_Plan-level metadata commit (this SUMMARY) is separate, per the executor protocol._

## Files Created/Modified

- `internal/controller/reporter_jobspec.go` - Adds `ReporterOTLPHeadersSecretName`/`ReporterOTLPHeadersSecretKey` consts; renames `ReporterOptions.OTLPHeaders` → `OTLPHeadersSecret`; env construction now emits `valueFrom.secretKeyRef` (Optional=true) instead of a literal `Value`; T-47-02 rationale removed from the doc comment
- `internal/controller/reporter_jobspec_test.go` - `TestBuildReporterJob_OTLPHeadersEnv` now asserts secretKeyRef shape (Name/Key/Optional) and an empty literal `Value`; `TestBuildReporterJob_OTLPHeadersWithoutEndpointNoEnv` renamed field, semantics unchanged
- `internal/controller/dispatch_helpers.go` - `PlannerReconcilerDeps.OTLPHeaders` → `OTLPHeadersSecret`, doc comment updated
- `internal/controller/task_controller.go` - `TaskReconcilerDeps.OTLPHeaders` → `OTLPHeadersSecret`; trace-only reporter spawn site updated
- `internal/controller/milestone_controller.go` - Spawn-site `ReporterOptions` literal updated
- `internal/controller/phase_controller.go` - Spawn-site `ReporterOptions` literal updated
- `internal/controller/plan_controller.go` - Spawn-site `ReporterOptions` literal updated
- `internal/controller/project_controller.go` - Spawn-site `ReporterOptions` literal updated
- `cmd/manager/main.go` - `otlpHeaders := os.Getenv(...)` replaced with presence-detection into `otlpHeadersSecret := controller.ReporterOTLPHeadersSecretName` (or `""`); both `plannerDeps` and `TaskReconcilerDeps` wiring updated; comment rewritten to state the decoded value never leaves the manager process
- `docs/observability.md` - Rewrote the `headersSecretRef` paragraph (~lines 170-179): removed the false RBAC-equivalence claim, added the true per-namespace-mirror/secretKeyRef posture and the Optional-degrade behavior
- `docs/INSTALL.md` - Added the `tide-otlp-headers` project-namespace mirror step inside "Enable tracing (Phoenix)" step 2, same voice as the signing-key mirror

## Decisions Made

- Root-fix per the user's standing "fix thoroughly" preference — no override recorded for the T-47-02 acceptance rationale, which the plan's objective established as factually unsound (a `secretKeyRef` exposes only the Secret NAME; the prior literal exposed the decoded credential itself — a strictly larger exposure, not an equivalent one).
- Kept `values.yaml` untouched — this plan is Go-side reporter-Job forwarding + docs only, per the plan's explicit constraint; the chart's existing `otel.exporter.headersSecretRef` wiring for the manager/dashboard Deployments was already correct.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] gofmt misalignment in four spawn-site struct literals**
- **Found during:** Post-Task-1 self-review (`gofmt -l` check before moving to Task 2)
- **Issue:** Renaming `OTLPHeaders` → `OTLPHeadersSecret` widened that struct-literal field-name column beyond the neighboring `ReporterImage`/`TraceParent` fields in the four spawn sites (milestone/phase/plan/project controllers), leaving them misaligned per `gofmt`'s column-alignment rule
- **Fix:** Ran `gofmt -w` on the four files; re-verified `gofmt -l` returns empty and the build/tests still pass
- **Files modified:** `internal/controller/milestone_controller.go`, `internal/controller/phase_controller.go`, `internal/controller/plan_controller.go`, `internal/controller/project_controller.go`
- **Verification:** `gofmt -l` returns no files; `go build` and the `TestBuildReporterJob*` suite pass after the realignment
- **Committed in:** `ec5372f`

---

**Total deviations:** 1 auto-fixed (formatting only, Rule 1)
**Impact on plan:** Purely cosmetic gofmt alignment; no behavior or test-assertion change. No scope creep.

## Issues Encountered

None beyond the gofmt formatting catch above.

**Pre-existing, out-of-scope observation (not fixed, logged per scope boundary):** `docs/INSTALL.md:303` contains `export ANTHROPIC_API_KEY='sk-ant-...'` (a placeholder, ellipsis-truncated, matching the file's own "never paste into YAML" convention used right there). This line pre-dates this plan (confirmed via `git diff` — not touched by either task) and causes the plan's stated acceptance grep `grep -rc 'sk-ant' docs/INSTALL.md docs/observability.md` to return `1` for INSTALL.md instead of the specified `0`. It is not a real credential and is unrelated to the OTLP-headers work this plan closes; left untouched per the deviation rules' scope boundary (fixing unrelated pre-existing content is out of scope for this task). All OTLP-headers-specific acceptance criteria for both tasks pass cleanly.

## User Setup Required

None - no external service configuration required. Operators applying this fix in a live cluster will need to create the `tide-otlp-headers` Secret in each Project namespace per the new `docs/INSTALL.md` step (existing operational task, now documented — no code-side action needed beyond redeploying the manager image).

## Next Phase Readiness

- Gap #1 (CR-02, blocker) and WR-03 are closed at the root; the `internal/controller` package builds, vets, and the full `TestBuildReporterJob*` suite (24 tests) passes.
- Ready for phase 47 re-verification to confirm gate closure against `47-VERIFICATION.md`'s gap list.
- No blockers surfaced for downstream work.

---
*Phase: 47-self-hosted-phoenix-install-end-to-end-proof*
*Completed: 2026-07-17*
