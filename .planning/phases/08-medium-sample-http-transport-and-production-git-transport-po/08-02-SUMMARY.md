---
phase: 08-medium-sample-http-transport-and-production-git-transport-po
plan: "02"
subsystem: images
tags: [revert, dockerfile, distroless, security, git-transport]
dependency_graph:
  requires: []
  provides: [git-less-tide-push-image, git-less-claude-subagent-image, transport-policy-doc]
  affects: [images/tide-push, images/claude-subagent, pkg/git]
tech_stack:
  added: []
  patterns: [distroless/static:nonroot runtime, pure-Go HTTP transport]
key_files:
  created: []
  modified:
    - images/tide-push/Dockerfile
    - images/claude-subagent/Dockerfile
    - pkg/git/doc.go
decisions:
  - "tide-push reverted to distroless/static:nonroot — no apk, no git binary, no shell"
  - "claude-subagent apt-get git layer removed — HTTP transport (pure-Go) needs no system git"
  - "pkg/git/doc.go transport paragraph replaced: HTTP(S)/SSH are pure-Go; file:// NOT a production transport"
  - "tide-demo-init remains unchanged (alpine:3.21 + apk add git — still uses file:// internally)"
metrics:
  duration: "~5 minutes"
  completed: "2026-06-03"
  tasks: 3
  files_modified: 3
---

# Phase 8 Plan 02: Revert Core-Image Git Additions Summary

Reverted 93595b9's core-image git additions for `tide-push` and `claude-subagent`, restoring distroless/slim git-less bases. Partially reframed `pkg/git/doc.go` to state that HTTP(S)/SSH are pure-Go (no git binary needed) and that `file://` is NOT a supported production transport.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Revert images/tide-push/Dockerfile to distroless/static:nonroot | ed4fd1d | images/tide-push/Dockerfile |
| 2 | Remove git layer from images/claude-subagent/Dockerfile | c6770ac | images/claude-subagent/Dockerfile |
| 3 | Reframe pkg/git/doc.go for production-only transport | 1f26822 | pkg/git/doc.go |

## What Changed

**Task 1 — images/tide-push/Dockerfile:**
- Stage 2 `FROM` reverted from `alpine:3.21` to `gcr.io/distroless/static:nonroot`
- Removed `RUN apk add --no-cache git && adduser -D -u 1000 nonroot`
- Removed `USER 1000` (distroless/static:nonroot is already nonroot by default)
- Stage 1 comment updated: references HTTP(S)/SSH as pure-Go; file:// transport NOT used by tide-push in Phase 8+
- Stage 2 comment updated: references `images/tide-git-http-server` as demo-only transport bridge

**Task 2 — images/claude-subagent/Dockerfile:**
- Removed `RUN --mount=type=cache,target=/var/cache/apt apt-get update && apt-get install -y --no-install-recommends git && rm -rf /var/lib/apt/lists/*` (4 lines)
- Removed the file:// rationale comment block that justified the git layer
- Stage 2 comment updated: subagent uses go-git HTTP transport (pure-Go); in-cluster git-http-server is the demo remote
- `node:22-slim` base, `npm install @anthropic-ai/claude-code`, `USER 1000`, `ENTRYPOINT` all unchanged

**Task 3 — pkg/git/doc.go:**
- Replaced "Transport dependency on a system git binary" paragraph with "Transport support"
- New paragraph states: HTTPS/SSH are pure-Go (no git binary needed in production images); file:// is NOT a supported production transport; `images/tide-demo-init` is the documented exception (carries git for internal file:// push to the demo PVC); medium sample uses in-cluster git-http-server over http:// (same pure-Go transport as production HTTPS)
- Import firewall note and all other paragraphs preserved unchanged

## Verification Results

All 6 plan verification steps passed:

1. `grep -n 'distroless/static:nonroot' images/tide-push/Dockerfile` → 3 results (Stage 2 comment + FROM line)
2. `grep -c 'apk add' images/tide-push/Dockerfile` → 0
3. `grep -c 'apt-get install.*git' images/claude-subagent/Dockerfile` → 0
4. `grep -n 'file:// is NOT' pkg/git/doc.go` → 1 result (line 42)
5. `go build ./pkg/git/...` → exit 0
6. `cat images/tide-demo-init/Dockerfile | grep -c 'apk add.*git'` → 1 (demo-init unchanged)

## Deviations from Plan

None — plan executed exactly as written.

## Known Stubs

None. All changes are doc/Dockerfile edits with no stub patterns.

## Threat Flags

None. This plan reduces attack surface (removes git binary from two production images). No new network endpoints, auth paths, or schema changes introduced.

## Self-Check: PASSED

- [x] images/tide-push/Dockerfile exists with `distroless/static:nonroot` FROM line
- [x] images/claude-subagent/Dockerfile exists with no `apt-get install git`
- [x] pkg/git/doc.go exists with `file:// is NOT` in transport paragraph
- [x] Commit ed4fd1d exists (Task 1)
- [x] Commit c6770ac exists (Task 2)
- [x] Commit 1f26822 exists (Task 3)
- [x] tide-demo-init/Dockerfile unchanged (alpine:3.21 + apk add git)
