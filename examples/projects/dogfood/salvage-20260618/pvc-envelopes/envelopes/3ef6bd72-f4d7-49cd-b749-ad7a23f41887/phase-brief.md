# Phase Brief — phase-02-codex-subagent-core

**Milestone:** milestone-03-hetero-integration  
**Phase:** phase-02-codex-subagent-core  
**Depends on:** phase-01-codex-pricing-usage  
**Status:** Planned — plans dispatched

---

## Objective

Author the `internal/subagent/codex/` Go package — the complete, tested
`pkg/dispatch.Subagent` implementation for the Codex (OpenAI) runtime — behind
the provider firewall. By this phase's exit the package is end-to-end correct
and fully offline-testable; the container image (phase-03) and the
controller-side vendor switch (phase-04) consume it unchanged.

---

## Scope

This phase delivers **six files**, no more, no less. All live in
`internal/subagent/codex/`. No files outside that directory are touched —
the provider firewall (`make verify-import-firewall`, `verify-dispatch-imports`)
enforces the boundary at build time.

| File | Purpose |
|------|---------|
| `doc.go` | Package comment — references D-C1 layering pattern; names the `"openai"` vendor sentinel; notes the CLI headless contract (D6) |
| `client.go` | `Client` struct + `Options` + `New()` + `NewWithExec()` — the execFunc seam that lets tests inject a fake `codex` binary without touching unexported fields |
| `stream_parser.go` | `ParseStream(r, rawSink)` — reads Codex `--json` JSONL event stream, tees raw bytes, extracts final message + `pkg/dispatch.Usage` per D5 mapping (`usage.prompt_tokens → InputTokens`, `usage.completion_tokens → OutputTokens`, `usage.prompt_tokens_details.cached_tokens → CacheReadTokens`, `CacheCreationTokens` fixed at 0) |
| `stream_parser_test.go` | Offline unit tests: happy path, no-result-event, tolerates-non-JSON, usage mapping invariants (CacheCreationTokens == 0) |
| `run.go` | `Subagent.Run(ctx, EnvelopeIn) (EnvelopeOut, error)` — vendor fail-fast guard (`"openai"` sentinel), D6 flag set (`--ephemeral --skip-git-repo-check --ignore-user-config --json --sandbox workspace-write --output-schema <schema>`), JSONL stream parsing, `Usage` population via phase-01 `estimatedCostCents`, child-CRD file handoff (planner role), prompt-artifact read (executor role), all traversal defenses mirroring the anthropic package |
| `run_test.go` | Offline unit tests via fake `codex` binary: vendor mismatch, fixture JSONL stream, child-CRD handoff, unknown-model conservative-tier fallback; real API call guarded by `OPENAI_API_KEY` env |

**Phase-01 pre-conditions (already present by the time this phase dispatches):**
`internal/subagent/codex/pricing.go` and `pricing_test.go` — consumed read-only
by `run.go` through the `estimatedCostCents` method on `Client`.

---

## Architecture Decisions (inherited — not re-derived here)

- **D1** — `client.go` / `run.go` / `stream_parser.go` mirror the
  `internal/subagent/anthropic/` layering exactly. File boundary rationale:
  `client.go` holds the constructor seam; `run.go` holds the CLI invocation
  and I/O orchestration; `stream_parser.go` is pure parsing with no side
  effects.

- **D5** — Usage normalisation: `cached_tokens → CacheReadTokens`;
  `CacheCreationTokens` **stays 0** (OpenAI caching is automatic, marker-free,
  1,024-token floor — there is no `cache_control` write phase, unlike
  Anthropic). `EstimatedCostCents` + `CacheSavingsCents` from the phase-01
  pricing table.

- **D6** — Codex CLI flags (carried verbatim per MILESTONE.md): `--ephemeral
  --skip-git-repo-check --ignore-user-config --json --sandbox workspace-write`.
  `--output-schema <file>` gates child-CRD emission to a JSON schema. Prompt
  delivered via `stdin` (not positional arg) to avoid Linux `MAX_ARG_STRLEN`.

- **Vendor sentinel** — `"openai"`. Locked by MILESTONE.md D2; `run.go`
  enforces it at the first line of `Run()` — mismatched image tag caught at
  dispatch, not mid-flight. String matches `pkg/dispatch.ProviderSpec.Vendor`
  canonical values.

- **`pkg/dispatch.Subagent` interface** — `Run(ctx, EnvelopeIn) (EnvelopeOut,
  error)` is the sole public surface the orchestrator touches. No Codex-specific
  type leaks into `pkg/dispatch`, `internal/controller/`, or `cmd/manager/`.

---

## Wave Decomposition

```
Wave 1 — parallel (no intra-phase deps):
  plan-01-codex-scaffold      doc.go + client.go
  plan-02-codex-stream-parser stream_parser.go + stream_parser_test.go

Wave 2 — serial after wave 1:
  plan-03-codex-run           run.go + run_test.go
                              (imports Client from plan-01, ParseStream from plan-02,
                               estimatedCostCents from phase-01 pricing.go)
```

Plans 01 and 02 declare no mutual dependency — they touch disjoint file sets
and share no package-level symbol definitions. Plan 03 imports both and is
therefore the natural join point.

---

## Expected Deliverables

1. `internal/subagent/codex/doc.go`
2. `internal/subagent/codex/client.go`
3. `internal/subagent/codex/stream_parser.go`
4. `internal/subagent/codex/stream_parser_test.go`
5. `internal/subagent/codex/run.go`
6. `internal/subagent/codex/run_test.go`

---

## Verification Gates

| Gate | Pass condition |
|------|---------------|
| V1 | `make test ./internal/subagent/codex/...` exits 0 with no network calls (offline) |
| V2 | `make verify-import-firewall` green — no `internal/subagent/codex` or OpenAI SDK import in `internal/controller/` or `cmd/manager/` |
| V3 | `make verify-dispatch-imports` green — provider firewall intact |
| V4 | `TestRun_VendorMismatch` passes: `Run()` returns non-nil error for `Provider.Vendor != "openai"` |
| V5 | `TestParseStream_UsageMapping` passes: `CacheReadTokens` populated from `usage.prompt_tokens_details.cached_tokens`; `CacheCreationTokens == 0` always |
| V6 | `TestEstimatedCostCents` (from phase-01 pricing.go) still passes after `run.go` calls `estimatedCostCents` |
| V7 | No real OpenAI API call fires unless `OPENAI_API_KEY` is set in the test environment |
| V8 | `make test` (full suite) exits 0 — no existing tests regressed |

---

## Constraints

- All Codex/OpenAI-specific code stays in `internal/subagent/codex/` behind
  `pkg/dispatch.Subagent` — zero provider leakage.
- `run.go` must NOT import any OpenAI Go SDK; the CLI binary is the only
  provider dependency (D6 / D-C1).
- Apache 2.0 license header on every new file. logr/zap logging pattern for
  any structured log lines.
- `CacheCreationTokens` MUST remain 0 — OpenAI has no cache-write billing
  concept. A test asserts this invariant explicitly.
- The `task` metric label is FORBIDDEN — metric label set stays
  `{project,phase,plan,wave}` per the locked cardinality analyzer.
