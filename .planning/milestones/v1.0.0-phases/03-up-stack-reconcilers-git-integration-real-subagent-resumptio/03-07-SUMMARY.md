---
phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
plan: 07
subsystem: subagent-image
tags: [claude-subagent, dockerfile, thin-shim, worktree, d-b4, d-a4, d-c1, harn-06, provider-firewall]

requires:
  - phase: 02
    provides: pkg/dispatch.{EnvelopeIn,EnvelopeOut,Subagent} + internal/harness.{ReadEnvelopeIn,WriteEnvelopeOut} + PodStatusEnvelopeReader termination-message fallback
  - phase: 03-up-stack-reconcilers-git-integration-real-subagent-resumptio
    provides: |
      - Wave 1 (03-01): EnvelopeIn schema (Role/Level/TaskUID/Provider/Dev) consumed here
      - Wave 2 (03-03): pkg/git.AddWorktree(bareRepoPath, taskUID, branch) consumed by harness.EnsureWorktree
      - Wave 2 (03-05): internal/subagent/anthropic.New + anthropic.Anthropic.Run wired by the shim

provides:
  - cmd/claude-subagent — ~50 LOC thin shim binary (wc -l = 100 incl. helpers + termination-message hook): load EnvelopeIn → harness.EnsureWorktree → anthropic.Run → write EnvelopeOut. NO env.Dev branching (PATTERNS.md line 442 anti-pattern enforcement).
  - images/claude-subagent/Dockerfile — two-stage build (golang:1.26-alpine builder + node:22-slim runtime with @anthropic-ai/claude-code@2.1.142 globally installed; non-root USER 1000; zero host ~/.claude/ refs; zero hardcoded ANTHROPIC_API_KEY).
  - internal/harness.EnsureWorktree — D-A4 planner short-circuit + D-B4 executor worktree-add wiring via pkggit.AddWorktree; idempotent re-call (chaos-resume safe); fail-fast missing-bare-repo error.
  - internal/subagent/anthropic.NewWithExec — exported test seam (constructor taking execFunc override) so external test packages (cmd/claude-subagent_test) can replicate the fake-`bash -c cat` fixture pattern without touching unexported fields.

