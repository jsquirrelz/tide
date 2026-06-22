# Phase Brief: phase-02-codex-image-binary

**Phase:** `phase-02-codex-image-binary`
**Milestone:** `milestone-02-provider-dispatch`
**Depends on:** `phase-01-codex-subagent-core`
**Date:** 2026-06-17

---

## Objective

Wire the Codex Go package authored in phase-01 into a runnable container — a
thin entrypoint binary at `cmd/codex-subagent/` that mirrors `cmd/claude-subagent/`
exactly — load `EnvelopeIn` → `codex.Run` → write `EnvelopeOut` — and a
multi-stage `Dockerfile` that bundles the Codex CLI binary into the final image
alongside the statically-compiled shim. After this phase a TIDE operator can
reference `ghcr.io/jsquirrelz/tide-codex-subagent` in a Project's per-level
image override and the orchestrator can dispatch real Codex-backed Tasks.

---

## Scope

All deliverables are confined to two packages:

- `cmd/codex-subagent/` — entrypoint shim + unit tests (zero controller,
  chart, or API-type touches)
- `internal/subagent/codex/Dockerfile` — multi-stage container build that
  consumes the phase-01 `internal/subagent/codex/` package

Hard phase boundary: no `ResolveProvider` wiring (phase-03 territory), no Helm
chart mutations (phase-04 territory), no controller changes. This phase delivers
exactly the binary + image contract that downstream phases consume; the contract
is carried verbatim in the MILESTONE.md architecture decisions and must not be
reopened here.

---

## Expected Deliverables

### D1 — `cmd/codex-subagent/main.go`

A thin shim mirroring `cmd/claude-subagent/main.go` beat-for-beat:

- Flags: `--envelope-path` (falls back to `$TIDE_ENVELOPE_PATH` then
  `/workspace/envelopes/$TIDE_TASK_UID/in.json`), `--workspace-root`
  (default `/workspace`).
- `main()` → `run(ctx, envelopePath, workspaceRoot, stdout, stderr) int`
  (testable entry point — no `os.Exit` inside `run`).
- `harness.ReadEnvelopeIn` → `harness.EnsureWorktree` → `codex.New().Run` →
  (executor path only) `harness.CommitWorktree` → `writeEnvelope`.
- `parsePricingOverridesFromEnv` reads `TIDE_PRICING_OVERRIDES_JSON` and
  passes it to `codex.New(codex.Options{PricingOverrides: ...})` — mirrors the
  D-02 per-instance pricing override seam.
- The `newSubagent` package-level var is the exec-seam for tests (mirrors the
  `anthropicRunner` interface + `newSubagent` pattern).
- `OPENAI_API_KEY` / `CODEX_API_KEY` are **not** read by this shim — they are
  mounted into the Pod by the Helm chart (phase-04) via the `providerSecretRef`
  pattern; the shim inherits them from the container env just as `claude-subagent`
  inherits `ANTHROPIC_API_KEY`.
- Apache 2.0 header; logr/zap absent (this is a thin shim, no logger needed
  beyond `fmt.Fprintf(stderr, ...)`).

### D2 — `cmd/codex-subagent/main_test.go`

Offline unit tests covering:

1. **HappyPath** — fixture JSONL stream (final `message` event with `usage`
   block), valid `EnvelopeIn` with `Provider.Vendor="openai"` → exit 0, valid
   `EnvelopeOut` written, `Usage.InputTokens` / `Usage.OutputTokens` populated.
2. **EnvelopeLoadFailure** — missing envelope path → exit 2, stderr mentions
   "envelope".
3. **VendorMismatch** — `Provider.Vendor="anthropic"` → non-zero exit, `out.json`
   has `ExitCode != 0` and non-empty `Reason`.
4. **EnsureWorktreeBefore Run ordering** — `ensureWorktreeFunc` seam records
   call order; assert `ensure-worktree` precedes `subagent-run`.
5. **BranchPropagation** — `EnvelopeIn.Branch` forwarded verbatim to
   `ensureWorktreeFunc`; not sourced from any filesystem artifact.

No real `OPENAI_API_KEY` required; all tests use a `codex.NewWithExec` seam
injecting a `bash -c 'cat <fixture>'` fake binary. Tests must pass fully offline
(`OPENAI_API_KEY` not set).

### D3 — `internal/subagent/codex/Dockerfile`

Multi-stage build — mirrors `images/claude-subagent/Dockerfile` structurally:

**Stage 1 (builder)** — `golang:1.26-alpine`:
- Copies `go.mod go.sum`, downloads modules (cache layer).
- Copies `pkg/dispatch/`, `internal/harness/`, `internal/subagent/`,
  `pkg/git/`, `cmd/codex-subagent/`.
- `CGO_ENABLED=0` static stripped binary → `/out/codex-subagent`.

**Stage 2 (runtime)** — `node:22-slim` (same base as the Claude image, pinned
digest):
- `apt-get install git` + `safe.directory '*'` (executor worktree git
  requirement — identical rationale to the Claude image).
- `npm install -g @openai/codex@<pinned>` — installs the Codex CLI;
  `--ephemeral --skip-git-repo-check --ignore-user-config` flags used at
  invocation (D6, carried verbatim from MILESTONE.md).
- `COPY --from=builder /out/codex-subagent /usr/local/bin/codex-subagent`.
- `USER 1000` — non-root, matches D-G3 subagent harness UID requirement.
- `ENTRYPOINT ["/usr/local/bin/codex-subagent"]`.
- Image name: `ghcr.io/jsquirrelz/tide-codex-subagent`.

The Codex CLI API-key auth path (`OPENAI_API_KEY` env) is the documented
container path (D6) — no OAuth, no headless-token problems. The key is
**never baked** into the image; it arrives via the Pod env at dispatch time.

---

## Verification Gates

1. `make test` exits 0 with no `OPENAI_API_KEY` set — all new tests use
   fake-exec and pass offline.
2. `make verify-import-firewall` and `make verify-dispatch-imports` are green:
   `cmd/codex-subagent` imports only `internal/subagent/codex`, `internal/harness`,
   `pkg/dispatch`, `pkg/git`, and stdlib — zero provider imports in
   `internal/controller/` or `cmd/manager/`.
3. `docker build -f internal/subagent/codex/Dockerfile .` completes clean from
   repo root (multi-stage, Go binary compiles successfully inside the builder).
4. Vendor-mismatch test: fixture `Provider.Vendor="anthropic"` → `ExitCode != 0`,
   non-empty `Reason` in `out.json`.
5. Happy-path test: fixture `Provider.Vendor="openai"` + fake-exec JSONL stream →
   `ExitCode=0`, `Usage.InputTokens > 0`, non-empty `Result` in `out.json`.

---

## Wave Plan (illustrative)

| Wave | Plan | Dependency | Rationale |
|------|------|-----------|-----------|
| 1 | `plan-01-codex-subagent-entrypoint` | *(none within phase)* | Pure Go cmd package — self-contained; imports the phase-01 codex package only |
| 2 | `plan-02-codex-subagent-dockerfile` | `plan-01-codex-subagent-entrypoint` | Multi-stage Dockerfile references `./cmd/codex-subagent`; authoring after plan-01 ensures the cmd path is present |

Authoritative wave assignment is derived by Kahn over the Task DAG after the
plan planner emits Task CRDs; this table is the planner's intent only.
