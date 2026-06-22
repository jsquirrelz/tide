# PLAN.md — plan-03-codex-usage-normalizer

## Scope

Add the `internal/subagent/codex/` package implementing `pkg/dispatch.Subagent` for
the OpenAI Codex runtime, wire the per-level vendor switch in the controller so operators
can configure heterogeneous mixed-provider waves (e.g. Claude planners + Codex executors),
produce the `cmd/codex-subagent` binary entry point and its container image, and extend
the Helm chart + dogfood example manifests to document the new capability.

## Acceptance criteria (whole plan)

1. `go build ./...` is clean — no compilation errors.
2. `make test` (unit tests) is green with no OPENAI_API_KEY required.
3. `internal/subagent/codex/` satisfies the `pkg/dispatch.Subagent` interface and rejects
   envelopes whose `Provider.Vendor != "openai"` at the vendor sentinel check.
4. `internal/subagent/codex/pricing.go` contains an OpenAI model price table; `estimatedCostCents`
   returns 0 for zero-token usage and a conservative-tier fallback on unknown models.
5. `internal/subagent/codex/stream_parser.go` maps `prompt_tokens` → `InputTokens`,
   `completion_tokens` → `OutputTokens`, `prompt_tokens_details.cached_tokens` → `CacheReadTokens`,
   and keeps `CacheCreationTokens = 0`.
6. `internal/controller/dispatch_helpers.go` `ResolveProvider` derives Vendor from
   `LevelConfig.Vendor` → `SubagentConfig.Vendor` → fallback `"anthropic"`, and all
   existing `ResolveProvider` tests pass unchanged.
7. `api/v1alpha1/project_types.go` `LevelConfig` carries a `Vendor string` field and
   `SubagentConfig` carries a `Vendor string` field; `zz_generated.deepcopy.go` is
   NOT modified (the `*out = *in` pattern handles plain string fields correctly).
8. `images/codex-subagent/Dockerfile` builds clean via multi-stage build and bundles
   the `codex` CLI binary from the official npm package.
9. `charts/tide/values.yaml` contains a `codexSubagent` image entry under `images:`;
   `examples/projects/dogfood/02-codex-runtime-project.yaml` demonstrates per-level
   vendor wiring (`subagent.levels.task.vendor: openai`).

---

## Tasks

### task-01-codex-subagent-pkg
**Objective:** Create the complete `internal/subagent/codex/` Go package — five source
files (doc.go, client.go, run.go, pricing.go, stream_parser.go) — implementing
`pkg/dispatch.Subagent` behind the provider firewall.

**Dependencies:** none (Wave 1)

**Files touched:**
- `internal/subagent/codex/doc.go`
- `internal/subagent/codex/client.go`
- `internal/subagent/codex/run.go`
- `internal/subagent/codex/pricing.go`
- `internal/subagent/codex/stream_parser.go`

**Acceptance:** `go build ./internal/subagent/codex/` succeeds; vendor sentinel check
rejects non-"openai" envelopes; estimatedCostCents returns conservative fallback on
unknown models; ParseStream maps OpenAI usage fields to pkg/dispatch.Usage.

---

### task-02-codex-tests
**Objective:** Author offline-safe unit tests for the Codex subagent package:
`subagent_test.go` (vendor sentinel + fake exec), `pricing_test.go` (price table
coverage + conservative fallback), `stream_parser_test.go` (JSONL event parsing).
No real OPENAI_API_KEY is used — all tests must pass offline.

**Dependencies:** task-01-codex-subagent-pkg (Wave 2)

**Files touched:**
- `internal/subagent/codex/subagent_test.go`
- `internal/subagent/codex/pricing_test.go`
- `internal/subagent/codex/stream_parser_test.go`

**Acceptance:** `go test ./internal/subagent/codex/...` passes with no network calls.

---

### task-03-codex-cmd
**Objective:** Create `cmd/codex-subagent/main.go` — the thin binary entry point that
mirrors `cmd/claude-subagent/main.go`: reads `EnvelopeIn` → calls `codex.Run()` →
writes `EnvelopeOut`. Wires `TIDE_PRICING_OVERRIDES_JSON`, worktree setup, and commit.

**Dependencies:** task-01-codex-subagent-pkg (Wave 2)

**Files touched:**
- `cmd/codex-subagent/main.go`

**Acceptance:** `go build ./cmd/codex-subagent/` produces a binary; structure mirrors
claude-subagent (same flags, same env-path resolution, same worktree commit flow).

---

### task-04-codex-dockerfile
**Objective:** Create `images/codex-subagent/Dockerfile` — multi-stage build that
compiles the `cmd/codex-subagent` Go binary statically and bundles it with the Codex CLI
npm package (`@openai/codex`) in a node:22-slim base image, mirroring
`images/claude-subagent/Dockerfile`.

**Dependencies:** task-03-codex-cmd (Wave 3)

**Files touched:**
- `images/codex-subagent/Dockerfile`

**Acceptance:** `docker build -t ghcr.io/jsquirrelz/tide-codex-subagent:test -f images/codex-subagent/Dockerfile .`
exits 0; final image contains `/usr/local/bin/codex-subagent` and `codex` on PATH; runs
as UID 1000.

---

### task-05-per-level-vendor
**Objective:** Add `Vendor string` to both `LevelConfig` and `SubagentConfig` in
`api/v1alpha1/project_types.go`, and update `ResolveProvider` in
`internal/controller/dispatch_helpers.go` so it derives `Vendor` from
`LevelConfig.Vendor` → `SubagentConfig.Vendor` → `"anthropic"` fallback. Update
`internal/controller/dispatch_helpers_test.go` to add per-vendor-resolution tests.

**Dependencies:** none (Wave 1)

**Files touched:**
- `api/v1alpha1/project_types.go`
- `internal/controller/dispatch_helpers.go`
- `internal/controller/dispatch_helpers_test.go`

**Acceptance:** Existing `ResolveProvider` tests pass unchanged; new tests verify that
`LevelConfig.Vendor = "openai"` is returned when set; nil vendor falls back to
"anthropic"; `go test ./internal/controller/...` is green.

---

### task-06-chart-examples
**Objective:** Update `charts/tide/values.yaml` to add a `codexSubagent` entry under
`images:` (`repository: ghcr.io/jsquirrelz/tide-codex-subagent`) and add an
`openaiSecretName` key under a new `subagent.openai:` block for the OPENAI_API_KEY
Secret reference. Update `examples/projects/dogfood/02-codex-runtime-project.yaml`
to demonstrate per-level vendor/image wiring (`subagent.levels.task.vendor: openai`,
`subagent.levels.task.image: ghcr.io/jsquirrelz/tide-codex-subagent:1.0.0`).

**Dependencies:** task-05-per-level-vendor (Wave 2)

**Files touched:**
- `charts/tide/values.yaml`
- `examples/projects/dogfood/02-codex-runtime-project.yaml`

**Acceptance:** `helm template tide charts/tide/ --set images.codexSubagent.tag=test`
renders without error; the dogfood example manifest is valid YAML with the new vendor
and image fields populated.

---

## Task DAG

```
Wave 1: task-01-codex-subagent-pkg, task-05-per-level-vendor
Wave 2: task-02-codex-tests, task-03-codex-cmd, task-06-chart-examples
Wave 3: task-04-codex-dockerfile
```

Edges:
- task-02-codex-tests → task-01-codex-subagent-pkg
- task-03-codex-cmd → task-01-codex-subagent-pkg
- task-04-codex-dockerfile → task-03-codex-cmd
- task-06-chart-examples → task-05-per-level-vendor
