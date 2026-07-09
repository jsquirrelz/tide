---
phase: 36-signed-commits-bot-identity
plan: 03
subsystem: dispatch
tags: [job-env, git-identity, dispatch, controller-runtime, precedence-chain, sign-01]

# Dependency graph
requires:
  - phase: 36-01
    provides: "pkg/git EnvAgentName/EnvAgentEmail constants + DefaultAgentName/Email + AgentIdentity() (in-Job reader)"
  - phase: 36-02
    provides: "resolveAgentIdentity(project, helmDefaults) + ProviderDefaults.AgentName/AgentEmail (the D-03 resolver + chart tier)"
provides:
  - "BuildOptions.AgentName/AgentEmail + unconditional subagent-Job env injection (TIDE_AGENT_NAME/TIDE_AGENT_EMAIL) for executor AND planner kinds"
  - "PushOptions.AgentName/AgentEmail + buildPushJob Env block on the push container (integrate merges + boundary commit)"
  - "helmDefaults threaded into triggerBoundaryPush + triggerWaveIntegrationJob; identity resolved once per dispatch"
  - "PodJobBackend.AgentName/AgentEmail + inline precedence mirror (no controller import)"
  - "All six subagent BuildOptions sites + both push sites populated end-to-end — the D-03 chain now reaches runtime"
