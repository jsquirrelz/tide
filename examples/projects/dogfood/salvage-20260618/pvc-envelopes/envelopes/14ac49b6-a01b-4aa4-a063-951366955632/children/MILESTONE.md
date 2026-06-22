# Milestone 02 — Provider Dispatch (Codex runtime + heterogeneous waves)

## Outcome

A TIDE operator can configure a single Project whose **planner pool runs Claude
and whose executor pool runs Codex (OpenAI)**, and TIDE dispatches a multi-phase
run to completion — with the spec's wave-boundary failure semantics holding
*unchanged* across both providers. The new product surface is exactly two things:
the `internal/subagent/codex/` package and the per-level provider switch that
makes a non-Anthropic image at a level actually run. Everything else
(v1alpha2 schema, global Execution DAG, dispatch/failure/gates/resumption,
cost/budget/telemetry/cache, dashboard) is given infrastructure and is built
*on*, not rebuilt.

## Exit criteria

- A second `pkg/dispatch.Subagent` implementation exists at
  `internal/subagent/codex/`, mirroring `internal/subagent/anthropic/`
  layering (D-C1). All OpenAI/Codex code stays behind the interface — zero
  Codex imports in `internal/controller/` or `cmd/manager/`; the
  `providerfirewall` analyzer and `verify-dispatch-imports` stay green.
- The Codex `Run()` drives `codex exec` headless (`--json` JSONL stream,
  `--output-schema` for child-CRD emission) and **normalizes the OpenAI usage
  block into `pkg/dispatch.Usage`** — `prompt_tokens` → InputTokens,
  `completion_tokens` → OutputTokens, `prompt_tokens_details.cached_tokens` →
  CacheReadTokens (CacheCreationTokens stays 0), with a Codex-side pricing
  table producing EstimatedCostCents + CacheSavingsCents. Budget enforcement,
  `tide_cost_cents_total`, and the cache-efficiency panel attribute Codex spend.
- `ResolveProvider` derives the per-level Vendor from the resolved level image
  instead of returning `"anthropic"` unconditionally; a Codex image pinned at a
  level emits an envelope the Codex image accepts and the Anthropic image
  rejects (vendor-sentinel fail-fast preserved both ways).
- The Codex container image builds clean from `internal/subagent/codex/Dockerfile`
  and bundles the Codex CLI; credentials arrive via an `OPENAI_API_KEY` /
  `CODEX_API_KEY` K8s Secret referenced by name only (never inlined; never host
  config; never headless OAuth). Chart values name the new Secret.
- A worked mixed-provider Project (planner=Claude, executor=Codex) runs a
  multi-phase DAG to Complete; an induced executor (Codex) task failure keeps
  independent same-wave siblings running, never dispatches dependents in later
  waves, and dispatches non-dependents per `failureProfile` — identical to the
  all-Anthropic path, enforced by the existing global indegree model (no
  parallel failure path added).
- `make test` is green and offline: no real OpenAI call runs without an explicit
  `OPENAI_API_KEY` test-env guard.

## Decomposition

The milestone fans into five phases over a shallow DAG. The Codex package
(P1) and the controller-side provider switch (P3) are independent roots and
plan/build in parallel; the image binary (P2) consumes the package; the
credential + chart wiring (P4) consumes the resolved-vendor switch; the
heterogeneous-dispatch demonstration (P5) is the capstone that proves the
outcome and depends on all four.

- **phase-01-codex-subagent-core** — the `internal/subagent/codex/` package:
  `client.go`, `run.go` (Subagent.Run over `codex exec`), JSONL stream parser,
  OpenAI pricing table + `pkg/dispatch.Usage` normalization, `doc.go`. Behind
  the Subagent interface; offline, env-guarded tests. *(root)*
- **phase-02-codex-image-binary** — `cmd/codex-subagent` shim mirroring
  `cmd/claude-subagent`, the multi-stage `internal/subagent/codex/Dockerfile`
  bundling the Codex CLI, and the `tide-codex-subagent` image-ref convention.
  *(depends on P1)*
- **phase-03-per-level-provider-switch** — make `ResolveProvider` derive Vendor
  per level so a Codex image selects `Vendor:"openai"`; controller stays
  provider-firewalled (no Codex import). *(root)*
- **phase-04-credential-and-chart-wiring** — route `OPENAI_API_KEY`/
  `CODEX_API_KEY` to the subagent container by Secret-ref (per-vendor env
  naming through credproxy) and add chart values for the new Secret name.
  *(depends on P3)*
- **phase-05-heterogeneous-dispatch-demo** — mixed-provider example Project,
  the multi-phase integration/e2e demonstrating completion + cross-provider
  wave-boundary failure semantics, and operator docs. *(depends on P1–P4)*
