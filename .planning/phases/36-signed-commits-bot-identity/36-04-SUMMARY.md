---
phase: 36-signed-commits-bot-identity
plan: 04
subsystem: infra
tags: [helm, helmify, chart, kubernetes, git-identity, deployment-env]

# Dependency graph
requires:
  - phase: 36-02
    provides: "cmd/manager/env.go envOrDefault reads of TIDE_AGENT_NAME/TIDE_AGENT_EMAIL into ProviderDefaults; config/crd/bases carrying agentName/agentEmail"
  - phase: 36-03
    provides: "resolved agent identity injected into subagent + push Job env"
provides:
  - "Chart tier of the D-03 identity precedence chain: agent.name/agent.email values render as TIDE_AGENT_NAME/TIDE_AGENT_EMAIL env on the manager Deployment"
  - "D-06 batched chart version bump 1.0.6 -> 1.0.7 (single bump across Phases 35+36)"
  - "Helm-template contract test pinning the env render against helmify-regen drift"
  - "Operator docs for git.agentName/git.agentEmail + install-wide chart tier + routable-email guidance"
affects: [signing, chart-release, operator-docs]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Marker-guarded augment block (phase36-agent-env-injected) copied from the ENV3 mechanism in augment-tide-chart.sh"
    - "Plain go-test helm-template contract test asserting committed template bytes (no cluster)"

key-files:
  created:
    - test/integration/kind/agent_identity_chart_test.go
  modified:
    - hack/helm/tide-values.yaml
    - hack/helm/augment-tide-chart.sh
    - hack/helm/tide-chart.yaml
    - hack/helm/tide-crds-chart.yaml
    - charts/tide/ (regenerated)
    - charts/tide-crds/ (regenerated)
    - docs/project-authoring.md

key-decisions:
  - "D-06 batched bump applied HERE: versions were still 1.0.6 (Phase 35 did not bump), so this plan performed the single 1.0.6 -> 1.0.7 bump across both chart + crds-chart sources"
  - "agent.* values placed as a top-level block (not nested under subagent.*) since identity applies to push Jobs too; deliberately distinct from the signingKey HMAC namespace"
  - "New env block anchored after the phase28-import-env-injected marker so it renders immediately before envFrom, matching the committed chart's env ordering"

patterns-established:
  - "Chart-contract go-test: assert env-name + value-path byte-strings against charts/tide/templates/deployment.yaml so a helmify regen dropping an augment block fails CI"

requirements-completed: [SIGN-01]

coverage:
  - id: D1
    description: "Manager Deployment renders TIDE_AGENT_NAME/TIDE_AGENT_EMAIL from .Values.agent.name/.Values.agent.email (empty defaults → compiled default wins)"
    requirement: "SIGN-01"
    verification:
      - kind: integration
        ref: "test/integration/kind/agent_identity_chart_test.go#TestHelmDeploymentTemplateRendersAgentIdentityEnv"
        status: pass
    human_judgment: false
  - id: D2
    description: "charts/ reproducible from hack/helm sources after regeneration (D-06 batched version bump 1.0.6 -> 1.0.7)"
    requirement: "SIGN-01"
    verification:
      - kind: automated
        ref: "make verify-chart-reproducible"
        status: pass
      - kind: automated
        ref: "make verify-version-consistency"
        status: pass
    human_judgment: false
  - id: D3
    description: "Operator docs cover both config tiers (per-Project git.agentName/agentEmail + install-wide agent.*), precedence chain, and routable-email guidance"
    requirement: "SIGN-01"
    verification:
      - kind: automated
        ref: "grep -c 'agentName' docs/project-authoring.md (>=2)"
        status: pass
    human_judgment: false

# Metrics
duration: 14min
completed: 2026-07-08
status: complete
---

# Phase 36 Plan 04: Chart-Tier Agent Identity Summary

**The manager Deployment now renders `TIDE_AGENT_NAME`/`TIDE_AGENT_EMAIL` from install-wide `agent.name`/`agent.email` chart values, completing the D-03 precedence chain (Project spec → chart → compiled default) with the single batched 1.0.7 version bump.**

## Performance

- **Duration:** ~14 min
- **Tasks:** 3 completed
- **Files modified:** 8 (1 created, 7 modified incl. regenerated charts/)