affects: [36-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Unconditional env injection for resolved-never-empty values (contrast with the operator-gated PricingOverridesJSON append)"
    - "Inline precedence mirror in the fixture backend to avoid a podjob→controller import cycle"
    - "Both-builders env-presence tests with non-default values (Pitfall 3: one-sided testing masks a missed injection surface)"

key-files:
  created: []
  modified:
    - internal/dispatch/podjob/jobspec.go
    - internal/dispatch/podjob/jobspec_test.go
    - internal/dispatch/podjob/backend.go
    - internal/dispatch/podjob/backend_test.go
    - cmd/manager/main.go
    - internal/controller/push_helpers.go
    - internal/controller/push_helpers_test.go
    - internal/controller/boundary_push.go
    - internal/controller/task_controller.go
    - internal/controller/project_controller.go
    - internal/controller/milestone_controller.go
    - internal/controller/phase_controller.go
    - internal/controller/plan_controller.go

key-decisions:
  - "Injection is UNCONDITIONAL at both builders — the controller resolves a non-empty value (compiled default backstops), so no operator-gated conditional append; planner Jobs receive the env too (uniform injection beats kind-discrimination per D-03)"
  - "podjob mirrors resolveAgentIdentity inline (project.Spec.Git → backend field → compiled default) rather than importing the controller package — preserves the leaf-package import direction and the DAG/provider firewalls"

patterns-established:
  - "Pattern: resolve identity once immediately before the BuildOptions/PushOptions literal, beside the SubagentImage resolveImage call, at every dispatch site"
  - "Pattern: env-presence assertions on BOTH job builders (subagent executor+planner, push) with non-default values"

requirements-completed: [SIGN-01]

coverage:
  - id: D1
    description: "Subagent Jobs (executor AND planner kinds) render TIDE_AGENT_NAME/TIDE_AGENT_EMAIL from BuildOptions with the exact resolved values"
    requirement: "SIGN-01"
    verification:
      - kind: unit
        ref: "internal/dispatch/podjob/jobspec_test.go#TestBuildJobSpec_AgentIdentityEnv (executor + planner subtests, non-default values)"
        status: pass
      - kind: other
        ref: "grep -c 'EnvAgentName\\|EnvAgentEmail' internal/dispatch/podjob/jobspec.go == 2 (constants, not literals)"
        status: pass
    human_judgment: false
  - id: D2
    description: "The fixture backend mirrors the D-03 precedence chain inline (project.Spec.Git → backend field → compiled default) without importing the controller package"
    requirement: "SIGN-01"
    verification:
      - kind: unit
        ref: "internal/dispatch/podjob/backend_test.go#TestPodJobBackend_Run_AgentIdentityPrecedence (3 subtests: backend>default, project>backend, default-when-unset)"
        status: pass
      - kind: other
        ref: "grep -c 'internal/controller' internal/dispatch/podjob/backend.go == 0 (no import cycle)"
        status: pass
    human_judgment: false
  - id: D3
    description: "Both push-Job variants (plan/phase/milestone boundary + wave-integration) stamp resolved identity env on the push container; the creds EnvFrom path is untouched"
    requirement: "SIGN-01"
    verification:
      - kind: unit
        ref: "internal/controller/push_helpers_test.go#TestBuildPushJob_AgentIdentityEnv (non-default values + creds EnvFrom intact assertion)"
        status: pass
      - kind: other
        ref: "grep -c 'helmDefaults ProviderDefaults' + 'resolveAgentIdentity' internal/controller/boundary_push.go == 2 each (both functions + both PushOptions sites)"
        status: pass
    human_judgment: false
  - id: D4
    description: "All six subagent BuildOptions sites resolve via resolveAgentIdentity; whole unit tier + lint gates green"
    requirement: "SIGN-01"
    verification:
      - kind: other
        ref: "resolveAgentIdentity counts: task=2, project/milestone/phase/plan=1 each (acceptance-criteria greps)"
        status: pass
      - kind: integration
        ref: "make test exit 0 with 0 FAIL lines (grep -cE '^--- FAIL|^FAIL\\s' == 0); make lint exit 0 (import firewalls pass)"
        status: pass
    human_judgment: false

# Metrics
duration: 15min
completed: 2026-07-08
status: complete
---

# Phase 36 Plan 03: Agent-Identity Job-Env Injection Summary

**The genuinely net-new mechanism of SIGN-01: the controller-resolved agent identity now reaches every pod that commits — injected unconditionally into the subagent Job env (executor + planner) and the push Job env (boundary + wave-integration variants), closing the D-03 chain end-to-end so the dead env-read machinery from Plan 36-01 finally has a producer.**

## Performance

- **Duration:** ~15 min
- **Started:** 2026-07-08T05:43Z
- **Completed:** 2026-07-08T05:58Z
- **Tasks:** 3
- **Files modified:** 13

## Accomplishments
- Added `BuildOptions.AgentName/AgentEmail` and appended `{pkggit.EnvAgentName}`/`{pkggit.EnvAgentEmail}` **unconditionally** to the base `subagentEnv` slice in `jobspec.go` — both executor and planner subagent containers now carry the identity (uniform injection per D-03, unlike the operator-gated `TIDE_PRICING_OVERRIDES_JSON` append).
- Added `PodJobBackend.AgentName/AgentEmail` with an inline precedence walk in `Run` (`project.Spec.Git.Agent*` → backend field → `pkggit.DefaultAgent*`) that mirrors `controller.resolveAgentIdentity` without importing the controller package (no cycle); wired the two new backend fields from `helmProviderDefaults` in `cmd/manager/main.go`.
- Added `PushOptions.AgentName/AgentEmail` and an `Env` block on the push container in `buildPushJob` (placed before the untouched creds `EnvFrom`), covering both in-pod commit sites (integrate merges + tide-push boundary signature).
- Threaded a `helmDefaults ProviderDefaults` parameter into `triggerBoundaryPush` and `triggerWaveIntegrationJob`; each resolves the identity once via `resolveAgentIdentity` and stamps both `PushOptions` literals. Updated the three boundary wrappers (milestone/phase/plan) and the plan wave-integration caller to pass `r.HelmProviderDefaults`.
- Populated all six subagent `BuildOptions` sites (task ×2 via `r.Deps.HelmProviderDefaults`, project/milestone/phase/plan via `r.HelmProviderDefaults`), resolving beside the existing `resolveImage` call; phase/plan rely on the nil-safe resolver with no caller-side guard.
- Env-presence tests on BOTH builders with non-default values (executor + planner subagent, push container) plus a 3-case backend precedence test. `make test` green (0 FAIL lines), `make lint` green (import firewalls pass with the new `pkg/git` imports).

## Task Commits

Each task was committed atomically:

1. **Task 1: Subagent Job env injection — BuildOptions fields + backend mirror (TDD)** - `8ee2df2` (feat)
2. **Task 2: Push Job env injection — PushOptions fields + helmDefaults threading (TDD)** - `bfdf23f` (feat)
3. **Task 3: Populate all six subagent BuildOptions sites + full unit tier + lint** - `c95cccf` (feat)

## Files Created/Modified
- `internal/dispatch/podjob/jobspec.go` - `BuildOptions.AgentName/AgentEmail`; unconditional identity env append to `subagentEnv`; `pkggit` import
- `internal/dispatch/podjob/jobspec_test.go` - `TestBuildJobSpec_AgentIdentityEnv` (executor + planner, non-default values)
- `internal/dispatch/podjob/backend.go` - `PodJobBackend.AgentName/AgentEmail` + inline precedence mirror in `Run`; `pkggit` import
- `internal/dispatch/podjob/backend_test.go` - `TestPodJobBackend_Run_AgentIdentityPrecedence` (backend>default, project>backend, default-when-unset)
- `cmd/manager/main.go` - PodJobBackend construction populates the two identity fields from `helmProviderDefaults`
- `internal/controller/push_helpers.go` - `PushOptions.AgentName/AgentEmail`; `Env` block on the push container; `pkggit` import
- `internal/controller/push_helpers_test.go` - `TestBuildPushJob_AgentIdentityEnv` (non-default values + creds EnvFrom intact)
- `internal/controller/boundary_push.go` - `helmDefaults ProviderDefaults` param on both dispatch functions; `resolveAgentIdentity` at both PushOptions sites; wrappers pass `r.HelmProviderDefaults`
- `internal/controller/task_controller.go` - both executor BuildOptions sites resolve + stamp identity (`r.Deps.HelmProviderDefaults`)
- `internal/controller/project_controller.go` / `milestone_controller.go` / `phase_controller.go` / `plan_controller.go` - planner BuildOptions sites resolve + stamp identity; plan caller threads HelmProviderDefaults into the wave-integration Job

## Decisions Made
- **Unconditional injection at both builders.** The controller always resolves a non-empty identity (the compiled default backstops the D-03 chain), so there is no operator-gated conditional append — the two env vars are always present. Planner Jobs receive them too; uniform injection is simpler and safer than discriminating by kind (D-03). Contrast documented against the pricing-override conditional in the same `subagentEnv` slice.
- **Inline mirror instead of a controller import.** `PodJobBackend` (fixture-only) re-implements the precedence walk inline rather than importing `controller.resolveAgentIdentity`, because `internal/dispatch/podjob` is a leaf that the controller imports — importing back would create a cycle and trip the import firewalls. The inline walk carries a comment stating it mirrors `controller.resolveAgentIdentity`.

## Deviations from Plan
None - plan executed exactly as written. (Two comments were reworded during execution so acceptance-criteria greps — `internal/controller` count == 0 in backend.go, `resolveAgentIdentity` count == 1 in phase/plan controllers — resolve against real code references only, not comment prose. No behavior change.)

## Known Stubs
None. Every injected value flows from `resolveAgentIdentity`/`ProviderDefaults` (36-02) → Job env → in-Job `AgentIdentity()` reader (36-01). No hardcoded empties, placeholders, or unwired data sources.

## Issues Encountered
- The `state.record-metric` / `state.advance-plan` SDK verbs expect the standard per-plan STATE.md format; this project tracks progress via a phase-level table (v1.0.7 Phase Tracking), so the plan-count row and metrics were updated in the STATE.md/ROADMAP.md tables directly (consistent with the CLAUDE.md note that STATE.md body position text is prose, not SDK-derived).

## User Setup Required
None - no external service configuration required. The chart value that feeds the manager env (`agent.name`/`agent.email` → `TIDE_AGENT_NAME`/`TIDE_AGENT_EMAIL`) and the batched chart version bump land in Plan 36-04; until then, unconfigured installs resolve to the compiled default `TIDE Agent <tide-agent@tideproject.k8s>` identically to Plan 36-01's in-Job reader.

## Next Phase Readiness
- SIGN-01's runtime mechanism is complete: a Project with a custom `spec.git.agentName/agentEmail` now produces Jobs stamped with that identity at all three commit sites; an unconfigured install behaves identically to the compiled default.
- Plan 36-04 remains: render the `agent.name`/`agent.email` chart values into the manager Deployment env and ship the batched chart version bump (D-06, with Phase 35).
- No blockers.

## Self-Check: PASSED
- Files verified present: jobspec.go, backend.go, push_helpers.go, boundary_push.go, cmd/manager/main.go (all FOUND)
- Commits verified in git log: 8ee2df2, bfdf23f, c95cccf (all FOUND)
- `make test` exit 0, 0 FAIL lines; `make lint` exit 0

---
*Phase: 36-signed-commits-bot-identity*
*Completed: 2026-07-08*
