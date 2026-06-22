# Project: dogfood-codex-runtime — Milestone Plan

**Outcome:** Add a second Subagent implementation for the Codex (OpenAI) runtime and demonstrate real heterogeneous dispatch — mixed-provider waves that actually run, not just interface scaffolding. Gate: a TIDE operator configures a Project where planner pool runs Claude and executor pool runs Codex; TIDE dispatches a multi-phase run to completion with correct wave-boundary failure semantics across both providers.

**Foundation (already in main — do NOT rebuild):** v1alpha2 global Execution DAG, layered Kahn wave derivation, global dispatch/failure/gates/resumption, cost/budget/telemetry/cache, and the full dashboard are all live.

---

## Milestone 01 — Codex Subagent Package

**Name:** `milestone-01-codex-subagent`
**DependsOn:** _(none — root milestone)_

### Outcome

A complete, independently testable Go package at `internal/subagent/codex/` that implements `pkg/dispatch.Subagent` against the Codex CLI (OpenAI). The package is provider-firewalled: zero Codex/OpenAI imports leak into `internal/controller/` or `cmd/manager/`. All tests pass offline (`make test` green; any live-API test is guarded by `OPENAI_API_KEY` env).

### Deliverables

- **`internal/subagent/codex/doc.go`** — package doc; references D-C1 layering pattern and the `vendorSentinel = "openai"` compile-time guard.
- **`internal/subagent/codex/client.go`** — `Client` struct + constructor; wraps Codex CLI invocation (`codex exec --ephemeral --skip-git-repo-check --ignore-user-config --json --sandbox workspace-write --output-schema <schema-file>`); reads `OPENAI_API_KEY` from env (mounted from K8s Secret by the dispatch Job spec).
- **`internal/subagent/codex/run.go`** — `Subagent.Run(ctx, EnvelopeIn) (EnvelopeOut, error)`. Parses the JSONL event stream from `codex exec --json`; extracts the final message; normalizes OpenAI usage block into `pkg/dispatch.Usage`:
  - `InputTokens` ← `usage.prompt_tokens`
  - `OutputTokens` ← `usage.completion_tokens`
  - `CacheReadTokens` ← `usage.prompt_tokens_details.cached_tokens` (OpenAI automatic caching — no `cache_control`, no `CacheCreationTokens`)
  - `EstimatedCostCents` + `CacheSavingsCents` ← per-model pricing table (below)
- **`internal/subagent/codex/pricing.go`** — per-model cents/MTok table mirroring `internal/subagent/anthropic/pricing.go`. Covers at minimum `codex-1` / `o4-mini` / `o3` / `gpt-4.1`. `cacheRead` column uses OpenAI's 50%-off cached-input rate.
- **`internal/subagent/codex/Dockerfile`** — multi-stage build mirroring `internal/subagent/anthropic/Dockerfile`; final image installs the Codex CLI binary; image name convention `ghcr.io/jsquirrelz/tide-codex-subagent`.
- **Unit tests** — `run_test.go`, `pricing_test.go`; all offline-safe; any test needing a real API call gated by `if os.Getenv("OPENAI_API_KEY") == ""`.

### Exit Criteria

1. `make test` green with zero new failures.
2. `make verify-*` guards green (Apache 2.0 headers, provider-firewall analyzer passes — no Codex imports in controller or manager).
3. `internal/subagent/codex/Dockerfile` builds a runnable image (`docker build` exits 0).
4. `Subagent.Run` correctly rejects an envelope whose `Provider.Vendor != "openai"` (vendor sentinel test).
5. Pricing table round-trips: given known prompt/completion/cache token counts, `EstimatedCostCents` matches expected value within floating-point tolerance.

---

## Milestone 02 — Per-Level Provider Dispatch

**Name:** `milestone-02-provider-dispatch`
**DependsOn:** `milestone-01-codex-subagent`

### Outcome

The TIDE controller dispatches subagent Jobs using the vendor declared in the per-level image config — not the hardcoded `"anthropic"` string that `ResolveProvider` currently returns unconditionally. An operator setting a Codex image at `spec.subagent.levels.task.image` (or an equivalent per-level vendor field) gets a Job whose `EnvelopeIn.Provider.Vendor == "openai"`, and the Codex subagent image accepts it. Helm chart gains the Codex image ref and a second provider Secret name. `make test` stays green.

### Deliverables

