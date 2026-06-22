# Phase Brief — phase-03-codex-image-entrypoint

**Phase:** phase-03-codex-image-entrypoint
**Milestone:** milestone-03-hetero-integration
**Depends on:** phase-02-codex-subagent-core

---

## Objective

Deliver the runtime image and entrypoint shim that make `internal/subagent/codex/`
dispatchable as a container — the final bridge between the Phase 02 Go package and a
running K8s Job pod. Phase 02 proves the codex subagent works as a library; this phase
produces the binary (`cmd/codex-subagent/`) that wraps it and the multi-stage Dockerfile
that bundles it with the Codex CLI, giving the controller a pullable image ref
(`ghcr.io/jsquirrelz/tide-codex-subagent`) to schedule.

Nothing in this phase touches the controller, the chart, or any v1alpha2 API type.
The provider firewall (`make verify-import-firewall`, `verify-dispatch-imports`) must
remain green — all Codex/OpenAI-specific code stays behind `pkg/dispatch.Subagent`.

---

## Scope

**In scope:**

- `cmd/codex-subagent/main.go` — thin shim mirroring `cmd/claude-subagent/main.go`:
  load `EnvelopeIn` via `harness.ReadEnvelopeIn`, call `codex.Run`, write `EnvelopeOut`
  via `harness.WriteEnvelopeOut` + termination stub. Vendor sentinel is `"openai"`.
  No `Dev.TestMode` branching (PATTERNS.md line 442 — real subagent images ignore
  `env.Dev` entirely). Executor role calls `harness.CommitWorktree` after zero exit;
  planner role skips commit.

- `cmd/codex-subagent/main_test.go` — six offline unit tests round-tripping fixture JSONL
  envelopes through the shim; mirrors `cmd/claude-subagent/main_test.go` test topology
  (HappyPath, EnvelopeLoadFailure, VendorMismatch, InvokesEnsureWorktreeBeforeRun,
  PassesEnvBranchToWorktree, IgnoresDevTestMode). Uses the `newSubagent` package-level
  fake seam — no live OpenAI calls, no `OPENAI_API_KEY` required.

- `cmd/codex-subagent/commit_test.go` — executor commit + empty-diff tests mirroring
  `cmd/claude-subagent/commit_test.go` (TestRunCommitsExecutorWorktree,
  TestRunEmptyDiffOverridesExitCode, TestRunPlannerSkipsCommit).

- `images/codex-subagent/Dockerfile` — multi-stage build mirroring
  `images/claude-subagent/Dockerfile`. Stage 1: static Linux Go binary compiled from
  `cmd/codex-subagent/` via `CGO_ENABLED=0`. Stage 2: Node slim base, pinned
  `@openai/codex` npm package (Codex CLI), Git installed, Go binary copied, UID 1000.
  Canonical image ref: `ghcr.io/jsquirrelz/tide-codex-subagent`.

- `Makefile` additions — `docker-build-codex-subagent` and `docker-push-codex-subagent`
  targets mirroring the existing claude-subagent targets; `IMAGE_TAG` convention unchanged;
  wired into the umbrella `docker-build` / `docker-push` goals.

**Out of scope (handled by other phases):**

- Helm chart wiring, Secret injection (phase-05-chart-secret-wiring).
- Per-level vendor switch in `ResolveProvider` (phase-04-per-level-vendor-switch).
- Heterogeneous integration test (phase-06-hetero-integration).
- `internal/subagent/codex/` package itself (phase-02-codex-subagent-core).

---

## Expected Deliverables

1. `cmd/codex-subagent/main.go` — harness-wrapped entrypoint; `run()` testable seam;
   `newSubagent` injectable fake seam.
2. `cmd/codex-subagent/main_test.go` — six offline unit tests, all green under `make test`
   without `OPENAI_API_KEY`.
3. `cmd/codex-subagent/commit_test.go` — three executor-path unit tests.
4. `images/codex-subagent/Dockerfile` — multi-stage; `docker build` clean from repo root.
5. `Makefile` additions — `docker-build-codex-subagent` / `docker-push-codex-subagent`.

---

## Verification Gates

| Gate | Criterion |
|------|-----------|
| G1 | `make test` exits 0 — all existing and new unit tests pass offline |
| G2 | `make verify-import-firewall` green — `internal/subagent/codex` not in `internal/controller/` or `cmd/manager/` |
| G3 | `make verify-dispatch-imports` green — no OpenAI SDK leak into controller/manager |
| G4 | `docker build -t ghcr.io/jsquirrelz/tide-codex-subagent:test -f images/codex-subagent/Dockerfile .` exits 0 |
| G5 | Shim round-trips `EnvelopeIn{Provider.Vendor:"openai"}` to `EnvelopeOut{ExitCode:0}` offline |
| G6 | Shim rejects `Provider.Vendor:"anthropic"` with non-zero exit + failure-shaped `out.json` |
| G7 | No `OPENAI_API_KEY` required for any test — suite passes fully offline |

---

## Wave Decomposition (illustrative — authoritative schedule derived by Kahn over Task DAG)

**Wave 1 — independent:**
- `plan-01-codex-entrypoint-shim` — `cmd/codex-subagent/` Go binary + all tests

**Wave 2 — depends on plan-01:**
- `plan-02-codex-dockerfile` — `images/codex-subagent/Dockerfile` + `Makefile` targets

---

## Interface Contracts (locked by MILESTONE.md — do not reinvent)

- Envelope default path: `/workspace/envelopes/<TIDE_TASK_UID>/in.json`
- Vendor sentinel string: `"openai"` — matches phase-02's `codex.vendorSentinel`
- Image ref: `ghcr.io/jsquirrelz/tide-codex-subagent` — phase-05 wires into Helm chart
- No `Dev.TestMode` branch — real runtime binary, not the stub
- `TIDE_PRICING_OVERRIDES_JSON` parsed via `parsePricingOverridesFromEnv` (Codex pricing table)
