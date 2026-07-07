---
phase: 35-git-base-ref
plan: 02
subsystem: git-clone-job
tags: [go-git, ref-resolution, envelope, clone-job, base-ref]
status: complete
requires:
  - "35-01: GitConfig.BaseRef (spec) + GitStatus.BaseSHA (status) in both API versions"
provides:
  - "pkg/git.EnsureRunBranch(bareRepoPath, branch, baseRef string) (plumbing.Hash, error)"
  - "pkg/git.ErrBaseRefUnresolvable sentinel"
  - "pkg/git.resolveBaseRef chain (D-01/D-02/D-03/D-11)"
  - "cmd/tide-push --base-ref flag + pushConfig.BaseRef"
  - "CloneResult envelope (envelopeKindClone) with baseSHA/baseRef keys"
  - "clone-mode exit-2 reason baseref-unresolvable; success reason \"\" carrying baseSHA"
affects:
  - "35-03: controller baseSHA stamping + BaseRefUnresolvable classification consumes this envelope contract"
tech-stack:
  added: []
  patterns: ["go-git ref chain (no ResolveRevision)", "termination-log + PVC envelope transport"]
key-files:
  created: []
  modified:
    - pkg/git/branch.go
    - pkg/git/branch_test.go
    - pkg/git/integrate.go
    - pkg/git/integrate_test.go
    - cmd/tide-push/main.go
    - cmd/tide-push/main_test.go
decisions:
  - "baseref-unresolvable rides exit 2 (exitInvariant); controller classifies on envelope.reason, not exit code — exit 14 stays Phase 34's integration-incomplete"
  - "Clone envelope Kind = CloneResult at envelopes/clone/<uid>.json, distinct from envelopes/push/ so a boundary push never clobbers clone provenance"
  - "One pushResult struct serves both modes; baseSHA/baseRef are omitempty"
metrics:
  duration: "~7 min"
  completed: "2026-07-07"
  tasks: 2
  files_changed: 6
---

# Phase 35 Plan 02: Git Base Ref (Job-side resolution + clone envelope) Summary

Implemented the D-01/D-02/D-03 baseRef resolution contract as the single
resolution site in `pkg/git.EnsureRunBranch`, and adopted the push-mode
envelope contract for clone mode in `cmd/tide-push` so an unresolvable baseRef
becomes a classifiable exit-2 `baseref-unresolvable` envelope and every
successful clone reports the resolved base SHA.

## What shipped

**Task 1 — `pkg/git` resolution chain (commits `08febbe` RED, `182493c` GREEN)**
- `EnsureRunBranch` signature changed to `(bareRepoPath, branch, baseRef string) (plumbing.Hash, error)`. The idempotent existence early-return stays FIRST (Pitfall 6): a retry against an existing run branch returns its tip without re-resolving, so a now-unresolvable baseRef on retry still exits 0. Empty baseRef preserves HEAD behavior, now returning HEAD's hash.
- `resolveBaseRef(repo, ref)` implements the ordered chain: `refs/`-verbatim (with a `refs/heads/`→`refs/remotes/origin/` fallback), branch (local then `refs/remotes/origin/*` — the Pitfall 1 load-bearing arm), tag (annotated tags peeled to the commit via `TagObject`/`Commit`, D-11), full 40-hex SHA gated by `plumbing.IsHash` + `CommitObject`. `ResolveRevision` is deliberately never used (it accepts the D-01-rejected forms). A non-tag hit is confirmed to name a commit.
- `var ErrBaseRefUnresolvable` sentinel; every unresolvable outcome wraps it via `%w` with an `unable to resolve %q to a commit SHA: … reachable from a branch or tag` message.
- Callers updated mechanically: `integrate.go` precondition doc, `integrate_test.go`, `cmd/tide-push/main.go` (empty baseRef in Task 1, real wiring in Task 2).

**Task 2 — clone-mode envelope + `--base-ref` (commits `77eda1d` RED, `8a24596` GREEN)**
- `pushConfig.BaseRef` + `--base-ref` flag; `pushResult` gains `baseSHA`/`baseRef` (omitempty); `envelopeKindClone = "CloneResult"`.
- `writeCloneEnvelope(cfg, baseSHA, exit, reason)` writes to `/dev/termination-log` + `<workspace>/envelopes/clone/<uid>.json` (skipped when project-uid empty).
- `runClone` rewired: every exit path emits an envelope — initial-clone / share / worktree / group-share failures → `clone-failed` at their existing exit codes; `errors.Is(ErrBaseRefUnresolvable)` → exit 2 reason `baseref-unresolvable`; success (with or without a run branch) → resolved `baseSHA` (empty on the legacy no-run-branch path), reason `""`. New stderr paths pass through `redactPAT`.