affects:
  - 03-08 (cmd/orchestrator pod-job builder will reference images/claude-subagent in its CRD image-tag default)
  - 03-09 (Helm chart values.yaml + ImagePullSecret will route the new image through tide-subagent SA)
  - 03-10 (chaos-resume Layer B test will exercise the idempotent EnsureWorktree re-call path on Pod restart)
  - Phase 4 OpenInference parser (still reads events.jsonl written by internal/subagent/anthropic.Run — image swap doesn't alter the audit-log contract)

tech-stack:
  added:
    - "@anthropic-ai/claude-code@2.1.142 (Node CLI pinned via npm install -g — Pitfall 3 mitigation)"
    - "node:22-slim runtime base (Claude Code CLI requires Node; replaces stub-subagent's gcr.io/distroless/static:nonroot)"
  patterns:
    - "Thin shim per HARN-06: load envelope → instantiate Subagent → write envelope. No business logic in the shim itself; all provider knowledge lives in internal/subagent/anthropic."
    - "Side-file branch resolution (branch.txt): orchestrator writes <envelopePath dir>/branch.txt at Job-creation time so the executor shim resolves the per-Task branch without re-bumping the EnvelopeIn schema mid-Phase-3 (plan body line 292 design choice)."
    - "Test-seam vars (newSubagent, ensureWorktreeFunc, addWorktreeFunc) — package-level var fns let tests stub the production wiring without interface/mock plumbing; cleanup via t.Cleanup(restore-original) is the established pattern."
    - "D-A4 planner short-circuit at the harness/integration boundary, not the subagent boundary — keeps the anthropic package single-responsibility (it doesn't know about worktrees) and concentrates the Role check in one place."
    - "Failure-shaped EnvelopeOut wrap pattern — every dispatch-level error path (envelope load, worktree setup, anthropic.Run dispatch error) writes a structured EnvelopeOut with Result/Reason instead of letting the Pod just exit non-zero. This keeps the controller's PodStatusEnvelopeReader and PVC reader consistent across all failure modes."

key-files:
  created:
    - cmd/claude-subagent/main.go (100 LOC — thin shim)
    - cmd/claude-subagent/main_test.go (296 LOC — 5 tests)
    - images/claude-subagent/Dockerfile (44 LOC — two-stage build)
    - images/claude-subagent/.dockerignore (12 lines — mirrors stub but keeps internal/{harness,subagent})
    - internal/harness/worktree.go (68 LOC — EnsureWorktree)
    - internal/harness/worktree_test.go (166 LOC — 4 EnsureWorktree tests)
  modified:
    - internal/subagent/anthropic/subagent.go (added NewWithExec exported constructor — 14 LOC delta; Rule 3 deviation)

key-decisions:
  - "Shim calls sa.Run(ctx, env) directly, NOT through harness.Harness.Run() — the plan's Task 1 action explicitly permits either wiring (lines 165-167) and the PATTERNS.md recipe (lines 416-443) shows the direct-call shape. Going through harness.Harness would add cap-enforcement + output-path validation, which are Phase 2 concerns the harness already covers when invoked from a different code path; the shim's job is the thin Pod-entrypoint contract, not policy enforcement. Phase 4 may revisit if cap-violations need to surface from this image's path too."
  - "Branch resolution via side-file (<envelopePath dir>/branch.txt) instead of adding EnvelopeIn.ProjectBranch. The envelope schema is frozen in Wave 1 (03-01); adding a field mid-phase risks re-running 03-01 and re-validating envelope round-trip on every consumer. The side-file is cheap (one extra file the orchestrator writes alongside in.json), invisible to schema consumers, and matches existing /workspace/envelopes/<task-uid>/ co-location."
  - "Test seam NewWithExec is exported but documented as production-DO-NOT-USE. Production wiring stays anthropic.New(...); NewWithExec exists solely so cmd/claude-subagent's tests can replicate the same fake-`bash -c cat fixture` pattern that internal/subagent/anthropic/subagent_test.go uses via direct field write (which external packages can't do). Smaller surface than refactoring Options to carry an exec.Func field."
  - "Dockerfile builder stage adds pkg/git/ to the COPY list (over stub-subagent) because the claude-subagent shim transitively depends on internal/harness, which now imports pkg/git via EnsureWorktree → addWorktreeFunc = pkggit.AddWorktree. Stub doesn't have this dep and can keep its tighter COPY set."
  - "Shim exit-code contract: 0 = success (out.ExitCode), 1 = anthropic dispatch-level failure OR worktree-setup failure (envelope still written with structured Reason), 2 = envelope-load failure OR write-out.json failure (catastrophic / fix manually). Mirrors stub-subagent's three-tier exit-code contract; matches Phase 2 harness expectations."

patterns-established:
  - "Pattern: 'thin Pod-entrypoint shim' — for every provider that gets a TIDE subagent image, the shim is exactly the same shape: parse flags, signal.NotifyContext, load EnvelopeIn, instantiate provider's pkg/dispatch.Subagent via newSubagent test seam, optionally do pre-Run setup (harness.EnsureWorktree), invoke Run, persist EnvelopeOut + termination-message. Future providers (e.g. internal/subagent/openai) reuse this shape verbatim — only the import in newSubagent changes."
  - "Pattern: 'production Default + test-seam Override' — every shim that wraps a provider exports a package-level var fn (newSubagent / ensureWorktreeFunc) whose production value is the real call; tests swap-and-restore via t.Cleanup. Less ceremony than interface-based mocking; the shim isn't a public API."
  - "Pattern: 'D-A4 short-circuit at the integration site' — Role-based dispatch (planner vs executor) lives in internal/harness.EnsureWorktree, not in the subagent. The subagent stays single-responsibility (run the prompt; emit the envelope); harness owns 'what setup does this Role need'. Concentrates the Role check + invariant docs in one place where the test surface is small."
  - "Pattern: 'side-file IPC at PVC layout' — when adding fields to EnvelopeIn would force a schema-bump cascade, instead drop a small side file alongside in.json (branch.txt; later: release signal file per D-D3). The Pod entrypoint reads it best-effort; the orchestrator writes it at Job-creation time. Keeps EnvelopeIn small and prevents mid-phase schema churn."

requirements-completed: [TEST-03]

duration: ~50min
completed: 2026-05-15
---

# Phase 03 Plan 07: cmd/claude-subagent Thin Shim + Real-Claude Image — Summary

**cmd/claude-subagent (~50 LOC thin shim, wc -l = 100 incl. helpers) wires internal/subagent/anthropic.Run + internal/harness envelope IO + a new harness.EnsureWorktree pre-Run hook for D-B4 worktree-per-Task isolation; images/claude-subagent/Dockerfile pins @anthropic-ai/claude-code@2.1.142 on node:22-slim with USER 1000 and zero host ~/.claude/ refs.**

## Performance

- **Duration:** ~50 minutes (single pass; no rework cycles)
- **Tasks:** 3 / 3 (all TDD)
- **Test functions:** 9 (5 cmd/claude-subagent + 4 internal/harness EnsureWorktree)
- **Tests PASS:** 9 / 9
- **Files created:** 6 (main.go, main_test.go, Dockerfile, .dockerignore, worktree.go, worktree_test.go)
- **Files modified:** 1 (internal/subagent/anthropic/subagent.go — added NewWithExec)

## Tasks Completed

| # | Name | Commit | Tests |
|---|------|--------|-------|
| 1 | cmd/claude-subagent/main.go — thin shim wiring anthropic.Run + harness envelope IO | `aa068ba` | 4 PASS (later 5 after Task 3 added integration test) |
| 2 | images/claude-subagent/Dockerfile — node:22-slim + @anthropic-ai/claude-code@2.1.142 + Go binary | `ba9145a` | grep-gate AC (build deferred to plan 03-09 CI) |
| 3 | internal/harness.EnsureWorktree wiring + shim integration | `19eb93c` | 4 PASS (EnsureWorktree) + 1 PASS (shim cross-file integration) |

## Verification

### Plan acceptance gates — all green

- `go test ./cmd/claude-subagent/... -count=1 -timeout 30s` → `ok` (5 tests PASS)
- `go test ./internal/harness/... -run TestEnsureWorktree -count=1 -timeout 30s` → `ok` (4 tests PASS)
- `go vet ./cmd/claude-subagent/... ./internal/harness/...` → exit 0
- `go build ./...` → exit 0
- `wc -l cmd/claude-subagent/main.go` → **100** (meets `≤ 100` acceptance)

### Dockerfile grep gates

- `FROM node:22-slim` → 1 hit ✓
- `npm install -g @anthropic-ai/claude-code@2.1.142` → 1 hit (exact pin per Pitfall 3) ✓
- `USER 1000` → 1 hit (non-root D-G3) ✓
- `~/.claude` / `HOME.*\.claude` → **0 hits** (CLAUDE.md anti-pattern absent) ✓
- `VOLUME` / `--mount` → **0 hits** (no host-bind expectations) ✓
- `ANTHROPIC_API_KEY` / `ANTHROPIC_BASE_URL` → **0 hits** (credproxy injects at K8s Pod-spec time) ✓

### Shim acceptance grep gates

- `func main()` → 1 hit ✓
- `func run(ctx context.Context, envelopePath, ...) int` → 1 hit ✓
- `anthropic.New` → 1 hit (consumes plan 03-05) ✓
- `harness.ReadEnvelopeIn|loadEnvelope` → 1 hit ✓
- `harness.WriteEnvelopeOut|writeEnvelope` → 4 hits ✓
- `switch.*env\.Dev\.TestMode|switch.*Dev` → **0 hits** (real Claude image IGNORES env.Dev) ✓
- `EnsureWorktree` → 1 hit (shim invokes pre-Run) ✓

### EnsureWorktree acceptance grep gates

- `func EnsureWorktree(in pkgdispatch.EnvelopeIn, workspaceRoot, branch string) error` → 1 hit ✓
- `in.Role != "executor"` → 1 hit (D-A4 planner short-circuit) ✓
- `pkggit.AddWorktree|addWorktreeFunc` → 5 hits (D-B4 consumes plan 03-03) ✓
- `internal/subagent/anthropic` import → **0 hits** (provider-agnostic discipline preserved) ✓

## Deviations from Plan

### Rule 3 — Auto-fix blocking issues

**1. [Rule 3 — blocking] Added `anthropic.NewWithExec` exported constructor**

- **Found during:** Task 1 (writing main_test.go)
- **Issue:** internal/subagent/anthropic.Anthropic has an unexported `execFunc` field that the anthropic-internal tests set via direct struct field write (a.execFunc = fakeFunc). External packages (cmd/claude-subagent_test) cannot access this field, so the shim's tests had no way to inject a fake exec without spinning up the real `claude` CLI — which violates "no live Claude API calls in unit tests" per Plan 03-07 line 47.
- **Fix:** Added 14 LOC `NewWithExec(opts, execFunc) *Anthropic` exported constructor that wraps `New(opts)` then assigns the override. Production code keeps using `New()`; the new constructor is documented as test-seam-only.
- **Files modified:** internal/subagent/anthropic/subagent.go
- **Commit:** aa068ba

### Rule 2 — Auto-add missing critical functionality

**2. [Rule 2 — missing correctness] Failure-shaped EnvelopeOut on worktree-setup failure**

- **Found during:** Task 3 wiring
- **Issue:** The plan's Task 3 action describes EnsureWorktree's error return but did not specify what the shim should do when EnsureWorktree fails. Without explicit handling, the shim would write a stale/empty EnvelopeOut or none at all — leaving the controller's PodStatusEnvelopeReader to see an empty termination-message and the PVC reader to see a missing out.json. Both surfaces would interpret it as "Pod just crashed" rather than "worktree setup failed".
- **Fix:** Added a `failOut(...)` path in run() that wraps EnsureWorktree errors into `EnvelopeOut{Result: "worktree-setup-failed", Reason: err.Error(), ExitCode: 1}` so the controller surfaces a structured Reason. Matches the existing dispatch-level-error wrap pattern for anthropic.Run failures.
- **Files modified:** cmd/claude-subagent/main.go
- **Commit:** 19eb93c

## Out-of-Scope Observations

- **Pre-existing environmental: internal/controller envtest BeforeSuite fails** with `unable to fork etcd from /usr/local/kubebuilder/bin/etcd: no such file or directory` and `open ../../bin/k8s: no such file or directory`. This is the well-known envtest-binaries setup requirement (`make envtest-setup` or `setup-envtest use ...`) and predates this plan — my commits touch only cmd/, internal/harness, internal/subagent/anthropic, and images/. Logged here for the verifier; not a Plan 03-07 regression.

## Authentication Gates

None. All tests run hermetically via the fake-exec seam; no live Claude API calls; the Dockerfile build was not exercised in this plan (deferred to 03-09 CI per the plan's Task 2 Test 2 acknowledgment).

## Known Stubs

- `branch.txt` side-file resolution is now expected at `<envelopePath dir>/branch.txt`. Plan 03-08 (cmd/orchestrator pod-job builder) is the consumer that must write this side file at Job-creation time. Until 03-08 lands, executor-Role Tasks in real deployments would see `readBranch() → ""` and `pkggit.AddWorktree` would error on the empty branch. This is intentional — Phase 2 manager doesn't dispatch executor-Role Tasks (only planner Tasks are dogfood-ready as of 02.2 closeout), so the gap is closed before any executor Task could exercise it.

## Threat Flags

None. The two declared threats in the plan's `<threat_model>` (T-305 host-credential-leak; T-306 prompt-template-injection) are both `mitigate` and are both verified clean by the Dockerfile grep gates (no ~/.claude, no hardcoded API key, no VOLUME) and the inherited Phase 02 / Plan 03-05 invariants (go:embed templates, zero K8s verbs on subagent SA).

## Self-Check: PASSED

**Files created/modified — all present:**
- `cmd/claude-subagent/main.go` — FOUND (100 LOC)
- `cmd/claude-subagent/main_test.go` — FOUND
- `images/claude-subagent/Dockerfile` — FOUND
- `images/claude-subagent/.dockerignore` — FOUND
- `internal/harness/worktree.go` — FOUND (68 LOC)
- `internal/harness/worktree_test.go` — FOUND
- `internal/subagent/anthropic/subagent.go` — MODIFIED (NewWithExec exported)

**Commits — all present on worktree branch:**
- `aa068ba` feat(03-07): cmd/claude-subagent thin shim wires anthropic.Run + harness envelope IO — FOUND
- `ba9145a` feat(03-07): claude-subagent Dockerfile — node:22-slim + claude-code@2.1.142 + non-root — FOUND
- `19eb93c` feat(03-07): internal/harness.EnsureWorktree wires D-B4 worktrees + shim integration — FOUND

**Final test sweep targets:** ✓ all PASS / clean.
