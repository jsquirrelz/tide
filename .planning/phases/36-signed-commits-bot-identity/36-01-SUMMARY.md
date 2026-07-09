---
phase: 36-signed-commits-bot-identity
plan: 01
subsystem: git-identity
tags: [git, identity, commit, agent, SIGN-01]
requires: []
provides:
  - "pkg/git.AgentIdentity() — single env-sourced agent-identity helper"
  - "pkg/git constants DefaultAgentName/DefaultAgentEmail/EnvAgentName/EnvAgentEmail"
  - "harness, integrate, and tide-push commit sites all sourcing identity from AgentIdentity()"
affects:
  - internal/harness
  - pkg/git
  - cmd/tide-push
tech-stack:
  added: []
  patterns:
    - "env-with-compiled-fallback consolidated into one leaf-package helper (mirrors envOrDefault empty-is-unset convention)"
key-files:
  created:
    - pkg/git/identity.go
    - pkg/git/identity_test.go
  modified:
    - internal/harness/commit.go
    - internal/harness/commit_test.go
    - pkg/git/integrate.go
    - pkg/git/integrate_test.go
    - cmd/tide-push/main.go
    - cmd/tide-push/main_test.go
decisions:
  - "D-04 bot→agent rename applied at all three commit sites; compiled default is TIDE Agent <tide-agent@tideproject.k8s>"
  - "D-05 tide-push hardcoded identity removed; W11 stability contract preserved (author==committer, only timestamp varies)"
  - "Empty-string env treated as unset so a zero-value Helm value falls through to the compiled default"
metrics:
  duration: ~5m
  completed: 2026-07-08
status: complete
---

# Phase 36 Plan 01: Shared Agent Identity Summary

One env-sourced agent-identity helper (`pkg/git.AgentIdentity()`) now backs all three TIDE commit sites — harness task commits, integrate merge commits, and tide-push boundary commits — completing the D-04 bot→agent rename and removing tide-push's hardcoded identity (D-05), with the compiled default `TIDE Agent <tide-agent@tideproject.k8s>` living in exactly one place.

## What Was Built

- **`pkg/git/identity.go` (NEW):** four exported constants (`DefaultAgentName`, `DefaultAgentEmail`, `EnvAgentName="TIDE_AGENT_NAME"`, `EnvAgentEmail="TIDE_AGENT_EMAIL"`) plus `AgentIdentity() (name, email string)`. Each field reads its env var and falls back to the compiled default independently; empty-string is treated as unset.
- **`internal/harness/commit.go`:** `CommitWorktree` replaced its inline 8-line `TIDE_BOT_*` env-fallback block with `pkggit.AgentIdentity()`; the git CLI `-c user.name/-c user.email` invocation shape is unchanged. Dropped the now-unused `os` import.
- **`pkg/git/integrate.go`:** `IntegrateTaskBranches` merge identity now sourced from `AgentIdentity()` (same package, direct call). Doc comments rewritten to the new chain.
- **`cmd/tide-push/main.go`:** deleted `tideBotSignature()`; added `agentSignature()` built from `pkggit.AgentIdentity()` + `time.Now()`. Only the `Author` signature is set — go-git copies Author to Committer (Pitfall 8), keeping author == committer. Package doc updated to describe the env-sourced identity; W11 stability contract preserved in the helper's doc comment.

## Verification

- `go test ./pkg/git/ ./internal/harness/ ./cmd/tide-push/` — all green.
- `go test ./pkg/git/ -run TestAgentIdentity -v` — default, override, and per-var-independence cases pass.
- Default identity now pinned at all three commit shapes: `identity_test` (helper), `commit_test` (task commit author), `integrate_test` (first-ever `--merges` merge-commit identity assertion), `main_test` (boundary commit author + committer==author).
- Repo-wide legacy-name gate: `grep -rn 'TIDE_BOT|tide-bot|TIDE Bot|tideBotSignature' --include='*.go' --exclude-dir=.claude . | grep -v 'cmd/tide-demo-init' | wc -l` → `0`. Prints `GATE-CLEAN`.
- `git diff --stat cmd/tide-demo-init/` — no changes (its deliberately-distinct seeding identity untouched).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Removed unused `os` import in commit.go**
- **Found during:** Task 2
- **Issue:** After replacing the `os.Getenv` env-fallback block with `AgentIdentity()`, the `os` import became unused, which fails `go build`.
- **Fix:** Removed `"os"` from the import block (`os/exec` retained).
- **Files modified:** internal/harness/commit.go
- **Commit:** 6e65848

### Criterion-alignment note

- Task 2 acceptance expected `grep -c 'AgentIdentity()' internal/harness/commit.go == 1`. The doc comment initially also referenced `pkggit.AgentIdentity()`, yielding 2. Reworded the doc-comment mention to `pkggit.AgentIdentity` (no parens) so the proxy check reflects the single actual call while keeping the doc reference.

### Test hardening beyond the letter of the plan

- Added explicit `t.Setenv("TIDE_AGENT_*", "")` guards before the default-identity assertions in `TestCommitWorktree` and `TestRunPushModeWritesExactBoundaryCommitMessage` so an ambient env value cannot flip the compiled-default assertion.
- Added a committer==author assertion to the tide-push test (Pitfall 8 — go-git Author→Committer copy).

## Out-of-Scope Observation (not fixed)

- `go build ./...` fails on `cmd/tide-demo-init/main.go:112: pattern all:fixture: no matching files found` — a pre-existing `//go:embed` state in a file this plan never touched. Out of scope per the scope boundary; the three in-scope packages build and test clean.

## Known Stubs

None.

## Follow-ups (sibling plans)

The env vars this plan reads (`TIDE_AGENT_NAME`/`TIDE_AGENT_EMAIL`) are set by nothing yet — 36-02 adds the CRD fields + resolver, 36-03 injects Job env at both builders, 36-04 wires chart values. SIGN-01's full precedence chain (Project spec → chart → compiled default) is not exercised end-to-end until those land; this plan delivers the compiled-default + env-read layer and the uniform call site.

## Self-Check: PASSED

- Created files present: pkg/git/identity.go, pkg/git/identity_test.go.
- Commits present: 2ec36ab (Task 1), 6e65848 (Task 2), e47e31a (Task 3).