- **`api/v1alpha1/project_types.go` — `LevelConfig` extension:** Add `Vendor string` field (or a documented convention that vendor is inferred from the image name when the image matches a known sentinel prefix). Either approach must be locked and justified in the PLAN; no reopening mid-implementation.
- **`internal/controller/dispatch_helpers.go` — `ResolveProvider` rewrite:** Walk the same precedence chain as `resolveImage` (`levels.<level>.vendor` → `spec.subagent.vendor` → Helm default) and populate `ProviderSpec.Vendor` from config rather than hardcoding `"anthropic"`. The model precedence chain is unchanged.
- **`api/v1alpha1/project_types.go` — `SecretRefs` extension:** Add `OpenAIAPIKey string` field (mirroring `AnthropicAPIKey`) so operators declare the Secret name carrying `OPENAI_API_KEY`; the dispatch Job spec mounts it into the subagent container's env.
- **`internal/controller/reporter_jobspec.go` (or equivalent):** Wire the per-level secret ref into the Job pod spec so Codex-dispatched pods receive `OPENAI_API_KEY` from the declared K8s Secret — never from a host-config mount, never inlined.
- **`charts/tide/values.yaml`:** Add `images.codexSubagent.*` block (repository, tag, pullPolicy) and `subagent.levels.task.*` defaults showing the Codex image and vendor. Add comment block for the new `secretRefs.openAIAPIKey` operator knob.
- **`zz_generated.deepcopy.go`** regenerated if `LevelConfig` or `SecretRefs` gains fields with pointer/slice/map types.
- **Tests:** Update `dispatch_helpers_test.go` and `dispatch_image_test.go` to cover the new per-level vendor resolution path; cover both `"anthropic"` and `"openai"` resolution branches.

### Exit Criteria

1. `make test` green; no new failures.
2. `ResolveProvider` with a project whose `spec.subagent.levels.task` declares vendor `"openai"` returns `ProviderSpec{Vendor: "openai", ...}` — asserted in unit tests.
3. `ResolveProvider` with no per-level override continues to return `Vendor: "anthropic"` (backward-compat regression test).
4. `make verify-*` guards green; no Codex imports in controller or manager.
5. `api/v1alpha1/zz_generated.deepcopy.go` is up to date (`make generate` idempotent).
6. Helm chart `helm lint` passes with the new values block.

---

## Milestone 03 — Heterogeneous Integration

**Name:** `milestone-03-hetero-integration`
**DependsOn:** `milestone-02-provider-dispatch`

### Outcome

An operator can apply a single Project CR that routes milestone/phase/plan levels to Claude and the task level to Codex; TIDE dispatches the run to completion with waves spanning both providers and correct wave-boundary failure semantics throughout. This milestone is the proof-of-concept that every prior deliverable is wired end-to-end.

### Deliverables

- **`examples/projects/hetero-codex/project.yaml`** — a minimal but real Project CR demonstrating the mixed-provider configuration:
  - `spec.subagent.image` set to the Claude image (planner default).
  - `spec.subagent.levels.task.image` and `.vendor` set to the Codex image/`"openai"`.
  - `spec.secretRefs.anthropicAPIKey` and `spec.secretRefs.openAIAPIKey` referencing separate K8s Secrets (no credential inlining).
  - `spec.subagent.levels.task.model` set to a Codex model identifier (e.g. `codex-1`).
  - At least two phases with a cross-phase `dependsOn` edge to exercise a wave boundary.
- **`docs/hetero-providers.md`** — operator runbook: how to create the two Secrets, apply the Project CR, watch the global DAG on the dashboard, and interpret wave-boundary failure events across providers.
- **Integration test / dogfood verification:** Either a `make test-int` target (guarded by `OPENAI_API_KEY` + `ANTHROPIC_API_KEY` env) that runs the hetero project against a live kind cluster, OR a recorded trace (wave log + cost telemetry snapshot) demonstrating mixed-provider dispatch. The test or trace must show:
  1. Wave N tasks dispatched to Claude subagent pods — `EnvelopeIn.Provider.Vendor == "anthropic"`.
  2. Wave M tasks (task-level, post-planning) dispatched to Codex subagent pods — `EnvelopeIn.Provider.Vendor == "openai"`.
  3. A simulated task failure in a Codex-dispatched wave does not abort sibling tasks in the same wave and correctly blocks only dependent waves — not independent later waves (wave-boundary failure semantics, unchanged by provider mix).
  4. `tide_cost_cents_total` emits non-zero values for both `vendor=anthropic` and `vendor=openai` label combinations, and the dashboard's cache-efficiency panel shows Codex cache-read savings.
- **`charts/tide/values.yaml` or example overlay:** A minimal Helm override snippet (`--set` one-liner or `values-hetero.yaml`) showing how to configure both provider images and both Secret refs in one install.

### Exit Criteria

1. `make test` green; no regressions.
2. Example Project CR passes `kubectl apply --dry-run=client` (schema validation) against the live CRDs.
3. Integration test or recorded trace documents steps 1–4 from the deliverable list above.
4. `docs/hetero-providers.md` is complete enough for an operator unfamiliar with TIDE internals to execute the mixed-provider setup from scratch.
5. Provider-firewall analyzer (`make verify-*`) still reports zero violations — no Codex/OpenAI imports in controller or manager packages after all wiring is complete.

---

## DAG Summary

```
milestone-01-codex-subagent   (root — no deps)
        |
        v
milestone-02-provider-dispatch
        |
        v
milestone-03-hetero-integration
```

**M01** delivers the Codex package in isolation — a reviewable, testable unit behind the `pkg/dispatch.Subagent` interface.
**M02** makes the controller provider-neutral — `ResolveProvider` reads config rather than hardcoding `"anthropic"`, Helm chart gains Codex knobs.
**M03** closes the loop — a real Project CR dispatches Claude planners + Codex executors across wave boundaries with correct failure semantics and observable cost telemetry for both providers.
