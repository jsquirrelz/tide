---
phase: 36-signed-commits-bot-identity
plan: 02
subsystem: api
tags: [crd, kubebuilder, controller-runtime, git-identity, precedence-chain]

# Dependency graph
requires:
  - phase: 36-01
    provides: "pkg/git constants DefaultAgentName/DefaultAgentEmail/EnvAgentName/EnvAgentEmail + AgentIdentity() — the compiled bottom of the D-03 chain"
provides:
  - "spec.git.agentName / spec.git.agentEmail CRD fields (both API versions, Pattern + MaxLength validated at admission)"
  - "resolveAgentIdentity(project, helmDefaults) (name, email) — the single D-03 precedence resolver, mirroring resolveImage"
  - "ProviderDefaults.AgentName/AgentEmail — the chart tier of the D-03 chain"
  - "cmd/manager env wiring: TIDE_AGENT_NAME/TIDE_AGENT_EMAIL → ProviderDefaults with empty-is-unset semantics"
affects: [36-03, 36-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Per-field independent precedence walk (spec → chart → compiled) mirroring resolveImage"
    - "kubebuilder Pattern markers as admission-time input sanitization (not CEL) for header-safety"

key-files:
  created: []
  modified:
    - api/v1alpha2/project_types.go
    - api/v1alpha1/project_types.go
    - api/v1alpha1/phase3_schema_test.go
    - api/v1alpha2/schema_test.go
    - config/crd/bases/tideproject.k8s_projects.yaml
    - charts/tide-crds/templates/project-crd.yaml
    - internal/controller/dispatch_helpers.go
    - internal/controller/dispatch_helpers_test.go
    - cmd/manager/env.go
    - cmd/manager/env_test.go

key-decisions:
  - "resolveAgentIdentity is pure (no os.Getenv) — env reading stays the manager's job; defaulting happens exactly once, at resolve time"
  - "Chart CRD subchart template regenerated to keep the verify-chart-reproducible gate green — CRD schema propagation, distinct from the batched chart version bump (D-06, Plan 36-04)"

patterns-established:
  - "Pattern: agentName/agentEmail validated with kubebuilder Pattern (reject <>/CR/LF; enforce x@y) — T-36-01 mitigation lands at admission"
  - "Pattern: chart tier carried as empty-string sentinel through ProviderDefaults so the resolver owns defaulting"

requirements-completed: [SIGN-01]

coverage:
  - id: D1
    description: "Project CRD accepts spec.git.agentName/agentEmail in both API versions; admission rejects angle brackets/newlines in name and non x@y shapes in email (Pattern + MaxLength)"
    requirement: "SIGN-01"
    verification:
      - kind: unit
        ref: "api/v1alpha1/phase3_schema_test.go#TestGitConfigRoundTrip"
        status: pass
      - kind: unit
        ref: "api/v1alpha2/schema_test.go#TestGitConfigAgentIdentity"
        status: pass
      - kind: other
        ref: "grep -c 'agentName' config/crd/bases/tideproject.k8s_projects.yaml == 2 (pattern/maxLength adjacent in both schemas)"
        status: pass
    human_judgment: false
  - id: D2
    description: "resolveAgentIdentity walks Project spec → chart tier → compiled default, per field, nil-safe on project and project.Spec.Git"
    requirement: "SIGN-01"
    verification:
      - kind: unit
        ref: "internal/controller/dispatch_helpers_test.go#TestResolveAgentIdentity_* (5 cases: compiled/chart/spec-beats-chart/per-field/nil-safe)"
        status: pass
    human_judgment: false
  - id: D3
    description: "Manager reads TIDE_AGENT_NAME/TIDE_AGENT_EMAIL into ProviderDefaults with empty-is-unset semantics; unset chart value stays empty (not collapsed into compiled default)"
    requirement: "SIGN-01"
    verification:
      - kind: unit
        ref: "cmd/manager/env_test.go#TestTideHelmProviderDefaults_AgentIdentitySet, TestTideHelmProviderDefaults_AgentIdentityUnset"
        status: pass
    human_judgment: false

# Metrics
duration: 8min
completed: 2026-07-08
status: complete
---

# Phase 36 Plan 02: D-03 Agent-Identity Configuration Surface Summary

**spec.git.agentName/agentEmail CRD fields (Pattern-validated in both API versions), a pure resolveAgentIdentity precedence resolver mirroring resolveImage, and the manager env wiring that carries the chart tier into ProviderDefaults — the full Project → chart → compiled D-03 chain.**

## Performance

- **Duration:** ~8 min
- **Started:** 2026-07-08T05:32Z
- **Completed:** 2026-07-08T05:40Z
- **Tasks:** 3
- **Files modified:** 10

## Accomplishments
- Added `AgentName` (MaxLength 100, Pattern `^[^<>\r\n]+$`) and `AgentEmail` (MaxLength 254, Pattern `^[^<>@\s]+@[^<>@\s]+$`) to `GitConfig` in both `api/v1alpha2` and `api/v1alpha1` for type parity — no conversion webhook introduced. Threat T-36-01 (commit-header corruption) is mitigated at admission via these Pattern markers.
- Implemented `resolveAgentIdentity(project, helmDefaults) (name, email)` beside `resolveImage`, walking the D-03 chain per field independently, nil-safe on both `project` and `project.Spec.Git` (`*GitConfig`), and pure (no `os.Getenv`). Anchored on `pkggit.DefaultAgentName/Email` from Plan 36-01.
- Wired `tideHelmProviderDefaults` to populate the new `ProviderDefaults` fields from `pkggit.EnvAgentName/EnvAgentEmail` with an empty-string fallback, preserving empty-is-unset so defaulting happens exactly once at resolve time.
- Regenerated `config/crd/bases` and the `charts/tide-crds` CRD subchart template; 10 unit tests added/extended, all green.

## Task Commits

Each task was committed atomically:

1. **Task 1: Add agentName/agentEmail to GitConfig in both API versions + regen + schema tests** - `a239ddf` (feat)
2. **Task 2: resolveAgentIdentity + ProviderDefaults extension (TDD)** - `f929b21` (feat)
3. **Task 3: Manager env wiring — TIDE_AGENT_NAME/EMAIL into ProviderDefaults** - `59e2710` (feat)

## Files Created/Modified
- `api/v1alpha2/project_types.go` / `api/v1alpha1/project_types.go` - GitConfig gains AgentName/AgentEmail with kubebuilder validation markers (D-03)
- `api/v1alpha1/phase3_schema_test.go` - TestGitConfigRoundTrip extended with the two new fields
- `api/v1alpha2/schema_test.go` - New TestGitConfigAgentIdentity (round-trip + reflect field presence)
- `config/crd/bases/tideproject.k8s_projects.yaml` - Regenerated with both fields (pattern + maxLength) in both version schemas
- `charts/tide-crds/templates/project-crd.yaml` - Regenerated CRD subchart template kept in sync (verify-chart-reproducible gate)
- `internal/controller/dispatch_helpers.go` - ProviderDefaults.AgentName/AgentEmail + resolveAgentIdentity
- `internal/controller/dispatch_helpers_test.go` - Five TestResolveAgentIdentity_* behavior tests
- `cmd/manager/env.go` - tideHelmProviderDefaults populates the identity fields via pkggit env constants
- `cmd/manager/env_test.go` - env-set / env-unset coverage for the chart tier

## Decisions Made
- resolveAgentIdentity is pure (no `os.Getenv`) — the resolver owns defaulting, the manager only transports the chart tier. This keeps the compiled default masking-free (Pitfall 3): tests use non-default strings at every tier.
- Chart CRD subchart template (`charts/tide-crds/templates/project-crd.yaml`) regenerated to satisfy the pre-commit `verify-chart-reproducible` gate. This is mechanical CRD schema propagation from `config/crd/bases`, not the values.yaml FIXED contract and not the batched chart version bump (D-06, deferred to Plan 36-04). No `charts/tide/values.yaml` edits and no version bump were made here.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Regenerated chart CRD subchart template to unblock commit**
- **Found during:** Task 1 (CRD field addition + `make generate manifests`)
- **Issue:** The pre-commit `chart-reproducibility` hook runs `make helm` and fails on drift. Adding the CRD fields to `config/crd/bases` left `charts/tide-crds/templates/project-crd.yaml` stale, blocking the Task 1 commit.
- **Fix:** Staged the hook-regenerated `charts/tide-crds/templates/project-crd.yaml` (agentName/agentEmail propagated into both version schemas). This is generated output gated for sync, distinct from the FIXED values.yaml contract and the deferred version bump.
- **Files modified:** charts/tide-crds/templates/project-crd.yaml
- **Verification:** `chart reproducibility (make helm + diff) ... Passed` on the successful Task 1 commit.
- **Committed in:** a239ddf (Task 1 commit)

---

**Total deviations:** 1 auto-fixed (1 blocking)
**Impact on plan:** The regeneration is required to keep the repo's chart-reproducibility gate green; it is CRD schema propagation only, no scope creep into the chart version bump or values contract owned by Plan 36-04.

## Issues Encountered
- None beyond the chart-reproducibility gate documented above. BaseRef (Phase 35) is not yet landed, so the plan's conditional "mirror baseRef dual-version shape" branch did not apply — followed the GitConfig precedent directly as instructed.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- `resolveAgentIdentity` is the single resolution point Plan 36-03 calls at every Job-build site (subagent Job env + push Job env).
- The chart tier transport (`TIDE_AGENT_NAME`/`TIDE_AGENT_EMAIL` → ProviderDefaults) is ready for Plan 36-04 to render `agent.name`/`agent.email` chart values into the manager Deployment env; the batched chart version bump (D-06, with Phase 35) also lands in 36-04.
- No blockers.

## Self-Check: PASSED

---
*Phase: 36-signed-commits-bot-identity*
*Completed: 2026-07-08*