## Accomplishments
- Wired the chart tier of the identity chain: `agent.name`/`agent.email` values → `TIDE_AGENT_NAME`/`TIDE_AGENT_EMAIL` env on the manager container, sourced in `cmd/manager/env.go` (Plan 36-02) byte-for-byte.
- Performed the D-06 batched chart version bump 1.0.6 → 1.0.7 (Phase 35 had not bumped; this was the single batched bump across `tide-chart.yaml` + `tide-crds-chart.yaml`).
- Added a plain go-test helm-template contract test that fails CI if a helmify regen drops the `phase36-agent-env-injected` augment block.
- Documented both config tiers, the precedence chain, and the routable-email guidance for future signing.

## Task Commits

1. **Task 1: Chart sources — values block, env augment, conditional version bump, regen** - `d47e950` (feat)
2. **Task 2: Helm-template contract test for the identity env render** - `c33aec5` (test)
3. **Task 3: Operator docs — GitConfig table rows + identity note** - `b5cd229` (docs)

## Files Created/Modified
- `test/integration/kind/agent_identity_chart_test.go` (NEW) - `TestHelmDeploymentTemplateRendersAgentIdentityEnv`; asserts the four byte-strings (`TIDE_AGENT_NAME`, `TIDE_AGENT_EMAIL`, `.Values.agent.name`, `.Values.agent.email`).
- `hack/helm/tide-values.yaml` - top-level `agent:` block (`name: ""`, `email: ""`) with precedence comment; distinct from the `signingKey` HMAC namespace.
- `hack/helm/augment-tide-chart.sh` - `phase36-agent-env-injected` marker block rendering the two env vars, anchored after `phase28-import-env-injected`.
- `hack/helm/tide-chart.yaml`, `hack/helm/tide-crds-chart.yaml` - version/appVersion 1.0.6 → 1.0.7.
- `charts/tide/`, `charts/tide-crds/` - regenerated via `make helm-controller helm-crds`.
- `docs/project-authoring.md` - `git.agentName`/`git.agentEmail` table rows + commit-author-identity prose with routable-email note.

## Decisions Made
- **D-06 case resolved: BUMPED here.** Both `hack/helm/tide-chart.yaml` and `hack/helm/tide-crds-chart.yaml` were still `1.0.6` at execution time (Phase 35 did not bump for this milestone), so this plan performed the single batched 1.0.6 → 1.0.7 bump. `make verify-version-consistency` confirms all 4 version files agree on 1.0.7.
- **Env-block anchor:** inserted after the `phase28-import-env-injected` marker (rendering immediately before `envFrom:`) rather than at the ENV3 anchor, keeping the committed chart's env ordering stable and reproducible.
- **Top-level `agent:` block** (not nested under `subagent.*`) because identity applies to push Jobs, not just subagents; kept byte-separate from `signingKey`.

## Deviations from Plan
None - plan executed exactly as written.

## Issues Encountered
- `make verify-chart-reproducible` initially reported drift because `git diff --quiet -- charts/` compares the working tree against the git index, and the regenerated charts were not yet staged. Staging the sources + regenerated `charts/` and re-running produced `OK: charts/ tree is reproducible`. This is the target's normal pre-stage behavior, not a real reproducibility failure. The pre-commit hook (which stages before checking) also passed on the Task 1 commit.

## User Setup Required
None - no external service configuration required. Operators may optionally set install-wide identity via `--set agent.name=... --set agent.email=...` at helm install.

## Next Phase Readiness
Phase 36 (SIGN-01) is complete: all four plans land the agent-identity surface across code, config, dispatch, and chart tiers. GPG signing (SIGN-02/03/04) remains deferred out of v1.0.7 per D-01. The 1.0.7 chart version is now consistent across all four version files, ready for the milestone release.

## Self-Check: PASSED

- Files verified present: `test/integration/kind/agent_identity_chart_test.go`, `hack/helm/tide-values.yaml`, `hack/helm/augment-tide-chart.sh`, `docs/project-authoring.md`, `charts/tide/templates/deployment.yaml` (renders both env vars).
- Commits verified in git log: `d47e950`, `c33aec5`, `b5cd229`.
- `make verify-chart-reproducible`, `make verify-version-consistency`, and all 7 `TestHelmDeploymentTemplate*` contract tests green.

---
*Phase: 36-signed-commits-bot-identity*
*Completed: 2026-07-08*
