# Phase Brief — phase-05-heterogeneous-dispatch-demo

**Phase:** `phase-05-heterogeneous-dispatch-demo`
**Milestone:** `milestone-03-hetero-integration`
**Status:** Planned

---

## Objective

Deliver the operator-facing configuration surface and end-to-end integration
demonstration for mixed-provider (Claude + Codex) dispatch. Earlier phases ship
the Codex subagent package (phases 01–02), the Codex container image (phase 03),
and the per-level vendor switch in the controller (phase 04). This phase wires
the Helm chart to expose those capabilities, closes the OPENAI_API_KEY credential
path, and exercises the full mixed-provider dispatch loop with a reference Project
YAML and an offline-safe integration test.

The waveboundary failure contract — failed task siblings continue, dependents
never dispatch, non-dependents dispatch in strict profile — must hold identically
regardless of which provider ran a task; no new enforcement path is added here
(the global indegree model from Phase 25 is authoritative and unchanged).

---

## Scope

**In scope:**

- `charts/tide/values.yaml` — add `images.codexSubagent` block; add
  `subagent.defaults.codexImage`; add per-level vendor default keys
  (`subagent.levels.{milestone,phase,plan,task}.vendor`) whose values flow
  through as `TIDE_DEFAULT_VENDOR_*` env vars on the manager Deployment.
- `charts/tide/templates/deployment.yaml` — emit `CODEX_SUBAGENT_IMAGE` env var
  (resolution mirror of `CLAUDE_SUBAGENT_IMAGE`); emit `TIDE_DEFAULT_VENDOR_*`
  env vars consuming the phase-04 `ResolveProvider` vendor precedence chain.
- `api/v1alpha1/project_types.go` — add `CodexProviderSecretRef` to `ProjectSpec`
  (schema-additive, backwards-compatible); regenerate CRD manifest + deepcopy.
- `internal/dispatch/podjob/jobspec.go` — mount `CodexProviderSecretRef` as a
  second `envFrom: secretRef` on dispatch Job pods, mirroring the existing
  `ProviderSecretRef` mount; injects `OPENAI_API_KEY` (or any key in the named
  Secret) without inlining credentials.
- `cmd/manager/env.go` + `cmd/manager/env_test.go` — add
  `resolvePerLevelVendors()` reading `TIDE_DEFAULT_VENDOR_{MILESTONE,PHASE,PLAN,
  TASK}`; extend `ProviderDefaults` (or `tideHelmProviderDefaults`) to carry
  the per-level vendor map alongside the existing per-level model map so
  `ResolveProvider` (phase-04) has a Helm-default vendor to fall through to.
- `examples/projects/hetero/project.yaml` — reference operator Project YAML
  with planner pool pinned to Claude and executor pool pinned to Codex via
  `subagent.levels.task.vendor: openai` + `subagent.levels.task.image:
  ghcr.io/jsquirrelz/tide-codex-subagent` + `codexProviderSecretRef`.
- `examples/projects/dogfood/02-codex-runtime-project.yaml` — update to consume
  the new `codexProviderSecretRef` + per-level vendor fields.
- `test/integration/kind/hetero_dispatch_test.go` — mixed-provider integration
  test; all real OpenAI API calls are guarded by `OPENAI_API_KEY != ""` and
  `testing.Short()`; exercises D4 failure semantics (forced Codex task failure →
  sibling continues, dependents skipped, non-dependents dispatch); asserts
  `tide_cost_cents_total` + cache-efficiency panel carry non-zero Codex spend
  under `{project,phase,plan,wave}` label set (no `task` label).

**Out of scope (prior phases):**

- `internal/subagent/codex/` package implementation — phase 02
- `internal/subagent/codex/Dockerfile` + `cmd/codex-subagent/main.go` — phase 03
- `api/v1alpha1/project_types.go` `LevelConfig.Vendor` field + `ResolveProvider`
  vendor chain — phase 04
- CRD manifest + deepcopy for `LevelConfig.Vendor` — phase 04

---

## Expected Deliverables

| # | Artifact | Owned by |
|---|----------|----------|
| 1 | `charts/tide/values.yaml` — `images.codexSubagent`, vendor defaults | plan-01 |
| 2 | `charts/tide/templates/deployment.yaml` — `CODEX_SUBAGENT_IMAGE`, `TIDE_DEFAULT_VENDOR_*` envs | plan-01 |
| 3 | `cmd/manager/env.go` + `env_test.go` — `resolvePerLevelVendors()`, updated defaults | plan-01 |
| 4 | `api/v1alpha1/project_types.go` — `CodexProviderSecretRef` field | plan-02 |
| 5 | `api/v1alpha1/zz_generated.deepcopy.go` — regenerated | plan-02 |
| 6 | `config/crd/bases/tideproject.k8s_projects.yaml` — regenerated | plan-02 |
| 7 | `internal/dispatch/podjob/jobspec.go` — second `envFrom: secretRef` for Codex | plan-02 |
| 8 | `internal/dispatch/podjob/jobspec_test.go` — new Secret-mount assertion | plan-02 |
| 9 | `examples/projects/hetero/project.yaml` — heterogeneous reference Project | plan-03 |
| 10 | `examples/projects/dogfood/02-codex-runtime-project.yaml` — updated | plan-03 |
| 11 | `test/integration/kind/hetero_dispatch_test.go` — mixed-provider integration test | plan-03 |

---

## Verification Gates

1. `make test` exits 0 — all existing unit and integration tests pass unmodified.
2. `make verify-import-firewall` + `make verify-dispatch-imports` green — no
   `internal/subagent/codex` or OpenAI SDK import leaks into
   `internal/controller/` or `cmd/manager/`.
3. `helm template charts/tide` renders `CODEX_SUBAGENT_IMAGE`, `TIDE_DEFAULT_VENDOR_*`
   env vars, and the `codexProviderSecretRef` Secret mount across enabled/disabled
   permutations without secret material inlined.
4. A Project with `spec.subagent.levels.task.vendor: openai` resolves
   `Vendor: "openai"` from `ResolveProvider` while its planner levels resolve
   `Vendor: "anthropic"` — confirmed by the phase-04 unit test suite (not
   new work here; gate is that phase-04 tests still pass).
5. The `hetero_dispatch_test.go` integration test: (a) skips cleanly when
   `OPENAI_API_KEY` is unset (offline-safe); (b) when run with credentials,
   dispatches a two-wave Project to completion across both providers; (c)
   reproduces D4 failure semantics for a forced Codex task failure.
6. `tide_cost_cents_total` and the cache-efficiency panel attribute non-zero
   Codex spend under `{project,phase,plan,wave}` — `task` label absent
   (metriccardinality analyzer green).
7. No secret material appears in any checked-in manifest or chart value —
   credentials are referenced by Secret name only.
8. `make manifests` + `make generate` exit 0 and produce no uncommitted diff
   after plan-02 lands.

---

## Plan DAG

```
Wave 1 (parallel)
  plan-01-chart-codex-provider-wiring  ─────────┐
  plan-02-codex-secret-injection       ─────────┤
                                                  ▼
Wave 2                                plan-03-hetero-demo-and-integration-test
```

`plan-01` and `plan-02` touch disjoint file sets and declare no dependency on
each other — they may execute in parallel within Wave 1. `plan-03` depends on
both: the example Project YAML uses the chart values added in plan-01, and the
integration test exercises the Secret injection added in plan-02.
