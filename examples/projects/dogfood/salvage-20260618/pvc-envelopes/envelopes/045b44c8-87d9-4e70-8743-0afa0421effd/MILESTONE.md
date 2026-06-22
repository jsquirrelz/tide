# Milestone 01 — Codex Subagent & Heterogeneous Dispatch

**Slug:** `milestone-01-codex-subagent`
**Status:** Planned — phases dispatched

---

## Outcome

A TIDE operator configures one Project whose planner pool runs Claude and whose
executor pool runs Codex (OpenAI), and TIDE dispatches a multi-phase run to
completion — mixed-provider waves that actually execute, with wave-boundary
failure semantics identical across both providers.

This is the milestone's single user-visible business outcome. Everything below
is the slice work that produces it; no phase delivers value on its own until
the heterogeneous run completes.

## What ships

The only new product surface is a second Subagent implementation
(`internal/subagent/codex/`, behind the `pkg/dispatch.Subagent` interface) plus
the per-level provider switch that lets a non-Anthropic vendor be selected and
honored at runtime. Everything else — the global Execution DAG, wave derivation,
global dispatch/failure/gates/resumption, cost/budget/telemetry/cache, the
dashboard — already ships in main and is built ON, never rebuilt.

## Exit criteria

- A Project pinning Claude at the planner levels and Codex at the executor
  level dispatches a multi-phase run to `Complete`.
- `ResolveProvider` derives the vendor per level; a Codex image at a level is
  selected AND accepted at runtime (no unconditional `Vendor:"anthropic"`).
- The Codex `Run()` normalizes the OpenAI usage block into `pkg/dispatch.Usage`
  (tokens + cost), so budget enforcement, `tide_cost_cents_total`, and the
  cache-efficiency panel attribute Codex spend instead of reading `$0`.
- Mixed-provider waves obey the unchanged wave-boundary failure contract:
  failed-task siblings continue, dependents in later waves never dispatch,
  non-dependents dispatch in strict profile — enforced by the existing global
  indegree model, no parallel path.
- `make test` and the `make verify-*` firewall guards stay green; no Codex
  imports leak into `internal/controller/` or `cmd/manager/`; tests pass
  offline (no real OpenAI call without an `OPENAI_API_KEY` env guard).

## Locked decisions (do not reopen in phases)

- **Location.** All OpenAI/Codex code lives in `internal/subagent/codex/`,
  mirroring `internal/subagent/anthropic/` layering, behind the Subagent
  interface (D-C1 firewall).
- **Granularity.** Per-level runtime selection — planner pool and executor pool
  each pin a runtime. Not per-task, not per-project (both rejected).
- **Credentials.** `OPENAI_API_KEY` / `CODEX_API_KEY` via a K8s Secret
  referenced by name (providerSecretRef pattern). Never host config, never
  headless OAuth, never inlined secret material.
- **Usage.** `InputTokens←prompt_tokens`, `OutputTokens←completion_tokens`,
  `CacheReadTokens←prompt_tokens_details.cached_tokens`,
  `CacheCreationTokens=0` (OpenAI caching is automatic, marker-free). Cost from
  a Codex-side pricing table mirroring `anthropic/pricing.go`.
- **Metric labels.** The label set stays `{project,phase,plan,wave}`; the
  `task` label remains forbidden.
- **Failure semantics.** Unchanged for mixed-provider waves.

---

## Phases

Two DAGs run through this milestone. The Execution DAG below derives the waves:

| Phase | Deliverable boundary | Depends on |
|-------|----------------------|------------|
| `phase-01-provider-switch` | Per-level vendor resolution in the controller: `ResolveProvider` derives the vendor (no hardcoded `anthropic`) so a non-Anthropic image at a level is selected AND accepted at runtime. Schema/wiring only — zero Codex imports in the controller. | — |
| `phase-02-codex-subagent` | The `internal/subagent/codex/` package implementing `pkg/dispatch.Subagent` end-to-end: `client.go`, `run.go` (codex exec, JSONL stream parse, `--output-schema` child-CRD emission), `doc.go`, usage normalization + pricing/cost table. Offline-testable behind the firewall. | — |
| `phase-03-image-and-credentials` | `internal/subagent/codex/Dockerfile` bundling the Codex CLI binary + chart `images.codexSubagent` ref and the new credential Secret-name value. Builds clean. | `phase-02-codex-subagent` |
| `phase-04-mixed-provider-integration` | The demonstration: an example Project (planner=Claude, executor=Codex) plus an integration test proving a multi-phase run completes with wave-boundary failure semantics holding across providers. | `phase-01-provider-switch`, `phase-03-image-and-credentials` |

Derived waves: **W1** `{phase-01, phase-02}` (parallel) → **W2** `{phase-03}` →
**W3** `{phase-04}`. The provider switch and the Codex package share only the
`pkg/dispatch.ProviderSpec` contract, so they fan out in the same wave; the
integration phase tidal-locks once both the switch and the buildable image
exist.