## Contract handed to plan 35-03 (fixed here)

- **EnsureRunBranch signature:** `func EnsureRunBranch(bareRepoPath, branch, baseRef string) (plumbing.Hash, error)`; sentinel `pkggit.ErrBaseRefUnresolvable` (match with `errors.Is`).
- **Envelope Kind:** `CloneResult` (`envelopeKindClone`). **PVC path:** `<workspace>/envelopes/clone/<projectUID>.json` (distinct from `envelopes/push/`). Also on `/dev/termination-log`.
- **JSON keys:** `kind`, `exitCode`, `reason`, `baseSHA` (omitempty; resolved 40-hex on success), `baseRef` (omitempty; the ref as given). Shares the `pushResult` struct so `apiVersion`/`projectUID` also serialize; `branch`/`headSHA` serialize empty in clone mode.
- **Exit / reason taxonomy:** unresolvable → **exit 2** + `reason: "baseref-unresolvable"`; other clone failure → existing code + `reason: "clone-failed"`; success → exit 0 + `reason: ""`. Controller MUST classify on `reason`, not exit code (exit 14 is Phase 34's `integration-incomplete`).

Example failure envelope:
```json
{"apiVersion":"tideproject.k8s/v1alpha1","kind":"CloneResult","projectUID":"c-bad","branch":"","headSHA":"","exitCode":2,"reason":"baseref-unresolvable","baseRef":"no-such-ref"}
```
Example success (feature branch):
```json
{"apiVersion":"tideproject.k8s/v1alpha1","kind":"CloneResult","projectUID":"c-feature","branch":"","headSHA":"","exitCode":0,"reason":"","baseSHA":"<40-hex feature tip>","baseRef":"feature/hotfix"}
```

## Verification (observed)

- `go test ./pkg/git/ -run TestEnsureRunBranch -count=1 -v` — all cases PASS, including the Pitfall 1 non-default-branch resolution against a production-shaped `pkggit.Clone` (feature tip lives only under `refs/remotes/origin/*`), annotated-tag peel, lightweight tag, full SHA, `refs/`-qualified verbatim + remote fallback, and the D-01 rejections (HEAD / 7-char SHA / `~1` / `^`) each `errors.Is(ErrBaseRefUnresolvable)`.
- `go test ./cmd/tide-push/ -run TestRunClone -count=1 -v` — all 8 clone tests PASS (default-HEAD success baseSHA, feature-branch success baseSHA+baseRef, unresolvable exit-2 envelope with no run branch created, no-run-branch empty-baseSHA success, project-uid-unset skips the PVC file). The `/dev/termination-log: operation not permitted` lines are the expected off-cluster best-effort behavior.
- `go test ./pkg/git/... ./cmd/tide-push/... -count=1` — both packages **ok**.
- `go build ./cmd/tide-push/... ./pkg/git/... ./internal/...` — **OK**; `go vet` on the two touched packages — **OK**; `gofmt -l` on the six files — clean.
- Build scoped to touched packages per the plan (the pre-existing `cmd/tide-demo-init` `//go:embed` failure on base `main` is unrelated and untouched).

## Deviations from Plan

None affecting behavior. Minor within-latitude choices:
- Resolution tests use a dedicated `seedResolvableClone` sibling helper (the plan permitted "extend seedBareRepo **or** add a sibling helper") to avoid disturbing the shared `seedBareRepo` used across `clone_test.go`/`integrate_test.go`. `cmd/tide-push` uses a parallel `seedSourceWithFeature` helper for the feature-branch case.
- Non-clone failure paths (share/worktree/group-share) were given `reason: "clone-failed"` envelopes at their existing exit codes, per the plan action; these were previously envelope-less bare exits.

## Threat surface

No new surface beyond the plan's `<threat_model>`. T-35-02 mitigated: all new clone-path stderr writes pass through `redactPAT`; `resolveBaseRef` errors contain only the ref + hash; the envelope carries no credential fields. baseRef reaches only exec-array Job args + pure go-git lookups (no git CLI receives it on the resolution path).

## Self-Check: PASSED
